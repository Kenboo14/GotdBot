package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
)

func (h *Handler) PromptAturJeda(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_jeda_menu")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)

	text := fmt.Sprintf(`⏳ <b>Atur jeda</b>

Belum yakin? Pilih <b>Jeda Aman</b>.
• <b>Jeda Aman:</b> bot mengatur jarak antar-grup (200–400 detik). Paling aman untuk akun.
• <b>Jeda Otomatis:</b> bot menghitung dari jumlah grup Anda.
• <b>Jeda Per Grup:</b> jarak antar-grup (5–300 detik). Contoh 30 detik: Grup A, tunggu 30 detik, Grup B. Cocok untuk 50 grup ke atas.
• <b>Jeda Per Semua Grup:</b> jarak antar-putaran (2–120 menit, atau 4/6 jam). Contoh 20 menit: kirim ke semua grup, tunggu 20 menit, ulang. Cocok di bawah 30 grup.
• <b>Skip FloodWait:</b> lewati grup bila Telegram meminta tunggu melebihi batas (10–3600 detik). Mati = selalu menunggu.

Jeda Per Grup dan Jeda Per Semua Grup bisa menyala bersamaan. Masing-masing bisa dimatikan lewat 🔕 <b>Matikan</b>; keduanya mati berarti tanpa jeda. Berlaku juga untuk set Expert Extra.

<blockquote>⚠️ Bila userbot aktif 24 jam, pakai minimal 30 detik untuk Jeda Per Grup atau 20 menit untuk Jeda Per Semua Grup. Jeda terlalu kecil bisa membuat akun diblokir.</blockquote>

🕒 <b>Jeda sekarang:</b> %s
⏭️ <b>Skip FloodWait:</b> %s`,
		html.EscapeString(cfg.FormatJedaSummary(gCount)),
		html.EscapeString(cfg.FormatSkipFloodWait()),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.AturJedaMarkup,
	})
}

func (h *Handler) applyJedaAman(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.DelayMode = "aman"
	cfg.Delay = 0
	cfg.DelayAllGroups = 0
	_ = h.db.SaveUserConfig(ctx, cfg)
	h.sendJedaSaved(ctx, b, chatID, userID, cfg)
}

func (h *Handler) applyJedaOtomatis(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.DelayMode = "otomatis"
	cfg.Delay = 0
	cfg.DelayAllGroups = 0
	_ = h.db.SaveUserConfig(ctx, cfg)
	h.sendJedaSaved(ctx, b, chatID, userID, cfg)
}

func (h *Handler) PromptJedaPerGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_jeda_per_grup")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)

	text := fmt.Sprintf(`⏰ <b>Pilih jeda per grup (detik)</b>

Ini jarak antar-grup, bukan jarak sesudah semua grup. Contoh 30 detik: Grup A, tunggu 30 detik, Grup B.
Tekan 🔕 <b>Matikan</b> untuk mematikan jeda per grup; jeda per semua grup tidak berubah.

<blockquote>ℹ️ Pakai 30 detik ke atas bila userbot aktif 24 jam.</blockquote>

🕒 <b>Jeda sekarang:</b> %s
Pilih tombol (5–300 detik) atau ketik angka 5–300.`, html.EscapeString(cfg.FormatJedaSummary(gCount)))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.JedaPerGrupMarkup,
	})
}

func (h *Handler) PromptJedaPerSemuaGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_jeda_per_semua_grup")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)

	text := fmt.Sprintf(`⏰ <b>Pilih jeda per semua grup (menit)</b>

Ini jarak sesudah semua grup terkirim, bukan antar-grup. Contoh 20 menit: kirim ke semua grup, tunggu 20 menit, ulang.
Tekan 🔕 <b>Matikan</b> untuk mematikan jeda per semua grup; jeda per grup tidak berubah.

<blockquote>ℹ️ Pakai 20 menit ke atas bila userbot aktif 24 jam.</blockquote>

🕒 <b>Jeda sekarang:</b> %s
Pilih tombol (2–120 menit, atau 240/360 menit) atau ketik angka.`, html.EscapeString(cfg.FormatJedaSummary(gCount)))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.JedaPerSemuaGrupMarkup,
	})
}

