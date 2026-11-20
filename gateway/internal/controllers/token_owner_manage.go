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
			Text:      `<h1>⚠️ Format Perintah Salah</h1><blockquote expandable>Gunakan: <code>/token upgrade &lt;user_id&gt; &lt;pro|pro++|vip&gt; &lt;durasi&gt;</code><br/>Contoh: <code>/token upgrade 123456789 pro++ 30h</code></blockquote><hr/><p>GotdBot • Owner License Manager</p>`,
			ParseMode: "HTML",
		})
		return
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil || targetID <= 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ User ID Tidak Valid</h1><blockquote expandable><code>user_id</code> harus berupa angka Telegram ID yang valid.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
		return
	}
	tier, ok := parseManagedTier(fields[3])
	if !ok {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ Tier Tidak Valid</h1><blockquote expandable>Tier harus berupa <code>free</code>, <code>pro</code>, <code>pro++</code>, atau <code>vip</code>.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
		return
	}
	dur, durErr := ParseDurationStr(fields[4])
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>❌ Durasi Tidak Valid</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, html.EscapeString(durErr.Error())), ParseMode: "HTML"})
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>❌ Gagal Menyimpan Lisensi</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, html.EscapeString(err.Error())), ParseMode: "HTML"})
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
		Text: fmt.Sprintf(`<h1>✅ Lisensi Pengguna Diperbarui</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👤 User ID</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Tier Baru</td><td><code>%s</code></td></tr>
  <tr><td>⏰ Aktif Sampai</td><td><code>%s WIB</code></td></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`, targetID, html.EscapeString(tier), expStr, html.EscapeString(tokenSHA)),
		ParseMode: "HTML",
	})
	if targetID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: targetID,
			Text: fmt.Sprintf(`<h1>🎉 Paket Diperbarui Oleh Owner</h1>
<table border="1">
  <tr><th>🎖️ Tier Aktif</th><td><code>%s</code></td></tr>
  <tr><th>⏰ Aktif Sampai</th><td><code>%s WIB</code></td></tr>
</table>
<blockquote expandable>Seluruh fitur dan kuota paket baru Anda sudah langsung aktif.</blockquote>
<hr/>
<p>GotdBot • Owner License Manager</p>`, html.EscapeString(tier), expStr),
			ParseMode: "HTML",
		})
	}
}

func (h *Handler) execOwnerTokenExtend(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 4 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      `<h1>⚠️ Format Perintah Salah</h1><blockquote expandable>Gunakan: <code>/token extend &lt;user_id&gt; &lt;durasi&gt;</code><br/>Contoh: <code>/token extend 123456789 7h</code></blockquote><hr/><p>GotdBot • Owner License Manager</p>`,
			ParseMode: "HTML",
		})
		return
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil || targetID <= 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ User ID Tidak Valid</h1><blockquote expandable><code>user_id</code> tidak valid.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
		return
	}
	dur, durErr := ParseDurationStr(fields[3])
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>❌ Durasi Tidak Valid</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, html.EscapeString(durErr.Error())), ParseMode: "HTML"})
		return
	}

	lic, _ := h.lookupLicenseByQuery(ctx, fields[2])
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>❌ Riwayat Tidak Ditemukan</h1><blockquote expandable>Pengguna <code>%d</code> belum memiliki riwayat lisensi.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, targetID), ParseMode: "HTML"})
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>❌ Gagal Memperpanjang</h1><blockquote expandable>%s</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, html.EscapeString(err.Error())), ParseMode: "HTML"})
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
		Text: fmt.Sprintf(`<h1>✅ Masa Aktif Diperpanjang</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👤 User ID</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s</code></td></tr>
  <tr><td>➕ Tambahan</td><td><code>%s</code></td></tr>
  <tr><td>⏰ Berakhir Baru</td><td><code>%s WIB</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`, targetID, html.EscapeString(lic.Tier), FormatDurationID(dur), expStr),
		ParseMode: "HTML",
	})
	if targetID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: targetID,
			Text: fmt.Sprintf(`<h1>⏳ Masa Aktif Diperpanjang</h1>
<table border="1">
  <tr><th>➕ Tambahan Durasi</th><td><code>+%s</code></td></tr>
  <tr><th>⏰ Aktif Sampai</th><td><code>%s WIB</code></td></tr>
</table>
<blockquote expandable>Masa aktif userbot Anda telah ditambahkan oleh Owner.</blockquote>
<hr/>
<p>GotdBot • Owner License Manager</p>`, FormatDurationID(dur), expStr),
			ParseMode: "HTML",
		})
	}
}

