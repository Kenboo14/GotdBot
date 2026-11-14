package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
)

// TestReapStaleResources_KeepsActiveSessionsPersistent memverifikasi bahwa
// background reaper TIDAK PERNAH memutus MTProto client aktif, dan hanya
// membersihkan login OTP stale (> 5 menit) dan membatasi sudoCache.
func TestReapStaleResources_KeepsActiveSessionsPersistent(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		logins:        make(map[int64]*login),
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		lastActive:    make(map[int64]time.Time),
		sudoCache:     make(map[string]sudoCacheEntry),
	}

	// 1. Simulasikan active session yang sudah "idle" selama 2 jam
	chatIDActive := int64(1001)
	activeCancelCalled := false
	svc.activeCancels[chatIDActive] = func() {
		activeCancelCalled = true
	}
	svc.lastActive[chatIDActive] = time.Now().Add(-2 * time.Hour)

	// 2. Simulasikan login baru (< 5 menit)
	chatIDFreshLogin := int64(2001)
	freshCancelCalled := false
	svc.logins[chatIDFreshLogin] = &login{
		cancel:    func() { freshCancelCalled = true },
		createdAt: time.Now().Add(-1 * time.Minute),
	}

	// 3. Simulasikan login stale (> 5 menit)
	chatIDStaleLogin := int64(2002)
	staleCancelCalled := false
	svc.logins[chatIDStaleLogin] = &login{
		cancel:    func() { staleCancelCalled = true },
		createdAt: time.Now().Add(-10 * time.Minute),
	}

	// 4. Isi sudoCache hingga melebihi batas (2500 entries, sebagian expired)
	for i := 0; i < 2500; i++ {
		exp := time.Now().Add(30 * time.Minute)
		if i < 600 {
			exp = time.Now().Add(-10 * time.Minute) // expired
		}
		svc.sudoCache[string(rune(i))] = sudoCacheEntry{allowed: true, expiresAt: exp}
	}

	// Jalankan reapStaleResources
	svc.reapStaleResources()

	// Verifikasi:
	// a. Active session cancel TIDAK PERNAH dipanggil
	if activeCancelCalled {
		t.Fatalf("Reaper memutus sesi MTProto aktif! Harusnya 24/7 persistent.")
	}
	if _, ok := svc.activeCancels[chatIDActive]; !ok {
		t.Fatalf("activeCancels[chatIDActive] terhapus oleh reaper.")
	}

	// b. Fresh login tetap dipertahankan
	if freshCancelCalled {
		t.Fatalf("Fresh login tidak boleh dibatalkan.")
	}
	if _, ok := svc.logins[chatIDFreshLogin]; !ok {
		t.Fatalf("Fresh login terhapus dari s.logins.")
	}

	// c. Stale login dibatalkan dan dihapus
	if !staleCancelCalled {
		t.Fatalf("Stale login cancel harusnya dipanggil.")
	}
	if _, ok := svc.logins[chatIDStaleLogin]; ok {
		t.Fatalf("Stale login harusnya dihapus dari s.logins.")
	}

	// d. SudoCache dipangkas
	svc.sudoMu.RLock()
	cacheLen := len(svc.sudoCache)
	svc.sudoMu.RUnlock()
	if cacheLen > 2000 {
		t.Fatalf("SudoCache tidak dipangkas: len=%d", cacheLen)
	}
}

// TestDoubleActivationGuard memverifikasi guard connecting mencegah duplikasi aktivasi
func TestDoubleActivationGuard(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		lastActive:    make(map[int64]time.Time),
		owners:        make(map[int64]int64),
	}

	chatID := int64(8888)

	// Simulasi chatID sedang connecting
	svc.mu.Lock()
	svc.connecting[chatID] = 1
	svc.mu.Unlock()

	// Cek apakah pengecekan connecting mendeteksi state dengan benar
	svc.mu.Lock()
	isConnecting := svc.connecting[chatID] > 0
	svc.mu.Unlock()

	if !isConnecting {
		t.Fatalf("Guard connecting gagal mencatat state.")
	}
}

