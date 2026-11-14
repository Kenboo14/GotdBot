package userbot

import (
	"context"
	"crypto/rand"
	"io"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// recordFloodWait mencatat masa cooldown FLOOD_WAIT Telegram untuk akun tertentu.
func (s *Service) recordFloodWait(chatID int64, dur time.Duration) {
	// Tambahkan jitter 500ms agar request berikutnya tidak menembak serentak tepat di detik ke-0
	until := time.Now().Add(dur + 500*time.Millisecond)
	s.floodCooldowns.Store(chatID, until)
	slog.Warn("flood_wait cooldown active",
		slog.Int64("chat_id", chatID),
		slog.Duration("duration", dur),
		slog.Time("cooldown_until", until),
	)
}

// getFloodWaitRemaining mengembalikan sisa durasi cooldown FLOOD_WAIT untuk akun ini.
func (s *Service) getFloodWaitRemaining(chatID int64) time.Duration {
	val, ok := s.floodCooldowns.Load(chatID)
	if !ok {
		return 0
	}
	until, ok := val.(time.Time)
	if !ok {
		return 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		s.floodCooldowns.Delete(chatID)
		return 0
	}
	return remaining
}

// randJitterMs menghasilkan durasi jitter acak [0, maxMs) ms menggunakan crypto/rand.
// Digunakan untuk mencegah thundering-herd saat banyak goroutine bangun serentak
// setelah FLOOD_WAIT selesai dan menembak MTProto pada millisecond yang sama.
// Jika crypto/rand gagal (sangat jarang), fallback ke 0 (aman, tidak panik).
func randJitterMs(maxMs int) time.Duration {
	if maxMs <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return 0
	}
	// Interpretasi 8 byte sebagai uint64 lalu ambil modulo
	v := uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	return time.Duration(v%uint64(maxMs)) * time.Millisecond
}

func (s *Service) startIdleReaper(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapStaleResources()
		}
	}
}

// reapStaleResources bertugas membersihkan resource sementara yang sudah tidak digunakan,
// TANPA memutus koneksi MTProto client aktif (Persistent MTProto 24/7).
func (s *Service) reapStaleResources() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Membersihkan proses login OTP (/create) yang ditinggalkan user > 5 menit
	for chatID, l := range s.logins {
		if l != nil && !l.createdAt.IsZero() && now.Sub(l.createdAt) > 5*time.Minute {
			l.cancel()
			delete(s.logins, chatID)
		}
	}

	// 2. Bounded Cache: Eviction bertahap berbasis TTL dan batas awal kapasitas (2.000)
	s.sudoMu.Lock()
	for k, entry := range s.sudoCache {
		if now.After(entry.expiresAt) {
			delete(s.sudoCache, k)
		}
	}
	// Jika setelah pembersihan TTL ukuran masih melebihi batas konfigurasi awal (2.000),
	// lakukan pemangkasan bertahap (maksimal 20% entri) untuk mencegah lonjakan cache miss serentak ke MongoDB
	if len(s.sudoCache) > 2000 {
		pruned := 0
		for k := range s.sudoCache {
			delete(s.sudoCache, k)
			pruned++
			if pruned >= 400 {
				break
			}
		}
	}
	s.sudoMu.Unlock()

	// 3. Eviction floodCooldowns yang sudah kadaluarsa.
	s.floodCooldowns.Range(func(key, val any) bool {
		until, ok := val.(time.Time)
		if !ok || now.After(until) {
			s.floodCooldowns.Delete(key)
		}
		return true
	})
}

func (s *Service) recordIncomingUpdate(chatID int64) {
	now := time.Now()
	s.mu.Lock()
	s.lastUpdate[chatID] = now
	s.lastActive[chatID] = now
	s.mu.Unlock()
}

