package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) handleOwnerTokenManage(ctx context.Context, b *botgotd.Bot, chatID, senderID int64, fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	switch strings.ToLower(fields[1]) {
	case "upgrade":
		h.execOwnerTokenUpgrade(ctx, b, chatID, fields)
		return true
	case "extend":
		h.execOwnerTokenExtend(ctx, b, chatID, fields)
		return true
	case "info":
		h.execOwnerTokenInfo(ctx, b, chatID, fields)
		return true
	case "revoke":
		h.execOwnerTokenRevoke(ctx, b, chatID, fields)
		return true
	default:
		return false
	}
}

func parseManagedTier(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "free", "pro", "pro++", "proplus", "pro+", "vip":
		return databases.NormalizeTier(raw), true
	default:
		return "", false
	}
}

func (h *Handler) lookupLicenseByQuery(ctx context.Context, query string) (*databases.UserLicense, int64) {
	if strings.HasPrefix(query, "GOTD_SHA:") {
		lic, _ := h.db.GetUserLicenseBySHA(ctx, query)
		if lic != nil {
			return lic, lic.OwnerChatID
		}
		return nil, 0
	}
	uid, err := strconv.ParseInt(query, 10, 64)
	if err != nil || uid <= 0 {
		return nil, 0
	}
	lic, _ := h.db.GetActiveUserLicense(ctx, uid)
	if lic == nil {
		lic, _ = h.db.GetLatestUserLicense(ctx, uid)
	}
	return lic, uid
}

func (h *Handler) execOwnerTokenUpgrade(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 5 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Format: <code>/token upgrade &lt;user_id&gt; &lt;pro|pro++|vip&gt; &lt;durasi&gt;</code>\nContoh: <code>/token upgrade 123456789 pro++ 30h</code>",
			ParseMode: "HTML",
		})
		return
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil || targetID <= 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ <code>user_id</code> harus berupa angka valid.", ParseMode: "HTML"})
		return
	}
	tier, ok := parseManagedTier(fields[3])
	if !ok {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Tier harus <code>free</code>, <code>pro</code>, <code>pro++</code>, atau <code>vip</code>.", ParseMode: "HTML"})
		return
	}
	dur, durErr := ParseDurationStr(fields[4])
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ " + html.EscapeString(durErr.Error()), ParseMode: "HTML"})
		return
	}

	now := time.Now()
	curLic, _ := h.db.GetActiveUserLicense(ctx, targetID)
	if curLic == nil {
		curLic, _ = h.db.GetLatestUserLicense(ctx, targetID)
	}

	var tokenSHA, masterToken string
	var userbotID int64 = targetID
	var expiresAt time.Time
	if curLic != nil {
		tokenSHA = curLic.TokenSHA
		masterToken = curLic.MasterToken
		if curLic.UserbotID != 0 {
			userbotID = curLic.UserbotID
		}
		if curLic.Status == "active" && databases.NormalizeTier(curLic.Tier) != "Free" && curLic.ExpiresAt.After(now) {
			expiresAt = curLic.ExpiresAt.Add(dur)
		} else {
			expiresAt = now.Add(dur)
		}
	} else {
		masterToken = GenerateMasterToken()
		tokenSHA = ComputeTokenSHA(masterToken, targetID)
		expiresAt = now.Add(dur)
	}

	newLic := &databases.UserLicense{
		TokenSHA:           tokenSHA,
		MasterToken:        masterToken,
		OwnerChatID:        targetID,
		UserbotID:          userbotID,
		Tier:               tier,
		CanCustomWatermark: databases.GetTierPolicy(tier).AllowCustomWatermark,
		ExpiresAt:          expiresAt,
		CreatedAt:          now,
		Status:             "active",
	}
	if err := h.db.SaveUserLicense(ctx, newLic); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Gagal menyimpan lisensi: " + html.EscapeString(err.Error()), ParseMode: "HTML"})
		return
	}
	if err := h.switchUserActiveToken(ctx, targetID, newLic); err != nil {
		cfg := h.db.GetUserConfig(ctx, targetID)
		cfg.MasaAktif = formatLicenseMasaAktif(newLic)
		_ = h.db.SaveUserConfig(ctx, cfg)
	}

	expStr := expiresAt.Local().Format("02-01-2006 15:04:05")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`✅ <b>Lisensi Pengguna Diperbarui</b>
• <b>User ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code>
• <b>Aktif sampai:</b> <code>%s WIB</code>
• <b>Token SHA:</b> <code>%s</code>`, targetID, html.EscapeString(tier), expStr, html.EscapeString(tokenSHA)),
		ParseMode: "HTML",
	})
	if targetID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    targetID,
			Text:      fmt.Sprintf("🎉 <b>Paket userbot Anda di-upgrade oleh Owner ke <code>%s</code>!</b>\n⏰ Aktif sampai: <code>%s WIB</code>", html.EscapeString(tier), expStr),
			ParseMode: "HTML",
		})
	}
}

