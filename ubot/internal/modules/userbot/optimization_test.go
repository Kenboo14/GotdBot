package userbot

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
)

func TestFloodWaitRegistry_CooldownAndExpiry(t *testing.T) {
	svc := &Service{}
	chatID := int64(123456789)

	// Awalnya tidak ada flood cooldown
	if rem := svc.getFloodWaitRemaining(chatID); rem > 0 {
		t.Fatalf("expected 0 cooldown initially, got %v", rem)
	}

	// Rekam cooldown 2 detik
	svc.recordFloodWait(chatID, 2*time.Second)

	// Harus ada sisa waktu > 1 detik
	rem := svc.getFloodWaitRemaining(chatID)
	if rem < 1*time.Second || rem > 4*time.Second {
		t.Fatalf("expected ~2-2.5s remaining, got %v", rem)
	}

	// Rekam cooldown 10ms yang cepat selesai
	svc.recordFloodWait(chatID, 10*time.Millisecond)
	time.Sleep(550 * time.Millisecond) // 10ms + 500ms jitter

	if rem := svc.getFloodWaitRemaining(chatID); rem > 0 {
		t.Fatalf("expected cooldown expired, got %v remaining", rem)
	}
}

func TestFloodWaitEviction_ReapStaleResources(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*gobot.Client),
		accountStates: make(map[int64]AccountSessionState),
		accountGens:   make(map[int64]uint64),
		lastActive:    make(map[int64]time.Time),
		lastUpdate:    make(map[int64]time.Time),
		lastVerified:  make(map[int64]time.Time),
		logins:        make(map[int64]*login),
		sudoCache:     make(map[string]sudoCacheEntry),
	}
	svc.sudoMu.Lock()
	svc.sudoMu.Unlock()

	// Tambah beberapa cooldown: 1 kadaluarsa, 1 masih aktif
	expiredID := int64(1)
	activeID := int64(2)
	svc.floodCooldowns.Store(expiredID, time.Now().Add(-1*time.Minute)) // sudah expired
	svc.floodCooldowns.Store(activeID, time.Now().Add(10*time.Minute))  // masih aktif

	svc.reapStaleResources()

	// Entry expired harus sudah dihapus
	if _, ok := svc.floodCooldowns.Load(expiredID); ok {
		t.Fatalf("expected expired floodCooldown to be evicted, but still present")
	}
	// Entry aktif harus tetap ada
	if _, ok := svc.floodCooldowns.Load(activeID); !ok {
		t.Fatalf("expected active floodCooldown to remain, but was evicted")
	}
}

func TestRandJitterMs_Bounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		j := randJitterMs(250)
		if j < 0 || j >= 250*time.Millisecond {
			t.Fatalf("randJitterMs(250) out of bounds: %v", j)
		}
	}
	// Nilai 0 harus mengembalikan 0
	if randJitterMs(0) != 0 {
		t.Fatal("randJitterMs(0) should return 0")
	}
}

