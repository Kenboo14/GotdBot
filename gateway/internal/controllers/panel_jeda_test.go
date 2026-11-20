package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/databases"
	"context"
	"path/filepath"
	"testing"
)

func TestJedaFormattingAndSummary(t *testing.T) {
	cfgAman := &databases.JasebConfig{DelayMode: "aman"}
	if got := cfgAman.FormatJedaSummary(20); got != "Jeda Aman (200–400 detik · antar-grup)" {
		t.Fatalf("aman summary mismatch: got %q", got)
	}

	cfgAuto15 := &databases.JasebConfig{DelayMode: "otomatis"}
	if got := cfgAuto15.FormatJedaSummary(15); got != "Jeda Otomatis (10 detik · antar-grup, 20 menit · antar-putaran)" {
		t.Fatalf("otomatis 15 groups mismatch: got %q", got)
	}

	cfgAuto80 := &databases.JasebConfig{DelayMode: "otomatis"}
	if got := cfgAuto80.FormatJedaSummary(80); got != "Jeda Otomatis (35 detik · antar-grup)" {
		t.Fatalf("otomatis 80 groups mismatch: got %q", got)
	}

	cfgPerGrup := &databases.JasebConfig{DelayMode: "manual", Delay: 30}
	if got := cfgPerGrup.FormatJedaSummary(10); got != "30 detik · antar-grup" {
		t.Fatalf("per-grup mismatch: got %q", got)
	}

	cfgPerSemua := &databases.JasebConfig{DelayMode: "manual", DelayAllGroups: 20}
	if got := cfgPerSemua.FormatJedaSummary(10); got != "20 menit · antar-putaran" {
		t.Fatalf("per-semua mismatch: got %q", got)
	}

	cfgSimultaneous := &databases.JasebConfig{DelayMode: "manual", Delay: 5, DelayAllGroups: 10}
	if got := cfgSimultaneous.FormatJedaSummary(10); got != "5 detik · antar-grup, 10 menit · antar-putaran" {
		t.Fatalf("simultaneous mismatch: got %q", got)
	}

	cfg4Jam := &databases.JasebConfig{DelayMode: "manual", DelayAllGroups: 240}
	if got := cfg4Jam.FormatJedaSummary(10); got != "4 jam · antar-putaran" {
		t.Fatalf("240m mismatch: got %q", got)
	}

	cfg6Jam := &databases.JasebConfig{DelayMode: "manual", DelayAllGroups: 360}
	if got := cfg6Jam.FormatJedaSummary(10); got != "6 jam · antar-putaran" {
		t.Fatalf("360m mismatch: got %q", got)
	}

	cfgBothOff := &databases.JasebConfig{DelayMode: "manual", Delay: 0, DelayAllGroups: 0}
	if got := cfgBothOff.FormatJedaSummary(10); got != "Tanpa jeda" {
		t.Fatalf("both off mismatch: got %q", got)
	}
}

func TestSkipFloodWaitAndTimerFormatting(t *testing.T) {
	if got := (&databases.JasebConfig{}).FormatSkipFloodWait(); got != "Mati" {
		t.Fatalf("expected Mati, got %q", got)
	}
	if got := (&databases.JasebConfig{SkipFloodWait: true, SkipFloodWaitSec: 30}).FormatSkipFloodWait(); got != "Aktif (> 30 detik)" {
		t.Fatalf("expected > 30 detik, got %q", got)
	}
	if got := (&databases.JasebConfig{SkipFloodWait: true, SkipFloodWaitSec: 120}).FormatSkipFloodWait(); got != "Aktif (> 2 menit)" {
		t.Fatalf("expected > 2 menit, got %q", got)
	}
	if got := (&databases.JasebConfig{SkipFloodWait: true, SkipFloodWaitSec: 0}).FormatSkipFloodWait(); got != "Aktif (Semua)" {
		t.Fatalf("expected legacy Aktif (Semua), got %q", got)
	}

	if got := (&databases.JasebConfig{Timer: "Belum diatur"}).FormatTimerDisplay(); got != "Belum diatur" {
		t.Fatalf("expected Belum diatur, got %q", got)
	}
	if got := (&databases.JasebConfig{Timer: "08:00 - 22:00"}).FormatTimerDisplay(); got != "08:00 - 22:00 WIB" {
		t.Fatalf("expected WIB suffix, got %q", got)
	}
}