func (h *Handler) execOwnerTokenInfo(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      `<h1>⚠️ Format Perintah Salah</h1><blockquote expandable>Gunakan: <code>/token info &lt;user_id|GOTD:...|GOTD_SHA:...&gt;</code></blockquote><hr/><p>GotdBot • Owner License Manager</p>`,
			ParseMode: "HTML",
		})
		return
	}
	query := strings.TrimSpace(fields[2])
	if strings.HasPrefix(query, "GOTD:") {
		tm, err := h.db.GetTokenMaster(ctx, query)
		if err != nil || tm == nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ Voucher Tidak Ditemukan</h1><blockquote expandable>Voucher master tersebut tidak terdaftar dalam database.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
			return
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>🔎 Info Voucher Master</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token</td><td><code>%s</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Durasi</td><td><code>%s</code></td></tr>
  <tr><td>👥 Shared / Klaim</td><td><code>%v · %d kali</code></td></tr>
  <tr><td>📌 Status</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`, html.EscapeString(tm.Token), html.EscapeString(tm.Tier), FormatDurationID(time.Duration(tm.DurationSeconds)*time.Second), tm.IsShared, tm.ClaimCount, html.EscapeString(tm.Status)),
			ParseMode: "HTML",
		})
		return
	}

	lic, targetID := h.lookupLicenseByQuery(ctx, query)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ Data Lisensi Tidak Ditemukan</h1><blockquote expandable>Tidak ada lisensi yang cocok dengan query Anda.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
		return
	}
	gCount, lCount := h.db.CountGroupsAndLists(ctx, targetID)
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🔎 Info Lisensi Pengguna</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👤 Owner / Ubot</td><td><code>%d · %d</code></td></tr>
  <tr><td>🎖️ Tier / Status</td><td><code>%s · %s</code></td></tr>
  <tr><td>⏰ Berakhir</td><td><code>%s WIB</code></td></tr>
  <tr><td>📦 Data Slot</td><td><code>%d grup · %d list</code></td></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`,
			lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier), html.EscapeString(lic.Status),
			lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"), gCount, lCount, html.EscapeString(lic.TokenSHA)),
		ParseMode: "HTML",
	})
}

func (h *Handler) execOwnerTokenRevoke(ctx context.Context, b *botgotd.Bot, chatID int64, fields []string) {
	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      `<h1>⚠️ Format Perintah Salah</h1><blockquote expandable>Gunakan: <code>/token revoke &lt;user_id|GOTD:...|GOTD_SHA:...&gt;</code></blockquote><hr/><p>GotdBot • Owner License Manager</p>`,
			ParseMode: "HTML",
		})
		return
	}
	query := strings.TrimSpace(fields[2])
	if strings.HasPrefix(query, "GOTD:") {
		_ = h.db.RevokeTokenMaster(ctx, query)
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf(`<h1>🚫 Voucher Dicabut</h1><table border="1"><tr><th>🔑 Token</th><td><code>%s</code></td></tr></table><hr/><p>GotdBot • Owner License Manager</p>`, html.EscapeString(query)), ParseMode: "HTML"})
		return
	}

	lic, _ := h.lookupLicenseByQuery(ctx, query)
	if lic == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: `<h1>❌ Lisensi Tidak Ditemukan</h1><blockquote expandable>Lisensi target yang ingin dicabut tidak ditemukan.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`, ParseMode: "HTML"})
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
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>🚫 Slot Lisensi Dicabut</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👤 Owner ID</td><td><code>%d</code></td></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
  <tr><td>🔄 Slot Fallback</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`, lic.OwnerChatID, html.EscapeString(lic.TokenSHA), html.EscapeString(databases.NormalizeTier(nextBest.Tier))),
			ParseMode: "HTML",
		})
		if lic.OwnerChatID != 0 && lic.OwnerChatID != chatID {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: lic.OwnerChatID,
				Text: fmt.Sprintf(`<h1>⛔ Slot Lisensi Dicabut Oleh Owner</h1>
<table border="1">
  <tr><th>❌ Slot Dicabut</th><td><code>%s</code></td></tr>
  <tr><th>🔄 Beralih Ke</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Akun Anda otomatis dialihkan ke slot token aktif yang tersisa.</blockquote>
<hr/>
<p>GotdBot • Owner License Manager</p>`, html.EscapeString(lic.Tier), html.EscapeString(databases.NormalizeTier(nextBest.Tier))),
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
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🚫 Lisensi &amp; Sesi Dihentikan</h1>
<table border="1">
  <tr><th>👤 Owner ID</th><td><code>%d</code></td></tr>
  <tr><th>🔑 Token SHA</th><td><code>%s</code></td></tr>
</table>
<hr/>
<p>GotdBot • Owner License Manager</p>`, lic.OwnerChatID, html.EscapeString(lic.TokenSHA)),
		ParseMode: "HTML",
	})
	if lic.OwnerChatID != 0 && lic.OwnerChatID != chatID {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      lic.OwnerChatID,
			Text:        `<h1>⛔ Lisensi Userbot Dicabut Oleh Owner</h1><blockquote expandable>Seluruh lisensi aktif akun Anda telah dicabut dan sesi userbot dihentikan. Hubungi Owner untuk informasi lebih lanjut.</blockquote><hr/><p>GotdBot • Owner License Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.RemoveKeyboard,
		})
	}
}
