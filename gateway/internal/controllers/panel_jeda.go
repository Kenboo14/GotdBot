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

	text := fmt.Sprintf(`<h1>⏳ Atur Jeda Siaran</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai Aktif</th></tr>
  <tr><td>🕒 Jeda Sekarang</td><td><code>%s</code></td></tr>
  <tr><td>⏭️ Skip FloodWait</td><td><code>%s</code></td></tr>
</table>
<table border="1">
  <tr><th>🔘 Opsi Jeda</th><th>📘 Fungsi</th></tr>
  <tr><td>🎁 Jeda Aman 🎁</td><td>Jarak antar-grup 200–400 detik (paling aman)</td></tr>
  <tr><td>💡 Jeda Otomatis 💡</td><td>Menyesuaikan jeda dari jumlah grup Anda</td></tr>
  <tr><td>🔗 Jeda Per Grup 🔗</td><td>Jeda 5–300 detik antar-grup (cocok &gt; 50 grup)</td></tr>
  <tr><td>🖇️ Jeda Per Semua Grup 🖇️</td><td>Jeda 2–120m atau 4/6 jam antar-putaran (&lt; 30 grup)</td></tr>
  <tr><td>⏭️ Skip FloodWait ⏭️</td><td>Lewati grup jika FloodWait melewati batas pilihan</td></tr>
</table>
<blockquote expandable>⚠️ Jeda Per Grup dan Jeda Per Semua Grup bisa menyala bersamaan; masing-masing dimatikan lewat <b>🔕 Matikan 🔕</b>. Bila userbot aktif 24 jam, pakai minimal 30 detik (per grup) atau 20 menit (per putaran). Berlaku juga untuk set Expert Extra.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`,
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
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowManualJeda {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Jeda Otomatis", "Pro", assets.AturJedaMarkup)
		return
	}
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.DelayMode = "otomatis"
	cfg.Delay = 0
	cfg.DelayAllGroups = 0
	_ = h.db.SaveUserConfig(ctx, cfg)
	h.sendJedaSaved(ctx, b, chatID, userID, cfg)
}

func (h *Handler) PromptJedaPerGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowManualJeda {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Jeda Per Grup", "Pro", assets.AturJedaMarkup)
		return
	}
	h.setPanelState(chatID, "waiting_jeda_per_grup")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)

	text := fmt.Sprintf(`<h1>🔗 Pilih Jeda Per Grup (Detik)</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🕒 Jeda Sekarang</td><td><code>%s</code></td></tr>
  <tr><td>📏 Rentang Valid</td><td><code>5–300 detik</code></td></tr>
</table>
<blockquote expandable>Ini jarak antar-grup, bukan jarak sesudah semua grup. Tekan <b>🔕 Matikan 🔕</b> untuk mematikan jeda per grup tanpa mengubah jeda per semua grup. Pakai 30 detik ke atas bila userbot aktif 24 jam.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`, html.EscapeString(cfg.FormatJedaSummary(gCount)))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.JedaPerGrupMarkup,
	})
}

func (h *Handler) PromptJedaPerSemuaGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowManualJeda {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Jeda Per Semua Grup", "Pro", assets.AturJedaMarkup)
		return
	}
	h.setPanelState(chatID, "waiting_jeda_per_semua_grup")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)

	text := fmt.Sprintf(`<h1>🖇️ Pilih Jeda Per Semua Grup (Menit)</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🕒 Jeda Sekarang</td><td><code>%s</code></td></tr>
  <tr><td>📏 Pilihan Valid</td><td><code>2–120 menit, 240 (4j), 360 (6j)</code></td></tr>
</table>
<blockquote expandable>Ini jarak sesudah semua grup terkirim (antar-putaran). Tekan <b>🔕 Matikan 🔕</b> untuk mematikan jeda putaran tanpa mengubah jeda per grup. Pakai 20 menit ke atas bila userbot aktif 24 jam.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`, html.EscapeString(cfg.FormatJedaSummary(gCount)))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.JedaPerSemuaGrupMarkup,
	})
}