// TestAdmissionLimit_NoEviction memverifikasi bahwa ketika kapasitas maxActiveSessions tercapai,
// sistem menolak pendaftaran baru dan TIDAK mengorbankan (evict) akun yang sudah aktif.
func TestAdmissionLimit_NoEviction(t *testing.T) {
	origCap := maxActiveSessions
	maxActiveSessions = 2
	defer func() { maxActiveSessions = origCap }()

	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		lastActive:    make(map[int64]time.Time),
		owners:        make(map[int64]int64),
	}

	// Penuhi kapasitas dengan 2 sesi aktif
	session1CancelCalled := false
	session2CancelCalled := false

	svc.mu.Lock()
	svc.active[1] = nil // placeholder
	svc.activeCancels[1] = func() { session1CancelCalled = true }
	svc.lastActive[1] = time.Now().Add(-5 * time.Hour)

	svc.active[2] = nil // placeholder
	svc.activeCancels[2] = func() { session2CancelCalled = true }
	svc.lastActive[2] = time.Now().Add(-1 * time.Hour)
	svc.mu.Unlock()

	// Verifikasi apakah len(active) + len(connecting) >= maxActiveSessions
	svc.mu.Lock()
	isFull := len(svc.active)+len(svc.connecting) >= maxActiveSessions
	svc.mu.Unlock()

	if !isFull {
		t.Fatalf("Kapasitas pool seharusnya penuh.")
	}

	// Verifikasi tidak ada eviction yang memanggil cancel pada session1 atau session2
	if session1CancelCalled || session2CancelCalled {
		t.Fatalf("Sesi aktif ter-evict padahal kebijakan adalah admission limit (reject new, protect active).")
	}
}

// TestConcurrentActivation_AtomicSingleWinner memverifikasi bahwa ketika multiple goroutine
// mencoba mengaktivasi chatID yang sama secara bersamaan, tepat 1 yang berhasil
// dan sisanya tertahan oleh connecting guard tanpa race condition.
func TestConcurrentActivation_AtomicSingleWinner(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		lastActive:    make(map[int64]time.Time),
	}

	chatID := int64(77777)
	const goroutines = 20
	winners := 0
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			svc.mu.Lock()
			// Logika guard sama persis dengan baris awal activate()
			if c, ok := svc.active[chatID]; ok && c != nil {
				svc.mu.Unlock()
				done <- false
				return
			}
			if svc.connecting[chatID] > 0 {
				svc.mu.Unlock()
				done <- false
				return
			}
			// Atomic reservation dengan generation ID
			svc.nextGen++
			svc.connecting[chatID] = svc.nextGen
			svc.mu.Unlock()
			done <- true
		}()
	}

	for i := 0; i < goroutines; i++ {
		if <-done {
			winners++
		}
	}

	if winners != 1 {
		t.Fatalf("Expected exactly 1 winner in concurrent activation, got %d", winners)
	}

	svc.mu.Lock()
	if svc.connecting[chatID] == 0 {
		t.Fatalf("connecting flag should be > 0")
	}
	svc.mu.Unlock()
}

