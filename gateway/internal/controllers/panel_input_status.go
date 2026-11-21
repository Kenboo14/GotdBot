package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"log"
	"strings"
)

func (h *Handler) handleStatusPanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state, text string, message *botgotd.Message) bool {
	switch state {
	case "waiting_set_mode":
		h.setPanelState(chatID, "")
		mode := strings.TrimSpace(text)
		for _, em := range []string{"👑", "⚡", "🔕", "📨"} {
			mode = strings.TrimSpace(strings.TrimPrefix(mode, em))
			mode = strings.TrimSpace(strings.TrimSuffix(mode, em))
		}
		if mode == "Forward" {
			if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowForwardMode {
				h.denyTierFeature(ctx, b, chatID, policy.Tier, "Mode Forward", "Pro++", statusMenuMarkup)
				return true
			}
		}
		if mode == "Normal" || mode == "Fast" || mode == "Silent" || mode == "Forward" {
			cfg := h.db.GetUserConfig(ctx, userID)
			cfg.Mode = mode
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("<h1>✅ Mode Siaran Diperbarui</h1><table border=\"1\"><tr><th>👑 Mode Aktif</th><td><code>%s</code></td></tr></table><hr/><p>GotdBot • Status Configuration</p>", mode),
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>⚠️ Pilihan Mode Tidak Dikenali</h1><blockquote expandable>Silakan pilih mode melalui tombol keyboard: Normal, Fast, Silent, atau Forward.</blockquote><hr/><p>GotdBot • Status Configuration</p>",
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_watermark":
		h.clearPanelPrompt(ctx, b, chatID)
		h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
		if message == nil || message.MediaGroupID == "" {
			h.setPanelState(chatID, "")
		}
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == assets.BtnHapusWatermark || strings.EqualFold(text, "hapus") {
			cfg.Watermark = ""
			cfg.WatermarkCacheID = 0
			_ = h.db.SaveUserConfig(ctx, cfg)
			if h.broker != nil {
				_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "clear_watermark"})
			}
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Watermark Berhasil Dihapus</h1>
<blockquote expandable>Custom watermark telah dihapus dari konfigurasi siaran Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		payload := models.PToService{
			ChatID:   chatID,
			UserID:   userID,
			Type:     "cache_watermark",
			Text:     text,
			Entities: panelEntities(message),
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("cache_watermark broker error: %v", err)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "<h1>❌ Gagal Menyimpan Watermark</h1><blockquote expandable>Broker userbot tidak merespons. Silakan coba beberapa saat lagi.</blockquote><hr/><p>GotdBot • Status Configuration</p>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>⏳ Menyimpan Watermark</h1><blockquote expandable>Mengamankan custom watermark ke saluran cache userbot...</blockquote><hr/><p>GotdBot • Status Configuration</p>",
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_email":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == "🗑️ Hapus Email 🗑️" || strings.EqualFold(text, "hapus") {
			cfg.Email = "Belum diatur"
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Email Berhasil Dihapus</h1>
<blockquote expandable>Alamat email notifikasi telah dinonaktifkan dari konfigurasi akun Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Email = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Email Berhasil Diperbarui</h1>
<blockquote expandable>Alamat email notifikasi akun Anda telah diatur ke: <code>%s</code>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(text)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_admin":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == "🗑️ Hapus Admin 🗑️" || strings.EqualFold(text, "hapus") {
			cfg.Admin = "Belum diatur"
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Admin Berhasil Dihapus</h1>
<blockquote expandable>Wewenang admin tambahan telah dihapus dari konfigurasi akun Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Admin = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Admin Berhasil Diperbarui</h1>
<blockquote expandable>Wewenang admin userbot telah diserahkan ke: <code>%s</code>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(text)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true
	}

	return false
}
