package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/databases"
	"assterisk/gateway/applications/models"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Ubot submits exactly one job to the worker. It deliberately never generates
// background load from a Telegram command.
func (h *Handler) Ubot(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	allowed, estimated, err := h.broker.CheckLimit(ctx, fmt.Sprintf("ubot:broadcast:%d", update.Message.From.ID), 1, 5*time.Second)
	if err != nil {
		log.Printf("rate limiter error: %v", err)
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Layanan sementara tidak tersedia."})
		return
	}
	if !allowed {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Tunggu " + estimated.Round(time.Second).String() + " sebelum mengirim lagi."})
		return
	}
	payload := models.PToService{ChatID: update.Message.Chat.ID, UserID: update.Message.From.ID, Username: update.Message.From.Username, Text: update.Message.Text, Type: "broadcast"}
	if err := h.broker.To(ctx, "to:ubot:broadcast", payload); err != nil {
		log.Printf("Redis stream: %v", err)
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Worker tidak dapat menerima pesan. Cek Redis."})
		return
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Pesan sedang diproses."})
}

func (h *Handler) SendMessage(ctx context.Context, b *botgotd.Bot, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		log.Printf("invalid Redis payload for %s", msg.ID)
		return
	}
	var data models.PFromService
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		log.Printf("invalid Redis JSON for %s: %v", msg.ID, err)
		return
	}
	if strings.HasPrefix(data.Type, "broadcast_") {
		h.handleBroadcastMonitor(ctx, b, data)
		return
	}
	if data.State == "panel_notice" {
		h.clearProgress(ctx, b, data.ChatID)
	}
	if data.ChatID == 0 {
		log.Printf("incomplete Redis message %s (no ChatID)", msg.ID)
		return
	}
	// Jika text kosong tapi state waiting_phone → hanya update state, tidak kirim pesan
	if strings.TrimSpace(data.Text) == "" {
		if data.State != "" {
			h.setInstallState(data.ChatID, data.State)
		}
		return
	}
	var pendingSess *UserSession
	if data.State == "installed" {
		h.Sessions.mu.RLock()
		if s, ok := h.Sessions.sessions[data.ChatID]; ok && s != nil {
			pendingSess = &UserSession{
				ChatID:             s.ChatID,
				PendingMasterToken: s.PendingMasterToken,
				PendingTokenSHA:    s.PendingTokenSHA,
				PendingTier:        s.PendingTier,
				PendingDuration:    s.PendingDuration,
				OldOwnerID:         s.OldOwnerID,
				ClaimedLicense:     s.ClaimedLicense,
			}
		}
		h.Sessions.mu.RUnlock()
	}

	if data.State != "" {
		h.setInstallState(data.ChatID, data.State)
	}
	params := &botgotd.SendMessageParams{ChatID: data.ChatID, Text: data.Text}
	if strings.Contains(data.Text, "<") && strings.Contains(data.Text, ">") {
		params.ParseMode = "HTML"
	}
	switch data.State {
	case "waiting_phone":
		params.ReplyMarkup = installPhoneKeyboard()
	case "waiting_otp", "waiting_password", "waiting_worker":
		params.ReplyMarkup = cancelKeyboard()
	case "installed":
		var tokenSHA string
		var tier string
		var expiresAt time.Time
		var masterToken string

		if pendingSess != nil && pendingSess.PendingTokenSHA != "" {
			tokenSHA = pendingSess.PendingTokenSHA
			tier = pendingSess.PendingTier
			masterToken = pendingSess.PendingMasterToken
			if pendingSess.ClaimedLicense != nil {
				expiresAt = pendingSess.ClaimedLicense.ExpiresAt
			} else if pendingSess.PendingDuration > 0 {
				expiresAt = time.Now().Add(time.Duration(pendingSess.PendingDuration) * time.Second)
			} else {
				expiresAt = time.Now().Add(7 * 24 * time.Hour)
			}
			if pendingSess.OldOwnerID > 0 && pendingSess.OldOwnerID != data.ChatID {
				_ = h.db.RelinkUserData(ctx, pendingSess.OldOwnerID, data.ChatID)
			}
		} else if pendingSess != nil && pendingSess.PendingMasterToken != "" {
			masterToken = pendingSess.PendingMasterToken
			tier = pendingSess.PendingTier
			if tier == "" {
				tier = "Free"
			}
			durSec := pendingSess.PendingDuration
			if durSec <= 0 {
				durSec = 7 * 86400
			}
			expiresAt = time.Now().Add(time.Duration(durSec) * time.Second)
			tokenSHA = ComputeTokenSHA(masterToken, data.ChatID)
			_ = h.db.IncrementTokenMasterClaim(ctx, masterToken)
		} else {
			existingLic, _ := h.db.GetActiveUserLicense(ctx, data.ChatID)
			if existingLic != nil {
				tokenSHA = existingLic.TokenSHA
				tier = existingLic.Tier
				masterToken = existingLic.MasterToken
				expiresAt = existingLic.ExpiresAt
			} else {
				tier = "Free"
				durSec := int64(7 * 86400)
				masterToken = GenerateMasterToken()
				tokenSHA = ComputeTokenSHA(masterToken, data.ChatID)
				expiresAt = time.Now().Add(time.Duration(durSec) * time.Second)
			}
		}

		userbotID := data.OwnerID
		if userbotID == 0 {
			userbotID = data.ChatID
		}
		canCustomWM := (tier == "Pro++" || tier == "Vip")

		lic := &databases.UserLicense{
			TokenSHA:           tokenSHA,
			MasterToken:        masterToken,
			OwnerChatID:        data.ChatID,
			UserbotID:          userbotID,
			Tier:               tier,
			CanCustomWatermark: canCustomWM,
			ExpiresAt:          expiresAt,
			CreatedAt:          time.Now(),
			Status:             "active",
		}
		if saveErr := h.db.SaveUserLicense(ctx, lic); saveErr != nil {
			log.Printf("[license] failed saving user license for chat %d: %v", data.ChatID, saveErr)
		}

		sisaDur := FormatDurationID(time.Until(expiresAt))
		expFormatted := expiresAt.Format("02-01-2006 15:04:05")

		params.Text = fmt.Sprintf(`🎉 <b>Userbot Berhasil Terpasang!</b>
━━━━━━━━━━━━━━━━━━━━
🎖️ <b>Tier Lisensi:</b> <code>%s</code>
⏳ <b>Masa Aktif:</b> <code>%s</code> (Sisa %s)

🔑 <b>Token SHA Backup (Kunci Brankas):</b>
<code>%s</code>

⚠️ <b>PENTING - Simpan token ini baik-baik!</b>
<i>Jika sesi userbot Anda terlepas di kemudian hari atau ingin memindahkan database ke akun lain, gunakan token SHA di atas saat <code>/start</code> agar seluruh data promosi, catatan notes, prefix, dan grup target tidak hilang.</i>`,
			tier, expFormatted, sisaDur, tokenSHA,
		)
		params.ParseMode = "HTML"
		params.ReplyMarkup = mainMenuMarkup
	case "failed":
		params.ReplyMarkup = cachedRemoveKeyboard
	case "session_revoked":
		params.ParseMode = "HTML"
		params.ReplyMarkup = cachedRemoveKeyboard
	case "panel_notice":
		params.ParseMode = "HTML"
		params.ReplyMarkup = grupListMenuMarkup
	}
	if data.Type == "list_detail" {
		sentMsg, err := b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    data.ChatID,
			Text:      data.Text,
			ParseMode: "HTML",
		})
		if err == nil && sentMsg != nil {
			h.trackDetailMessageIDs(ctx, data.ChatID, []int{sentMsg.ID})
		}
		return
	}
	if data.Type == "list_previews" {
		params.ParseMode = "HTML"
		count, _ := strconv.Atoi(data.TaskID)
		params.ReplyMarkup = browseListMarkup(count)
	}
	if data.MediaCacheID > 0 {
		cache := strings.TrimPrefix(strings.TrimSpace(os.Getenv("GROUP_CACHE")), "@")
		if cache == "" {
			cache = "AiotdCaches"
		}
		if _, err := b.CopyMessage(ctx, &botgotd.CopyMessageParams{
			ChatID:      data.ChatID,
			FromChatID:  "@" + cache,
			MessageID:   data.MediaCacheID,
			ReplyMarkup: params.ReplyMarkup,
		}); err == nil {
			return
		} else {
			log.Printf("Telegram copy cached media failed (cache=%s message=%d): %v", cache, data.MediaCacheID, err)
		}
	}
	if len(data.MediaCacheIDs) > 0 {
		cache := strings.TrimPrefix(strings.TrimSpace(os.Getenv("GROUP_CACHE")), "@")
		if cache == "" {
			cache = "AiotdCaches"
		}
		copied, err := b.CopyMessages(ctx, &botgotd.CopyMessagesParams{
			ChatID:     data.ChatID,
			FromChatID: "@" + cache,
			MessageIDs: data.MediaCacheIDs,
		})
		if err == nil {
			// copyMessages preserves the album, while the markup is attached
			// afterward to the first copied media message.
			if len(copied) == 0 {
				log.Printf("Telegram copy cached media group returned no message IDs")
				return
			}
			if params.ReplyMarkup != nil {
				for _, copiedMessage := range copied {
					var markupErr error
					for attempt := 1; attempt <= 3; attempt++ {
						_, markupErr = b.EditMessageReplyMarkup(ctx, &botgotd.EditMessageReplyMarkupParams{
							ChatID: data.ChatID, MessageID: copiedMessage.ID, ReplyMarkup: params.ReplyMarkup,
						})
						if markupErr == nil || strings.Contains(strings.ToLower(markupErr.Error()), "message is not modified") {
							log.Printf("Telegram detail media markup ready on message %d", copiedMessage.ID)
							markupErr = nil
							break
						}
						if attempt < 3 {
							time.Sleep(300 * time.Millisecond)
						}
					}
					if markupErr != nil {
						log.Printf("Telegram detail media markup failed on message %d: %v", copiedMessage.ID, markupErr)
					}
				}
			}
			return
		} else {
			log.Printf("Telegram copy cached media group failed (cache=%s messages=%v): %v", cache, data.MediaCacheIDs, err)
		}
	}
	if _, err := b.SendMessage(ctx, params); err != nil {
		log.Printf("Telegram send failed: %v", err)
	}
}

