package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"html"
	"strings"
	"time"
)

func (h *Handler) getEffectiveTierPolicy(ctx context.Context, userID int64) (databases.TierPolicy, bool) {
	if h == nil || h.db == nil || userID == 0 {
		return databases.GetTierPolicy("Vip"), false
	}
	if lic, _ := h.db.GetActiveUserLicense(ctx, userID); lic != nil {
		return databases.GetTierPolicy(lic.Tier), true
	}
	if latest, _ := h.db.GetLatestUserLicense(ctx, userID); latest != nil {
		return databases.GetTierPolicy(latest.Tier), true
	}
	return databases.GetTierPolicy("Vip"), false
}

func (h *Handler) denyTierFeature(ctx context.Context, b *botgotd.Bot, chatID int64, currentTier, featureName, minTier string, markup any) {
	if markup == nil {
		markup = assets.MainMenuMarkup
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🔒 Fitur %s Terkunci</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🎖️ Paket Anda</td><td><code>%s</code></td></tr>
  <tr><td>🚀 Minimal Tier</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Gunakan menu <b>⚙️ Settings ⚙️ → 🔑 Tambah Token 🔑</b> untuk mengaktifkan paket lebih tinggi tanpa perlu login ulang.</blockquote>
<hr/>
<p>GotdBot • Tier Policy Control</p>`,
			html.EscapeString(featureName),
			html.EscapeString(currentTier),
			html.EscapeString(minTier),
		),
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
}

func (h *Handler) renderSettingsView(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, lCount := h.db.CountGroupsAndLists(ctx, userID)
	activeSlots, _ := h.db.ListActiveUserLicenses(ctx, userID)

	tierLabel := "Belum terdaftar"
	expLabel := html.EscapeString(cfg.MasaAktif)
	if lic, _ := h.db.GetActiveUserLicense(ctx, userID); lic != nil {
		tierLabel = databases.NormalizeTier(lic.Tier)
		expLabel = lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
	} else if latest, _ := h.db.GetLatestUserLicense(ctx, userID); latest != nil {
		tierLabel = databases.NormalizeTier(latest.Tier) + " (Expired)"
	}

	text := fmt.Sprintf(`<h1>⚙️ Pengaturan &amp; Keamanan Akun</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🎖️ Paket Aktif</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Slot Token</td><td><code>%d slot aktif</code></td></tr>
  <tr><td>⏳ Masa Aktif</td><td><code>%s</code></td></tr>
  <tr><td>📦 Data Slot Ini</td><td><code>%d grup · %d list</code></td></tr>
  <tr><td>✉️ Email Darurat</td><td>%s</td></tr>
  <tr><td>👤 Admin Darurat</td><td>%s</td></tr>
</table>
<blockquote expandable>🔄 Gunakan <b>🔑 Tambah Token 🔑</b> untuk menambah slot baru atau <b>🔄 Ganti Token 🔄</b> untuk berpindah slot. 🔒 <b>Ambil Token</b> melihat kode akses slot aktif, 📎 <b>Backup Data</b> menyalin List &amp; Grup antar-slot atau akun.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:token:menu" style="primary">🔄 Ganti Token</tg-button>
  <tg-button type="callback_data" data="panel:settings:backup">📎 Backup Data</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
		html.EscapeString(tierLabel), len(activeSlots), expLabel, gCount, lCount,
		html.EscapeString(cfg.Email), html.EscapeString(cfg.Admin),
	)

	if editMsgID > 0 {
		if _, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      text,
			ParseMode: "HTML",
		}); err == nil {
			return
		}
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
}

func (h *Handler) ShowSettings(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.renderSettingsView(ctx, b, chatID, userID, 0)
}

func (h *Handler) HandleAmbilToken(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
	if enforced && !policy.AllowLicenseToken {
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Ambil Token Akses", "Pro", assets.SettingsMenuMarkup)
		return
	}
	lic, _ := h.db.GetActiveUserLicense(ctx, userID)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Lisensi Tidak Ditemukan</h1><blockquote expandable>Akun Anda tidak memiliki lisensi aktif. Gunakan <b>🔑 Tambah Token 🔑</b> untuk mengaktifkan voucher baru, atau <b>📎 Backup Data 📎</b> untuk menyimpan data list &amp; grup.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🔒 Token Akses Akun</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token Akses</td><td><code>%s</code></td></tr>
  <tr><td>🎖️ Tier Paket</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Aktif Sampai</td><td><code>%s WIB</code></td></tr>
</table>
<blockquote expandable>⚠️ Siapa pun yang memegang token ini dapat memindahkan paket beserta data slot ini ke akun Telegram lain. Simpan baik-baik dan jangan bagikan ke orang lain.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
			html.EscapeString(lic.TokenSHA),
			html.EscapeString(lic.Tier),
			lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
}

