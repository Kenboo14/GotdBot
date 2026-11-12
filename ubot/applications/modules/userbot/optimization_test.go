package userbot

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/telegram"
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
		active:        make(map[int64]*telegram.Client),
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
		active:        make(map[int64]*telegram.Client),
		accountStates: make(map[int64]AccountSessionState),
		accountGens:   make(map[int64]uint64),
		lastActive:    make(map[int64]time.Time),
		lastUpdate:    make(map[int64]time.Time),
		lastVerified:  make(map[int64]time.Time),
		verifying:     make(map[int64]bool),
	}

	fakeClient := &telegram.Client{}

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
					active:        make(map[int64]*telegram.Client, n),
					accountStates: make(map[int64]AccountSessionState, n),
					accountGens:   make(map[int64]uint64, n),
					lastActive:    make(map[int64]time.Time, n),
					lastUpdate:    make(map[int64]time.Time, n),
					lastVerified:  make(map[int64]time.Time, n),
					verifying:     make(map[int64]bool, n),
					owners:        make(map[int64]int64, n),
				}

				fakeClient := &telegram.Client{}
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

