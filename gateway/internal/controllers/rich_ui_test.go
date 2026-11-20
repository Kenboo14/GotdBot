package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func isEmojiToken(tok string) bool {
	if tok == "" {
		return false
	}
	if strings.ContainsRune(tok, '\u20E3') {
		return true
	}
	r, _ := utf8.DecodeRuneInString(tok)
	return r >= 0x203C
}

func isEmoteWrappedButton(text string) bool {
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return false
	}
	return isEmojiToken(fields[0]) && isEmojiToken(fields[len(fields)-1])
}

func extractRPCMessages(rpcLog *mockRPCLog) []string {
	var out []string
	for _, req := range rpcLog.getRequests() {
		if strings.HasSuffix(req.Path, "/sendMessage") || strings.HasSuffix(req.Path, "/editMessageText") {
			if txt, ok := req.Body["text"].(string); ok && txt != "" {
				out = append(out, txt)
			}
		}
		if strings.HasSuffix(req.Path, "/sendRichMessage") {
			if rm, ok := req.Body["rich_message"].(map[string]any); ok {
				if htmlStr, ok := rm["html"].(string); ok && htmlStr != "" {
					out = append(out, htmlStr)
				}
			}
		}
	}
	return out
}

func makeTestCBUpdate(uid int64, msgID int, data string) *botgotd.Update {
	return &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_" + data,
			Data: data,
			From: botgotd.User{ID: uid},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{
					ID:   msgID,
					Chat: botgotd.Chat{ID: uid},
				},
			},
		},
	}
}

func TestReplyKeyboardButtonsEmoteWrapped(t *testing.T) {
	markups := []struct {
		name string
		kb   *botgotd.ReplyKeyboardMarkup
	}{
		{"MainMenuMarkup", assets.MainMenuMarkup},
		{"StatusMenuMarkup", assets.StatusMenuMarkup},
		{"SettingsMenuMarkup", assets.SettingsMenuMarkup},
		{"GrupListMenuMarkup", assets.GrupListMenuMarkup},
		{"TambahGrupOptionsMarkup", assets.TambahGrupOptionsMarkup},
		{"CancelOnlyMarkup", assets.CancelOnlyMarkup},
		{"AturJedaMarkup", assets.AturJedaMarkup},
		{"JedaPerGrupMarkup", assets.JedaPerGrupMarkup},
		{"JedaPerSemuaGrupMarkup", assets.JedaPerSemuaGrupMarkup},
		{"SkipFloodWaitMarkup", assets.SkipFloodWaitMarkup},
		{"UbahModeMarkup", assets.UbahModeMarkup},
		{"TimerMarkup", assets.TimerMarkup},
		{"ResetMarkup", assets.ResetMarkup},
		{"HapusListMarkup", assets.HapusListMarkup},
		{"HapusGrupMarkup", assets.HapusGrupMarkup},
		{"CreateChoiceMarkup", assets.CreateChoiceMarkup},
		{"InstallPhoneMarkup", assets.InstallPhoneMarkup},
		{"WatermarkPromptMarkup", assets.WatermarkPromptMarkup},
		{"browseListMarkup", browseListMarkup(5)},
		{"browseGroupMarkup", browseGroupMarkup(5)},
		{"topicGroupReplyMarkup", topicGroupReplyMarkup(5)},
		{"topicSelectReplyMarkup", topicSelectReplyMarkup(5, true)},
	}
	for _, item := range markups {
		if item.kb == nil {
			t.Fatalf("%s is nil", item.name)
		}
		for rowIdx, row := range item.kb.Keyboard {
			for colIdx, btn := range row {
				trimmed := strings.TrimSpace(btn.Text)
				if trimmed == "" {
					t.Fatalf("%s[%d][%d] has empty text", item.name, rowIdx, colIdx)
				}
				if _, err := strconv.Atoi(trimmed); err == nil && trimmed[0] >= '0' && trimmed[0] <= '9' {
					continue
				}
				if !isEmoteWrappedButton(trimmed) {
					t.Errorf("%s[%d][%d] = %q does not follow emote namatombol emote", item.name, rowIdx, colIdx, btn.Text)
				}
			}
		}
	}
}

