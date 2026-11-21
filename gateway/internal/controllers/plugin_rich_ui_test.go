package controllers

import (
	"assterisk/gateway/internal/databases"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestDynamicHelpNoStatusEmojisAndDisabledTable(t *testing.T) {
	ctx := context.Background()
	h := NewHandler(nil, nil)

	home := h.buildLiveHelpHomePage(ctx, 5001)
	for _, row := range home.markup.InlineKeyboard {
		for _, btn := range row {
			for _, bad := range []string{"🟢", "🟡", "🔴"} {
				if strings.Contains(btn.Text, bad) {
					t.Fatalf("unexpected status emoji %q in !help home button %q", bad, btn.Text)
				}
			}
		}
	}
	if !strings.Contains(home.text, "<tr><td>🚫 Dimatikan</td><td><code>PM Permit</code></td></tr>") {
		t.Fatalf("expected disabled summary in !help home table, got %q", home.text)
	}

	adminBefore := h.buildLiveHelpCategoryPage(ctx, 5001, helpCatalog[0])
	if !strings.Contains(adminBefore.text, "<tr><td>🚫 Dimatikan</td><td><code>Tidak ada</code></td></tr>") {
		t.Fatalf("expected Tidak ada before disabling admin sub-command, got %q", adminBefore.text)
	}

	h.togglePluginSubCommand(ctx, 0, "admin", "ban")

	homeAfter := h.buildLiveHelpHomePage(ctx, 5001)
	if !strings.Contains(homeAfter.text, "!ban") || !strings.Contains(homeAfter.text, "<tr><td>🚫 Dimatikan</td>") {
		t.Fatalf("expected !ban in !help home disabled table row, got %q", homeAfter.text)
	}

	adminAfter := h.buildLiveHelpCategoryPage(ctx, 5001, helpCatalog[0])
	for _, row := range adminAfter.markup.InlineKeyboard {
		for _, btn := range row {
			for _, bad := range []string{"🟢", "🟡", "🔴"} {
				if strings.Contains(btn.Text, bad) {
					t.Fatalf("unexpected status emoji %q in !help admin button %q", bad, btn.Text)
				}
			}
		}
	}
	if !strings.Contains(adminAfter.text, "<tr><td>🚫 Dimatikan</td><td><code>!ban</code></td></tr>") {
		t.Fatalf("expected !ban in !help admin disabled table row, got %q", adminAfter.text)
	}

	banSnap, ok := h.resolveHelpPage(ctx, 5001, "assist_help:cmd:admin:ban")
	if !ok || !strings.Contains(banSnap.text, "<tr><td>🚫 Dimatikan</td><td><code>!ban</code></td></tr>") {
		t.Fatalf("expected !ban in !help command detail disabled row, got %q", banSnap.text)
	}

	h.togglePluginModule(ctx, 0, "admin")
	adminAllOff := h.buildLiveHelpCategoryPage(ctx, 5001, helpCatalog[0])
	if !strings.Contains(adminAllOff.text, "Semua (") {
		t.Fatalf("expected Semua (...) when entire module disabled, got %q", adminAllOff.text)
	}
}

func TestOwnerPluginRichInlineButtonsRenderedInMessage(t *testing.T) {
	ctx := context.Background()
	h := NewHandler(nil, nil)
	h.togglePluginSubCommand(ctx, 0, "admin", "ban")

	text, markup := h.renderOwnerPluginDetail(ctx, 0, "admin")
	if !strings.Contains(text, `<tg-button type="callback_data" data="owner_plug:cmd:0:admin:ban" style="danger">🔴 Ban</tg-button>`) {
		t.Fatalf("expected danger rich inline button for disabled !ban, got %q", text)
	}
	if !strings.Contains(text, `<tg-button type="callback_data" data="owner_plug:cmd:0:admin:kick" style="primary">🟢 Kick</tg-button>`) {
		t.Fatalf("expected primary rich inline button for enabled !kick, got %q", text)
	}
	if !strings.Contains(text, `<tg-button type="callback_data" data="owner_help:page:5">⬅️ Kembali</tg-button>`) {
		t.Fatalf("expected rich inline Back button in global plugin detail, got %q", text)
	}
	if markup == nil || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("expected owner help navigation markup on global detail view")
	}
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if strings.HasPrefix(btn.CallbackData, "owner_plug:") {
				t.Fatalf("sub-command button %+v must be rich inline, not InlineKeyboardMarkup", btn)
			}
		}
	}

	userListText, userListMarkup := h.renderOwnerPluginListView(ctx, 7777, "7777")
	if userListMarkup != nil {
		t.Fatalf("expected nil bottom markup for /plugin <uid> rich inline view, got %#v", userListMarkup)
	}
	if !strings.Contains(userListText, `<tg-button type="callback_data" data="owner_plug:reset:7777">🔄 Reset ke Default Global</tg-button>`) {
		t.Fatalf("expected rich inline reset button for /plugin 7777, got %q", userListText)
	}

	userDetailText, userDetailMarkup := h.renderOwnerPluginDetail(ctx, 7777, "admin")
	if userDetailMarkup != nil {
		t.Fatalf("expected nil bottom markup for /plugin <uid> detail rich inline view, got %#v", userDetailMarkup)
	}
	if !strings.Contains(userDetailText, `<tg-button type="callback_data" data="owner_plug:list:7777">⬅️ Kembali</tg-button>`) {
		t.Fatalf("expected rich inline back button to user plugin list, got %q", userDetailText)
	}
}

