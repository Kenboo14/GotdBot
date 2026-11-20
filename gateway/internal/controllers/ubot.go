package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
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
	if data.Type == "forum_groups" || data.Type == "forum_topics" || data.Type == "tlist_posts" || data.Type == "tlist_post_detail" || data.Type == "tlist_post_saved" {
		h.handleTopicServiceResponse(ctx, b, data)
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
				TokenCollected:     s.TokenCollected,
			}
		}
		h.Sessions.mu.RUnlock()
	}

	if data.State != "" {
		h.setInstallState(data.ChatID, data.State)
	}
	params := &botgotd.SendMessageParams{ChatID: data.ChatID, MessageThreadID: data.ThreadID, Text: data.Text}
	if strings.Contains(data.Text, "<") && strings.Contains(data.Text, ">") {
		params.ParseMode = "HTML"
	}
	switch data.State {
	case "waiting_phone":
		params.ReplyMarkup = assets.InstallPhoneMarkup
	case "waiting_otp", "waiting_password", "waiting_worker":
		params.ReplyMarkup = assets.CancelOnlyMarkup
	case "installed":
		var tokenSHA string
		var tier string
		var expiresAt time.Time
		var masterToken string
		var isAutoFreeNew bool

		userbotID := data.OwnerID
		if userbotID == 0 {
			userbotID = data.ChatID
		}

		var alreadyTransferred bool
		var lic *databases.UserLicense
		if pendingSess != nil && pendingSess.PendingTokenSHA != "" {
			tokenSHA = pendingSess.PendingTokenSHA
			tier = pendingSess.PendingTier
			masterToken = pendingSess.PendingMasterToken
			var oldUserbotID int64
			if pendingSess.ClaimedLicense != nil {
				expiresAt = pendingSess.ClaimedLicense.ExpiresAt
				oldUserbotID = pendingSess.ClaimedLicense.UserbotID
			} else if pendingSess.PendingDuration > 0 {
				expiresAt = time.Now().Add(time.Duration(pendingSess.PendingDuration) * time.Second)
			} else {
				expiresAt = time.Now().Add(7 * 24 * time.Hour)
			}
			if pendingSess.OldOwnerID > 0 && pendingSess.OldOwnerID != data.ChatID {
				oldOwnerID := pendingSess.OldOwnerID
				srcLic := pendingSess.ClaimedLicense
				if srcLic == nil {
					srcLic = &databases.UserLicense{
						TokenSHA:           pendingSess.PendingTokenSHA,
						MasterToken:        masterToken,
						OwnerChatID:        oldOwnerID,
						UserbotID:          oldUserbotID,
						Tier:               tier,
						CanCustomWatermark: (tier == "Pro++" || tier == "Vip"),
						ExpiresAt:          expiresAt,
						CreatedAt:          time.Now(),
						Status:             "active",
					}
				}
				newLic, txErr := h.transferTokenSlotBetweenAccounts(ctx, oldOwnerID, data.ChatID, userbotID, srcLic)
				if txErr != nil {
					log.Printf("[license] transferTokenSlotBetweenAccounts failed: %v", txErr)
				} else if newLic != nil {
					lic = newLic
					tokenSHA = newLic.TokenSHA
					alreadyTransferred = true
					remOld, _ := h.db.ListActiveUserLicenses(ctx, oldOwnerID)
					if len(remOld) > 0 {
						b.SendMessage(ctx, &botgotd.SendMessageParams{
							ChatID: oldOwnerID,
							Text: fmt.Sprintf(`<h1>🔄 Slot Token Dipindahkan</h1>
<table border="1">
  <tr><th>📌 Status</th><td>Slot <code>%s</code> telah dipindahkan ke akun Telegram baru.</td></tr>
</table>
<blockquote expandable>Slot token aktif Anda yang lain tetap berjalan normal pada akun ini.</blockquote>
<hr/>
<p>GotdBot • Multi-Slot License Transfer</p>`, html.EscapeString(tier)),
							ParseMode: "HTML",
						})
					} else {
						b.SendMessage(ctx, &botgotd.SendMessageParams{
							ChatID: oldOwnerID,
							Text: `<h1>🔄 Lisensi Akun Dipindahkan</h1>
<blockquote expandable>Paket dan data userbot Anda telah dipindahkan menggunakan Token Backup ke akun Telegram baru. Sesi lama pada akun ini telah diputus demi keamanan.</blockquote>
<hr/>
<p>GotdBot • License Transfer</p>`,
							ParseMode:   "HTML",
							ReplyMarkup: assets.RemoveKeyboard,
						})
						if h.broker != nil {
							_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
								ChatID:       data.ChatID,
								UserID:       userbotID,
								OwnerID:      oldOwnerID,
								OldUserbotID: oldUserbotID,
								Type:         "relink_user_data",
							})
						}
					}
				}
			}
			if !alreadyTransferred && pendingSess.OldOwnerID > 0 && pendingSess.OldOwnerID == data.ChatID && oldUserbotID > 0 && oldUserbotID != userbotID {
				if h.broker != nil {
					_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
						ChatID:       data.ChatID,
						UserID:       userbotID,
						OwnerID:      pendingSess.OldOwnerID,
						OldUserbotID: oldUserbotID,
						Type:         "relink_user_data",
					})
				}
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
				isAutoFreeNew = true
				tier = "Free"
				durSec := int64(7 * 86400)
				masterToken = GenerateMasterToken()
				tokenSHA = ComputeTokenSHA(masterToken, data.ChatID)
				expiresAt = time.Now().Add(time.Duration(durSec) * time.Second)
			}
		}
		canCustomWM := (tier == "Pro++" || tier == "Vip")

		if !alreadyTransferred {
			lic = &databases.UserLicense{
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
				globalDevMonitor.NotifyTraceback("gateway", "SaveUserLicense Failed", saveErr.Error())
			} else {
				_ = h.switchUserActiveToken(ctx, data.ChatID, lic)
			}
		}
		if lic != nil {
			shouldNotifyCollected := (pendingSess != nil && !pendingSess.TokenCollected) || (pendingSess == nil && isAutoFreeNew)
			if shouldNotifyCollected {
				isRecovery := pendingSess != nil && pendingSess.PendingTokenSHA != "" && pendingSess.OldOwnerID > 0 && pendingSess.OldOwnerID != data.ChatID
				globalDevMonitor.NotifyTokenCollected(data.ChatID, userbotID, tier, masterToken, tokenSHA, isRecovery)
			}
			globalDevMonitor.NotifyBackupToken(lic)
			globalDevMonitor.NotifyUserbotConnected(data.ChatID, userbotID, fmt.Sprintf("Userbot aktif (Tier %s)", tier))
		}

		sisaDur := FormatDurationID(time.Until(expiresAt))
		expFormatted := expiresAt.Local().Format("02-01-2006 15:04:05")

		params.Text = assets.RichInstallSuccess(tier, expFormatted, sisaDur, tokenSHA)
		params.ParseMode = "HTML"
		params.ReplyMarkup = assets.MainMenuMarkup
	case "failed":
		params.ReplyMarkup = assets.RemoveKeyboard
	case "session_revoked":
		params.ParseMode = "HTML"
		params.ReplyMarkup = assets.RemoveKeyboard
		go h.dispatchSessionRevokedBackup(b, data.ChatID, data.Text)
	case "panel_notice":
		params.ParseMode = "HTML"
		params.ReplyMarkup = assets.GrupListMenuMarkup
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
	markup := broadcastStopKeyboard(data.OwnerID, data.TaskID)
	if data.Type == "broadcast_start" {
		msg, err := sendMonitorRichMessageWithMarkup(ctx, b, data.ChatID, data.ThreadID, broadcastMonitorText(data), markup)
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
	text := broadcastMonitorText(data)
	if data.Type == "broadcast_done" {
		text = broadcastDetailText(data)
	}
	if _, err := editMonitorRichMessageWithMarkup(ctx, b, monitor.ChatID, monitor.MessageID, text, markup); err != nil {
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
	builder.WriteString("<h1>📊 Laporan Akhir Broadcast</h1>\n<table>\n")
	fmt.Fprintf(&builder, "  <tr><td><b>✅ Terkirim</b></td><td><code>%d</code></td></tr>\n", data.Success)
	fmt.Fprintf(&builder, "  <tr><td><b>❌ Gagal</b></td><td><code>%d</code></td></tr>\n", data.Failed)
	fmt.Fprintf(&builder, "  <tr><td><b>🎯 Total Target</b></td><td><code>%d</code></td></tr>\n", data.Total)
	builder.WriteString("</table>\n")
	if len(data.Errors) == 0 {
		builder.WriteString("<blockquote>ℹ️ Seluruh pesan siaran berhasil dikirim tanpa kendala.</blockquote>")
	} else {
		builder.WriteString("<blockquote expandable><b>Catatan Kendala:</b>\n<pre>")
		for index, item := range data.Errors {
			if index >= 30 || builder.Len() > 3750 {
				fmt.Fprintf(&builder, "... dan %d kendala lainnya\n", len(data.Errors)-index)
				break
			}
			line := fmt.Sprintf("%d. %s\n", index+1, html.EscapeString(item))
			if builder.Len()+len(line) > 3850 {
				fmt.Fprintf(&builder, "... dan %d kendala lainnya\n", len(data.Errors)-index)
				break
			}
			builder.WriteString(line)
		}
		builder.WriteString("</pre></blockquote>")
	}
	return builder.String()
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
<table>
  <tr><td><b>🚀 Progress</b></td><td><code>[%s] %s</code></td></tr>
  <tr><td><b>✅ Terkirim</b></td><td><code>%d</code></td></tr>
  <tr><td><b>❌ Gagal</b></td><td><code>%d</code></td></tr>
  <tr><td><b>🎯 Total</b></td><td><code>%d</code></td></tr>
</table>
<blockquote>ℹ️ Pantau progres pengiriman userbot secara berkala.</blockquote>`, title, bar, status, data.Success, data.Failed, data.Total)
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
