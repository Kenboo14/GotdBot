package userbot

import (
	"context"
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

func TestHeartbeatOptimization_SkipsRecentlyActiveAccounts(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := &Service{
		root:          root,
		active:        make(map[int64]*telegram.Client),
		accountStates: make(map[int64]AccountSessionState),
		accountGens:   make(map[int64]uint64),
		lastActive:    make(map[int64]time.Time),
		lastUpdate:    make(map[int64]time.Time),
	}

	activeChatID := int64(1001)
	idleChatID := int64(1002)

	// Simulasikan client pointer (non-nil)
	fakeClient := &telegram.Client{}
	svc.active[activeChatID] = fakeClient
	svc.accountStates[activeChatID] = StateConnected
	svc.lastActive[activeChatID] = time.Now().Add(-10 * time.Second) // aktif 10 detik lalu

	svc.active[idleChatID] = fakeClient
	svc.accountStates[idleChatID] = StateConnected
	svc.lastActive[idleChatID] = time.Now().Add(-2 * time.Minute) // idle 2 menit
	svc.lastUpdate[idleChatID] = time.Now().Add(-2 * time.Minute)

	// Evaluasi kandidat heartbeat
	now := time.Now()
	const activityThreshold = 60 * time.Second

	var candidates []int64
	for chatID, client := range svc.active {
		if client == nil || svc.accountStates[chatID] == StateSessionInvalid || svc.accountStates[chatID] == StateStopped {
			continue
		}
		if now.Sub(svc.lastActive[chatID]) < activityThreshold || now.Sub(svc.lastUpdate[chatID]) < activityThreshold {
			continue // skip akun aktif!
		}
		candidates = append(candidates, chatID)
	}

	if len(candidates) != 1 || candidates[0] != idleChatID {
		t.Fatalf("expected only idleChatID (%d) in heartbeat candidates, got %+v", idleChatID, candidates)
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
