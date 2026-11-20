package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
)

func (h *Handler) PromptTimer(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_timer")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`⏱️ <b>Timer Harian (WIB · 24 Jam)</b>

Atur jam nyala dan jam mati harian. Di luar jadwal userbot berhenti otomatis, lalu menyala lagi pada jam mulai. Berlaku juga untuk tipe Autoreply.

<blockquote>ℹ️ Pilih preset di bawah, ketik rentang langsung (mis. <code>08:00 - 22:00</code>), atau ketik <b>jam nyala</b> dulu (mis. <code>08:00</code>).</blockquote>

⏰ <b>Jadwal saat ini:</b> <code>%s</code>`, html.EscapeString(cfg.FormatTimerDisplay()))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.TimerMarkup,
	})
}

func (h *Handler) handleTimerStartInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	if isDisableCommand(text) {
		h.saveTimerAndReply(ctx, b, chatID, userID, "Belum diatur", "✅ <b>Timer dinonaktifkan.</b>\nJadwal aktif kembali ke 24 jam penuh.")
		return true
	}
	if canon, ok := parseDailyTimerPair(text); ok {
		h.saveTimerAndReply(ctx, b, chatID, userID, canon, fmt.Sprintf("✅ <b>Timer harian tersimpan</b>\n⏰ <b>Jadwal aktif:</b> <code>%s WIB</code>", html.EscapeString(canon)))
		return true
	}
	if _, _, startCanon, ok := parseClockHHMM(text); ok {
		h.setPanelState(chatID, "waiting_set_timer_end:"+startCanon)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("⏰ Jam nyala dipilih: <code>%s WIB</code>\n\nSekarang ketik <b>jam mati</b> harian dalam format 24 jam (mis. <code>22:00</code>).", startCanon),
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return true
	}
	if isKnownPanelButton(text) {
		h.setPanelState(chatID, "")
		return false
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⚠️ Format timer tidak valid. Gunakan format 24 jam seperti <code>08:00 - 22:00</code> atau ketik jam nyala dulu (<code>08:00</code>).",
		ParseMode:   "HTML",
		ReplyMarkup: assets.TimerMarkup,
	})
	return true
}

func (h *Handler) handleTimerEndInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, startCanon, text string) bool {
	if isDisableCommand(text) {
		h.saveTimerAndReply(ctx, b, chatID, userID, "Belum diatur", "✅ <b>Timer dinonaktifkan.</b>\nJadwal aktif kembali ke 24 jam penuh.")
		return true
	}
	if _, _, endCanon, ok := parseClockHHMM(text); ok {
		if endCanon == startCanon {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("⚠️ Jam mati tidak boleh sama dengan jam nyala (<code>%s</code>). Masukkan jam mati yang berbeda.", startCanon),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		canon := startCanon + " - " + endCanon
		h.saveTimerAndReply(ctx, b, chatID, userID, canon, fmt.Sprintf("✅ <b>Timer harian tersimpan</b>\n⏰ <b>Jadwal aktif:</b> <code>%s WIB</code>", html.EscapeString(canon)))
		return true
	}
	if isKnownPanelButton(text) {
		h.setPanelState(chatID, "")
		return false
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⚠️ Format jam mati tidak valid. Gunakan format 24 jam, mis. <code>22:00</code>.",
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
	return true
}

func (h *Handler) saveTimerAndReply(ctx context.Context, b *botgotd.Bot, chatID, userID int64, timerVal, msg string) {
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.Timer = timerVal
	_ = h.db.SaveUserConfig(ctx, cfg)
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        msg,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

func parseClockHHMM(raw string) (int, int, string, bool) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.TrimSuffix(clean, "wib")
	clean = strings.TrimSpace(clean)
	clean = strings.ReplaceAll(clean, ".", ":")
	parts := strings.Split(clean, ":")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 2 || len(parts[1]) != 2 {
		return 0, 0, "", false
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, "", false
	}
	return h, m, fmt.Sprintf("%02d:%02d", h, m), true
}

func parseDailyTimerPair(raw string) (string, bool) {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "⏰")
	clean = strings.TrimPrefix(clean, "⏱️")
	clean = strings.TrimSpace(clean)
	lower := strings.ToLower(clean)
	lower = strings.TrimSpace(strings.TrimSuffix(lower, "wib"))

	for _, sep := range []string{"-", "–", "—", "s/d", "sampai"} {
		if strings.Contains(lower, sep) {
			parts := strings.SplitN(lower, sep, 2)
			if len(parts) == 2 {
				_, _, sCanon, okS := parseClockHHMM(parts[0])
				_, _, eCanon, okE := parseClockHHMM(parts[1])
				if okS && okE && sCanon != eCanon {
					return sCanon + " - " + eCanon, true
				}
			}
			return "", false
		}
	}

	fields := strings.Fields(lower)
	if len(fields) == 2 {
		_, _, sCanon, okS := parseClockHHMM(fields[0])
		_, _, eCanon, okE := parseClockHHMM(fields[1])
		if okS && okE && sCanon != eCanon {
			return sCanon + " - " + eCanon, true
		}
	}
	return "", false
}