func (h *Handler) handleBroadcastMonitor(ctx context.Context, b *botgotd.Bot, data models.PFromService) {
	if data.ChatID == 0 || data.TaskID == "" {
		return
	}
	if data.Type == "broadcast_start" {
		msg, err := b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: data.ChatID, Text: broadcastMonitorText(data), ParseMode: "HTML",
			ReplyMarkup: broadcastStopKeyboard(data.OwnerID, data.TaskID),
		})
		if err != nil {
			log.Printf("broadcast monitor start failed: %v", err)
			return
		}
		h.broadcastMu.Lock()
		h.broadcastTasks[data.TaskID] = broadcastMonitor{ChatID: data.ChatID, MessageID: msg.ID}
		h.broadcastMu.Unlock()
		return
	}
	h.broadcastMu.Lock()
	monitor, ok := h.broadcastTasks[data.TaskID]
	h.broadcastMu.Unlock()
	if !ok {
		return
	}
	params := &botgotd.EditMessageTextParams{
		ChatID: monitor.ChatID, MessageID: monitor.MessageID,
		Text: broadcastMonitorText(data), ParseMode: "HTML",
	}
	if data.Type == "broadcast_done" {
		// The final report replaces the progress message instead of creating a
		// second message in the owner's private chat.
		params.Text = broadcastDetailText(data)
	}
	// Keep the stop button visible on the same monitor message, including the
	// final report. Pressing it after completion is harmless because the worker
	// has already removed the task.
	params.ReplyMarkup = broadcastStopKeyboard(data.OwnerID, data.TaskID)
	if _, err := b.EditMessageText(ctx, params); err != nil {
		log.Printf("broadcast monitor update failed: %v", err)
	}
	if data.Type == "broadcast_done" {
		h.broadcastMu.Lock()
		delete(h.broadcastTasks, data.TaskID)
		h.broadcastMu.Unlock()
	}
}

