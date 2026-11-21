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
	"time"
)

func (h *Handler) HandlePindahAkses(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID ...int) {
	if h == nil || h.db == nil {
		return
	}
	policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
	if enforced && !policy.AllowLicenseToken {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Pindah Akses Token", "Pro", assets.SettingsMenuMarkup)
		return
	}
	lic, _ := h.db.GetActiveUserLicense(ctx, userID)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Tidak Ada Slot Token Aktif</h1><blockquote expandable>Anda belum memiliki slot token aktif untuk dipindahkan ke akun lain.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	h.setPanelState(chatID, "waiting_transfer_token_target")
	expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
	sisaDur := FormatDurationID(time.Until(lic.ExpiresAt))

	text := fmt.Sprintf(`<h1>🔀 Pindah Akses Token</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🎖️ Paket Aktif</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Sisa / Berlaku</td><td><code>%s</code> (<code>%s</code>)</td></tr>
  <tr><td>🔑 Kode Token</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable><b>Dua Cara Memindahkan Akses:</b>
1. Kirimkan <b>Telegram User ID</b> akun tujuan di chat ini agar bot langsung mengirimkan tombol klaim pemasangan ke user tersebut.
2. Atau salin dan berikan <b>Kode Token</b> di atas ke akun tujuan untuk dimasukkan lewat <b>/start → 🔑 Input Token 🔑</b> atau <b>⚙️ Settings ⚙️ → 🔑 Tambah Token 🔑</b>.

⚠️ Sesi userbot pada akun ini akan otomatis dicopot setelah user tujuan berhasil memasang userbot.</blockquote>
<hr/>
<p>Kirimkan <b>Telegram User ID</b> tujuan sekarang, atau tekan <b>⬅️ Batal ⬅️</b>.</p>`,
		html.EscapeString(databases.NormalizeTier(lic.Tier)),
		html.EscapeString(sisaDur),
		html.EscapeString(expStr),
		html.EscapeString(lic.TokenSHA),
	)

	if len(editMsgID) > 0 && editMsgID[0] > 0 {
		if _, err := editMonitorRichMessageWithMarkup(ctx, b, chatID, editMsgID[0], text, nil); err == nil {
			return
		}
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) handleTransferTargetInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	if isKnownPanelButton(text) {
		h.setPanelState(chatID, "")
		return false
	}
	effectiveID := h.GetEffectiveUserID(chatID, userID)
	trimmed := strings.TrimSpace(text)
	targetID, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || targetID <= 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Format User ID Tidak Valid</h1><blockquote expandable>Masukkan <b>Telegram User ID</b> berupa angka (contoh: <code>123456789</code>). User tujuan dapat mengecek ID lewat perintah <code>!id</code> atau bot pencari ID.</blockquote><hr/><p>Kirimkan User ID angka yang valid, atau tekan <b>⬅️ Batal ⬅️</b>.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return true
	}
	if targetID == effectiveID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ ID Tujuan Sama</h1><blockquote expandable>Anda memasukkan User ID akun Anda sendiri. Masukkan <b>Telegram User ID akun lain</b> yang akan menerima token ini.</blockquote><hr/><p>Kirimkan User ID tujuan atau tekan <b>⬅️ Batal ⬅️</b>.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return true
	}

	lic, _ := h.db.GetActiveUserLicense(ctx, effectiveID)
	if lic == nil || lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>❌ Token Aktif Tidak Ditemukan</h1><blockquote expandable>Slot token Anda sudah kedaluwarsa atau tidak tersedia untuk dipindahkan.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return true
	}

	h.Sessions.mu.Lock()
	h.Sessions.sessions[targetID] = &UserSession{
		ChatID:             targetID,
		State:              "waiting_create_choice",
		PendingMasterToken: lic.MasterToken,
		PendingTokenSHA:    lic.TokenSHA,
		PendingTier:        lic.Tier,
		PendingDuration:    int64(time.Until(lic.ExpiresAt).Seconds()),
		OldOwnerID:         lic.OwnerChatID,
		ClaimedLicense:     lic,
		TokenCollected:     true,
	}
	h.Sessions.mu.Unlock()

	sisaDur := FormatDurationID(time.Until(lic.ExpiresAt))
	inviteText := fmt.Sprintf(`<h1>🔀 Undangan Pindah Akses Userbot</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👤 Pengirim</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Paket</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Sisa Aktif</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token Akses</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Anda menerima kiriman akses lisensi beserta data konfigurasi userbot. Tekan tombol <b>🚀 Pasang Userbot Sekarang</b> di bawah untuk menghubungkan userbot ke akun Anda. Sesi owner lama hanya akan dicopot setelah userbot Anda berhasil terpasang.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:transfer:claim" style="primary">🚀 Pasang Userbot Sekarang</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
		effectiveID,
		html.EscapeString(databases.NormalizeTier(lic.Tier)),
		html.EscapeString(sisaDur),
		html.EscapeString(lic.TokenSHA),
	)

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, sendErr := b.SendMessage(sendCtx, &botgotd.SendMessageParams{
		ChatID:    targetID,
		Text:      inviteText,
		ParseMode: "HTML",
	})
	cancel()

	h.setPanelState(chatID, "")
	if sendErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>ℹ️ Kirim Token Secara Manual</h1>
<table border="1">
  <tr><th>🎯 Tujuan</th><td><code>%d</code></td></tr>
  <tr><th>🔑 Token Akses</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Bot tidak dapat mengirim DM ke <code>%d</code> karena akun tujuan belum pernah memulai chat dengan bot ini.

Berikan <b>Kode Token</b> di atas ke penerima agar mereka menempelkannya di <b>/start → 🔑 Input Token 🔑</b> (atau <b>⚙️ Settings ⚙️ → 🔑 Tambah Token 🔑</b>). Setelah userbot mereka terpasang, sesi Anda otomatis dicopot.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
				targetID, html.EscapeString(lic.TokenSHA), targetID,
			),
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return true
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>✅ Undangan Pindah Akses Terkirim</h1>
<table border="1">
  <tr><th>🎯 Akun Tujuan</th><td><code>%d</code></td></tr>
  <tr><th>🎖️ Slot Token</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Notifikasi pindah akses sudah dikirim ke <code>%d</code>. Sesi userbot Anda tetap berjalan normal hingga penerima selesai memasang userbot, lalu sesi lama Anda akan otomatis dicopot.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
			targetID, html.EscapeString(databases.NormalizeTier(lic.Tier)), targetID,
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
	return true
}

func (h *Handler) handleTransferClaimCallback(ctx context.Context, b *botgotd.Bot, chatID int64, cb *botgotd.CallbackQuery) {
	if h == nil || h.db == nil {
		return
	}
	targetID := chatID
	if targetID == 0 && cb != nil {
		targetID = cb.From.ID
	}

	h.Sessions.mu.RLock()
	sess := h.Sessions.sessions[targetID]
	pendingSHA := ""
	if sess != nil {
		pendingSHA = sess.PendingTokenSHA
	}
	h.Sessions.mu.RUnlock()

	if pendingSHA == "" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⚠️ Sesi pindah akses sudah kedaluwarsa. Masukkan token lewat /start → Input Token.",
			ShowAlert:       true,
		})
		return
	}

	lic, _ := h.db.GetUserLicenseBySHA(ctx, pendingSHA)
	if lic == nil || lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
		h.Sessions.mu.Lock()
		delete(h.Sessions.sessions, targetID)
		h.Sessions.mu.Unlock()
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "❌ Token pindah akses sudah tidak aktif atau sudah diklaim.",
			ShowAlert:       true,
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            "✅ Token siap dipasang!",
	})
	h.startForeignTokenInstall(ctx, b, targetID, lic)
}

func (h *Handler) startForeignTokenInstall(ctx context.Context, b *botgotd.Bot, chatID int64, lic *databases.UserLicense) {
	if h == nil || lic == nil {
		return
	}
	sisaDur := FormatDurationID(time.Until(lic.ExpiresAt))
	h.setPanelState(chatID, "")
	h.Sessions.mu.Lock()
	h.Sessions.sessions[chatID] = &UserSession{
		ChatID:             chatID,
		State:              "waiting_phone",
		PendingMasterToken: lic.MasterToken,
		PendingTokenSHA:    lic.TokenSHA,
		PendingTier:        lic.Tier,
		PendingDuration:    int64(time.Until(lic.ExpiresAt).Seconds()),
		OldOwnerID:         lic.OwnerChatID,
		ClaimedLicense:     lic,
		TokenCollected:     true,
	}
	h.Sessions.mu.Unlock()

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtTokenDetected(lic.Tier, sisaDur),
		ParseMode:   "HTML",
		ReplyMarkup: assets.InstallPhoneMarkup,
	})
}
