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
<blockquote expandable>🔄 Gunakan <b>🔑 Tambah Token 🔑</b> untuk menambah slot baru atau <b>🔄 Ganti Token 🔄</b> untuk berpindah slot. 🔒 <code>GOTD_SHA:</code> memindahkan slot paket (Pro+), 📎 <code>GOTD_BKP:</code> menyalin List &amp; Grup antar-slot atau akun.</blockquote>
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
		h.denyTierFeature(ctx, b, chatID, policy.Tier, "Ambil Token Paket (GOTD_SHA)", "Pro", assets.SettingsMenuMarkup)
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
		Text: fmt.Sprintf(`<h1>🔒 Token Lisensi Akun (Backup Key)</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
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
<blockquote expandable>Kirimkan voucher master (<code>GOTD:&lt;32_huruf&gt;</code>) dari Owner untuk membuka slot token baru tersendiri (data token lama tetap aman tersimpan dan bisa dipilih kapan saja lewat <b>🔄 Ganti Token 🔄</b>), atau tempel <code>GOTD_SHA:...</code> milik slot Anda untuk langsung berpindah.</blockquote>
<hr/>
<p>GotdBot • Account &amp; Slot Security</p>`,
			html.EscapeString(curTier), len(activeSlots), html.EscapeString(curExp),
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) createAndRenderDataBackup(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	cfg := h.db.GetUserConfig(ctx, userID)
	code := GenerateBackupCode()
	backup := &databases.DataBackupCode{
		Code:       code,
		OwnerID:    userID,
		Lists:      append([]int(nil), cfg.Lists...),
		Groups:     append([]databases.GroupEntry(nil), cfg.Groups...),
		Topics:     append([]databases.TopicEntry(nil), cfg.Topics...),
		TopicLists: append([]databases.TopicListEntry(nil), cfg.TopicLists...),
		CreatedAt:  time.Now(),
	}
	if err := h.db.SaveDataBackup(ctx, backup); err != nil {
		errHTML := `<h1>❌ Gagal Membuat Backup</h1><blockquote expandable>Terjadi kesalahan saat menyimpan kode backup data ke database.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`
		if editMsgID > 0 {
			if _, editErr := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: editMsgID,
				Text:      errHTML,
				ParseMode: "HTML",
			}); editErr == nil {
				return
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        errHTML,
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	text := fmt.Sprintf(`<h1>📎 Kode Backup Data Berhasil Dibuat</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Kode Backup</td><td><code>%s</code></td></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
<blockquote expandable>ℹ️ Gunakan menu <b>♻️ Restore Data ♻️</b> dan kirim kode di atas untuk memulihkan list &amp; grup ke slot atau akun baru (tersedia untuk semua tier termasuk Free).</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:settings:home">◀️ Kembali</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Data Backup Manager</p>`,
		html.EscapeString(code), len(backup.Lists), len(backup.Groups), len(backup.Topics), len(backup.TopicLists),
	)

	if editMsgID > 0 {
		if _, editErr := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      text,
			ParseMode: "HTML",
		}); editErr == nil {
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

func (h *Handler) HandleCreateDataBackup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.createAndRenderDataBackup(ctx, b, chatID, userID, 0)
}

func (h *Handler) PromptRestoreDataBackup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_restore_backup")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: `<h1>♻️ Pulihkan Data (Restore Backup)</h1>
<table border="1">
  <tr><th>Format Kode</th><td><code>GOTD_BKP:...</code></td></tr>
</table>
<blockquote expandable>Kirimkan kode backup (<code>GOTD_BKP:...</code>) untuk memulihkan daftar List, Grup, dan Topic ke slot token aktif ini.</blockquote>
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
		if strings.HasPrefix(trimmed, "GOTD_SHA:") {
			lic, err := h.db.GetUserLicenseBySHA(ctx, trimmed)
			if err != nil || lic == nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token SHA Tidak Ditemukan</h1><blockquote expandable>Token SHA yang Anda masukkan tidak terdaftar dalam database.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token SHA Kedaluwarsa</h1><blockquote expandable>Token SHA tersebut sudah tidak aktif atau masa aktifnya telah berakhir.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.OwnerChatID != effectiveID && lic.UserbotID != effectiveID {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>ℹ️ Token Milik Akun Lain</h1><blockquote expandable>Token SHA tersebut terdaftar di akun Telegram lain. Gunakan menu <b>/start → 🔑 Input Token 🔑</b> untuk memindahkan slot tersebut ke akun ini.</blockquote><hr/><p>GotdBot • Account &amp; Slot Security</p>`,
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
		code := strings.TrimSpace(text)
		if !strings.HasPrefix(code, "GOTD_BKP:") {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>⚠️ Format Kode Tidak Valid</h1><blockquote expandable>Masukkan kode backup yang diawali <code>GOTD_BKP:</code>.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		backup, err := h.db.GetDataBackup(ctx, code)
		if err != nil || backup == nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>❌ Kode Backup Tidak Ditemukan</h1><blockquote expandable>Kode backup yang Anda kirimkan tidak terdaftar di database.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		lists := append([]int(nil), backup.Lists...)
		groups := append([]databases.GroupEntry(nil), backup.Groups...)
		topics := append([]databases.TopicEntry(nil), backup.Topics...)
		topicLists := append([]databases.TopicListEntry(nil), backup.TopicLists...)

		policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
		var notes []string
		if enforced && policy.MaxLists > 0 && len(lists) > policy.MaxLists {
			lists = lists[:policy.MaxLists]
			notes = append(notes, fmt.Sprintf("List dibatasi %d item (paket Free)", policy.MaxLists))
		}
		if enforced && policy.MaxGroups > 0 && len(groups) > policy.MaxGroups {
			groups = groups[:policy.MaxGroups]
			notes = append(notes, fmt.Sprintf("Grup dibatasi %d target (paket Free)", policy.MaxGroups))
		}
		if enforced && !policy.AllowTopic && (len(topics) > 0 || len(topicLists) > 0) {
			topics = []databases.TopicEntry{}
			topicLists = []databases.TopicListEntry{}
			notes = append(notes, "Topic tidak dipulihkan pada paket Free")
		}

		cfg.Lists = lists
		cfg.Groups = groups
		cfg.Topics = topics
		cfg.TopicLists = topicLists
		_ = h.db.SaveUserConfig(ctx, cfg)
		h.resetTopicForumScan(chatID)

		noteBlock := `<blockquote expandable>Seluruh materi List, target Grup, Topic, dan List Topic telah dipulihkan ke slot aktif ini.</blockquote>`
		if len(notes) > 0 {
			noteBlock = fmt.Sprintf(`<blockquote expandable>⚠️ <b>Catatan Penyesuaian Tier:</b><br/>%s</blockquote>`, html.EscapeString(strings.Join(notes, "; ")))
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Data Berhasil Dipulihkan</h1>
<table border="1">
  <tr><th>📌 Data</th><th>Jumlah</th></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
%s
<hr/>
<p>GotdBot • Data Backup Manager</p>`,
				len(lists), len(groups), len(topics), len(topicLists), noteBlock,
			),
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return true
	}
	return false
}