// triggerImmediateVerification menjadwalkan verifikasi sesi 'Check' seketika murni berbasis event MTProto.
func (s *Service) triggerImmediateVerification(chatID int64) {
	s.mu.Lock()
	genID, ok := s.accountGens[chatID]
	isVerif := s.verifying[chatID]
	lastVer := s.lastVerified[chatID]
	client := s.active[chatID]

	// Anti-flood cooldown 15 detik untuk membatasi frekuensi verifikasi Check
	if !ok || client == nil || isVerif || (!lastVer.IsZero() && time.Since(lastVer) < 15*time.Second) {
		s.mu.Unlock()
		return
	}
	s.accountStates[chatID] = StateSuspected
	ownerID := s.owners[chatID]
	s.mu.Unlock()
	s.publishSessionState(chatID, ownerID, StateSuspected, "Telegram mengirim service notification; memverifikasi sesi")

	go s.verifyAccountSession(s.root, chatID, genID)
}

// startSessionHeartbeatWorker memvalidasi AuthKey akun aktif secara berkala.
func (s *Service) startSessionHeartbeatWorker(ctx context.Context) {
	interval := time.Duration(sessionHeartbeatSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.heartbeatActiveSessions(ctx)
		}
	}
}

func (s *Service) heartbeatActiveSessions(ctx context.Context) {
	type candidate struct {
		chatID   int64
		genID    uint64
		client   *telegram.Client
		priority bool // true = akun bermasalah, selalu diperiksa lebih dulu
	}

	s.mu.Lock()
	now := time.Now()
	const activityThreshold = 60 * time.Second
	const verifyMaxGap = 3 * time.Minute

	candidates := make([]candidate, 0, len(s.active))
	for chatID, client := range s.active {
		if client == nil {
			continue
		}
		state := s.accountStates[chatID]
		if state == StateSessionInvalid || state == StateStopped {
			continue
		}

		isTroubled := state == StateSuspected || state == StateConnecting || state == StateReconnecting
		if isTroubled {
			candidates = append(candidates, candidate{
				chatID:   chatID,
				genID:    s.accountGens[chatID],
				client:   client,
				priority: true,
			})
			continue
		}

		recentActivity := now.Sub(s.lastActive[chatID]) < activityThreshold || now.Sub(s.lastUpdate[chatID]) < activityThreshold
		recentVerified := !s.lastVerified[chatID].IsZero() && now.Sub(s.lastVerified[chatID]) < verifyMaxGap
		if recentActivity && recentVerified {
			continue
		}

		candidates = append(candidates, candidate{
			chatID: chatID,
			genID:  s.accountGens[chatID],
			client: client,
		})
	}
	s.mu.Unlock()

	if len(candidates) == 0 {
		return
	}

	for i := 1; i < len(candidates); i++ {
		if candidates[i].priority && !candidates[i-1].priority {
			for j := i; j > 0 && !candidates[j-1].priority; j-- {
				candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
			}
		}
	}

	numWorkers := len(candidates)
	if numWorkers > 8 {
		numWorkers = 8
	}

	candCh := make(chan candidate, len(candidates))
	for _, c := range candidates {
		candCh <- c
	}
	close(candCh)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range candCh {
				s.heartbeatSession(ctx, item.chatID, item.genID, item.client)
			}
		}()
	}
	wg.Wait()
}