func TestJedaAndTimerInputParsers(t *testing.T) {
	if sec, dis, ok := parseJedaPerGrupInput("5"); !ok || dis || sec != 5 {
		t.Fatalf("expected (5,false,true), got (%d,%v,%v)", sec, dis, ok)
	}
	if sec, dis, ok := parseJedaPerGrupInput("300 detik"); !ok || dis || sec != 300 {
		t.Fatalf("expected (300,false,true), got (%d,%v,%v)", sec, dis, ok)
	}
	if _, dis, ok := parseJedaPerGrupInput(assets.BtnMatikan); !ok || !dis {
		t.Fatalf("expected disable true on BtnMatikan")
	}
	if _, _, ok := parseJedaPerGrupInput("4"); ok {
		t.Fatalf("expected 4 to be rejected")
	}
	if _, _, ok := parseJedaPerGrupInput("301"); ok {
		t.Fatalf("expected 301 to be rejected")
	}

	if min, dis, ok := parseJedaPerSemuaGrupInput("20"); !ok || dis || min != 20 {
		t.Fatalf("expected 20m, got (%d,%v,%v)", min, dis, ok)
	}
	if min, dis, ok := parseJedaPerSemuaGrupInput("240"); !ok || dis || min != 240 {
		t.Fatalf("expected 240m, got (%d,%v,%v)", min, dis, ok)
	}
	if min, dis, ok := parseJedaPerSemuaGrupInput("6 jam"); !ok || dis || min != 360 {
		t.Fatalf("expected 360m, got (%d,%v,%v)", min, dis, ok)
	}
	if _, _, ok := parseJedaPerSemuaGrupInput("180"); ok {
		t.Fatalf("expected 180 to be rejected")
	}

	for _, tc := range []struct {
		in      string
		wantSec int
		wantDis bool
	}{
		{assets.BtnSkipFW30s, 30, false},
		{assets.BtnSkipFW1m, 60, false},
		{assets.BtnSkipFW2m, 120, false},
		{assets.BtnSkipFW10m, 600, false},
		{"10", 10, false},
		{"3600", 3600, false},
		{assets.BtnMatikan, 0, true},
	} {
		sec, dis, ok := parseSkipFloodWaitInput(tc.in)
		if !ok || sec != tc.wantSec || dis != tc.wantDis {
			t.Fatalf("parseSkipFloodWaitInput(%q) = (%d,%v,%v), want (%d,%v,true)", tc.in, sec, dis, ok, tc.wantSec, tc.wantDis)
		}
	}
	if _, _, ok := parseSkipFloodWaitInput("9"); ok {
		t.Fatalf("expected 9 to be rejected")
	}
	if _, _, ok := parseSkipFloodWaitInput("3601"); ok {
		t.Fatalf("expected 3601 to be rejected")
	}

	if _, _, canon, ok := parseClockHHMM("8:00"); !ok || canon != "08:00" {
		t.Fatalf("expected 08:00, got %q (ok=%v)", canon, ok)
	}
	if _, _, _, ok := parseClockHHMM("25:00"); ok {
		t.Fatalf("expected 25:00 to fail")
	}
	if pair, ok := parseDailyTimerPair("⏰ 08:00 - 22:00"); !ok || pair != "08:00 - 22:00" {
		t.Fatalf("expected preset pair, got %q (ok=%v)", pair, ok)
	}
	if pair, ok := parseDailyTimerPair("22.00 06.00"); !ok || pair != "22:00 - 06:00" {
		t.Fatalf("expected space pair, got %q (ok=%v)", pair, ok)
	}
	if _, ok := parseDailyTimerPair("08:00 - 08:00"); ok {
		t.Fatalf("expected identical start/end to fail")
	}
}

