package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTierRuntimePriorityAndEnforcement(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tier_runtime.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	const uid int64 = 8801
	_, err = sq.DB.ExecContext(ctx, `
		INSERT INTO user_licenses (token_sha, master_token, owner_chat_id, userbot_id, tier, expires_at, status) VALUES
		('sha_free', 'GOTD:FREE', ?, ?, 'Free', ?, 'active'),
		('sha_pro', 'GOTD:PRO', ?, ?, 'Pro++', ?, 'active'),
		('sha_exp', 'GOTD:EXP', ?, ?, 'Vip', ?, 'expired')`,
		uid, uid, time.Now().Add(30*24*time.Hour),
		uid, uid, time.Now().Add(7*24*time.Hour),
		uid, uid, time.Now().Add(90*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("insert licenses failed: %v", err)
	}

	svc := &Service{sqlite: sq}
	if got := svc.getActiveTier(ctx, uid); got != "Pro++" {
		t.Fatalf("expected active tier 'Pro++', got %q", got)
	}
	if got := svc.getActiveTier(ctx, 999999); got != "" {
		t.Fatalf("expected empty tier for unlicensed user, got %q", got)
	}

	rawCfg := jasebConfigDoc{
		DelayMode:        "manual",
		Delay:            5,
		DelayAllGroups:   2,
		SkipFloodWait:    true,
		SkipFloodWaitSec: 30,
		Mode:             "Forward",
		Lists:            []int{1, 2, 3},
		Groups:           make([]jasebGroupDoc, 45),
	}
	freeCfg := enforceTierBroadcastConfig(rawCfg, "Free")
	if freeCfg.DelayMode != "aman" || freeCfg.Delay != 0 || freeCfg.SkipFloodWait || freeCfg.SkipFloodWaitSec != 0 || freeCfg.Mode != "Normal" || len(freeCfg.Groups) != 30 || len(freeCfg.Lists) != 1 {
		t.Fatalf("Free enforcement mismatch: %+v", freeCfg)
	}

	proCfg := enforceTierBroadcastConfig(rawCfg, "Pro++")
	if proCfg.DelayMode != "manual" || proCfg.Delay != 5 || len(proCfg.Groups) != 45 || len(proCfg.Lists) != 3 || proCfg.Mode != "Forward" {
		t.Fatalf("Pro++ config should stay untouched, got %+v", proCfg)
	}
}

func TestDailySendQuotaAndMidnightWIB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "quota.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	const uid int64 = 9901
	loc := jakartaLocation()
	day1 := todayWIBKey(time.Date(2026, 11, 20, 23, 30, 0, 0, loc))
	day2 := todayWIBKey(time.Date(2026, 11, 21, 0, 1, 0, 0, loc))
	if day1 == day2 {
		t.Fatalf("expected different WIB date keys across midnight: %q", day1)
	}

	for i := 1; i <= 50; i++ {
		got, incErr := sq.IncrementDailySendCount(ctx, uid, day1)
		if incErr != nil || got != i {
			t.Fatalf("increment step %d failed: got=%d err=%v", i, got, incErr)
		}
	}
	if cur, _ := sq.GetDailySendCount(ctx, uid, day1); cur != 50 {
		t.Fatalf("expected day1 count 50, got %d", cur)
	}
	if nextDay, _ := sq.GetDailySendCount(ctx, uid, day2); nextDay != 0 {
		t.Fatalf("expected day2 count 0 after reset, got %d", nextDay)
	}

	now := time.Date(2026, 11, 20, 14, 25, 10, 0, loc)
	mid := nextMidnightWIB(now)
	want := time.Date(2026, 11, 21, 0, 0, 0, 0, loc)
	if !mid.Equal(want) {
		t.Fatalf("nextMidnightWIB mismatch: got %v want %v", mid, want)
	}
}

func TestTierRuntimeActiveTokenSlotSelection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slot_tier.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	const uid int64 = 7701
	_, err = sq.DB.ExecContext(ctx, `
		INSERT INTO user_licenses (token_sha, master_token, owner_chat_id, userbot_id, tier, expires_at, status) VALUES
		('sha_free', 'GOTD:FREE', ?, ?, 'Free', ?, 'active'),
		('sha_pro', 'GOTD:PRO', ?, ?, 'Pro', ?, 'active'),
		('sha_propp', 'GOTD:PROPP', ?, ?, 'Pro++', ?, 'active')`,
		uid, uid, time.Now().Add(7*24*time.Hour),
		uid, uid, time.Now().Add(14*24*time.Hour),
		uid, uid, time.Now().Add(30*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("insert licenses failed: %v", err)
	}

	svc := &Service{sqlite: sq}
	if got := svc.getActiveTier(ctx, uid); got != "Pro++" {
		t.Fatalf("expected fallback highest tier Pro++, got %q", got)
	}

	_, err = sq.DB.ExecContext(ctx, `INSERT INTO jaseb_configurasi (owner_id, active_token_sha) VALUES (?, 'sha_pro')`, uid)
	if err != nil {
		t.Fatalf("insert jaseb_configurasi failed: %v", err)
	}
	if got := svc.getActiveTier(ctx, uid); got != "Pro" {
		t.Fatalf("expected active slot tier Pro, got %q", got)
	}

	_, _ = sq.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET active_token_sha = 'sha_free' WHERE owner_id = ?`, uid)
	if got := svc.getActiveTier(ctx, uid); got != "Free" {
		t.Fatalf("expected switched active slot tier Free, got %q", got)
	}

	_, _ = sq.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET active_token_sha = 'sha_nonexistent' WHERE owner_id = ?`, uid)
	if got := svc.getActiveTier(ctx, uid); got != "Pro++" {
		t.Fatalf("expected missing active_token_sha to fallback to highest tier Pro++, got %q", got)
	}
}
