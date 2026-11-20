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
		Text: fmt.Sprintf(
			"🔒 <b>Fitur %s Terkunci</b>\n\nPaket Anda saat ini: <code>%s</code>.\nFitur ini tersedia mulai tier <b>%s</b>.\n\n<i>Gunakan ⚙️ Settings → 🔑 Upgrade / Perpanjang untuk menaikkan paket tanpa perlu login ulang.</i>",
			html.EscapeString(featureName),
			html.EscapeString(currentTier),
			html.EscapeString(minTier),
		),
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
}

func (h *Handler) ShowSettings(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
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

	text := fmt.Sprintf(`⚙️ <b>Pengaturan &amp; Keamanan Akun</b>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🎖️ Paket Aktif</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Slot Token</td><td><code>%d slot aktif</code></td></tr>
  <tr><td>⏳ Masa Aktif</td><td><code>%s</code></td></tr>
  <tr><td>📦 Data Slot Ini</td><td><code>%d grup · %d list</code></td></tr>
  <tr><td>✉️ Email Darurat</td><td>%s</td></tr>
  <tr><td>👤 Admin Darurat</td><td>%s</td></tr>
</table>
<blockquote>🔄 Gunakan <b>🔑 Tambah Token</b> untuk menambah slot baru atau <b>🔄 Ganti Token</b> untuk berpindah slot. 🔒 <code>GOTD_SHA:</code> memindahkan slot paket (Pro+), 📎 <code>GOTD_BKP:</code> menyalin List &amp; Grup antar-slot/akun.</blockquote>`,
		html.EscapeString(tierLabel), len(activeSlots), expLabel, gCount, lCount,
		html.EscapeString(cfg.Email), html.EscapeString(cfg.Admin),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
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
			Text:        "⚠️ Akun Anda tidak memiliki lisensi aktif. Gunakan <b>🔑 Tambah Token</b> untuk mengaktifkan voucher baru, atau <b>📎 Backup Data</b> untuk menyimpan data list &amp; grup.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`🔒 <b>Token Lisensi Akun (Backup Key)</b>

<code>%s</code>

• <b>Tier:</b> <code>%s</code>
• <b>Aktif sampai:</b> <code>%s WIB</code>

<blockquote>⚠️ Siapa pun yang memegang token ini dapat memindahkan paket beserta data ke akun Telegram lain. Simpan baik-baik dan jangan bagikan ke orang lain.</blockquote>`,
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
		Text: fmt.Sprintf(`🔑 <b>Tambah / Ganti Slot Token</b>

• <b>Slot aktif sekarang:</b> <code>%s</code> (<code>%d slot aktif</code>)
• <b>Masa aktif:</b> <code>%s</code>

Kirimkan voucher master (<code>GOTD:&lt;32_huruf&gt;</code>) dari Owner untuk membuka slot token baru tersendiri (data token lama tetap aman tersimpan dan bisa dipilih kapan saja lewat <b>🔄 Ganti Token</b>), atau tempel <code>GOTD_SHA:...</code> milik slot Anda untuk langsung berpindah.`,
			html.EscapeString(curTier), len(activeSlots), html.EscapeString(curExp),
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
}

func (h *Handler) HandleCreateDataBackup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Gagal membuat kode backup data.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`📎 <b>Kode Backup Data Berhasil Dibuat</b>

<code>%s</code>

• <b>List:</b> <code>%d</code> · <b>Grup:</b> <code>%d</code>
• <b>Topic:</b> <code>%d</code> · <b>List Topic:</b> <code>%d</code>

<blockquote>ℹ️ Gunakan menu <b>♻️ Restore Data</b> dan kirim kode di atas untuk memulihkan list &amp; grup ke akun baru (tersedia untuk semua tier termasuk Free).</blockquote>`,
			html.EscapeString(code), len(backup.Lists), len(backup.Groups), len(backup.Topics), len(backup.TopicLists),
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
}

func (h *Handler) PromptRestoreDataBackup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_restore_backup")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: `♻️ <b>Pulihkan Data (Restore Backup)</b>

Kirimkan kode backup (<code>GOTD_BKP:...</code>) untuk memulihkan daftar List dan Grup ke akun ini.`,
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
					Text:        "❌ Token SHA tidak ditemukan dalam database.",
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        "❌ Token SHA tersebut sudah tidak aktif atau kedaluwarsa.",
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			if lic.OwnerChatID != effectiveID && lic.UserbotID != effectiveID {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        "ℹ️ Token SHA tersebut terdaftar di akun Telegram lain.\nGunakan menu <b>/start → Input Token</b> untuk memindahkan slot tersebut ke akun ini.",
					ParseMode:   "HTML",
					ReplyMarkup: assets.SettingsMenuMarkup,
				})
				h.setPanelState(chatID, "")
				return true
			}
			if err := h.switchUserActiveToken(ctx, effectiveID, lic); err != nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        "❌ Gagal beralih slot token: " + html.EscapeString(err.Error()),
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return true
			}
			h.setPanelState(chatID, "")
			expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: fmt.Sprintf(`✅ <b>Berhasil Beralih Slot Token!</b>
• <b>Tier Aktif:</b> <code>%s</code>
• <b>Berlaku sampai:</b> <code>%s</code>
• <b>Token Slot:</b> <code>%s</code>

<i>Data List, Grup, Topic, Jeda, dan Timer untuk slot ini telah dimuat.</i>`,
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
				Text:        errMsg,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		h.setPanelState(chatID, "")
		expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`✅ <b>%s Aktif!</b>
• <b>Tier Aktif:</b> <code>%s</code>
• <b>Berlaku sampai:</b> <code>%s</code>
• <b>Token Slot:</b> <code>%s</code>

<i>Slot baru aktif dengan data kosong tersendiri. Data token Anda sebelumnya tetap aman tersimpan dan bisa dipilih kapan saja lewat 🔄 Ganti Token.</i>`,
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
				Text:        "⚠️ Format tidak valid. Masukkan kode yang diawali <code>GOTD_BKP:</code>.",
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		backup, err := h.db.GetDataBackup(ctx, code)
		if err != nil || backup == nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Kode backup tidak ditemukan dalam database.",
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

		noteText := ""
		if len(notes) > 0 {
			noteText = "\n\n⚠️ <b>Catatan:</b> " + html.EscapeString(strings.Join(notes, "; "))
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`✅ <b>Data Berhasil Dipulihkan</b>
• <b>List:</b> <code>%d</code> · <b>Grup:</b> <code>%d</code>
• <b>Topic:</b> <code>%d</code> · <b>List Topic:</b> <code>%d</code>%s`,
				len(lists), len(groups), len(topics), len(topicLists), noteText,
			),
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return true
	}
	return false
}