func broadcastDetailText(data models.PFromService) string {
	var builder strings.Builder
	builder.WriteString("<b>📊 Laporan Akhir Broadcast</b>\n━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&builder, "<b>Terkirim:</b> <code>%d</code>\n<b>Gagal:</b>    <code>%d</code>\n<b>Total:</b>    <code>%d</code>\n", data.Success, data.Failed, data.Total)
	if len(data.Errors) == 0 {
		builder.WriteString("\n<i>Semua pesan berhasil dikirim tanpa kendala.</i>")
	} else {
		builder.WriteString("\n<b>Catatan Kendala:</b>\n<pre>")
		for index, item := range data.Errors {
			if index >= 30 {
				fmt.Fprintf(&builder, "... dan %d kendala lainnya\n", len(data.Errors)-30)
				break
			}
			fmt.Fprintf(&builder, "%d. %s\n", index+1, html.EscapeString(item))
		}
		builder.WriteString("</pre>")
	}
	text := builder.String()
	if len(text) > 4000 {
		return text[:3990] + "\n...</pre>"
	}
	return text
}

func broadcastMonitorText(data models.PFromService) string {
	progress := data.Progress
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	filled := progress / 10
	bar := strings.Repeat("■", filled) + strings.Repeat("□", 10-filled)
	status := fmt.Sprintf("%d%%", progress)
	title := "📡 Monitor Broadcast"
	if data.Type == "broadcast_done" {
		if progress == 100 {
			title = "✅ Broadcast Selesai"
			status = "100% selesai"
		} else {
			title = "🛑 Broadcast Dibatalkan"
			status = fmt.Sprintf("%d%% dibatalkan", progress)
		}
	}
	return fmt.Sprintf(`<h1>%s</h1>
<table border="1">
  <tr><th>📊 Metrik</th><th>📌 Status</th></tr>
  <tr><td>🚀 Progress</td><td><code>[%s] %s</code></td></tr>
  <tr><td>✅ Terkirim</td><td><code>%d</code></td></tr>
  <tr><td>❌ Gagal</td><td><code>%d</code></td></tr>
  <tr><td>🎯 Total</td><td><code>%d</code></td></tr>
</table>
<hr/>
<p>Userbot Broadcast Service • Gotd</p>`, title, bar, status, data.Success, data.Failed, data.Total)
}

func broadcastStopKeyboard(ownerID int64, taskID string) *botgotd.InlineKeyboardMarkup {
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{
		Text: "🛑 Hentikan Broadcast", SwitchInlineQueryCurrentChat: fmt.Sprintf("gcancel %d %s", ownerID, taskID),
	}}}}
}