func (h *Handler) PromptUpgradeToken(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_upgrade_token")
	curTier := "Free"
	curExp := "-"
	if lic, _ := h.db.GetActiveUserLicense(ctx, userID); lic != nil {
		curTier = databases.NormalizeTier(lic.Tier)
		curExp = lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
	}
	activeSlots, _ := h.db.ListActiveUserLicenses(ctx, userID)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🔑 Tambah / Ganti Slot Token</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🎖️ Slot Aktif Sekarang</td><td><code>%s</code> (<code>%d slot aktif</code>)</td></tr>
  <tr><td>⏳ Masa Aktif</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Kirimkan voucher (<code>GOTD:M...</code>) dari Owner untuk membuka slot baru tersendiri, atau kirimkan token akses (<code>GOTD:S...</code>) milik slot Anda untuk langsung berpindah.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
			html.EscapeString(curTier), len(activeSlots), html.EscapeString(curExp),
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) HandleCreateDataBackup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.createAndRenderDataBackup(ctx, b, chatID, userID, 0)
}

func (h *Handler) PromptRestoreDataBackup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_restore_backup")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: `<h1>♻️ Pulihkan Data (Restore Backup)</h1>
<table border="1">
  <tr><th>Format Kode</th><td><code>GOTD:B...</code> atau File <code>.txt</code></td></tr>
</table>
<blockquote expandable>Kirimkan kode backup (<code>GOTD:B...</code>) atau unggah dokumen <code>.txt</code> hasil backup untuk memulihkan daftar List, Grup, Topic &amp; pengaturan ke akun aktif ini.</blockquote>
<hr/>
<p>GotdBot • Data Backup Manager</p>`,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) handleSettingsPanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state, text string) bool {
	switch state {
	case "waiting_upgrade_token":
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		effectiveID := h.GetEffectiveUserID(chatID, userID)
		trimmed := strings.TrimSpace(text)
		if IsSessionTokenPrefix(trimmed) {
			if strings.HasPrefix(trimmed, tokenPrefixSession) {
				if _, _, decErr := DecodeFernetSessionToken(trimmed); decErr != nil {
					b.SendMessage(ctx, &botgotd.SendMessageParams{
						ChatID:      chatID,
						Text:        `<h1>❌ Token Tidak Dikenali</h1><blockquote expandable>Token slot yang Anda masukkan tidak valid atau tidak terdaftar.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
						ParseMode:   "HTML",
						ReplyMarkup: assets.CancelOnlyMarkup,
					})
					return true
				}
			}
			lic, err := h.db.GetUserLicenseBySHA(ctx, trimmed)
			if err != nil || lic == nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token Tidak Ditemukan</h1><blockquote expandable>Token yang Anda masukkan tidak valid atau tidak terdaftar.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token Kedaluwarsa</h1><blockquote expandable>Token tersebut sudah tidak aktif atau masa aktifnya telah berakhir.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.OwnerChatID != effectiveID && lic.UserbotID != effectiveID {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>ℹ️ Token Milik Akun Lain</h1><blockquote expandable>Token tersebut terdaftar di akun Telegram lain. Gunakan menu <b>/start → 🔑 Input Token 🔑</b> untuk memindahkan slot tersebut ke akun ini.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.SettingsMenuMarkup,
				})
				h.setPanelState(chatID, "")
				return true
			}
			if err := h.switchUserActiveToken(ctx, effectiveID, lic); err != nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        fmt.Sprintf(`<h1>❌ Gagal Beralih Slot</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`, html.EscapeString(err.Error())),
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			h.setPanelState(chatID, "")
			expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: fmt.Sprintf(`<h1>✅ Berhasil Beralih Slot Token</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🎖️ Tier Aktif</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Berlaku Sampai</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token Slot</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Data List, Grup, Topic, Jeda, Timer, dan Watermark khusus slot ini telah dimuat.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
					html.EscapeString(databases.NormalizeTier(lic.Tier)),
					expStr,
					html.EscapeString(lic.TokenSHA),
				),
				ParseMode:   "HTML",
				ReplyMarkup: assets.SettingsMenuMarkup,
			})
			return true
		}
		lic, actionLabel, errMsg := h.applyVoucherUpgrade(ctx, effectiveID, text)
		if errMsg != "" {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf(`<h1>⚠️ Gagal Memproses Token</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`, errMsg),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		h.setPanelState(chatID, "")
		expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ %s Aktif</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🎖️ Tier Aktif</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Berlaku Sampai</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token Slot</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Slot baru aktif dengan data kosong tersendiri. Data token Anda sebelumnya tetap aman tersimpan dan bisa dipilih kapan saja lewat <b>🔄 Ganti Token 🔄</b>.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
				html.EscapeString(actionLabel),
				html.EscapeString(lic.Tier),
				expStr,
				html.EscapeString(lic.TokenSHA),
			),
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return true

	case "waiting_restore_backup":
		if isKnownPanelButton(text) {
			h.setPanelState(chatID, "")
			return false
		}
		return h.processRestoreBackupCode(ctx, b, chatID, userID, text)
	}
	return false
}
