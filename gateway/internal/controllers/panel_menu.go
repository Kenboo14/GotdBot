package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"strings"
	"time"
)

var (
	mainMenuMarkup          = assets.MainMenuMarkup
	statusMenuMarkup        = assets.StatusMenuMarkup
	grupListMenuMarkup      = assets.GrupListMenuMarkup
	tambahGrupOptionsMarkup = assets.TambahGrupOptionsMarkup
	cancelOnlyMarkup        = assets.CancelOnlyMarkup
	aturJedaMarkup          = assets.AturJedaMarkup
	ubahModeMarkup          = assets.UbahModeMarkup
	timerMarkup             = assets.TimerMarkup
	resetMarkup             = assets.ResetMarkup
	hapusListMarkup         = assets.HapusListMarkup
	hapusGrupMarkup         = assets.HapusGrupMarkup
)

// ShowMainMenu renders the main control panel
func (h *Handler) ShowMainMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, userName string) {
	h.setPanelState(chatID, "")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtMainMenu(userID, userName),
		ParseMode:   "HTML",
		ReplyMarkup: assets.MainMenuMarkup,
	})
}

// ShowShop displays Shop info
func (h *Handler) ShowShop(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("<h1>🛒 GotdBot Store &amp; Layanan Premium</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>💎 Layanan</th><th>⚡ Keunggulan</th></tr>\n")
	sb.WriteString("  <tr><td>👑 VIP Userbot</td><td>Masa aktif tanpa batas &amp; prioritas MTProto</td></tr>\n")
	sb.WriteString("  <tr><td>🌐 Multi-Account Hub</td><td>Koneksikan puluhan nomor paralel</td></tr>\n")
	sb.WriteString("  <tr><td>🚀 Dedicated Instance</td><td>Bot gateway privat eksklusif</td></tr>\n")
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n<p>Untuk konsultasi atau upgrade paket, silakan hubungi admin botgotd.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowExpertExtra displays Expert Extra info
func (h *Handler) ShowExpertExtra(ctx context.Context, b *botgotd.Bot, chatID int64) {
	text := `<h1>⚡ Expert Extra Features</h1>
<table border="1">
  <tr><th>🛠️ Fitur</th><th>📌 Keterangan</th></tr>
  <tr><td>🛡️ FloodWait Bypass</td><td>Antrean adaptif saat sinyal flood Telegram</td></tr>
  <tr><td>🔍 Anti-Duplicate</td><td>Cegah pesan ganda ke dialog sama</td></tr>
  <tr><td>⚡ Pure-Go MTProto</td><td>High performance gotd/td + SQLite WAL</td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="assist_server:refresh" style="primary">🔄 Server Status</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Multi-Configuration VPS Control Panel</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowBantuan displays Bantuan info
func (h *Handler) ShowBantuan(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	effectiveID := h.GetEffectiveUserID(chatID, userID)
	hasActive := false
	if h.db != nil && effectiveID > 0 {
		hasActive = h.db.HasActiveSession(ctx, effectiveID)
	}

	markup := any(assets.CreateChoiceMarkup)
	if hasActive {
		markup = assets.MainMenuMarkup
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtBantuan,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
}

// ShowSettings delegates to ShowStatus
func (h *Handler) ShowSettings(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.ShowStatus(ctx, b, chatID, userID)
}

// ShowFeed displays updates feed
func (h *Handler) ShowFeed(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("<h1>📰 Informasi &amp; Berita GotdBot</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>🚀 Versi</th><th><code>v2.5.0-HighPerformance</code></th></tr>\n")
	sb.WriteString("  <tr><td>🔄 Sinkronisasi</td><td>MongoDB Cloud Atlas &amp; SQLite WAL</td></tr>\n")
	sb.WriteString("  <tr><td>⚡ Messaging</td><td>Pure Gotd MTProto Layer 227</td></tr>\n")
	sb.WriteString("  <tr><td>📦 Manajemen</td><td>Grup &amp; List Terintegrasi</td></tr>\n")
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/><p>Dapatkan pembaruan rilis berkala di channel resmi GotdBot.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowWebapp displays Webapp info
func (h *Handler) ShowWebapp(ctx context.Context, b *botgotd.Bot, chatID int64) {
	text := `<h1>🌐 GotdBot Web Dashboard</h1>
<blockquote expandable>Akses panel kendali visual untuk memantau performa siaran secara langsung melalui browser atau Telegram Mini App.</blockquote>
<hr/>
<p>Integrasi Web Dashboard sedang dalam tahap penyempurnaan cloud.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

func isKnownPanelButton(text string) bool {
	switch text {
	case assets.BtnHome, "/menu",
		assets.BtnStatus, "/status",
		assets.BtnShop,
		assets.BtnGrupList,
		assets.BtnExpert,
		assets.BtnReset,
		assets.BtnAturJeda,
		assets.BtnSettings,
		assets.BtnFeed,
		assets.BtnWebapp,
		assets.BtnCustomWatermark,
		assets.BtnAturEmail,
		assets.BtnSafemode,
		assets.BtnTimer,
		assets.BtnUbahMode,
		assets.BtnAdmin,
		assets.BtnUbahStatus,
		assets.BtnNotifikasi,
		assets.BtnCekList,
		assets.BtnCekGrup,
		assets.BtnTambahList,
		assets.BtnTambahGrup,
		assets.BtnTambahkanSemuaGrup,
		assets.BtnTambahGrupManual,
		assets.BtnCekTopic, assets.BtnListTopic,
		assets.BtnTopic,
		assets.BtnSebarKeRC,
		assets.BtnHapusSemuaGrup,
		assets.BtnHapusSemuaList,
		assets.BtnResetKonfigurasi,
		assets.BtnBatal:
		return true
	default:
		return isRestartBrowseButton(text)
	}
}

// RoutePanelButton handles any ReplyKeyboard button click
func (h *Handler) RoutePanelButton(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) bool {
	if update.Message == nil || update.Message.From == nil {
		return false
	}
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	h.SetAccountName(userID, userName)
	text := strings.TrimSpace(update.Message.Text)

	// Intersep tombol Control
	if text == assets.BtnControl || text == assets.BtnControlAlt || strings.EqualFold(text, "control") || text == "/control" {
		username := ""
		if update.Message.From != nil {
			username = update.Message.From.Username
		}
		h.ShowControlMenu(ctx, b, chatID, userID, username)
		return true
	}

	// Intersep tombol Bantuan
	if text == assets.BtnBantuan || strings.EqualFold(text, "bantuan") || text == "/help" {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true
	}

	effectiveUserID := h.GetEffectiveUserID(chatID, userID)
	effectiveUserName := userName
	if effectiveUserID != userID {
		if tName := h.GetAccountName(effectiveUserID); tName != "" {
			effectiveUserName = tName
		}
	}

	// Cek apakah user sedang berada dalam input state
	if h.HandlePanelInput(ctx, b, chatID, effectiveUserID, text, update.Message) {
		return true
	}

	// Hanya tangani jika teks benar-benar salah satu tombol ReplyKeyboard panel yang dikenali
	if !isKnownPanelButton(text) {
		return false
	}

	// Periksa apakah user memiliki sesi userbot yang aktif
	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, effectiveUserID)
	}

	// Jika belum memasang userbot dan tidak sedang me-remote akun aktif, tolak akses tombol kontrol panel
	if !hasActive {
		h.setPanelState(chatID, "")
		if text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel" {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "↩️ Tindakan dibatalkan.",
				ReplyMarkup: assets.CreateChoiceMarkup,
			})
			return true
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("<h1>👋 Halo, %s</h1>\n", html.EscapeString(userName)))
		sb.WriteString("<blockquote expandable>⚠️ <b>Sesi Userbot Belum Terpasang</b>\n")
		sb.WriteString("<i>Menu kontrol panel (Status, Grup &amp; List, Atur Jeda, dll.) hanya untuk akun yang sudah memasang userbot aktif atau akun admin yang sedang me-remote.</i></blockquote>\n\n")
		sb.WriteString("<p>Silakan gunakan <b>🔑 Input Token 🔑</b> atau <b>🚀 Buat Userbot 🚀</b> untuk memasang userbot Anda, atau gunakan <b>🎯 Control 🎯</b> jika Anda admin dari akun lain.</p>")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: assets.CreateChoiceMarkup,
		})
		return true
	}

	// Jika server baru restart (state kosong di RAM) dan user mengklik tombol dari Cek List / Cek Grup
	if isRestartBrowseButton(text) {
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true
	}

	switch text {
	// Main Menu Navigation
	case assets.BtnHome, "/menu":
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	case assets.BtnStatus, "/status":
		h.ShowStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnShop:
		h.ShowShop(ctx, b, chatID)
		return true

	case assets.BtnGrupList:
		h.ShowGrupList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnExpert:
		h.ShowExpertExtra(ctx, b, chatID)
		return true

	case assets.BtnReset:
		h.PromptReset(ctx, b, chatID)
		return true

	case assets.BtnAturJeda:
		h.PromptAturJeda(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBantuan:
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSettings:
		h.ShowSettings(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnFeed:
		h.ShowFeed(ctx, b, chatID)
		return true

	case assets.BtnWebapp:
		h.ShowWebapp(ctx, b, chatID)
		return true

	// Status Menu Actions
	case assets.BtnCustomWatermark:
		h.PromptWatermark(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAturEmail:
		h.PromptEmail(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSafemode:
		h.ToggleSafemode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTimer:
		h.PromptTimer(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahMode:
		h.PromptUbahMode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAdmin:
		h.PromptAdmin(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahStatus:
		h.ToggleStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnNotifikasi:
		h.ToggleNotifikasi(ctx, b, chatID, effectiveUserID)
		return true

	// Grup & List Actions
	case assets.BtnCekList:
		h.DisplayList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnCekGrup:
		h.DisplayGroup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahList:
		h.PromptTambahList(ctx, b, chatID)
		return true

	case assets.BtnTambahGrup:
		h.PromptTambahGrup(ctx, b, chatID)
		return true

	case assets.BtnTambahkanSemuaGrup:
		h.FetchAllGroupsFromAccount(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahGrupManual:
		h.PromptTambahGrupManual(ctx, b, chatID)
		return true

	case assets.BtnCekTopic, assets.BtnListTopic:
		h.CekTopic(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTopic:
		h.ToggleTopic(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSebarKeRC:
		h.SebarKeRC(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBatal:
		h.setPanelState(chatID, "")
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	// Reset Menu Sub-Actions
	case assets.BtnHapusSemuaGrup:
		_ = h.db.ClearGroups(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_groups"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaGrupSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnHapusSemuaList:
		_ = h.db.ClearLists(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaListSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnResetKonfigurasi:
		defaultCfg := &databases.UserConfig{
			OwnerID:          effectiveUserID,
			Type:             "Broadcast",
			Delay:            5,
			SkipFloodWait:    false,
			Timer:            "Belum diatur",
			Extra:            0,
			Mode:             "Normal",
			Safemode:         false,
			Status:           "Berhenti",
			MasaAktif:        "November 29, 2026 at 14:18",
			Notifikasi:       "Aktif · Kirim ke Saya",
			Email:            "Belum diatur",
			Admin:            "Belum diatur",
			Watermark:        "",
			TopicEnabled:     false,
			TindakanTerkini:  "Siaran otomatis dimatikan",
			TindakanTerakhir: "-",
			UpdatedAt:        time.Now(),
		}
		_ = h.db.SaveUserConfig(ctx, defaultCfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetKonfigurasiSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true
	}

	return false
}