func TestJedaKeyboardsAndSQLiteFlow(t *testing.T) {
	pgRows := assets.JedaPerGrupMarkup.Keyboard
	if len(pgRows) != 14 {
		t.Fatalf("expected 14 rows in JedaPerGrupMarkup (12 grid + Matikan + Batal), got %d", len(pgRows))
	}
	if len(pgRows[11]) != 5 || pgRows[11][4].Text != "300" {
		t.Fatalf("expected last grid row to end with 300, got %+v", pgRows[11])
	}
	if pgRows[12][0].Text != assets.BtnMatikan || pgRows[13][0].Text != assets.BtnBatal {
		t.Fatalf("unexpected footer buttons in JedaPerGrupMarkup")
	}

	psRows := assets.JedaPerSemuaGrupMarkup.Keyboard
	if len(psRows) != 15 {
		t.Fatalf("expected 15 rows in JedaPerSemuaGrupMarkup (12 grid + 240/360 + Matikan + Batal), got %d", len(psRows))
	}
	if psRows[11][4].Text != "120" || psRows[12][0].Text != "240" || psRows[12][1].Text != "360" {
		t.Fatalf("unexpected end buttons in JedaPerSemuaGrupMarkup")
	}

	server, b, _ := setupMockTelegramServer(t, nil)
	defer server.Close()
	dbPath := filepath.Join(t.TempDir(), "jeda_flow.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dm.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	h := NewHandler(nil, dm)
	ctx := context.Background()
	const chatID int64 = 5001

	h.PromptAturJeda(ctx, b, chatID, chatID)
	if !h.HandlePanelInput(ctx, b, chatID, chatID, assets.BtnJedaPerGrup, nil) {
		t.Fatalf("expected BtnJedaPerGrup handled")
	}
	if !h.HandlePanelInput(ctx, b, chatID, chatID, "5", nil) {
		t.Fatalf("expected 5s input handled")
	}
	h.PromptJedaPerSemuaGrup(ctx, b, chatID, chatID)
	if !h.HandlePanelInput(ctx, b, chatID, chatID, "10", nil) {
		t.Fatalf("expected 10m input handled")
	}

	cfg := dm.GetUserConfig(ctx, chatID)
	if cfg.DelayMode != "manual" || cfg.Delay != 5 || cfg.DelayAllGroups != 10 {
		t.Fatalf("expected simultaneous Delay=5 & DelayAllGroups=10, got %+v", cfg)
	}

	h.PromptJedaPerGrup(ctx, b, chatID, chatID)
	if !h.HandlePanelInput(ctx, b, chatID, chatID, assets.BtnMatikan, nil) {
		t.Fatalf("expected BtnMatikan handled")
	}
	cfg = dm.GetUserConfig(ctx, chatID)
	if cfg.Delay != 0 || cfg.DelayAllGroups != 10 {
		t.Fatalf("expected Delay=0 while preserving DelayAllGroups=10, got %+v", cfg)
	}

	h.PromptSkipFloodWait(ctx, b, chatID, chatID)
	if !h.HandlePanelInput(ctx, b, chatID, chatID, assets.BtnSkipFW2m, nil) {
		t.Fatalf("expected SkipFW2m handled")
	}
	cfg = dm.GetUserConfig(ctx, chatID)
	if !cfg.SkipFloodWait || cfg.SkipFloodWaitSec != 120 {
		t.Fatalf("expected SkipFloodWaitSec=120, got %+v", cfg)
	}

	h.PromptTimer(ctx, b, chatID, chatID)
	if !h.HandlePanelInput(ctx, b, chatID, chatID, "08:00", nil) {
		t.Fatalf("expected start hour handled")
	}
	if !h.HandlePanelInput(ctx, b, chatID, chatID, "22:00", nil) {
		t.Fatalf("expected end hour handled")
	}
	cfg = dm.GetUserConfig(ctx, chatID)
	if cfg.Timer != "08:00 - 22:00" {
		t.Fatalf("expected Timer '08:00 - 22:00', got %q", cfg.Timer)
	}
}