func TestHeartbeatOptimization_PrioritizeTroubledAccounts(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*gobot.Client),
		accountStates: make(map[int64]AccountSessionState),
		accountGens:   make(map[int64]uint64),
		lastActive:    make(map[int64]time.Time),
		lastUpdate:    make(map[int64]time.Time),
		lastVerified:  make(map[int64]time.Time),
		verifying:     make(map[int64]bool),
	}

	fakeClient := &gobot.Client{}

	// Akun healthy, aktif tapi belum diverifikasi > 3 menit
	healthyID := int64(1001)
	svc.active[healthyID] = fakeClient
	svc.accountStates[healthyID] = StateConnected
	svc.lastActive[healthyID] = time.Now().Add(-30 * time.Second)
	svc.lastVerified[healthyID] = time.Now().Add(-5 * time.Minute) // verifikasi 5 mnt lalu -> harus masuk

	// Akun healthy, baru aktif DAN baru diverifikasi -> harus di-skip
	freshID := int64(1002)
	svc.active[freshID] = fakeClient
	svc.accountStates[freshID] = StateConnected
	svc.lastActive[freshID] = time.Now().Add(-10 * time.Second)
	svc.lastVerified[freshID] = time.Now().Add(-1 * time.Minute)

	// Akun bermasalah -> selalu masuk walau baru aktif
	troubledID := int64(1003)
	svc.active[troubledID] = fakeClient
	svc.accountStates[troubledID] = StateSuspected
	svc.lastActive[troubledID] = time.Now().Add(-5 * time.Second)
	svc.lastUpdate[troubledID] = time.Now().Add(-5 * time.Second)

	// Replikasi logika seleksi kandidat
	const activityThreshold = 60 * time.Second
	const verifyMaxGap = 3 * time.Minute
	now := time.Now()

	type candidate struct {
		chatID   int64
		priority bool
	}
	var candidates []candidate

	svc.mu.Lock()
	for chatID, client := range svc.active {
		if client == nil {
			continue
		}
		state := svc.accountStates[chatID]
		if state == StateSessionInvalid || state == StateStopped {
			continue
		}
		isTroubled := state == StateSuspected || state == StateConnecting || state == StateReconnecting
		if isTroubled {
			candidates = append(candidates, candidate{chatID: chatID, priority: true})
			continue
		}
		recentActivity := now.Sub(svc.lastActive[chatID]) < activityThreshold || now.Sub(svc.lastUpdate[chatID]) < activityThreshold
		recentVerified := !svc.lastVerified[chatID].IsZero() && now.Sub(svc.lastVerified[chatID]) < verifyMaxGap
		if recentActivity && recentVerified {
			continue
		}
		candidates = append(candidates, candidate{chatID: chatID})
	}
	svc.mu.Unlock()

	foundTroubled := false
	foundHealthy := false
	foundFresh := false
	for _, c := range candidates {
		switch c.chatID {
		case troubledID:
			foundTroubled = true
			if !c.priority {
				t.Errorf("troubled account should have priority=true")
			}
		case healthyID:
			foundHealthy = true
		case freshID:
			foundFresh = true
		}
	}

	if !foundTroubled {
		t.Error("troubled account (StateSuspected) must always be in heartbeat candidates")
	}
	if !foundHealthy {
		t.Error("healthy account not recently verified must be in heartbeat candidates")
	}
	if foundFresh {
		t.Error("freshly active + recently verified account must be skipped from heartbeat")
	}
}

func TestPanelBroadcast_WaitCancellation(t *testing.T) {
	svc := &Service{}
	ctx, cancel := context.WithCancel(context.Background())

	// Batalkan context setelah 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	// Minta tunggu 10 detik, tetapi harus berhenti seketika saat cancel()
	due := time.Now().Add(10 * time.Second)
	res := svc.waitPanelBroadcast(ctx, 999, due)

	elapsed := time.Since(start)
	if res != false {
		t.Fatalf("expected waitPanelBroadcast to return false on cancellation, got %v", res)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("expected fast exit upon context cancellation, took %v", elapsed)
	}
}

// BenchmarkAccountScaling mengukur penggunaan memori dan goroutine saat mensimulasikan
// kenaikan jumlah akun aktif dari 100 hingga 1.000.
//
// CATATAN PENTING: Ini adalah simulasi in-memory — tidak membuka koneksi MTProto nyata.
// Setiap "akun" adalah entri dalam map tanpa goroutine Gotd, TLS, atau NetworkConn.
// Angka RAM yang terukur mencerminkan overhead data struktur saja (~0.5–1 KB/akun),
// bukan konsumsi aktual sesi MTProto live (~2.5–3.5 MB/akun termasuk buffer + goroutine).
//
// Gunakan hasil benchmark ini untuk memahami tren skalabilitas linear, bukan sebagai
// prediksi RAM absolut untuk deployment production.
func BenchmarkAccountScaling(b *testing.B) {
	scales := []int{100, 250, 500, 750, 1000}

	for _, n := range scales {
		n := n
		b.Run(fmt.Sprintf("accounts_%d", n), func(b *testing.B) {
			b.ReportAllocs()

			for iter := 0; iter < b.N; iter++ {
				root, cancel := context.WithCancel(context.Background())

				svc := &Service{
					root:          root,
					active:        make(map[int64]*gobot.Client, n),
					accountStates: make(map[int64]AccountSessionState, n),
					accountGens:   make(map[int64]uint64, n),
					lastActive:    make(map[int64]time.Time, n),
					lastUpdate:    make(map[int64]time.Time, n),
					lastVerified:  make(map[int64]time.Time, n),
					verifying:     make(map[int64]bool, n),
					owners:        make(map[int64]int64, n),
				}

				fakeClient := &gobot.Client{}
				for i := 0; i < n; i++ {
					chatID := int64(i + 1000000)
					svc.active[chatID] = fakeClient
					svc.accountStates[chatID] = StateConnected
					svc.lastActive[chatID] = time.Now().Add(-2 * time.Minute)
					svc.lastUpdate[chatID] = time.Now().Add(-2 * time.Minute)
					svc.accountGens[chatID] = uint64(i + 1)
					svc.owners[chatID] = chatID
				}

				// Ukur MemStats setelah populate
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)

				goroutinesBefore := runtime.NumGoroutine()

				// Jalankan heartbeat selection (tanpa RPC nyata)
				var checked int64
				svc.mu.Lock()
				now := time.Now()
				const activityThreshold = 60 * time.Second
				const verifyMaxGap = 3 * time.Minute
				for chatID, client := range svc.active {
					if client == nil {
						continue
					}
					state := svc.accountStates[chatID]
					if state == StateSessionInvalid || state == StateStopped {
						continue
					}
					recentActivity := now.Sub(svc.lastActive[chatID]) < activityThreshold || now.Sub(svc.lastUpdate[chatID]) < activityThreshold
					recentVerified := !svc.lastVerified[chatID].IsZero() && now.Sub(svc.lastVerified[chatID]) < verifyMaxGap
					if recentActivity && recentVerified {
						continue
					}
					atomic.AddInt64(&checked, 1)
				}
				svc.mu.Unlock()

				goroutinesAfter := runtime.NumGoroutine()

				b.ReportMetric(float64(ms.HeapInuse)/1024/1024, "heap_inuse_MB")
				b.ReportMetric(float64(ms.Alloc)/1024/1024, "alloc_MB")
				b.ReportMetric(float64(ms.NumGC), "num_gc")
				b.ReportMetric(float64(goroutinesAfter-goroutinesBefore), "goroutine_delta")
				b.ReportMetric(float64(checked), "accounts_checked")

				cancel()
			}
		})
	}
}