func TestEmoteWrappedInputParsers(t *testing.T) {
	for _, s := range []string{assets.BtnMatikan, "Matikan", "matikan"} {
		if !isDisableCommand(s) {
			t.Errorf("isDisableCommand(%q) = false, want true", s)
		}
	}
	skipCases := []struct {
		raw      string
		wantSec  int
		wantOff  bool
		wantOkay bool
	}{
		{assets.BtnSkipFW30s, 30, false, true},
		{assets.BtnSkipFW1m, 60, false, true},
		{assets.BtnSkipFW2m, 120, false, true},
		{assets.BtnSkipFW3m, 180, false, true},
		{assets.BtnSkipFW5m, 300, false, true},
		{assets.BtnSkipFW10m, 600, false, true},
		{assets.BtnMatikan, 0, true, true},
		{"45", 45, false, true},
	}
	for _, tc := range skipCases {
		gotSec, gotOff, gotOK := parseSkipFloodWaitInput(tc.raw)
		if gotSec != tc.wantSec || gotOff != tc.wantOff || gotOK != tc.wantOkay {
			t.Errorf("parseSkipFloodWaitInput(%q) = (%d,%v,%v), want (%d,%v,%v)", tc.raw, gotSec, gotOff, gotOK, tc.wantSec, tc.wantOff, tc.wantOkay)
		}
	}
	for _, preset := range []string{
		assets.BtnTimerPreset1, assets.BtnTimerPreset2, assets.BtnTimerPreset3, assets.BtnTimerPreset4, "09:00 - 21:00",
	} {
		if normalized, ok := parseDailyTimerPair(preset); !ok || normalized == "" {
			t.Errorf("parseDailyTimerPair(%q) failed: (%q, %v)", preset, normalized, ok)
		}
	}

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()
	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 7101
	for _, rawBtn := range []string{assets.BtnModeNormal, assets.BtnModeFast, assets.BtnModeSilent, assets.BtnModeForward} {
		h.setPanelState(chatID, "waiting_set_mode")
		if !h.HandlePanelInput(ctx, b, chatID, chatID, rawBtn, nil) {
			t.Fatalf("HandlePanelInput(waiting_set_mode, %q) returned false", rawBtn)
		}
	}
	for _, txt := range extractRPCMessages(rpcLog) {
		if !botgotd.IsRichContent(txt) {
			t.Errorf("waiting_set_mode reply is not rich content: %q", txt)
		}
	}
}

func TestMonitorAndJasebRichContent(t *testing.T) {
	dashHTML := (&telemetryTracker{bootTime: time.Now()}).formatDashboardHTML()
	if !botgotd.IsRichContent(dashHTML) {
		t.Errorf("formatDashboardHTML is not rich content")
	}
	svcPayload := models.PFromService{
		OwnerID: 7201, ChatID: 7201, TaskID: "task_rich_1",
		Total: 5, Success: 4, Failed: 1, Errors: []string{"FloodWait 10s"},
	}
	if !botgotd.IsRichContent(broadcastMonitorText(svcPayload)) {
		t.Errorf("broadcastMonitorText is not rich content")
	}
	if !botgotd.IsRichContent(broadcastDetailText(svcPayload)) {
		t.Errorf("broadcastDetailText is not rich content")
	}

	var mu sync.Mutex
	var monSent []string
	fastCfg := defaultMonitorDispatcherConfig()
	fastCfg.burst = 50
	fastCfg.refillEvery = time.Millisecond
	fastCfg.minGap = 0
	disp := newMonitorDispatcher(fastCfg, nil, int64(999), 1)
	disp.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		monSent = append(monSent, p.Text)
		mu.Unlock()
		return &botgotd.Message{ID: 1}, nil
	}
	dm := &devMonitor{chatTarget: int64(999), fallbackTop: 1, dispatcher: disp, dedup: make(map[string]time.Time)}
	lic := &databases.UserLicense{
		TokenSHA: "GOTD_SHA:deadbeef", MasterToken: "GOTD:MASTER123",
		OwnerChatID: 7201, UserbotID: 8201, Tier: "Pro",
		ExpiresAt: time.Now().Add(24 * time.Hour), Status: "active",
	}
	dm.NotifyBotLoader("gateway", "boot ok")
	dm.NotifyDatabase("sqlite", "wal ok")
	dm.NotifyInvoice("TRX-01 paid")
	dm.NotifyUserStart(7201, "tester", "Tester Satu", true)
	dm.NotifyTokenCreated(1, "Pro", "GOTD:ABC", "30 hari", false)
	dm.NotifyTokenCollected(7201, 8201, "Pro", "GOTD:ABC", "GOTD_SHA:deadbeef", false)
	dm.NotifyUserbotConnected(7201, 8201, "login_qr")
	dm.NotifySessionTrack(7201, 7201, "CONNECTED", "healthy")
	dm.NotifyTraceback("panel", "panic sample", "stacktrace")
	dm.NotifyBackupToken(lic)
	dm.NotifyTokenExpired(lic)
	disp.EnqueueBatchEntry("MONITOR_USER_START", "👤 Ringkasan Start", monitorPriorityNormal, "User A started", "single")
	disp.EnqueueBatchEntry("MONITOR_USER_START", "👤 Ringkasan Start", monitorPriorityNormal, "User B started", "single")

	ctx := context.Background()
	disp.flushBatches(dm.resolveTopic)
	for draining := true; draining; {
		select {
		case j := <-disp.highQueue:
			disp.deliverJob(ctx, j)
		case j := <-disp.normalQueue:
			disp.deliverJob(ctx, j)
		default:
			draining = false
		}
	}
	if len(monSent) == 0 {
		t.Fatalf("expected devMonitor to emit at least 1 message")
	}
	for i, msg := range monSent {
		if !botgotd.IsRichContent(msg) {
			t.Errorf("monitor message %d is not rich content: %q", i, msg)
		}
	}

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()
	h := newTestHandlerWithSQLite(t)
	const uid int64 = 7202
	lic.OwnerChatID = uid
	lic.TokenSHA = "GOTD_SHA:cafebabe"
	if err := h.db.SaveUserLicense(ctx, lic); err != nil {
		t.Fatalf("SaveUserLicense: %v", err)
	}
	cfg := h.db.GetUserConfig(ctx, uid)
	cfg.Groups = []databases.GroupEntry{{ChatID: -1001, Title: "Grup 1"}}
	cfg.Lists = []int{101}
	if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	h.PromptAturJeda(ctx, b, uid, uid)
	h.PromptTimer(ctx, b, uid, uid)
	h.ShowStatus(ctx, b, uid, uid)
	h.ShowSettings(ctx, b, uid, uid)
	h.ShowSwitchTokenMenu(ctx, b, uid, uid)
	h.ToggleStatus(ctx, b, uid, uid)
	h.ToggleNotifikasi(ctx, b, uid, uid)

	msgs := extractRPCMessages(rpcLog)
	if len(msgs) < 7 {
		t.Fatalf("expected >= 7 panel messages, got %d", len(msgs))
	}
	for i, msg := range msgs {
		if !botgotd.IsRichContent(msg) {
			t.Errorf("panel message %d is not rich content: %q", i, msg)
		}
	}
}