// SimulateLoad is retained for compatibility. /broadcast now submits one job.
func (h *Handler) SimulateLoad(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/broadcast"))
	if message == "" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Gunakan: /broadcast <pesan>"})
		return
	}
	update.Message.Text = message
	h.Ubot(ctx, b, update)
}

var gatewayHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	},
}

func sendRichMessage(ctx context.Context, token string, chatID int64, htmlContent string, markup any) (int, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendRichMessage", token)
	payload := map[string]any{
		"chat_id": chatID,
		"rich_message": map[string]any{
			"html": botgotd.FormatRichHTML(htmlContent),
		},
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	bodyData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := gatewayHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(respBody, &res)
	return res.Result.MessageID, nil
}

func stripRichTags(s string) string {
	s = strings.ReplaceAll(s, "<h1>", "<b>")
	s = strings.ReplaceAll(s, "</h1>", "</b>\n")
	s = strings.ReplaceAll(s, "<h2>", "<b>")
	s = strings.ReplaceAll(s, "</h2>", "</b>\n")
	if idx := strings.Index(s, "<tg-button-row>"); idx != -1 {
		if endIdx := strings.Index(s, "</tg-button-row>"); endIdx != -1 {
			s = s[:idx] + s[endIdx+len("</tg-button-row>"):]
		}
	}
	s = strings.ReplaceAll(s, "<tr><th>", "• <b>")
	s = strings.ReplaceAll(s, "</th><th>", " | ")
	s = strings.ReplaceAll(s, "</th></tr>", "</b>\n")
	s = strings.ReplaceAll(s, "<tr><td>", "• ")
	s = strings.ReplaceAll(s, "</td><td>", ": ")
	s = strings.ReplaceAll(s, "</td></tr>", "\n")
	tableTags := []string{
		"<table border=\"1\">", "<table>", "</table>",
		"<tr>", "</tr>", "<th>", "</th>", "<td>", "</td>",
		"<thead>", "</thead>", "<tbody>", "</tbody>",
	}
	for _, tag := range tableTags {
		s = strings.ReplaceAll(s, tag, "")
	}
	s = strings.ReplaceAll(s, "<hr/>", "")
	s = strings.ReplaceAll(s, "<hr />", "")
	s = strings.ReplaceAll(s, "<hr>", "")
	return strings.TrimSpace(s)
}