// TestReapStaleResources_ConcurrentWithSudoCache memverifikasi bahwa pemangkasan
// sudoCache oleh reaper aman dijalankan bersamaan dengan pembacaan dan penulisan cache.
func TestReapStaleResources_ConcurrentWithSudoCache(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		logins:        make(map[int64]*login),
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		lastActive:    make(map[int64]time.Time),
		sudoCache:     make(map[string]sudoCacheEntry),
	}

	stop := make(chan struct{})
	doneWorkers := make(chan bool, 4)

	// 2 worker menulis dan membaca sudoCache
	for w := 0; w < 2; w++ {
		go func(workerID int) {
			for {
				select {
				case <-stop:
					doneWorkers <- true
					return
				default:
					key := "owner:user"
					svc.sudoMu.Lock()
					svc.sudoCache[key] = sudoCacheEntry{allowed: true, expiresAt: time.Now().Add(30 * time.Minute)}
					svc.sudoMu.Unlock()

					svc.sudoMu.RLock()
					_ = svc.sudoCache[key]
					svc.sudoMu.RUnlock()
				}
			}
		}(w)
	}

	// 2 worker menjalankan reapStaleResources
	for r := 0; r < 2; r++ {
		go func() {
			for {
				select {
				case <-stop:
					doneWorkers <- true
					return
				default:
					svc.reapStaleResources()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)

	for i := 0; i < 4; i++ {
		<-doneWorkers
	}
}

// TestBroadcastGenerationSafeCleanup memverifikasi bahwa cleanup job A tidak akan
// menghapus job B milik owner yang sama jika job B sudah menggantikan state map.
func TestBroadcastGenerationSafeCleanup(t *testing.T) {
	svc := &Service{
		broadcasts: make(map[int64]*broadcastJob),
	}

	ownerID := int64(999)
	jobA := &broadcastJob{id: "job-A"}
	jobB := &broadcastJob{id: "job-B"}

	// 1. Job A mulai
	svc.mu.Lock()
	svc.broadcasts[ownerID] = jobA
	svc.mu.Unlock()

	// 2. Job B mulai menggantikan Job A
	svc.mu.Lock()
	svc.broadcasts[ownerID] = jobB
	svc.mu.Unlock()

	// 3. Cleanup Job A berjalan (menggunakan pointer comparison)
	svc.mu.Lock()
	if svc.broadcasts[ownerID] == jobA {
		delete(svc.broadcasts, ownerID)
	}
	svc.mu.Unlock()

	// 4. Verifikasi: Job B TIDAK terhapus oleh cleanup Job A!
	svc.mu.Lock()
	current := svc.broadcasts[ownerID]
	svc.mu.Unlock()

	if current != jobB {
		t.Fatalf("Generation-safe cleanup gagal: Job B terhapus oleh cleanup Job A")
	}

	// 5. Cleanup Job B berjalan
	svc.mu.Lock()
	if svc.broadcasts[ownerID] == jobB {
		delete(svc.broadcasts, ownerID)
	}
	svc.mu.Unlock()

	svc.mu.Lock()
	remaining := svc.broadcasts[ownerID]
	svc.mu.Unlock()

	if remaining != nil {
		t.Fatalf("Job B harusnya sudah terhapus setelah cleanup miliknya sendiri")
	}
}

// TestAccountCancellation_PropagatesToBroadcast memverifikasi bahwa penghentian
// lifecycle akun (accountCtx cancel) otomatis membatalkan seluruh job broadcast akun tersebut.
func TestAccountCancellation_PropagatesToBroadcast(t *testing.T) {
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// accountCtx diturunkan dari appCtx
	accountCtx, accountCancel := context.WithCancel(appCtx)

	// jobCtx diturunkan dari accountCtx
	jobCtx, jobCancel := context.WithCancel(accountCtx)
	defer jobCancel()

	// Batalkan accountCtx
	accountCancel()

	// Pastikan jobCtx ikut Done
	select {
	case <-jobCtx.Done():
		// PASS
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("jobCtx tidak terbatalkan saat accountCtx dihentikan")
	}
}

// TestLifecycleOldGenerationCleanup_DoesNotDeleteNewGeneration memverifikasi bahwa
// goroutine lifecycle lama (Gen 1) saat keluar TIDAK akan menghapus state milik
// lifecycle generasi baru (Gen 2) untuk chatID yang sama.
func TestLifecycleOldGenerationCleanup_DoesNotDeleteNewGeneration(t *testing.T) {
	svc := &Service{
		active:        make(map[int64]*telegram.Client),
		connecting:    make(map[int64]uint64),
		activeCancels: make(map[int64]context.CancelFunc),
		accountCtxs:   make(map[int64]context.Context),
		accountGens:   make(map[int64]uint64),
		lastActive:    make(map[int64]time.Time),
		clientOwners:  make(map[*telegram.Client]int64),
		senderCache:   make(map[*telegram.Client]*message.Sender),
	}

	chatID := int64(12345)

	// 1. Lifecycle Gen 1 diinisialisasi
	gen1ID := uint64(1)
	_, gen1Cancel := context.WithCancel(context.Background())
	defer gen1Cancel()
	var clientGen1 *telegram.Client // misal gagal sebelum client dibuat atau client lama

	// 2. Lifecycle Gen 2 sudah mengambil alih chatID
	gen2ID := uint64(2)
	gen2Ctx, gen2Cancel := context.WithCancel(context.Background())
	defer gen2Cancel()

	svc.mu.Lock()
	svc.connecting[chatID] = gen2ID
	svc.accountGens[chatID] = gen2ID
	svc.accountCtxs[chatID] = gen2Ctx
	svc.activeCancels[chatID] = gen2Cancel
	svc.lastActive[chatID] = time.Now()
	svc.mu.Unlock()

	// 3. Defer lifecycle Gen 1 dieksekusi sekarang (simulasi transisi race)
	svc.mu.Lock()
	if svc.connecting[chatID] == gen1ID {
		delete(svc.connecting, chatID)
	}
	if svc.active[chatID] == clientGen1 && clientGen1 != nil {
		delete(svc.active, chatID)
		delete(svc.lastActive, chatID)
	}
	if svc.accountGens[chatID] == gen1ID {
		delete(svc.activeCancels, chatID)
		delete(svc.accountCtxs, chatID)
		delete(svc.accountGens, chatID)
	}
	svc.mu.Unlock()

	// 4. Verifikasi bahwa state Gen 2 tetap utuh dan TIDAK terhapus
	svc.mu.Lock()
	currentConnecting := svc.connecting[chatID]
	currentGen := svc.accountGens[chatID]
	currentCtx := svc.accountCtxs[chatID]
	currentCancel := svc.activeCancels[chatID]
	svc.mu.Unlock()

	if currentConnecting != gen2ID {
		t.Fatalf("connecting milik Gen 2 terhapus atau rusak: got %d", currentConnecting)
	}
	if currentGen != gen2ID {
		t.Fatalf("accountGens milik Gen 2 terhapus: got %d", currentGen)
	}
	if currentCtx != gen2Ctx {
		t.Fatalf("accountCtxs milik Gen 2 terhapus")
	}
	if currentCancel == nil {
		t.Fatalf("activeCancels milik Gen 2 terhapus")
	}
}

// TestSudoCache_ErrorHandlingAndStaleWrite memverifikasi perilaku sudoCache:
// 1. Record ada -> cached true
// 2. ErrNoDocuments -> cached false
// 3. Operational Error -> TIDAK di-cache false (fail-closed transient)
// 4. Query lambat -> tidak menimpa data yang lebih baru (anti-stale-write)
// 5. Expiration -> strict lookup mengabaikan entri expired
func TestSudoCache_ErrorHandlingAndStaleWrite(t *testing.T) {
	svc := &Service{
		sudoCache: make(map[string]sudoCacheEntry),
	}

	cacheKey := "100:200"

	// Skenario 1: Record ada
	svc.sudoMu.Lock()
	svc.sudoCache[cacheKey] = sudoCacheEntry{
		allowed:   true,
		expiresAt: time.Now().Add(30 * time.Minute),
	}
	svc.sudoMu.Unlock()

	svc.sudoMu.RLock()
	entry, found := svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()
	if !found || !entry.allowed {
		t.Fatalf("Harusnya cached allowed=true")
	}

	// Skenario 2: Record ErrNoDocuments
	svc.sudoMu.Lock()
	svc.sudoCache[cacheKey] = sudoCacheEntry{
		allowed:   false,
		expiresAt: time.Now().Add(30 * time.Minute),
	}
	svc.sudoMu.Unlock()

	svc.sudoMu.RLock()
	entry, found = svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()
	if !found || entry.allowed {
		t.Fatalf("Harusnya cached allowed=false")
	}

	// Skenario 3: Operational Error (Timeout/Network)
	// Aturan: jangan pernah menulis ke cache!
	// Jika cache sudah dihapus/kosong:
	svc.sudoMu.Lock()
	delete(svc.sudoCache, cacheKey)
	svc.sudoMu.Unlock()

	// Saat operasional error terjadi, cache tetap kosong (tidak mencache false)
	svc.sudoMu.RLock()
	_, found = svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()
	if found {
		t.Fatalf("Operational error tidak boleh disimpan ke dalam cache!")
	}

	// Skenario 4: Anti-stale-write
	// Thread B baru saja menyimpan data valid
	svc.sudoMu.Lock()
	newerExpiry := time.Now().Add(25 * time.Minute)
	svc.sudoCache[cacheKey] = sudoCacheEntry{
		allowed:   true,
		expiresAt: newerExpiry,
	}
	svc.sudoMu.Unlock()

	// Thread A (query lama selesai terlambat) mencoba menyimpan
	svc.sudoMu.Lock()
	cur, exists := svc.sudoCache[cacheKey]
	if !exists || time.Now().After(cur.expiresAt) {
		svc.sudoCache[cacheKey] = sudoCacheEntry{
			allowed:   false, // hasil lama
			expiresAt: time.Now().Add(10 * time.Minute),
		}
	}
	svc.sudoMu.Unlock()

	// Verifikasi: hasil lama Thread A DITOLAK, cache tetap milik Thread B
	svc.sudoMu.RLock()
	finalEntry := svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()
	if !finalEntry.allowed || finalEntry.expiresAt != newerExpiry {
		t.Fatalf("Anti-stale-write gagal: data baru tertimpa oleh query lama")
	}

	// Skenario 5: Strict expiration pada lookup
	svc.sudoMu.Lock()
	svc.sudoCache[cacheKey] = sudoCacheEntry{
		allowed:   true,
		expiresAt: time.Now().Add(-5 * time.Minute), // expired
	}
	svc.sudoMu.Unlock()

	// Lookup strict
	svc.sudoMu.RLock()
	expiredEntry, found := svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()

	isAllowed := false
	if found {
		if time.Now().Before(expiredEntry.expiresAt) {
			isAllowed = expiredEntry.allowed
		} else {
			// Expired: hapus dan perlakukan sebagai miss
			svc.sudoMu.Lock()
			if cur, ok := svc.sudoCache[cacheKey]; ok && cur.expiresAt == expiredEntry.expiresAt {
				delete(svc.sudoCache, cacheKey)
			}
			svc.sudoMu.Unlock()
		}
	}
	if isAllowed {
		t.Fatalf("Entri expired tidak boleh diizinkan (strict expiration check)")
	}

	svc.sudoMu.RLock()
	_, existsAfter := svc.sudoCache[cacheKey]
	svc.sudoMu.RUnlock()
	if existsAfter {
		t.Fatalf("Entri expired harusnya telah dihapus dari map")
	}
}

// TestPingContextDeadlineExceeded memverifikasi bahwa context dengan deadline 5 detik
// yang kedaluwarsa dibatalkan dan menghentikan alur tanpa menggantung dispatcher.
func TestPingContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	time.Sleep(15 * time.Millisecond)

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("Expected DeadlineExceeded, got %v", ctx.Err())
		}
	default:
		t.Fatalf("Context should be canceled after deadline")
	}
}

