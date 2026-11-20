package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeNextBroadcastWait(t *testing.T) {
	amanCfg := jasebConfigDoc{DelayMode: "aman"}
	minWait := computeNextBroadcastWait(amanCfg, 0, 10, func(n int) int { return 0 })
	maxWait := computeNextBroadcastWait(amanCfg, 0, 10, func(n int) int { return n - 1 })
	if minWait != 200*time.Second || maxWait != 400*time.Second {
		t.Fatalf("expected aman [200s,400s], got [%v,%v]", minWait, maxWait)
	}

	autoCfg := jasebConfigDoc{DelayMode: "otomatis"}
	if w := computeNextBroadcastWait(autoCfg, 0, 15, nil); w != 10*time.Second {
		t.Fatalf("auto 15 groups mid-round expected 10s, got %v", w)
	}
	if w := computeNextBroadcastWait(autoCfg, 14, 15, nil); w != 20*time.Minute {
		t.Fatalf("auto 15 groups end-round expected 20m, got %v", w)
	}
	if w := computeNextBroadcastWait(autoCfg, 79, 80, nil); w != 35*time.Second {
		t.Fatalf("auto 80 groups expected 35s, got %v", w)
	}

	manualBoth := jasebConfigDoc{DelayMode: "manual", Delay: 30, DelayAllGroups: 20}
	if w := computeNextBroadcastWait(manualBoth, 0, 3, nil); w != 30*time.Second {
		t.Fatalf("manual both mid-round expected 30s, got %v", w)
	}
	if w := computeNextBroadcastWait(manualBoth, 2, 3, nil); w != 20*time.Minute {
		t.Fatalf("manual both end-round expected 20m, got %v", w)
	}

	manualOnlyAll := jasebConfigDoc{DelayMode: "manual", Delay: 0, DelayAllGroups: 15}
	if w := computeNextBroadcastWait(manualOnlyAll, 0, 3, nil); w != 0 {
		t.Fatalf("manual all-only mid-round expected 0, got %v", w)
	}
	if w := computeNextBroadcastWait(manualOnlyAll, 2, 3, nil); w != 15*time.Minute {
		t.Fatalf("manual all-only end-round expected 15m, got %v", w)
	}

	manualBothOff := jasebConfigDoc{DelayMode: "manual", Delay: 0, DelayAllGroups: 0}
	if w := computeNextBroadcastWait(manualBothOff, 0, 3, nil); w != 0 {
		t.Fatalf("both off expected 0 delay, got %v", w)
	}

	safeCfg := jasebConfigDoc{DelayMode: "manual", Delay: 5, Safemode: true}
	if w := computeNextBroadcastWait(safeCfg, 0, 2, nil); w != 5500*time.Millisecond {
		t.Fatalf("safemode expected 5.5s, got %v", w)
	}
}

func TestShouldSkipFloodWait(t *testing.T) {
	if shouldSkipFloodWait(jasebConfigDoc{}, 120*time.Second) {
		t.Fatalf("disabled SkipFloodWait must never skip")
	}

	cfg60 := jasebConfigDoc{SkipFloodWait: true, SkipFloodWaitSec: 60}
	if shouldSkipFloodWait(cfg60, 60*time.Second) {
		t.Fatalf("60s flood should wait when limit is 60s")
	}
	if !shouldSkipFloodWait(cfg60, 61*time.Second) {
		t.Fatalf("61s flood should skip when limit is 60s")
	}

	legacy := jasebConfigDoc{SkipFloodWait: true, SkipFloodWaitSec: 0}
	if !shouldSkipFloodWait(legacy, 5*time.Second) {
		t.Fatalf("legacy SkipFloodWait=true should skip")
	}
}

func TestDailyTimerWindowAndBoundaries(t *testing.T) {
	loc := jakartaLocation()

	if paused, _, _ := shouldPauseForDailyTimer("Belum diatur", time.Now()); paused {
		t.Fatalf("Belum diatur should not pause")
	}
	if paused, _, _ := shouldPauseForDailyTimer("Tiap 1 Jam", time.Now()); paused {
		t.Fatalf("legacy interval string should not pause")
	}

	t0759 := time.Date(2026, 11, 20, 7, 59, 0, 0, loc)
	paused, resume, _ := shouldPauseForDailyTimer("08:00 - 22:00", t0759)
	if !paused || !resume.Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, loc)) {
		t.Fatalf("07:59 expected paused until 08:00 same day, got paused=%v resume=%v", paused, resume)
	}

	t0800 := time.Date(2026, 11, 20, 8, 0, 0, 0, loc)
	if p, _, _ := shouldPauseForDailyTimer("08:00 - 22:00", t0800); p {
		t.Fatalf("08:00 should be active")
	}

	t2159 := time.Date(2026, 11, 20, 21, 59, 0, 0, loc)
	if p, _, _ := shouldPauseForDailyTimer("08:00 - 22:00", t2159); p {
		t.Fatalf("21:59 should be active")
	}

	t2200 := time.Date(2026, 11, 20, 22, 0, 0, 0, loc)
	paused, resume, _ = shouldPauseForDailyTimer("08:00 - 22:00", t2200)
	if !paused || !resume.Equal(time.Date(2026, 11, 21, 8, 0, 0, 0, loc)) {
		t.Fatalf("22:00 expected paused until tomorrow 08:00, got paused=%v resume=%v", paused, resume)
	}

	t2130 := time.Date(2026, 11, 20, 21, 30, 0, 0, loc)
	paused, resume, _ = shouldPauseForDailyTimer("22:00 - 06:00", t2130)
	if !paused || !resume.Equal(time.Date(2026, 11, 20, 22, 0, 0, 0, loc)) {
		t.Fatalf("overnight 21:30 expected paused until 22:00, got paused=%v resume=%v", paused, resume)
	}

	t0230 := time.Date(2026, 11, 21, 2, 30, 0, 0, loc)
	if p, _, _ := shouldPauseForDailyTimer("22:00 - 06:00", t0230); p {
		t.Fatalf("overnight 02:30 should be active")
	}

	t0600 := time.Date(2026, 11, 21, 6, 0, 0, 0, loc)
	paused, resume, _ = shouldPauseForDailyTimer("22:00 - 06:00", t0600)
	if !paused || !resume.Equal(time.Date(2026, 11, 21, 22, 0, 0, 0, loc)) {
		t.Fatalf("overnight 06:00 expected paused until 22:00, got paused=%v resume=%v", paused, resume)
	}
}

func TestUbotSQLiteJedaTimerRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ubot_jeda.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	const ownerID int64 = 777111
	_, err = sq.DB.ExecContext(ctx, `
		INSERT INTO jaseb_configurasi (
			owner_id, delay, delay_mode, delay_all_groups, skip_floodwait, skip_floodwait_sec, timer, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ownerID, 30, "manual", 20, 1, 120, "08:00 - 22:00", "Berhenti",
	)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	svc := &Service{sqlite: sq}
	cfg := svc.getJasebConfig(ctx, ownerID)
	if cfg.Delay != 30 || cfg.DelayMode != "manual" || cfg.DelayAllGroups != 20 || !cfg.SkipFloodWait || cfg.SkipFloodWaitSec != 120 || cfg.Timer != "08:00 - 22:00" {
		t.Fatalf("getJasebConfig mismatch: %+v", cfg)
	}
}