func TestRefreshCallbacksEditInPlaceOnlyNeverSendNewMessage(t *testing.T) {
	ctx := context.Background()
	dmDB, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "test_refresh.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dmDB.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	h := NewHandler(nil, dmDB)
	t.Setenv("OWNER", "9001")

	server, b, rpcLog := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	defer server.Close()

	h.PanelCallback(ctx, b, makeTestCBUpdate(9001, 601, "panel:status:refresh"))
	h.PanelCallback(ctx, b, makeTestCBUpdate(9001, 601, "panel:token:menu"))
	h.OwnerHelpCallback(ctx, b, makeTestCBUpdate(9001, 602, "owner_help:page:5"))
	h.OwnerPluginCallback(ctx, b, makeTestCBUpdate(9001, 602, "owner_plug:open:0:admin"))

	if got := rpcLog.countByMethod("editMessageText"); got != 4 {
		t.Fatalf("expected 4 editMessageText calls, got %d", got)
	}
	if got := rpcLog.countByMethod("sendMessage"); got != 0 {
		t.Fatalf("expected 0 sendMessage calls on refresh/callback, got %d", got)
	}
	if got := rpcLog.countByMethod("sendRichMessage"); got != 0 {
		t.Fatalf("expected 0 sendRichMessage calls on refresh/callback, got %d", got)
	}
}

func TestGlobalOffOverridesPerUserAndSubSync(t *testing.T) {
	ctx := context.Background()
	h := NewHandler(nil, nil)

	h.setAllPluginStatesForTarget(ctx, 7001, true)
	h.togglePluginModule(ctx, 0, "vctools")
	if h.isEffectiveModuleEnabled(ctx, 7001, "vctools") {
		t.Fatalf("expected global vctools OFF to override user 7001 ON")
	}
	if h.isEffectiveCommandEnabled(ctx, 7001, "vctools", "joinvc") {
		t.Fatalf("expected global vctools OFF to disable joinvc for user 7001")
	}

	h.togglePluginModule(ctx, 0, "logger")
	home := h.buildLiveHelpHomePage(ctx, 7001)
	if !strings.Contains(home.text, "Logger") {
		t.Fatalf("expected disabled Logger in !help home summary, got %q", home.text)
	}
	extraPage := h.buildLiveHelpCategoryPage(ctx, 7001, helpCatalog[4])
	if !strings.Contains(extraPage.text, "!logger") {
		t.Fatalf("expected !logger in !help extra disabled table row, got %q", extraPage.text)
	}

	h.togglePluginModule(ctx, 0, "logger")
	h.togglePluginSubCommand(ctx, 0, "logger", "log")
	homeSub := h.buildLiveHelpHomePage(ctx, 7001)
	if !strings.Contains(homeSub.text, "!log") {
		t.Fatalf("expected !log in !help home disabled summary, got %q", homeSub.text)
	}

	for _, sub := range pluginSubCommandsForModule("sticker") {
		h.togglePluginSubCommand(ctx, 0, "sticker", sub.Key)
	}
	if h.isEffectiveModuleEnabled(ctx, 0, "sticker") {
		t.Fatalf("expected sticker module OFF when all sub-commands are turned off")
	}
	h.togglePluginModule(ctx, 0, "sticker")
	if !h.isEffectiveCommandEnabled(ctx, 0, "sticker", "kang") {
		t.Fatalf("expected sticker sub-commands re-enabled when module toggled back ON")
	}
	h.togglePluginModule(ctx, 0, "sticker")
	h.togglePluginSubCommand(ctx, 0, "sticker", "kang")
	if !h.isEffectiveCommandEnabled(ctx, 0, "sticker", "kang") || h.isEffectiveCommandEnabled(ctx, 0, "sticker", "strinfo") {
		t.Fatalf("expected only kang ON and strinfo OFF when enabling single sub on OFF module")
	}
}