func (h *Handler) execOwnerTokenExtend(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 4 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Format: <code>/token extend &lt;user_id&gt; &lt;durasi&gt;</code>\nContoh: <code>/token extend 123456789 7h</code>",
			ParseMode: "HTML",
		})
		return
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil || targetID <= 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ <code>user_id</code> tidak valid.", ParseMode: "HTML"})
		return
	}
	dur, durErr := ParseDurationStr(fields[3])
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ " + html.EscapeString(durErr.Error()), ParseMode: "HTML"})
		return
	}

	lic, _ := h.lookupLicenseByQuery(ctx, fields[2])
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("❌ Pengguna <code>%d</code> belum memiliki riwayat lisensi.", targetID), ParseMode: "HTML"})
		return
	}

	base := lic.ExpiresAt
	if base.Before(time.Now()) {
		base = time.Now()
	}
	lic.ExpiresAt = base.Add(dur)
	lic.Status = "active"
	lic.CanCustomWatermark = databases.GetTierPolicy(lic.Tier).AllowCustomWatermark
	if err := h.db.SaveUserLicense(ctx, lic); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Gagal memperpanjang: " + html.EscapeString(err.Error()), ParseMode: "HTML"})
		return
	}
	cfg := h.db.GetUserConfig(ctx, targetID)
	if cfg.ActiveTokenSHA == "" {
		cfg.ActiveTokenSHA = lic.TokenSHA
	}
	if cfg.ActiveTokenSHA == lic.TokenSHA {
		cfg.MasaAktif = formatLicenseMasaAktif(lic)
		_ = h.db.SaveUserConfig(ctx, cfg)
	}

	expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`✅ <b>Masa Aktif Diperpanjang</b>
• <b>User ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code>
• <b>Tambahan:</b> <code>%s</code>
• <b>Berakhir baru:</b> <code>%s WIB</code>`, targetID, html.EscapeString(lic.Tier), FormatDurationID(dur), expStr),
		ParseMode: "HTML",
	})
	if targetID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    targetID,
			Text:      fmt.Sprintf("⏳ <b>Masa aktif userbot Anda diperpanjang +%s oleh Owner!</b>\n⏰ Aktif sampai: <code>%s WIB</code>", FormatDurationID(dur), expStr),
			ParseMode: "HTML",
		})
	}
}

func (h *Handler) execOwnerTokenInfo(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Format: <code>/token info &lt;user_id|GOTD:...|GOTD_SHA:...&gt;</code>",
			ParseMode: "HTML",
		})
		return
	}
	query := strings.TrimSpace(fields[2])
	if strings.HasPrefix(query, "GOTD:") {
		tm, err := h.db.GetTokenMaster(ctx, query)
		if err != nil || tm == nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Voucher master tidak ditemukan.", ParseMode: "HTML"})
			return
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`🔎 <b>Info Voucher Master</b>
• <b>Token:</b> <code>%s</code>
• <b>Tier:</b> <code>%s</code>
• <b>Durasi:</b> <code>%s</code>
• <b>Shared:</b> <code>%v</code> · <b>Klaim:</b> <code>%d</code>
• <b>Status:</b> <code>%s</code>`, html.EscapeString(tm.Token), html.EscapeString(tm.Tier), FormatDurationID(time.Duration(tm.DurationSeconds)*time.Second), tm.IsShared, tm.ClaimCount, html.EscapeString(tm.Status)),
			ParseMode: "HTML",
		})
		return
	}

	lic, targetID := h.lookupLicenseByQuery(ctx, query)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Data lisensi tidak ditemukan.", ParseMode: "HTML"})
		return
	}
	gCount, lCount := h.db.CountGroupsAndLists(ctx, targetID)
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`🔎 <b>Info Lisensi Pengguna</b>
• <b>Owner ChatID:</b> <code>%d</code> · <b>Userbot ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code> · <b>Status:</b> <code>%s</code>
• <b>Berakhir:</b> <code>%s WIB</code>
• <b>Data:</b> <code>%d grup · %d list</code>
• <b>Token SHA:</b> <code>%s</code>`,
			lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier), html.EscapeString(lic.Status),
			lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"), gCount, lCount, html.EscapeString(lic.TokenSHA)),
		ParseMode: "HTML",
	})
}

