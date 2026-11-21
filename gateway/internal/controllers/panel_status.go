package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"time"
)

func (h *Handler) renderStatusDashboard(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	h.setPanelState(chatID, "")

	cfg := h.db.GetUserConfig(ctx, userID)
	h.syncConfigMasaAktif(ctx, userID, cfg)
	gCount, lCount := h.db.CountGroupsAndLists(ctx, userID)

	safeMode := "Nonaktif"
	if cfg.Safemode {
		safeMode = "Aktif"
	}

	policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
	tierDisplay := "Belum diatur"
	if enforced {
		tierDisplay = policy.Tier
	}

	rows := []assets.StatusRow{
		{Label: "🏷️ UserID", Value: fmt.Sprintf("%d", cfg.OwnerID)},
		{Label: "🎖️ Paket", Value: html.EscapeString(tierDisplay)},
		{Label: "🧩 Tipe", Value: html.EscapeString(cfg.Type)},
		{Label: "👑 Mode", Value: html.EscapeString(cfg.Mode)},
		{Label: "🕒 Jeda", Value: html.EscapeString(cfg.FormatJedaSummary(gCount))},
		{Label: "🌐 Target Grup", Value: fmt.Sprintf("%d grup", gCount)},
		{Label: "📚 Materi List", Value: fmt.Sprintf("%d list", lCount)},
		{Label: "⚠️ Safemode", Value: safeMode},
		{Label: "⏭️ Skip Flood", Value: html.EscapeString(cfg.FormatSkipFloodWait())},
		{Label: "⏱️ Timer", Value: html.EscapeString(cfg.FormatTimerDisplay())},
		{Label: "🚀 Status", Value: html.EscapeString(cfg.Status)},
		{Label: "🔥 Masa Aktif", Value: html.EscapeString(cfg.MasaAktif)},
	}
	if enforced && policy.DailySendLimit > 0 {
		rows = append(rows, assets.StatusRow{Label: "📨 Kuota Harian", Value: fmt.Sprintf("Maks %d pesan/hari (reset 00:00 WIB)", policy.DailySendLimit)})
	}
	rows = append(rows,
		assets.StatusRow{Label: "🔔 Notifikasi", Value: html.EscapeString(normalizeNotifikasiMode(cfg.Notifikasi))},
		assets.StatusRow{Label: "✉️ Email", Value: html.EscapeString(cfg.Email)},
		assets.StatusRow{Label: "👤 Admin", Value: html.EscapeString(cfg.Admin)},
	)

	tbl := assets.RichStatusTable(rows)
	toggleBtn := `<tg-button type="callback_data" data="panel:status:toggle" style="danger">🛑 Hentikan Siaran</tg-button>`
	if strings.HasPrefix(cfg.Status, "Berhenti") {
		toggleBtn = `<tg-button type="callback_data" data="panel:status:toggle" style="primary">🚀 Mulai Siaran</tg-button>`
	}
	actions := "\n<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:status:refresh\" style=\"primary\">🔄 Refresh Status</tg-button>\n  " + toggleBtn + "\n</tg-button-row>\n<hr/>"
	base := assets.RichStatusDashboard(tbl, html.EscapeString(cfg.TindakanTerkini), html.EscapeString(cfg.TindakanTerakhir))
	text := strings.Replace(base, "<hr/>", actions, 1)

	if editMsgID > 0 {
		_, _ = editMonitorRichMessageWithMarkup(ctx, b, chatID, editMsgID, text, nil)
		return
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

func (h *Handler) ShowStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.renderStatusDashboard(ctx, b, chatID, userID, 0)
}

func (h *Handler) ToggleSafemode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.Safemode = !cfg.Safemode
	_ = h.db.SaveUserConfig(ctx, cfg)

	statusStr := "🔴 Nonaktif"
	if cfg.Safemode {
		statusStr = "🟢 Aktif"
	}

	text := fmt.Sprintf(`<h1>🛡️ Pengaturan Safemode</h1>
<table border="1">
  <tr><th>📌 Fitur</th><th>Status</th></tr>
  <tr><td>🛡️ Safemode Anti-Flood</td><td><b>%s</b></td></tr>
</table>
<blockquote expandable>Safemode secara otomatis mengontrol delay dinamis untuk melindungi akun Telegram dari batasan limitasi spam.</blockquote>
<hr/>
<p>Konfigurasi berhasil disimpan.</p>`, statusStr)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

func (h *Handler) PromptUbahMode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_mode")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>👑 Ubah Mode Siaran</h1>
<table border="1">
  <tr><th>📌 Mode</th><th>Karakteristik</th></tr>
  <tr><td>👑 Normal</td><td>Kecepatan seimbang &amp; interval stabil</td></tr>
  <tr><td>⚡ Fast</td><td>Kecepatan tinggi, minim jeda waktu</td></tr>
  <tr><td>🔕 Silent</td><td>Tanpa notifikasi suara di grup target</td></tr>
  <tr><td>📨 Forward</td><td>Diteruskan dari pesan asli (khusus Pro++ / Vip)</td></tr>
</table>
<blockquote expandable>Mode aktif saat ini: <b>%s</b></blockquote>
<hr/>
<p>Silakan pilih mode siaran dari tombol keyboard di bawah.</p>`, html.EscapeString(cfg.Mode))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.UbahModeMarkup,
	})
}

func (h *Handler) executeToggleStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) (bool, string, string) {
	cfg := h.db.GetUserConfig(ctx, userID)
	if strings.HasPrefix(cfg.Status, "Berhenti") {
		if ok, expAt := h.ensureActiveLicenseOrExpire(ctx, b, userID); !ok {
			return false, expAt, ""
		}
		cfg.Status = "Berjalan"
		cfg.TindakanTerkini = "Siaran otomatis diaktifkan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "start_panel_broadcast"}
		if h.broker != nil {
			if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
				log.Printf("start_panel_broadcast broker error: %v", err)
			}
		}
		return true, "", "Berjalan"
	}

	cfg.Status = "Berhenti"
	cfg.TindakanTerkini = "Siaran otomatis dimatikan"
	cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
	_ = h.db.SaveUserConfig(ctx, cfg)

	payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "stop_panel_broadcast"}
	if h.broker != nil {
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("stop_panel_broadcast broker error: %v", err)
		}
	}
	return true, "", "Berhenti"
}

func (h *Handler) ToggleStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	ok, expAt, newStatus := h.executeToggleStatus(ctx, b, chatID, userID)
	if !ok {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>❌ Masa Aktif Token Berakhir</h1>
<table border="1">
  <tr><th>⏳ Berakhir Pada</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Masa aktif slot token Anda telah habis. Silakan tekan <b>/start</b> untuk memasukkan token baru atau buka <b>⚙️ Settings ⚙️ → 🔄 Ganti Token 🔄</b>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(expAt)),
			ParseMode:   "HTML",
			ReplyMarkup: assets.RemoveKeyboard,
		})
		return
	}

	var text string
	if newStatus == "Berjalan" {
		text = `<h1>🚀 Siaran Dimulai</h1>
<table border="1">
  <tr><th>📌 Status</th><td><code>🟢 Berjalan</code></td></tr>
</table>
<blockquote expandable>Userbot aktif mengirim materi promosi ke seluruh grup target sesuai jeda dan mode aktif.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`
	} else {
		text = `<h1>🛑 Siaran Dihentikan</h1>
<table border="1">
  <tr><th>📌 Status</th><td><code>🔴 Berhenti</code></td></tr>
</table>
<blockquote expandable>Seluruh antrean pengiriman siaran dinonaktifkan oleh pengguna.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

func (h *Handler) ToggleNotifikasi(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")
	h.renderNotifikasiView(ctx, b, chatID, userID, 0)
}

func (h *Handler) PromptWatermark(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lic, _ := h.db.GetActiveUserLicense(ctx, userID)
	if lic == nil && chatID != userID {
		lic, _ = h.db.GetActiveUserLicense(ctx, chatID)
	}
	tier := "Free"
	if lic != nil && lic.Tier != "" {
		tier = databases.NormalizeTier(lic.Tier)
	}
	if !databases.GetTierPolicy(tier).AllowCustomWatermark {
		h.denyTierFeature(ctx, b, chatID, tier, "Custom Watermark", "Pro++", assets.StatusMenuMarkup)
		return
	}

	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_set_watermark")
	cfg := h.db.GetUserConfig(ctx, userID)

	hasWM := cfg.Watermark != "" || cfg.WatermarkCacheID > 0
	wm := "<i>(Belum diatur)</i>"
	if cfg.Watermark != "" {
		wm = html.EscapeString(cfg.Watermark)
	} else if cfg.WatermarkCacheID > 0 {
		wm = fmt.Sprintf("<code>Media Cache #%d</code>", cfg.WatermarkCacheID)
	}

	deleteBtn := ""
	markup := assets.CancelOnlyMarkup
	if hasWM {
		deleteBtn = "\n<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:watermark:delete\" style=\"danger\">🗑️ Hapus Watermark</tg-button>\n</tg-button-row>"
		markup = assets.WatermarkPromptMarkup
	}

	text := fmt.Sprintf(`<h1>📌 Custom Watermark</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🏷️ Watermark Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan teks atau media watermark baru. Watermark akan disematkan pada pesan siaran Anda.</blockquote>%s
<hr/>
<p>Kirimkan watermark sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, wm, deleteBtn)

	msg, err := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
	if err == nil && msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
		h.trackDetailMessageIDs(ctx, chatID, []int{msg.ID})
	}
}

func (h *Handler) PromptEmail(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowEmailBackup {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Email Cadangan Darurat", "Pro", assets.StatusMenuMarkup)
		return
	}
	h.setPanelState(chatID, "waiting_set_email")
	cfg := h.db.GetUserConfig(ctx, userID)

	em := cfg.Email
	if em == "" {
		em = "<i>(Belum diatur)</i>"
	} else {
		em = html.EscapeString(em)
	}

	text := fmt.Sprintf(`<h1>✉️ Email Notifikasi</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>📧 Email Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan alamat email valid untuk menerima laporan siaran berkala dan notifikasi alert sistem.</blockquote>
<hr/>
<p>Kirimkan alamat email sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, em)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) PromptAdmin(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_admin")
	cfg := h.db.GetUserConfig(ctx, userID)

	adm := cfg.Admin
	if adm == "" {
		adm = "<i>(Belum diatur)</i>"
	} else {
		adm = html.EscapeString(adm)
	}

	text := fmt.Sprintf(`<h1>👤 Pengaturan Admin / Sudo</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>👑 Admin ID Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan Telegram User ID atau Username yang diberikan akses wewenang pengelolaan userbot.</blockquote>
<hr/>
<p>Kirimkan User ID sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, adm)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) PromptReset(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	text := `<h1>🗑️ Pusat Reset Data</h1>
<blockquote expandable>⚠️ <b>PERINGATAN:</b> Tindakan reset akan menghapus data yang tersimpan di database permanen. Pastikan Anda telah melakukan konfirmasi sebelum melanjutkan.</blockquote>
<table border="1">
  <tr><th>Opsi Reset</th><th>Cakupan</th></tr>
  <tr><td>🗑️ Reset Grup</td><td>Menghapus seluruh daftar grup target</td></tr>
  <tr><td>🗑️ Reset List</td><td>Menghapus seluruh materi promosi tersimpan</td></tr>
  <tr><td>🗑️ Reset Semua</td><td>Mengembalikan semua konfigurasi ke default</td></tr>
</table>
<hr/>
<p>Pilih jenis reset dari tombol keyboard navigasi di bawah.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.ResetMarkup,
	})
}