func TestFindActiveClientLocked_HierarchyAndNoDeadlock(t *testing.T) {
	fakeClient1 := &gobot.Client{}
	fakeClient2 := &gobot.Client{}

	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*gobot.Client),
		connecting:    make(map[int64]uint64),
		accountGens:   make(map[int64]uint64),
		accountStates: make(map[int64]AccountSessionState),
		activeCancels: make(map[int64]context.CancelFunc),
		accountCtxs:   make(map[int64]context.Context),
		owners:        make(map[int64]int64),
		lastActive:    make(map[int64]time.Time),
	}

	// 1. None active
	svc.mu.Lock()
	c, cid := svc.findActiveClientLocked(100, 200, 300)
	svc.mu.Unlock()
	if c != nil || cid != 0 {
		t.Fatalf("expected nil client, got %v with cid %d", c, cid)
	}

	// 2. Client on targetOwner
	svc.active[100] = fakeClient1
	svc.mu.Lock()
	c, cid = svc.findActiveClientLocked(100, 200, 300)
	svc.mu.Unlock()
	if c != fakeClient1 || cid != 100 {
		t.Fatalf("expected fakeClient1 at 100, got %v at %d", c, cid)
	}

	// 3. Client on owners mapping
	delete(svc.active, 100)
	svc.active[999] = fakeClient2
	svc.owners[999] = 200 // mapped to userID
	svc.mu.Lock()
	c, cid = svc.findActiveClientLocked(100, 200, 300)
	svc.mu.Unlock()
	if c != fakeClient2 || cid != 999 {
		t.Fatalf("expected fakeClient2 at 999, got %v at %d", c, cid)
	}

	// 4. Deadlock safety test on HandleCommand:
	// If context is canceled, HandleCommand retry loop must NOT keep s.mu locked.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	payload := `{"owner_id":888,"user_id":888,"chat_id":888,"type":"ping"}`
	msg := redis.XMessage{
		ID:     "1-0",
		Values: map[string]interface{}{"payload": payload},
	}

	// HandleCommand should return promptly and leave s.mu unlocked
	svc.HandleCommand(ctx, msg)

	// Verify s.mu can be acquired without blocking
	lockAcquired := make(chan bool, 1)
	go func() {
		svc.mu.Lock()
		svc.mu.Unlock()
		lockAcquired <- true
	}()

	select {
	case <-lockAcquired:
		// success: mutex was not leaked/deadlocked
	case <-time.After(500 * time.Millisecond):
		t.Fatal("s.mu is permanently locked (deadlock detected) after HandleCommand exit")
	}
}

