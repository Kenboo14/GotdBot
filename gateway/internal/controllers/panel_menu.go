package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strings"
)

func (h *Handler) ShowMainMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, userName string) {
	h.setPanelState(chatID, "")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtMainMenu(userID, userName),
		ParseMode:   "HTML",
		ReplyMarkup: assets.MainMenuMarkup,
	})
}

func (h *Handler) ShowExpertExtra(ctx context.Context, b *botgotd.Bot, chatID int64) {
	text := `<h1>⚡ Expert Extra Features</h1>
<table border="1">
  <tr><th>🛠️ Fitur</th><th>📌 Keterangan</th></tr>
  <tr><td>⏳ Jeda &amp; Skip FloodWait</td><td>Pengaturan Jeda &amp; Skip FloodWait berlaku juga untuk set Expert Extra</td></tr>
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
		assets.BtnJedaAman,
		assets.BtnJedaOtomatis,
		assets.BtnJedaPerGrup,
		assets.BtnJedaPerSemuaGrup,
		assets.BtnSkipFloodWait,
		assets.BtnSettings,
		assets.BtnUpgradeToken,
		assets.BtnGantiToken,
		assets.BtnAmbilToken,
		assets.BtnPindahAkses,
		assets.BtnBackupData,
		assets.BtnRestoreData,
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
		assets.BtnSemuaTopic, assets.BtnHapusTopic,
		assets.BtnSebarKeRC,
		assets.BtnHapusSemuaGrup,
		assets.BtnHapusSemuaList,
		assets.BtnResetKonfigurasi,
		assets.BtnBatal, "🔙 Batal 🔙":
		return true
	default:
		return isRestartBrowseButton(text)
	}
}

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

	if text == assets.BtnControl || text == assets.BtnControlAlt || strings.EqualFold(text, "control") || text == "/control" {
		username := ""
		if update.Message.From != nil {
			username = update.Message.From.Username
		}
		h.ShowControlMenu(ctx, b, chatID, userID, username)
		return true
	}

	if text == assets.BtnBantuan || strings.EqualFold(text, "bantuan") || text == "/help" {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true
	}

	if text == assets.BtnShop || strings.EqualFold(text, "shop") {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		h.renderShopCatalog(ctx, b, chatID, effectiveUserID, 0)
		return true
	}

	effectiveUserID := h.GetEffectiveUserID(chatID, userID)
	effectiveUserName := userName
	if effectiveUserID != userID {
		if tName := h.GetAccountName(effectiveUserID); tName != "" {
			effectiveUserName = tName
		}
	}

	if h.HandlePanelInput(ctx, b, chatID, effectiveUserID, text, update.Message) {
		return true
	}

	if !isKnownPanelButton(text) {
		return false
	}

	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, effectiveUserID)
	}

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

	if isRestartBrowseButton(text) {
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true
	}

	return h.routePanelAction(ctx, b, chatID, effectiveUserID, effectiveUserName, text)
}

func (h *Handler) allowTopicFeature(ctx context.Context, b *botgotd.Bot, chatID, userID int64) bool {
	if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowTopic {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Topic Forum & List Topic", "Pro", assets.GrupListMenuMarkup)
		return false
	}
	return true
}