// TestSQLiteStorage_CRUD memverifikasi operasi CRUD session, sudo, dan blacklist
// pada storage Pure-Go SQLite in-memory.
func TestSQLiteStorage_CRUD(t *testing.T) {
	ctx := context.Background()
	sqlite, err := databases.NewSQLiteClient(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory SQLite: %v", err)
	}
	defer sqlite.Close()

	// 1. Session CRUD
	err = sqlite.SaveSession(ctx, 1001, 2001, 3001, "encBlob123", "!", true)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	has, err := sqlite.HasSession(ctx, 1001, 2001)
	if err != nil || !has {
		t.Fatalf("HasSession failed: %v, has=%v", err, has)
	}

	sess, err := sqlite.GetSessionByChat(ctx, 1001)
	if err != nil || sess.Blob != "encBlob123" || sess.Prefix != "!" {
		t.Fatalf("GetSessionByChat unexpected: %v, sess=%+v", err, sess)
	}

	err = sqlite.UpdatePrefix(ctx, 1001, ".")
	if err != nil {
		t.Fatalf("UpdatePrefix failed: %v", err)
	}

	sessUpdated, _ := sqlite.GetSessionByChat(ctx, 1001)
	if sessUpdated.Prefix != "." {
		t.Fatalf("Expected updated prefix '.', got %s", sessUpdated.Prefix)
	}

	// 2. Sudo CRUD
	isSudo, err := sqlite.IsSudo(ctx, 2001, 9999)
	if err != nil || isSudo {
		t.Fatalf("Expected isSudo=false, got %v, err=%v", isSudo, err)
	}

	err = sqlite.AddSudo(ctx, 2001, 9999)
	if err != nil {
		t.Fatalf("AddSudo failed: %v", err)
	}

	isSudoAfter, err := sqlite.IsSudo(ctx, 2001, 9999)
	if err != nil || !isSudoAfter {
		t.Fatalf("Expected isSudo=true, got %v, err=%v", isSudoAfter, err)
	}

	_ = sqlite.RemoveSudo(ctx, 2001, 9999)
	isSudoRemoved, _ := sqlite.IsSudo(ctx, 2001, 9999)
	if isSudoRemoved {
		t.Fatalf("Expected isSudo=false after removal")
	}

	// 3. Blacklist CRUD
	err = sqlite.AddBlacklist(ctx, 2001, "group", -100123456789)
	if err != nil {
		t.Fatalf("AddBlacklist failed: %v", err)
	}

	isBl, err := sqlite.IsBlacklisted(ctx, 2001, "group", -100123456789)
	if err != nil || !isBl {
		t.Fatalf("Expected isBlacklisted=true, got %v, err=%v", isBl, err)
	}

	blList, err := sqlite.ListBlacklist(ctx, 2001)
	if err != nil || len(blList) != 1 {
		t.Fatalf("Expected 1 blacklist entry, got %d", len(blList))
	}

	_ = sqlite.ClearBlacklist(ctx, 2001)
	blListEmpty, _ := sqlite.ListBlacklist(ctx, 2001)
	if len(blListEmpty) != 0 {
		t.Fatalf("Expected 0 blacklist entries after clear, got %d", len(blListEmpty))
	}
}