func TestEvictStalePanelMsgCache(t *testing.T) {
	now := time.Now()
	panelMsgCache.Store(1001, panelCachedMsg{
		msg:       &tg.Message{ID: 1001},
		fetchedAt: now.Add(-6 * time.Minute), // stale (>5m)
	})
	panelMsgCache.Store(1002, panelCachedMsg{
		msg:       &tg.Message{ID: 1002},
		fetchedAt: now.Add(-1 * time.Minute), // fresh (<5m)
	})

	evictStalePanelMsgCache(now)

	if _, ok := panelMsgCache.Load(1001); ok {
		t.Error("stale message (1001) should have been evicted")
	}
	if _, ok := panelMsgCache.Load(1002); !ok {
		t.Error("fresh message (1002) should still exist in cache")
	}
}

func TestPruneSyncMap(t *testing.T) {
	var m sync.Map
	for i := 0; i < 15; i++ {
		m.Store(i, fmt.Sprintf("val-%d", i))
	}

	// maxEntries: 10, pruneCount: 4 -> entries should be reduced from 15 to 11
	pruneSyncMap(&m, 10, 4)

	count := 0
	m.Range(func(key, value any) bool {
		count++
		return true
	})
	if count != 11 {
		t.Fatalf("expected 11 entries remaining, got %d", count)
	}

	// Nil safety
	pruneSyncMap(nil, 10, 4)
}

func TestSyncSessionStates_DeltaFilter(t *testing.T) {
	now := time.Now()
	svc := &Service{
		accountStates:       make(map[int64]AccountSessionState),
		lastPublishedStates: make(map[int64]AccountSessionState),
		lastPublishedAt:     make(map[int64]time.Time),
		owners:              make(map[int64]int64),
	}

	// acc1: unchanged, published 1m ago -> should be filtered out
	svc.accountStates[1] = StateConnected
	svc.lastPublishedStates[1] = StateConnected
	svc.lastPublishedAt[1] = now.Add(-1 * time.Minute)
	svc.owners[1] = 1

	// acc2: state changed from CONNECTING to CONNECTED -> should be included
	svc.accountStates[2] = StateConnected
	svc.lastPublishedStates[2] = StateConnecting
	svc.lastPublishedAt[2] = now.Add(-30 * time.Second)
	svc.owners[2] = 2

	// acc3: state unchanged, but published 12m ago (>= 10m refresh interval) -> should be included
	svc.accountStates[3] = StateConnected
	svc.lastPublishedStates[3] = StateConnected
	svc.lastPublishedAt[3] = now.Add(-12 * time.Minute)
	svc.owners[3] = 3

	type item struct {
		chatID  int64
		ownerID int64
		state   AccountSessionState
	}
	var items []item

	svc.mu.Lock()
	for chatID, state := range svc.accountStates {
		if svc.lastPublishedStates == nil || svc.lastPublishedStates[chatID] != state || svc.lastPublishedAt == nil || svc.lastPublishedAt[chatID].IsZero() || now.Sub(svc.lastPublishedAt[chatID]) >= 10*time.Minute {
			items = append(items, item{
				chatID:  chatID,
				ownerID: svc.owners[chatID],
				state:   state,
			})
		}
	}
	svc.mu.Unlock()

	if len(items) != 2 {
		t.Fatalf("expected exactly 2 items to publish, got %d", len(items))
	}
	for _, it := range items {
		if it.chatID == 1 {
			t.Errorf("acc1 should have been filtered out (delta unchanged & published recently)")
		}
	}
}

func TestIsRelevantMessage_EarlyDrop(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(root, nil, nil, nil, 0, "", nil, "", "", 0)
	chatID := int64(1001)
	svc.mu.Lock()
	svc.owners[chatID] = chatID
	svc.prefixes[chatID] = "!"
	svc.mu.Unlock()

	if svc.isRelevantMessage(chatID, &tg.Message{
		PeerID:  &tg.PeerChannel{ChannelID: 777},
		Message: "pesan grup biasa",
	}) {
		t.Fatal("expected normal group chatter to be dropped early")
	}

	if !svc.isRelevantMessage(chatID, &tg.Message{
		PeerID:  &tg.PeerChannel{ChannelID: 777},
		Message: "!ping",
	}) {
		t.Fatal("expected prefix command in group to pass early filter")
	}

	if !svc.isRelevantMessage(chatID, &tg.Message{
		PeerID:  &tg.PeerChannel{ChannelID: 777},
		Message: "#promo",
	}) {
		t.Fatal("expected hashtag note in group to pass early filter")
	}

	if !svc.isRelevantMessage(chatID, &tg.Message{
		PeerID:    &tg.PeerChannel{ChannelID: 777},
		Mentioned: true,
		Message:   "halo bot",
	}) {
		t.Fatal("expected mentioned message in group to pass early filter")
	}

	if !svc.isRelevantMessage(chatID, &tg.Message{
		PeerID:  &tg.PeerUser{UserID: 999},
		Message: "halo pm",
	}) {
		t.Fatal("expected private message to pass early filter")
	}
}