func (h *Handler) execOwnerTokenRevoke(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Format: <code>/token revoke &lt;user_id|GOTD:...|GOTD_SHA:...&gt;</code>",
			ParseMode: "HTML",
		})
		return
	}
	query := strings.TrimSpace(fields[2])
	if strings.HasPrefix(query, "GOTD:") {
		_ = h.db.RevokeTokenMaster(ctx, query)
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("🚫 Voucher <code>%s</code> berhasil dicabut.", html.EscapeString(query)), ParseMode: "HTML"})
		return
	}

	lic, _ := h.lookupLicenseByQuery(ctx, query)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Lisensi target tidak ditemukan.", ParseMode: "HTML"})
		return
	}

	_ = h.db.MarkUserLicenseStatus(ctx, lic.TokenSHA, "revoked")
	cfg := h.db.GetUserConfig(ctx, lic.OwnerChatID)
	wasActiveSlot := cfg.ActiveTokenSHA == lic.TokenSHA
	if wasActiveSlot {
		h.snapshotCurrentTokenProfile(ctx, lic.OwnerChatID, "")
	}
	remaining, _ := h.db.ListActiveUserLicenses(ctx, lic.OwnerChatID)
	nextBest := pickFallbackActiveLicense(remaining, lic.TokenSHA)
	if nextBest != nil {
		if wasActiveSlot || cfg.ActiveTokenSHA == "" {
			_ = h.switchUserActiveToken(ctx, lic.OwnerChatID, nextBest)
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("🚫 Slot lisensi <code>%d</code> (<code>%s</code>) dicabut. User otomatis memakai slot aktif tersisa: <code>%s</code>.", lic.OwnerChatID, html.EscapeString(lic.TokenSHA), html.EscapeString(databases.NormalizeTier(nextBest.Tier))),
			ParseMode: "HTML",
		})
		if lic.OwnerChatID != 0 && lic.OwnerChatID != chatID {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:    lic.OwnerChatID,
				Text:      fmt.Sprintf("⛔ <b>Slot lisensi <code>%s</code> telah dicabut oleh Owner.</b>\n🔄 Akun Anda dialihkan ke slot aktif lain: <b>%s</b>.", html.EscapeString(lic.Tier), html.EscapeString(databases.NormalizeTier(nextBest.Tier))),
				ParseMode: "HTML",
			})
		}
		return
	}
	cfg.ActiveTokenSHA = ""
	cfg.Status = "Berhenti"
	cfg.MasaAktif = "Dicabut"
	cfg.TindakanTerkini = "Lisensi dicabut oleh Owner"
	cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
	_ = h.db.SaveUserConfig(ctx, cfg)

	if h.broker != nil {
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
			ChatID:  lic.OwnerChatID,
			UserID:  lic.UserbotID,
			OwnerID: lic.OwnerChatID,
			Text:    "Lisensi dicabut oleh Owner",
			Type:    "revoke_transferred_session",
		})
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("🚫 Lisensi <code>%d</code> (<code>%s</code>) berhasil dicabut dan sesi dihentikan.", lic.OwnerChatID, html.EscapeString(lic.TokenSHA)),
		ParseMode: "HTML",
	})
	if lic.OwnerChatID != 0 && lic.OwnerChatID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      lic.OwnerChatID,
			Text:        "⛔ <b>Lisensi userbot Anda telah dicabut oleh Owner.</b>\nHubungi Owner untuk informasi lebih lanjut.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.RemoveKeyboard,
		})
	}
}