func (h *Handler) PromptSkipFloodWait(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowSkipFloodWait {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Skip FloodWait", "Pro", assets.AturJedaMarkup)
		return
	}
	h.setPanelState(chatID, "waiting_skip_floodwait")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>⏭️ Atur Skip FloodWait (Detik)</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>⏭️ Status Saat Ini</td><td><code>%s</code></td></tr>
  <tr><td>📏 Rentang Custom</td><td><code>10–3600 detik</code></td></tr>
</table>
<blockquote expandable>Bila Telegram meminta akun menunggu (FloodWait) lebih lama dari batas pilihan Anda, grup itu dilewati dan siaran lanjut ke grup berikutnya. Berlaku juga untuk set Expert Extra; tekan <b>🔕 Matikan 🔕</b> = selalu menunggu.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`, html.EscapeString(cfg.FormatSkipFloodWait()))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.SkipFloodWaitMarkup,
	})
}

func (h *Handler) sendJedaSaved(ctx context.Context, b *botgotd.Bot, chatID, userID int64, cfg *databases.JasebConfig) {
	gCount, _ := h.db.CountGroupsAndLists(ctx, userID)
	text := fmt.Sprintf(`<h1>✅ Jeda Tersimpan</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai Aktif</th></tr>
  <tr><td>🕒 Jeda Sekarang</td><td><code>%s</code></td></tr>
  <tr><td>⏭️ Skip FloodWait</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Konfigurasi jeda berhasil diperbarui dan siap digunakan oleh worker userbot.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`, html.EscapeString(cfg.FormatJedaSummary(gCount)), html.EscapeString(cfg.FormatSkipFloodWait()))

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
			Text:        `<h1>⚠️ Pilihan Jeda Belum Valid</h1><blockquote expandable>Silakan pilih salah satu opsi jeda menggunakan tombol keyboard di bawah ini.</blockquote><hr/><p>GotdBot • Delay &amp; Anti-Flood Manager</p>`,
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
			Text:        `<h1>⚠️ Nilai Jeda Tidak Valid</h1><blockquote expandable>Masukkan angka <b>5–300</b> detik atau tekan <b>🔕 Matikan 🔕</b>.</blockquote><hr/><p>GotdBot • Delay &amp; Anti-Flood Manager</p>`,
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
			Text:        `<h1>⚠️ Nilai Jeda Putaran Tidak Valid</h1><blockquote expandable>Pilih <b>2–120</b> menit, <b>240</b> (4 jam), <b>360</b> (6 jam), atau tekan <b>🔕 Matikan 🔕</b>.</blockquote><hr/><p>GotdBot • Delay &amp; Anti-Flood Manager</p>`,
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
			msg := fmt.Sprintf(`<h1>✅ Skip FloodWait Tersimpan</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai Aktif</th></tr>
  <tr><td>⏭️ Status</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Pengaturan batas FloodWait berhasil diperbarui dan berlaku untuk broadcast utama maupun Expert Extra.</blockquote>
<hr/>
<p>GotdBot • Delay &amp; Anti-Flood Manager</p>`, html.EscapeString(cfg.FormatSkipFloodWait()))
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
			Text:        `<h1>⚠️ Batas FloodWait Tidak Valid</h1><blockquote expandable>Pilih tombol preset atau ketik angka <b>10–3600</b> detik, atau tekan <b>🔕 Matikan 🔕</b>.</blockquote><hr/><p>GotdBot • Delay &amp; Anti-Flood Manager</p>`,
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
	for _, em := range []string{"🔕", "⏱️"} {
		clean = strings.TrimSpace(strings.TrimPrefix(clean, em))
		clean = strings.TrimSpace(strings.TrimSuffix(clean, em))
	}
	lower := strings.ToLower(clean)
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
	clean := strings.TrimSpace(raw)
	for _, em := range []string{"⏱️", "⏰"} {
		clean = strings.TrimSpace(strings.TrimPrefix(clean, em))
		clean = strings.TrimSpace(strings.TrimSuffix(clean, em))
	}
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