func TestAlignedCallbacksEditInPlaceAndMaxLen(t *testing.T) {
	callbackDataList := []string{
		"panel:status:refresh",
		"panel:status:toggle",
		"panel:token:menu",
		"panel:settings:home",
		"panel:settings:backup",
		"panel:list:back",
		"panel:group:back",
		"token_set_duration:menu:" + strings.Repeat("a", 32),
		"token_set_duration:back:" + strings.Repeat("a", 32),
		"token_set_duration:apply:" + strings.Repeat("a", 32) + ":90d",
	}
	for _, cb := range callbackDataList {
		if len(cb) > 64 {
			t.Errorf("callback data %q exceeds 64 bytes (%d)", cb, len(cb))
		}
	}

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()
	brk, getPayloads := startMockBroker(t)
	ctx := context.Background()
	dmDB, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "test_cb.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dmDB.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	h := NewHandler(brk, dmDB)
	const uid int64 = 7301
	t.Setenv("OWNER", "7301")

	lic := &databases.UserLicense{
		TokenSHA: "GOTD_SHA:7301aa", MasterToken: "GOTD:MASTER7301",
		OwnerChatID: uid, UserbotID: 8301, Tier: "Pro++",
		ExpiresAt: time.Now().Add(48 * time.Hour), Status: "active",
	}
	if err := h.db.SaveUserLicense(ctx, lic); err != nil {
		t.Fatalf("SaveUserLicense: %v", err)
	}
	cfg := h.db.GetUserConfig(ctx, uid)
	cfg.Lists = []int{201}
	if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	if err := h.db.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token: "GOTD:FREEBACKTEST", Tier: "Free",
		DurationSeconds: 7 * 86400, IsShared: true, Status: "active",
	}); err != nil {
		t.Fatalf("SaveTokenMaster: %v", err)
	}

	for _, cbData := range []string{
		"panel:status:refresh", "panel:status:toggle",
		"panel:token:menu", "panel:settings:home", "panel:settings:backup",
	} {
		h.PanelCallback(ctx, b, makeTestCBUpdate(uid, 501, cbData))
	}
	if len(getPayloads()) == 0 {
		t.Fatalf("expected panel:status:toggle to dispatch broker payload")
	}

	for _, cbData := range []string{
		"token_set_duration:menu:FREEBACKTEST",
		"token_set_duration:apply:FREEBACKTEST:30d",
		"token_set_duration:back:FREEBACKTEST",
	} {
		h.TokenCallback(ctx, b, makeTestCBUpdate(uid, 502, cbData))
	}
	tmUpdated, err := h.db.GetTokenMaster(ctx, "GOTD:FREEBACKTEST")
	if err != nil || tmUpdated == nil || tmUpdated.DurationSeconds != 30*86400 {
		t.Fatalf("expected Free token duration updated to 30d, got %+v (err=%v)", tmUpdated, err)
	}
	if got := rpcLog.countByMethod("editMessageText"); got < 8 {
		t.Fatalf("expected >= 8 editMessageText calls for aligned callbacks, got %d", got)
	}
	if got := rpcLog.countByMethod("answerCallbackQuery"); got < 8 {
		t.Fatalf("expected answerCallbackQuery >= 8, got %d", got)
	}
	for _, msg := range extractRPCMessages(rpcLog) {
		if !botgotd.IsRichContent(msg) {
			t.Errorf("edited callback message is not rich content: %q", msg)
		}
	}
}
