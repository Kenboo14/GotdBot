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
	"time"
)

// ShowStatus renders the userbot status dashboard
func (h *Handler) ShowStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")

	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, lCount := h.db.CountGroupsAndLists(ctx, userID)

	skipFw := "Mati"
	if cfg.SkipFloodWait {
		skipFw = "Aktif"
	}

	safeMode := "Nonaktif"
	if cfg.Safemode {
		safeMode = "Aktif"
	}

	rows := []assets.StatusRow{
		{Label: "🏷️ UserID", Value: fmt.Sprintf("%d", cfg.OwnerID)},
		{Label: "🧩 Tipe", Value: html.EscapeString(cfg.Type)},
		{Label: "👑 Mode", Value: html.EscapeString(cfg.Mode)},
		{Label: "🕒 Jeda", Value: fmt.Sprintf("%d detik", cfg.Delay)},
		{Label: "🌐 Target Grup", Value: fmt.Sprintf("%d grup", gCount)},
		{Label: "📚 Materi List", Value: fmt.Sprintf("%d list", lCount)},
		{Label: "⚠️ Safemode", Value: safeMode},
		{Label: "⏭️ Skip Flood", Value: skipFw},
		{Label: "⏱️ Timer", Value: html.EscapeString(cfg.Timer)},
		{Label: "🚀 Status", Value: html.EscapeString(cfg.Status)},
		{Label: "🔥 Masa Aktif", Value: html.EscapeString(cfg.MasaAktif)},
		{Label: "🔔 Notifikasi", Value: html.EscapeString(cfg.Notifikasi)},
		{Label: "✉️ Email", Value: html.EscapeString(cfg.Email)},
		{Label: "👤 Admin", Value: html.EscapeString(cfg.Admin)},
	}

	tbl := assets.RichStatusTable(rows)
	text := assets.RichStatusDashboard(tbl, html.EscapeString(cfg.TindakanTerkini), html.EscapeString(cfg.TindakanTerakhir))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

// PromptAturJeda displays delay settings
func (h *Handler) PromptAturJeda(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_jeda")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>⏱️ Atur Jeda Broadcast</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>⏳ Jeda Saat Ini</td><td><code>%d detik</code></td></tr>
</table>
<blockquote expandable>Pilih interval cepat dari tombol keyboard di bawah, atau ketik angka detik secara langsung.</blockquote>
<hr/>
<p>Silakan tentukan interval jeda siaran.</p>`, cfg.Delay)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.AturJedaMarkup,
	})
}

// ToggleSafemode toggles safemode
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

// PromptUbahMode displays mode options
func (h *Handler) PromptUbahMode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_mode")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>👑 Ubah Mode Siaran</h1>
<table border="1">
  <tr><th>📌 Mode</th><th>Karakteristik</th></tr>
  <tr><td>👑 Normal</td><td>Kecepatan seimbang &amp; interval stabil</td></tr>
  <tr><td>⚡ Fast</td><td>Kecepatan tinggi, minim jeda waktu</td></tr>
  <tr><td>🔕 Silent</td><td>Tanpa notifikasi suara di grup target</td></tr>
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

// ToggleStatus toggles userbot broadcast status
func (h *Handler) ToggleStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	var text string
	if strings.HasPrefix(cfg.Status, "Berhenti") {
		cfg.Status = "Berjalan"
		cfg.TindakanTerkini = "Siaran otomatis diaktifkan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "start_panel_broadcast"}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("start_panel_broadcast broker error: %v", err)
		}
		text = "🚀 <b>Siaran dimulai.</b>\n\nStatus: <code>Berjalan</code>\n\n<i>Userbot aktif mengirim materi ke seluruh grup target.</i>"
	} else {
		cfg.Status = "Berhenti"
		cfg.TindakanTerkini = "Siaran otomatis dimatikan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "stop_panel_broadcast"}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("stop_panel_broadcast broker error: %v", err)
		}
		text = "🛑 <b>Siaran dihentikan.</b>\n\nStatus: <code>Berhenti</code>\n\n<i>Seluruh antrean pengiriman dinonaktifkan.</i>"
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

// ToggleNotifikasi toggles notification setting
func (h *Handler) ToggleNotifikasi(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	if strings.HasPrefix(cfg.Notifikasi, "Aktif") {
		cfg.Notifikasi = "Nonaktif"
	} else {
		cfg.Notifikasi = "Aktif · Kirim ke Saya"
	}
	_ = h.db.SaveUserConfig(ctx, cfg)

	text := fmt.Sprintf(
		"✅ <b>Notifikasi diperbarui.</b>\n\nStatus: <code>%s</code>",
		html.EscapeString(cfg.Notifikasi),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

// PromptWatermark prompts user for watermark text
func (h *Handler) PromptWatermark(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lic, _ := h.db.GetActiveUserLicense(ctx, chatID)
	tier := "Free"
	if lic != nil && lic.Tier != "" {
		tier = lic.Tier
	}
	if tier == "Free" || tier == "Pro" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("⚠️ Lisensi Anda (<b>%s</b>) tidak mendukung Custom Watermark. Silakan upgrade ke lisensi <b>PRO++</b> atau <b>VIP</b>.", tier),
			ParseMode: "HTML",
		})
		return
	}

	h.setPanelState(chatID, "waiting_set_watermark")
	cfg := h.db.GetUserConfig(ctx, userID)

	wm := cfg.Watermark
	if wm == "" {
		wm = "<i>(Belum diatur)</i>"
	} else {
		wm = html.EscapeString(wm)
	}

	text := fmt.Sprintf(`<h1>📌 Custom Watermark</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🏷️ Watermark Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan teks watermark baru. Watermark akan disematkan pada baris paling akhir setiap pesan siaran.</blockquote>
<hr/>
<p>Kirimkan teks sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, wm)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

// PromptEmail prompts user for email
func (h *Handler) PromptEmail(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
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

// PromptAdmin prompts user for admin ID
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

// PromptTimer prompts user for timer
func (h *Handler) PromptTimer(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_timer")
	cfg := h.db.GetUserConfig(ctx, userID)

	curr := "🔴 Nonaktif"
	if cfg.Timer != "" {
		curr = html.EscapeString(cfg.Timer)
	}

	text := fmt.Sprintf(`<h1>⏱️ Timer Siaran Otomatis</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>⏰ Jadwal Saat Ini</td><td><b>%s</b></td></tr>
</table>
<blockquote expandable>Pilih interval pengulangan siaran berkala otomatis dari tombol keyboard di bawah.</blockquote>
<hr/>
<p>Pilih interval timer yang diinginkan.</p>`, curr)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.TimerMarkup,
	})
}

// PromptReset prompts user for reset options
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
