package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

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
	if data.Type == "panel_broadcast_log" {
		h.handlePanelBroadcastLog(ctx, b, data)
		return
	}
	if data.Type == "forum_groups" || data.Type == "forum_topics" || data.Type == "tlist_posts" || data.Type == "tlist_post_detail" || data.Type == "tlist_post_saved" {
		h.handleTopicServiceResponse(ctx, b, data)
		return
	}
	if data.Type == "manual_groups_synced" {
		targetOwner := data.OwnerID
		if targetOwner == 0 {
			targetOwner = data.ChatID
		}
		h.applySyncedManualGroups(ctx, targetOwner, data.TaskID)
	}
	if data.State == "panel_notice" || data.State == "remote_panel_notice" {
		h.clearProgress(ctx, b, data.ChatID)
	}
	if data.ChatID == 0 {
		log.Printf("incomplete Redis message %s (no ChatID)", msg.ID)
		return
	}
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
						_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
							ChatID: oldOwnerID,
							Text: fmt.Sprintf(`<h1>🔄 Pindah Akses Selesai · Sesi Dicopot</h1>
<table border="1">
  <tr><th>📌 Status</th><td>Slot <code>%s</code> dipindahkan ke akun Telegram baru.</td></tr>
</table>
<blockquote expandable>Demi keamanan, sesi userbot aktif pada akun ini telah dicopot. Slot token lain Anda masih tersimpan aman dan dapat dipasang kembali melalui <b>/start → 🔑 Input Token 🔑</b> atau tombol <b>🚀 Buat Userbot 🚀</b>.</blockquote>
<hr/>
<p>GotdBot • License Transfer</p>`, html.EscapeString(tier)),
							ParseMode:   "HTML",
							ReplyMarkup: assets.RemoveKeyboard,
						})
					} else {
						_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
							ChatID: oldOwnerID,
							Text: `<h1>🔄 Pindah Akses Selesai · Sesi Dicopot</h1>
<blockquote expandable>Paket dan data userbot Anda telah dipindahkan ke akun Telegram baru dan berhasil dipasang. Sesi userbot pada akun ini telah otomatis dicopot demi keamanan.</blockquote>
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
	case "remote_panel_notice":
		params.ParseMode = "HTML"
		if data.OwnerID != 0 {
			params.ReplyMarkup = assets.CtrlDoneIKB(data.OwnerID)
		}
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