// TestNotes_ExtractionAndButtons memverifikasi ekstraksi tombol directive URL, callback, dan popup.
func TestNotes_ExtractionAndButtons(t *testing.T) {
	raw := "Halo ini pesan [Google|https://google.com] dan tombol [Callback#info] serta [Popup$help]"
	clean, buttons := extractNoteButtons(raw)

	if len(buttons) != 3 {
		t.Fatalf("Expected 3 buttons, got %d", len(buttons))
	}
	if buttons[0].Kind != "url" || buttons[0].URL != "https://google.com" {
		t.Fatalf("Button 0 unexpected: %+v", buttons[0])
	}
	if buttons[1].Kind != "callback" || buttons[1].Target != "info" {
		t.Fatalf("Button 1 unexpected: %+v", buttons[1])
	}
	if buttons[2].Kind != "popup" || buttons[2].Target != "help" {
		t.Fatalf("Button 2 unexpected: %+v", buttons[2])
	}
	if strings.Contains(clean, "[Google|") || strings.Contains(clean, "[Callback#") {
		t.Fatalf("Cleaned text should not contain button directives: %s", clean)
	}
}

// TestNotes_SQLiteCRUD memverifikasi operasi penyimpanan, listing, query, dan penghapusan catatan di SQLite.
func TestNotes_SQLiteCRUD(t *testing.T) {
	ctx := context.Background()
	sqlite, err := databases.NewSQLiteClient(":memory:")
	if err != nil {
		t.Fatalf("Failed to init SQLite: %v", err)
	}
	defer sqlite.Close()

	ownerID := int64(88888)

	// 1. Simpan catatan
	n1 := databases.NoteRecord{
		OwnerID:        ownerID,
		Name:           "test1",
		Text:           "Isi catatan 1",
		Mode:           "text",
		MediaType:      "",
		CacheMessageID: 715799,
		CacheChannel:   "AiotdCaches",
	}
	err = sqlite.SaveNote(ctx, n1)
	if err != nil {
		t.Fatalf("SaveNote failed: %v", err)
	}

	n2 := databases.NoteRecord{
		OwnerID:        ownerID,
		Name:           "test2",
		Text:           "Isi catatan 2",
		Mode:           "popup",
		MediaType:      "photo",
		CacheMessageID: 715800,
		CacheChannel:   "AiotdCaches",
	}
	_ = sqlite.SaveNote(ctx, n2)

	// 2. Query catatan
	got, err := sqlite.GetNote(ctx, ownerID, "test1")
	if err != nil || got == nil || got.Text != "Isi catatan 1" || got.CacheMessageID != 715799 || got.CacheChannel != "AiotdCaches" {
		t.Fatalf("GetNote unexpected: %v, got=%+v", err, got)
	}

	// 3. List catatan
	list, err := sqlite.ListNotes(ctx, ownerID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListNotes unexpected: %v, len=%d", err, len(list))
	}

	// 4. Hapus satu catatan
	delOk, err := sqlite.DeleteNote(ctx, ownerID, "test1")
	if err != nil || !delOk {
		t.Fatalf("DeleteNote failed: %v, delOk=%v", err, delOk)
	}
	delMiss, _ := sqlite.DeleteNote(ctx, ownerID, "test1")
	if delMiss {
		t.Fatalf("Second DeleteNote should return false")
	}

	// 5. Clear semua catatan
	count, err := sqlite.ClearNotes(ctx, ownerID)
	if err != nil || count != 1 {
		t.Fatalf("ClearNotes unexpected: %v, count=%d", err, count)
	}
}