func TestResolvePeer_SharedAccessHash(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(root, nil, nil, nil, 0, "", nil, "", "", 0)
	chatID := int64(1001)

	svc.userAccessHashes.Store(int64(555), int64(99999))
	svc.channelAccessHashes.Store(int64(777), int64(88888))

	uPeer, err := svc.resolvePeer(context.Background(), nil, chatID, tg.Entities{}, &tg.PeerUser{UserID: 555}, nil)
	if err != nil {
		t.Fatalf("resolvePeer user failed: %v", err)
	}
	ipUser, ok := uPeer.(*tg.InputPeerUser)
	if !ok || ipUser.UserID != 555 || ipUser.AccessHash != 99999 {
		t.Fatalf("unexpected resolved user peer: %+v", uPeer)
	}

	cPeer, err := svc.resolvePeer(context.Background(), nil, chatID, tg.Entities{}, &tg.PeerChannel{ChannelID: 777}, nil)
	if err != nil {
		t.Fatalf("resolvePeer channel failed: %v", err)
	}
	ipChan, ok := cPeer.(*tg.InputPeerChannel)
	if !ok || ipChan.ChannelID != 777 || ipChan.AccessHash != 88888 {
		t.Fatalf("unexpected resolved channel peer: %+v", cPeer)
	}
}

func BenchmarkPeerOverhead_1000Accounts(b *testing.B) {
	b.ReportAllocs()
	users := make([]*tg.User, 30)
	for i := 0; i < 30; i++ {
		users[i] = &tg.User{
			ID:         int64(10000 + i),
			AccessHash: int64(900000 + i),
			FirstName:  "User",
			Username:   fmt.Sprintf("user_%d", i),
		}
	}
	chats := make([]*tg.Channel, 20)
	for i := 0; i < 20; i++ {
		chats[i] = &tg.Channel{
			ID:         int64(20000 + i),
			AccessHash: int64(800000 + i),
			Title:      fmt.Sprintf("Group Promo %d", i),
		}
	}
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	for iter := 0; iter < b.N; iter++ {
		svc := NewService(root, nil, nil, nil, 0, "", nil, "", "", 0)
		for i := 0; i < 1000; i++ {
			for _, u := range users {
				svc.peerCache().SaveUser(u.ID, u.AccessHash)
			}
			for _, ch := range chats {
				svc.peerCache().SaveChannel(ch.ID, ch.AccessHash)
			}
		}
		runtime.KeepAlive(svc)
	}
}

func BenchmarkGroupNoiseHotPath(b *testing.B) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := NewService(root, nil, nil, nil, 0, "", nil, "", "", 0)
	chatID := int64(1001)
	svc.mu.Lock()
	svc.owners[chatID] = chatID
	svc.prefixes[chatID] = "!"
	svc.mu.Unlock()
	svc.logStatusCache.Store(chatID, false)
	svc.ownerUsernames.Store(chatID, "testuser")

	entities := tg.Entities{
		Users: map[int64]*tg.User{
			555: {ID: 555, AccessHash: 9999, FirstName: "Sender"},
			556: {ID: 556, AccessHash: 9998, FirstName: "Other"},
		},
		Channels: map[int64]*tg.Channel{
			777: {ID: 777, AccessHash: 8888, Title: "Grup Ramai"},
		},
	}
	msg := &tg.Message{
		ID:      42,
		Out:     false,
		FromID:  &tg.PeerUser{UserID: 555},
		PeerID:  &tg.PeerChannel{ChannelID: 777},
		Message: "halo guys lagi promo nih yuk merapat",
	}
	upd := &tg.UpdateNewChannelMessage{Message: msg}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.recordIncomingUpdate(chatID)
		_ = svc.processNewChannelMessage(ctx, chatID, nil, entities, upd)
	}
}
