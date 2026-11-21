package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"strings"
	"time"
)

func normalizeNotifikasiMode(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(lower, "nonaktif") || strings.Contains(lower, "mati"):
		return "Nonaktif"
	case strings.Contains(lower, "admin"):
		return "Kirim ke saya & Admin"
	default:
		return "Kirim ke Saya"
	}
}

func notifikasiDescription(mode string) string {
	switch normalizeNotifikasiMode(mode) {
	case "Kirim ke saya & Admin":
		return "Log pengiriman &amp; flood dikirim ke private chat Anda dan seluruh ID Admin terdaftar."
	case "Nonaktif":
		return "Seluruh log pengiriman siaran &amp; flood dinonaktifkan."
	default:
		return "Log pengiriman &amp; flood dikirim hanya ke private chat pemilik userbot."
	}
}

func buildNotifikasiHTML(mode string) string {
	current := normalizeNotifikasiMode(mode)

	labelBoth := "🔔 Kirim ke saya & Admin"
	labelSelf := "🔔 Kirim ke Saya"
	labelOff := "🔕 Nonaktif"
	styleBoth := ""
	styleSelf := ""
	styleOff := ` style="danger"`

	switch current {
	case "Kirim ke saya & Admin":
		labelBoth = "✅ Kirim ke saya & Admin"
		styleBoth = ` style="primary"`
	case "Kirim ke Saya":
		labelSelf = "✅ Kirim ke Saya"
		styleSelf = ` style="primary"`
	case "Nonaktif":
		labelOff = "✅ Nonaktif"
	}

	return fmt.Sprintf(`<h1>🔔 Pengaturan Notifikasi Siaran</h1>
<table border="1">
  <tr><th>📌 Status Aktif</th><td><code>%s</code></td></tr>
  <tr><th>🔔 Saya &amp; Admin</th><td>Log putaran &amp; flood ke Anda + semua Admin</td></tr>
  <tr><th>🔔 Kirim ke Saya</th><td>Log putaran &amp; flood hanya ke private chat Anda</td></tr>
  <tr><th>🔕 Nonaktif</th><td>Tidak mengirim log putaran / flood sama sekali</td></tr>
</table>
<blockquote expandable>%s</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:notif:set:both"%s>%s</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:notif:set:self"%s>%s</tg-button>
  <tg-button type="callback_data" data="panel:notif:set:off"%s>%s</tg-button>
</tg-button-row>
<hr/>
<p>Pilih salah satu mode notifikasi di atas:</p>`,
		html.EscapeString(current),
		notifikasiDescription(current),
		styleBoth, labelBoth,
		styleSelf, labelSelf,
		styleOff, labelOff,
	)
}

func (h *Handler) renderNotifikasiView(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	if h == nil || h.db == nil || b == nil {
		return
	}
	cfg := h.db.GetUserConfig(ctx, userID)
	text := buildNotifikasiHTML(cfg.Notifikasi)

	if editMsgID > 0 {
		_, _ = editMonitorRichMessageWithMarkup(ctx, b, chatID, editMsgID, text, nil)
		return
	}
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

func (h *Handler) handleNotifikasiCallback(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, cb *botgotd.CallbackQuery) {
	effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
	mode := "Kirim ke Saya"
	switch cb.Data {
	case "panel:notif:set:both":
		mode = "Kirim ke saya & Admin"
	case "panel:notif:set:off":
		mode = "Nonaktif"
	}

	cfg := h.db.GetUserConfig(ctx, effectiveID)
	if normalizeNotifikasiMode(cfg.Notifikasi) == mode {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "ℹ️ Notifikasi sudah: " + mode,
		})
		return
	}

	cfg.Notifikasi = mode
	_ = h.db.SaveUserConfig(ctx, cfg)
	if h.broker != nil {
		bCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = h.broker.To(bCtx, "to:ubot:command", models.PToService{
			ChatID:  chatID,
			UserID:  effectiveID,
			OwnerID: effectiveID,
			Type:    "sync_panel_config",
		})
		cancel()
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            "✅ Notifikasi: " + mode,
	})
	if messageID > 0 {
		h.renderNotifikasiView(ctx, b, chatID, effectiveID, messageID)
	}
}

func stripLogHTMLToPlain(raw string) string {
	replacer := strings.NewReplacer(
		"<blockquote>", "",
		"</blockquote>", "\n",
		"<blockquote expandable>", "",
		"<b>", "",
		"</b>", "",
		"<i>", "",
		"</i>", "",
		"<code>", "",
		"</code>", "",
		"<pre>", "",
		"</pre>", "",
		"<br/>", "\n",
		"<br>", "\n",
	)
	return strings.TrimSpace(html.UnescapeString(replacer.Replace(raw)))
}

func isHTMLParseErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "parse entities") || strings.Contains(lower, "can't parse")
}

func (h *Handler) handlePanelBroadcastLog(ctx context.Context, b *botgotd.Bot, data models.PFromService) {
	if h == nil || h.db == nil || b == nil {
		return
	}
	ownerID := data.OwnerID
	if ownerID == 0 {
		ownerID = data.ChatID
	}
	if ownerID == 0 || strings.TrimSpace(data.Text) == "" {
		return
	}

	cfg := h.db.GetUserConfig(ctx, ownerID)
	mode := normalizeNotifikasiMode(cfg.Notifikasi)
	if mode == "Nonaktif" {
		return
	}

	recipients := make([]int64, 0, 4)
	seen := make(map[int64]bool)
	addRecipient := func(id int64) {
		if id > 0 && !seen[id] {
			seen[id] = true
			recipients = append(recipients, id)
		}
	}
	addRecipient(ownerID)
	if data.ChatID > 0 {
		addRecipient(data.ChatID)
	}
	if mode == "Kirim ke saya & Admin" {
		for _, adminID := range parseAdminChatIDs(cfg.Admin, ownerID) {
			addRecipient(adminID)
		}
	}

	plainFallback := ""
	for _, rcptID := range recipients {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := b.SendMessage(sendCtx, &botgotd.SendMessageParams{
			ChatID:    rcptID,
			Text:      data.Text,
			ParseMode: "HTML",
		})
		cancel()
		if err != nil && isHTMLParseErr(err) {
			if plainFallback == "" {
				plainFallback = stripLogHTMLToPlain(data.Text)
			}
			if plainFallback != "" {
				fbCtx, fbCancel := context.WithTimeout(ctx, 5*time.Second)
				_, _ = b.SendMessage(fbCtx, &botgotd.SendMessageParams{
					ChatID: rcptID,
					Text:   plainFallback,
				})
				fbCancel()
			}
		}
	}
}