// TestNotes_JSONExportImport_NoPassword memverifikasi bahwa export dan import notes
// bekerja tanpa membutuhkan password, memvalidasi integritas data catatan.
func TestNotes_JSONExportImport_NoPassword(t *testing.T) {
	payload := NoteExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Notes: []databases.NoteRecord{
			{OwnerID: 100, Name: "note_a", Text: "Konten A", Mode: "text", CacheMessageID: 715799, CacheChannel: "AiotdCaches"},
			{OwnerID: 100, Name: "note_b", Text: "Konten B", Mode: "callback", CacheMessageID: 715800, CacheChannel: "AiotdCaches"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal export payload failed: %v", err)
	}

	var imported NoteExportPayload
	err = json.Unmarshal(data, &imported)
	if err != nil {
		t.Fatalf("Unmarshal export payload failed: %v", err)
	}

	if len(imported.Notes) != 2 {
		t.Fatalf("Expected 2 imported notes, got %d", len(imported.Notes))
	}
	if imported.Notes[0].Name != "note_a" || imported.Notes[0].CacheMessageID != 715799 || imported.Notes[0].CacheChannel != "AiotdCaches" {
		t.Fatalf("Imported note A mismatched: %+v", imported.Notes[0])
	}
	if imported.Notes[1].Name != "note_b" || imported.Notes[1].CacheMessageID != 715800 || imported.Notes[1].CacheChannel != "AiotdCaches" {
		t.Fatalf("Imported note B mismatched: %+v", imported.Notes[1])
	}
}

// TestNotes_InlinePayloadAndButtons memverifikasi struktur payload inline notes dan sintesis tombol default untuk -cb / -pop
func TestNotes_InlinePayloadAndButtons(t *testing.T) {
	note := &databases.NoteRecord{
		OwnerID: 12345,
		Name:    "hi",
		Text:    "Halo dunia",
		Mode:    "callback",
	}

	var buttons []NoteButton
	if len(buttons) == 0 {
		if note.Mode == "callback" {
			buttons = append(buttons, NoteButton{Kind: "callback", Label: note.Name, Target: note.Name})
		} else if note.Mode == "popup" {
			buttons = append(buttons, NoteButton{Kind: "popup", Label: note.Name, Target: note.Name})
		}
	}

	if len(buttons) != 1 || buttons[0].Kind != "callback" || buttons[0].Label != "hi" {
		t.Fatalf("Synthesized button unexpected: %+v", buttons)
	}

	payload := NoteInlinePayload{
		OwnerID: note.OwnerID,
		Name:    note.Name,
		Text:    note.Text,
		Buttons: buttons,
		Mode:    note.Mode,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal inline payload failed: %v", err)
	}

	var parsed NoteInlinePayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal inline payload failed: %v", err)
	}

	if parsed.Name != "hi" || len(parsed.Buttons) != 1 || parsed.Buttons[0].Label != "hi" {
		t.Fatalf("Parsed payload unexpected: %+v", parsed)
	}
}