func (h *Handler) PromptSkipFloodWait(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_skip_floodwait")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`⏭️ <b>Atur Skip FloodWait (detik)</b>

Bila Telegram meminta akun menunggu (FloodWait) lebih lama dari batas pilihan Anda, grup itu dilewati dan siaran lanjut ke grup berikutnya. Berlaku juga untuk set Expert Extra; 🔕 <b>Matikan</b> = selalu menunggu.

<blockquote>ℹ️ Pilih 30 detik–10 menit lewat tombol di bawah, atau ketik langsung angka <b>10–3600</b> detik.</blockquote>

⏭️ <b>Skip FloodWait saat ini:</b> %s`, html.EscapeString(cfg.FormatSkipFloodWait()))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.SkipFloodWaitMarkup,
	})
}

func (h *Handler) sendJedaSaved(ctx context.Context, b *botgotd.Bot, chatID, userID int64, cfg *databases.JasebConfig) {
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)
	text := fmt.Sprintf(`✅ <b>Jeda tersimpan</b>
🕒 <b>Jeda sekarang:</b> %s

🏠 <b>Menu utama</b>
Pilih lewat tombol di bawah.`, html.EscapeString(cfg.FormatJedaSummary(gCount)))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.MainMenuMarkup,
	})
}

func (h *Handler) dispatchJedaSubmenuAction(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	switch text {
	case assets.BtnJedaAman:
		h.applyJedaAman(ctx, b, chatID, userID)
		return true
	case assets.BtnJedaOtomatis:
		h.applyJedaOtomatis(ctx, b, chatID, userID)
		return true
	case assets.BtnJedaPerGrup:
		h.PromptJedaPerGrup(ctx, b, chatID, userID)
		return true
	case assets.BtnJedaPerSemuaGrup:
		h.PromptJedaPerSemuaGrup(ctx, b, chatID, userID)
		return true
	case assets.BtnSkipFloodWait:
		h.PromptSkipFloodWait(ctx, b, chatID, userID)
		return true
	default:
		return false
	}
}

func (h *Handler) handleJedaTimerPanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state, text string) bool {
	switch {
	case state == "waiting_jeda_menu":
		if h.dispatchJedaSubmenuAction(ctx, b, chatID, userID, text) {
			return true
		}
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Silakan pilih salah satu opsi jeda pada tombol di bawah.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.AturJedaMarkup,
		})
		return true

	case state == "waiting_jeda_per_grup":
		if h.dispatchJedaSubmenuAction(ctx, b, chatID, userID, text) {
			return true
		}
		if sec, _, ok := parseJedaPerGrupInput(text); ok {
			h.setPanelState(chatID, "")
			cfg := h.db.GetUserConfig(ctx, userID)
			cfg.DelayMode = "manual"
			cfg.Delay = sec
			_ = h.db.SaveUserConfig(ctx, cfg)
			h.sendJedaSaved(ctx, b, chatID, userID, cfg)
			return true
		}
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Nilai tidak valid. Masukkan angka <b>5–300</b> detik atau tekan 🔕 <b>Matikan</b>.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.JedaPerGrupMarkup,
		})
		return true

	case state == "waiting_jeda_per_semua_grup":
		if h.dispatchJedaSubmenuAction(ctx, b, chatID, userID, text) {
			return true
		}
		if min, _, ok := parseJedaPerSemuaGrupInput(text); ok {
			h.setPanelState(chatID, "")
			cfg := h.db.GetUserConfig(ctx, userID)
			cfg.DelayMode = "manual"
			cfg.DelayAllGroups = min
			_ = h.db.SaveUserConfig(ctx, cfg)
			h.sendJedaSaved(ctx, b, chatID, userID, cfg)
			return true
		}
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Nilai tidak valid. Pilih <b>2–120</b> menit, <b>240</b> (4 jam), <b>360</b> (6 jam), atau tekan 🔕 <b>Matikan</b>.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.JedaPerSemuaGrupMarkup,
		})
		return true

	case state == "waiting_skip_floodwait":
		if h.dispatchJedaSubmenuAction(ctx, b, chatID, userID, text) {
			return true
		}
		if sec, disable, ok := parseSkipFloodWaitInput(text); ok {
			h.setPanelState(chatID, "")
			cfg := h.db.GetUserConfig(ctx, userID)
			if disable {
				cfg.SkipFloodWait = false
				cfg.SkipFloodWaitSec = 0
			} else {
				cfg.SkipFloodWait = true
				cfg.SkipFloodWaitSec = sec
			}
			_ = h.db.SaveUserConfig(ctx, cfg)
			msg := fmt.Sprintf("✅ <b>Skip FloodWait tersimpan</b>\n⏭️ <b>Status:</b> <code>%s</code>", html.EscapeString(cfg.FormatSkipFloodWait()))
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        msg,
				ParseMode:   "HTML",
				ReplyMarkup: assets.MainMenuMarkup,
			})
			return true
		}
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Batas tidak valid. Pilih tombol preset atau ketik angka <b>10–3600</b> detik, atau tekan 🔕 <b>Matikan</b>.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.SkipFloodWaitMarkup,
		})
		return true

	case state == "waiting_set_timer":
		return h.handleTimerStartInput(ctx, b, chatID, userID, text)

	case strings.HasPrefix(state, "waiting_set_timer_end:"):
		startCanon := strings.TrimPrefix(state, "waiting_set_timer_end:")
		return h.handleTimerEndInput(ctx, b, chatID, userID, startCanon, text)
	}
	return false
}

func isDisableCommand(raw string) bool {
	if raw == assets.BtnMatikan || raw == assets.BtnTimerNonaktif {
		return true
	}
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "🔕 ")
	clean = strings.TrimPrefix(clean, "⏱️ ")
	lower := strings.ToLower(strings.TrimSpace(clean))
	return lower == "matikan" || lower == "mati" || lower == "off" || lower == "0" || lower == "nonaktifkan timer" || lower == "nonaktifkan"
}

func parseJedaPerGrupInput(raw string) (int, bool, bool) {
	if isDisableCommand(raw) {
		return 0, true, true
	}
	clean := strings.ToLower(strings.TrimSpace(raw))
	for _, suf := range []string{"detik", "dtk", "s"} {
		clean = strings.TrimSpace(strings.TrimSuffix(clean, suf))
	}
	v, err := strconv.Atoi(clean)
	if err != nil || v < 5 || v > 300 {
		return 0, false, false
	}
	return v, false, true
}

func parseJedaPerSemuaGrupInput(raw string) (int, bool, bool) {
	if isDisableCommand(raw) {
		return 0, true, true
	}
	clean := strings.ToLower(strings.TrimSpace(raw))
	switch clean {
	case "4 jam", "4jam", "4j":
		return 240, false, true
	case "6 jam", "6jam", "6j":
		return 360, false, true
	}
	for _, suf := range []string{"menit", "mnt", "m"} {
		clean = strings.TrimSpace(strings.TrimSuffix(clean, suf))
	}
	v, err := strconv.Atoi(clean)
	if err != nil {
		return 0, false, false
	}
	if (v >= 2 && v <= 120) || v == 240 || v == 360 {
		return v, false, true
	}
	return 0, false, false
}

func parseSkipFloodWaitInput(raw string) (int, bool, bool) {
	if isDisableCommand(raw) {
		return 0, true, true
	}
	clean := strings.TrimSpace(strings.TrimPrefix(raw, "⏱️ "))
	lower := strings.ToLower(clean)
	for _, suf := range []string{"menit", "mnt", "m"} {
		if strings.HasSuffix(lower, suf) {
			numStr := strings.TrimSpace(strings.TrimSuffix(lower, suf))
			m, err := strconv.Atoi(numStr)
			if err != nil {
				return 0, false, false
			}
			sec := m * 60
			if sec < 10 || sec > 3600 {
				return 0, false, false
			}
			return sec, false, true
		}
	}
	for _, suf := range []string{"detik", "dtk", "s"} {
		lower = strings.TrimSpace(strings.TrimSuffix(lower, suf))
	}
	sec, err := strconv.Atoi(lower)
	if err != nil || sec < 10 || sec > 3600 {
		return 0, false, false
	}
	return sec, false, true
}