func (s *Service) heartbeatSession(ctx context.Context, chatID int64, genID uint64, client *telegram.Client) {
	select {
	case s.heartbeatSem <- struct{}{}:
		defer func() { <-s.heartbeatSem }()
	case <-ctx.Done():
		return
	}

	s.mu.Lock()
	if s.accountGens[chatID] != genID || s.active[chatID] != client {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	_, err := client.API().UpdatesGetState(checkCtx)
	cancel()
	if err != nil {
		if isSessionRevokedError(err) {
			log.Printf("[SessionHeartbeat] sesi revoked chatID=%d: %v", chatID, err)
			s.handleSessionRevoked(chatID, genID, err)
			return
		}
		log.Printf("[SessionHeartbeat] RPC sementara gagal chatID=%d: %v", chatID, err)
		return
	}

	s.mu.Lock()
	if s.accountGens[chatID] == genID && s.active[chatID] == client {
		now := time.Now()
		s.lastVerified[chatID] = now
		s.lastActive[chatID] = now
	}
	s.mu.Unlock()
}

// verifyAccountSession memverifikasi keabsahan sesi secara silent via RPC Telegram tanpa spam pesan di chat.
func (s *Service) verifyAccountSession(ctx context.Context, chatID int64, genID uint64) {
	select {
	case s.verifSem <- struct{}{}:
		defer func() { <-s.verifSem }()
	case <-ctx.Done():
		return
	}

	s.mu.Lock()
	if s.accountGens[chatID] != genID {
		s.mu.Unlock()
		return
	}
	client := s.active[chatID]
	if s.verifying[chatID] || client == nil {
		s.mu.Unlock()
		return
	}
	s.verifying[chatID] = true
	s.accountStates[chatID] = StateVerifying
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.verifying, chatID)
		s.mu.Unlock()
	}()

	stateCtx, stateCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err := client.API().UpdatesGetState(stateCtx)
	stateCancel()

	if err != nil {
		if isSessionRevokedError(err) {
			log.Printf("⚠️ [verifyAccount %d] Sesi terbukti revoked via Silent RPC UpdatesGetState: %v", chatID, err)
			go s.handleSessionRevoked(chatID, genID, err)
			return
		}

		log.Printf("ℹ️ [verifyAccount %d] Silent RPC gagal (%v), mencoba fallback verifikasi 'Check'...", chatID, err)
		s.verifyViaCheckMessage(ctx, chatID, genID, client)
		return
	}

	now := time.Now()
	s.mu.Lock()
	if s.accountGens[chatID] == genID {
		s.accountStates[chatID] = StateConnected
		s.lastVerified[chatID] = now
		s.lastUpdate[chatID] = now
		s.lastActive[chatID] = now
	}
	s.mu.Unlock()
}

// verifyViaCheckMessage adalah fallback verifikasi tahap 2 menggunakan pesan 'Check' ke @AiotdCaches reply-to 7073.
func (s *Service) verifyViaCheckMessage(ctx context.Context, chatID int64, genID uint64, client *telegram.Client) {
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		log.Printf("[verifyAccount %d] Gagal resolve cache: %v (bukan auth error, sesi dipertahankan)", chatID, err)
		if isSessionRevokedError(err) {
			go s.handleSessionRevoked(chatID, genID, err)
		}
		return
	}

	topicID := getSessionCacheTopicID()
	replyTo := &tg.InputReplyToMessage{
		ReplyToMsgID: topicID,
		TopMsgID:     topicID,
	}
	randomID := time.Now().UnixNano()
	req := &tg.MessagesSendMessageRequest{
		Peer:     cachePeer,
		ReplyTo:  replyTo,
		Message:  "Check",
		RandomID: randomID,
	}

	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	sentUpdates, err := client.API().MessagesSendMessage(sendCtx, req)
	sendCancel()

	if err != nil {
		if isSessionRevokedError(err) {
			log.Printf("⚠️ [verifyAccount %d] Sesi terbukti revoked via verifikasi 'Check': %v", chatID, err)
			go s.handleSessionRevoked(chatID, genID, err)
			return
		}

		log.Printf("ℹ️ [verifyAccount %d] Error non-auth saat verifikasi 'Check': %v (sesi dipertahankan)", chatID, err)
		s.mu.Lock()
		if s.accountGens[chatID] == genID {
			s.lastVerified[chatID] = time.Now()
			s.accountStates[chatID] = StateConnected
		}
		s.mu.Unlock()
		return
	}

	if sentUpdates != nil {
		go func() {
			delCtx, delCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer delCancel()
			if updShort, ok := sentUpdates.(*tg.UpdateShortSentMessage); ok {
				_, _ = client.API().MessagesDeleteMessages(delCtx, &tg.MessagesDeleteMessagesRequest{
					Revoke: true,
					ID:     []int{updShort.ID},
				})
			}
		}()
	}

	now := time.Now()
	s.mu.Lock()
	if s.accountGens[chatID] == genID {
		s.accountStates[chatID] = StateConnected
		s.lastVerified[chatID] = now
		s.lastUpdate[chatID] = now
		s.lastActive[chatID] = now
	}
	s.mu.Unlock()
	log.Printf("✅ [verifyAccount %d] Verifikasi 'Check' sukses! Akun aktif & terautentikasi penuh.", chatID)
}
