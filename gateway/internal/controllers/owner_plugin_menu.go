package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

type ownerRichButton struct {
	Text  string
	Data  string
	Style string
}

func renderOwnerRichButtonRows(rows [][]ownerRichButton) string {
	var sb strings.Builder
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		sb.WriteString("<tg-button-row>\n")
		for _, btn := range row {
			styleAttr := ""
			if btn.Style != "" {
				styleAttr = ` style="` + html.EscapeString(btn.Style) + `"`
			}
			fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="%s"%s>%s</tg-button>`+"\n",
				html.EscapeString(btn.Data), styleAttr, html.EscapeString(btn.Text))
		}
		sb.WriteString("</tg-button-row>\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (h *Handler) appendOwnerPluginGrid(ctx context.Context, ownerID int64, rows []monitorRichRow) ([]monitorRichRow, [][]ownerRichButton) {
	states := h.getEffectivePluginStates(ctx, ownerID)
	btnRows := make([][]ownerRichButton, 0, 8)
	curRow := make([]ownerRichButton, 0, 3)
	for _, sp := range ownerPluginSpecs {
		enabled := states[sp.Key]
		activeSub, totalSub := h.moduleSubActiveCount(states, sp.Key)
		icon := "🟢"
		style := "primary"
		statusText := fmt.Sprintf("🟢 ON (%d/%d)", activeSub, totalSub)
		if !enabled || activeSub == 0 {
			icon = "🔴"
			style = "danger"
			statusText = "🔴 OFF"
		} else if activeSub < totalSub {
			icon = "🟡"
			statusText = fmt.Sprintf("🟡 %d/%d ON", activeSub, totalSub)
		}
		rows = append(rows, monitorRichRow{
			Label: statusText + " · " + sp.Title,
			Value: "<code>" + html.EscapeString(sp.Commands) + "</code>",
		})
		curRow = append(curRow, ownerRichButton{
			Text:  icon + " " + sp.Title,
			Data:  fmt.Sprintf("owner_plug:open:%d:%s", ownerID, sp.Key),
			Style: style,
		})
		if len(curRow) == 3 {
			btnRows = append(btnRows, curRow)
			curRow = make([]ownerRichButton, 0, 3)
		}
	}
	if len(curRow) > 0 {
		btnRows = append(btnRows, curRow)
	}
	btnRows = append(btnRows, []ownerRichButton{
		{Text: "✅ Aktifkan Semua", Data: fmt.Sprintf("owner_plug:all:%d:1", ownerID), Style: "primary"},
		{Text: "⛔ Matikan Semua", Data: fmt.Sprintf("owner_plug:all:%d:0", ownerID), Style: "danger"},
	})
	if ownerID > 0 {
		btnRows = append(btnRows,
			[]ownerRichButton{{Text: "🔄 Reset ke Default Global", Data: fmt.Sprintf("owner_plug:reset:%d", ownerID)}},
			[]ownerRichButton{{Text: "🗑️ Tutup", Data: "owner_help:close", Style: "danger"}},
		)
	}
	return rows, btnRows
}

func (h *Handler) renderOwnerPluginListView(ctx context.Context, ownerID int64, targetLabel string) (string, *botgotd.InlineKeyboardMarkup) {
	title := "🔌 Kontrol Plugin · Global (Semua Userbot)"
	quote := "ℹ️ Klik modul untuk mengatur fitur utama &amp; sub-fitur secara global. Untuk per-user gunakan <code>/plugin &lt;user_id|@username&gt;</code>."
	if ownerID > 0 {
		title = fmt.Sprintf("🔌 Kontrol Plugin · User %s", html.EscapeString(targetLabel))
		quote = fmt.Sprintf("ℹ️ Mode khusus UserID <code>%d</code>: matikan modul/sub-fitur yang tidak dipakai agar eksekusi lebih ringan dan cepat.", ownerID)
	}
	rows, btnRows := h.appendOwnerPluginGrid(ctx, ownerID, make([]monitorRichRow, 0, len(ownerPluginSpecs)))
	text := renderMonitorRichCard(title, rows, quote, false) + "\n" + renderOwnerRichButtonRows(btnRows)
	return text, nil
}

func (h *Handler) renderOwnerPluginDetail(ctx context.Context, ownerID int64, modKey string) (string, *botgotd.InlineKeyboardMarkup) {
	sp, ok := findOwnerPluginSpec(modKey)
	if !ok {
		sp = ownerPluginSpecs[0]
	}
	states := h.getEffectivePluginStates(ctx, ownerID)
	modEnabled := states[sp.Key]
	subs := pluginSubCommandsForModule(sp.Key)
	activeSub, totalSub := h.moduleSubActiveCount(states, sp.Key)

	modBadge := "🟢 ON"
	modStyle := "primary"
	modBtnText := fmt.Sprintf("🟢 Modul %s: ON", sp.Title)
	if !modEnabled {
		modBadge = "🔴 OFF"
		modStyle = "danger"
		modBtnText = fmt.Sprintf("🔴 Modul %s: OFF", sp.Title)
	}

	rows := make([]monitorRichRow, 0, len(subs)+1)
	rows = append(rows, monitorRichRow{
		Label: "⚡ Modul Utama · " + sp.Title,
		Value: fmt.Sprintf("%s · Sub aktif: <code>%d/%d</code>", modBadge, activeSub, totalSub),
	})

	btnRows := make([][]ownerRichButton, 0, 8)
	btnRows = append(btnRows,
		[]ownerRichButton{{Text: modBtnText, Data: fmt.Sprintf("owner_plug:mod:%d:%s", ownerID, sp.Key), Style: modStyle}},
		[]ownerRichButton{
			{Text: "✅ Semua Sub ON", Data: fmt.Sprintf("owner_plug:suball:%d:%s:1", ownerID, sp.Key), Style: "primary"},
			{Text: "⛔ Semua Sub OFF", Data: fmt.Sprintf("owner_plug:suball:%d:%s:0", ownerID, sp.Key), Style: "danger"},
		},
	)

	curRow := make([]ownerRichButton, 0, 3)
	for _, sub := range subs {
		subOn := modEnabled
		if subOn {
			if val, exists := states["cmd:"+sub.Key]; exists {
				subOn = val
			}
		}
		icon := "🟢"
		subStyle := "primary"
		stText := "🟢 ON"
		if !subOn {
			icon = "🔴"
			subStyle = "danger"
			stText = "🔴 OFF"
		}
		rows = append(rows, monitorRichRow{
			Label: fmt.Sprintf("%s !%s", stText, html.EscapeString(sub.Key)),
			Value: html.EscapeString(sub.Description),
		})
		curRow = append(curRow, ownerRichButton{
			Text:  fmt.Sprintf("%s %s", icon, sub.Label),
			Data:  fmt.Sprintf("owner_plug:cmd:%d:%s:%s", ownerID, sp.Key, sub.Key),
			Style: subStyle,
		})
		if len(curRow) == 3 {
			btnRows = append(btnRows, curRow)
			curRow = make([]ownerRichButton, 0, 3)
		}
	}
	if len(curRow) > 0 {
		btnRows = append(btnRows, curRow)
	}

	scopeNote := "Global (Semua Userbot)"
	var markup *botgotd.InlineKeyboardMarkup
	if ownerID > 0 {
		scopeNote = fmt.Sprintf("Khusus UserID <code>%d</code>", ownerID)
		btnRows = append(btnRows, []ownerRichButton{
			{Text: "⬅️ Kembali", Data: fmt.Sprintf("owner_plug:list:%d", ownerID)},
			{Text: "🗑️ Tutup", Data: "owner_help:close", Style: "danger"},
		})
	} else {
		btnRows = append(btnRows, []ownerRichButton{
			{Text: "⬅️ Kembali", Data: "owner_help:page:5"},
		})
		markup = &botgotd.InlineKeyboardMarkup{InlineKeyboard: buildOwnerHelpNavRows(len(ownerHelpPages))}
	}

	title := fmt.Sprintf("🔌 Plugin · %s", sp.Title)
	if ownerID > 0 {
		title = fmt.Sprintf("🔌 Plugin · %s (%d)", sp.Title, ownerID)
	}
	quote := fmt.Sprintf("ℹ️ Scope: <b>%s</b> · Ketuk tombol di atas untuk On/Off modul utama atau masing-masing sub-command.", scopeNote)
	text := renderMonitorRichCard(title, rows, quote, false) + "\n" + renderOwnerRichButtonRows(btnRows)
	return text, markup
}

func (h *Handler) resolvePluginTargetOwner(ctx context.Context, b *botgotd.Bot, update *botgotd.Update, rawArg string) (int64, string, bool) {
	arg := strings.TrimSpace(rawArg)
	if arg == "" && update.Message != nil && update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.From != nil {
		u := update.Message.ReplyToMessage.From
		lbl := strconv.FormatInt(u.ID, 10)
		if u.Username != "" {
			lbl = fmt.Sprintf("@%s (%d)", u.Username, u.ID)
		} else if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
			lbl = fmt.Sprintf("%s (%d)", name, u.ID)
		}
		return u.ID, lbl, true
	}
	if arg == "" || strings.EqualFold(arg, "global") || strings.EqualFold(arg, "all") {
		return 0, "Global", true
	}
	if uid, err := strconv.ParseInt(strings.TrimPrefix(arg, "@"), 10, 64); err == nil && uid > 0 {
		label := strconv.FormatInt(uid, 10)
		if h != nil {
			if name := strings.TrimSpace(h.GetAccountName(uid)); name != "" {
				label = fmt.Sprintf("%s (%d)", name, uid)
			}
		}
		return uid, label, true
	}
	uname := strings.TrimPrefix(arg, "@")
	if b != nil && b.API != nil && uname != "" {
		if res, err := b.API.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: uname}); err == nil && res != nil {
			if pu, ok := res.Peer.(*tg.PeerUser); ok && pu.UserID > 0 {
				return pu.UserID, fmt.Sprintf("@%s (%d)", uname, pu.UserID), true
			}
		}
	}
	return 0, "", false
}

func (h *Handler) PluginCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	if !isOwner(update.Message.From.ID) {
		return
	}
	globalDevMonitor.ObserveIncomingChat(update.Message.Chat)
	fields := strings.Fields(update.Message.Text)
	rawArg := ""
	if len(fields) > 1 {
		rawArg = fields[1]
	}
	ownerID, targetLabel, ok := h.resolvePluginTargetOwner(ctx, b, update, rawArg)
	if !ok {
		errCard := renderMonitorRichCard("❌ Target User Tidak Ditemukan", []monitorRichRow{
			{Label: "Target", Value: "<code>" + html.EscapeString(rawArg) + "</code>"},
			{Label: "Format", Value: "<code>/plugin &lt;user_id|@username&gt;</code> atau reply pesan user"},
		}, "ℹ️ Gunakan <code>/plugin</code> tanpa argumen untuk pengaturan Global.", false)
		_, _ = sendMonitorRichMessage(ctx, b, update.Message.Chat.ID, update.Message.MessageThreadID, errCard)
		return
	}
	if ownerID == 0 {
		text, markup := h.renderOwnerHelp(ctx, len(ownerHelpPages))
		_, _ = sendMonitorRichMessageWithMarkup(ctx, b, update.Message.Chat.ID, update.Message.MessageThreadID, text, markup)
		return
	}
	text, markup := h.renderOwnerPluginListView(ctx, ownerID, targetLabel)
	_, _ = sendMonitorRichMessageWithMarkup(ctx, b, update.Message.Chat.ID, update.Message.MessageThreadID, text, markup)
}

func (h *Handler) OwnerPluginCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	if !isOwner(cb.From.ID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⛔ Kontrol plugin khusus Owner / Developer.",
			ShowAlert:       true,
		})
		return
	}

	parts := strings.Split(strings.TrimSpace(cb.Data), ":")
	if len(parts) < 3 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		return
	}
	action := parts[1]
	ownerID, _ := strconv.ParseInt(parts[2], 10, 64)

	var text string
	var markup *botgotd.InlineKeyboardMarkup
	toast := ""

	switch action {
	case "list":
		if ownerID == 0 {
			text, markup = h.renderOwnerHelp(ctx, len(ownerHelpPages))
		} else {
			text, markup = h.renderOwnerPluginListView(ctx, ownerID, strconv.FormatInt(ownerID, 10))
		}
	case "open":
		modKey := ""
		if len(parts) >= 4 {
			modKey = parts[3]
		}
		text, markup = h.renderOwnerPluginDetail(ctx, ownerID, modKey)
	case "mod":
		modKey := ""
		if len(parts) >= 4 {
			modKey = parts[3]
		}
		if sp, next, ok := h.togglePluginModule(ctx, ownerID, modKey); ok {
			if next {
				toast = fmt.Sprintf("🟢 Modul %s diaktifkan", sp.Title)
			} else {
				toast = fmt.Sprintf("🔴 Modul %s dinonaktifkan", sp.Title)
			}
		}
		text, markup = h.renderOwnerPluginDetail(ctx, ownerID, modKey)
	case "cmd":
		modKey, cmdKey := "", ""
		if len(parts) >= 5 {
			modKey = parts[3]
			cmdKey = parts[4]
		}
		if canon, next, ok := h.togglePluginSubCommand(ctx, ownerID, modKey, cmdKey); ok {
			if next {
				toast = fmt.Sprintf("🟢 Sub-fitur !%s diaktifkan", canon)
			} else {
				toast = fmt.Sprintf("🔴 Sub-fitur !%s dinonaktifkan", canon)
			}
		}
		text, markup = h.renderOwnerPluginDetail(ctx, ownerID, modKey)
	case "suball":
		modKey := ""
		enable := len(parts) >= 5 && parts[4] != "0"
		if len(parts) >= 4 {
			modKey = parts[3]
		}
		h.setAllModuleSubCommands(ctx, ownerID, modKey, enable)
		if enable {
			toast = "✅ Semua sub-fitur modul diaktifkan"
		} else {
			toast = "⛔ Semua sub-fitur modul dinonaktifkan"
		}
		text, markup = h.renderOwnerPluginDetail(ctx, ownerID, modKey)
	case "all":
		enable := len(parts) >= 4 && parts[3] != "0"
		h.setAllPluginStatesForTarget(ctx, ownerID, enable)
		if enable {
			toast = "✅ Seluruh plugin diaktifkan"
		} else {
			toast = "⛔ Seluruh plugin dinonaktifkan"
		}
		if ownerID == 0 {
			text, markup = h.renderOwnerHelp(ctx, len(ownerHelpPages))
		} else {
			text, markup = h.renderOwnerPluginListView(ctx, ownerID, strconv.FormatInt(ownerID, 10))
		}
	case "reset":
		h.resetUserPluginStates(ctx, ownerID)
		toast = "🔄 Pengaturan user direset ke default Global"
		text, markup = h.renderOwnerPluginListView(ctx, ownerID, strconv.FormatInt(ownerID, 10))
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            toast,
	})

	if cb.Message == nil || cb.Message.Message == nil || text == "" {
		return
	}
	msg := cb.Message.Message
	_, _ = editMonitorRichMessageWithMarkup(ctx, b, msg.Chat.ID, msg.ID, text, markup)
}
