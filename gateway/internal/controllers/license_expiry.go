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

func formatLicenseMasaAktif(lic *databases.UserLicense) string {
	if lic == nil {
		return "Belum diatur"
	}
	if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
		return "Expired (" + lic.ExpiresAt.Local().Format("02-01-2006 15:04") + ")"
	}
	return lic.ExpiresAt.Local().Format("02-01-2006 15:04:05")
}

func (h *Handler) syncConfigMasaAktif(ctx context.Context, ownerID int64, cfg *databases.UserConfig) {
	if h == nil || h.db == nil || cfg == nil || ownerID == 0 {
		return
	}
	lic, _ := h.db.GetActiveUserLicense(ctx, ownerID)
	if lic == nil {
		lic, _ = h.db.GetLatestUserLicense(ctx, ownerID)
	}
	if lic == nil {
		return
	}
	formatted := formatLicenseMasaAktif(lic)
	if cfg.MasaAktif != formatted {
		cfg.MasaAktif = formatted
		_ = h.db.SaveUserConfig(ctx, cfg)
	}
}

func (h *Handler) startLicenseExpiryMonitor(ctx context.Context, b *botgotd.Bot) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}

	h.processExpiredLicenses(ctx, b, true)
	lastRemoteSync := time.Now()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			includeRemote := time.Since(lastRemoteSync) >= 15*time.Minute
			if includeRemote {
				lastRemoteSync = time.Now()
			}
			h.processExpiredLicenses(ctx, b, includeRemote)
		}
	}
}

func (h *Handler) processExpiredLicenses(ctx context.Context, b *botgotd.Bot, includeRemote ...bool) {
	if h == nil || h.db == nil {
		return
	}
	expired, err := h.db.ListExpiredUserLicenses(ctx, time.Now(), includeRemote...)
	if err != nil {
		log.Printf("⚠️ [license] ListExpiredUserLicenses error: %v", err)
		return
	}
	for _, lic := range expired {
		h.expireSingleLicense(ctx, b, lic)
	}
}

func (h *Handler) expireSingleLicense(ctx context.Context, b *botgotd.Bot, lic *databases.UserLicense) {
	if h == nil || h.db == nil || lic == nil {
		return
	}
	cfg := h.db.GetUserConfig(ctx, lic.OwnerChatID)
	wasActiveSlot := cfg.ActiveTokenSHA == lic.TokenSHA
	if wasActiveSlot {
		h.snapshotCurrentTokenProfile(ctx, lic.OwnerChatID, "")
	}
	if err := h.db.MarkUserLicenseExpired(ctx, lic.TokenSHA); err != nil {
		log.Printf("⚠️ [license] MarkUserLicenseExpired (%s) error: %v", lic.TokenSHA, err)
		globalDevMonitor.NotifyTraceback("gateway", "MarkUserLicenseExpired Error", err.Error())
	}
	globalDevMonitor.NotifyTokenExpired(lic)
	if lic.MasterToken != "" {
		if tm, err := h.db.GetTokenMaster(ctx, lic.MasterToken); err == nil && tm != nil && !tm.IsShared {
			if err := h.db.MarkTokenMasterExpired(ctx, lic.MasterToken); err != nil {
				log.Printf("⚠️ [license] MarkTokenMasterExpired error: %v", err)
			}
		}
	}

	remaining, _ := h.db.ListActiveUserLicenses(ctx, lic.OwnerChatID)
	otherActive := pickFallbackActiveLicense(remaining, lic.TokenSHA)
	if otherActive != nil {
		if wasActiveSlot || cfg.ActiveTokenSHA == "" {
			_ = h.switchUserActiveToken(ctx, lic.OwnerChatID, otherActive)
			if b != nil && lic.OwnerChatID != 0 {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: lic.OwnerChatID,
					Text: fmt.Sprintf(`<h1>⏳ Slot Token Berakhir</h1>
<table border="1">
  <tr><th>📌 Status</th><th>Nilai</th></tr>
  <tr><td>❌ Slot Berakhir</td><td><code>%s</code></td></tr>
  <tr><td>🔄 Beralih Otomatis Ke</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Masa aktif salah satu slot token Anda telah habis. Sistem otomatis mengaktifkan slot tersisa tanpa memutus sesi userbot.</blockquote>
<hr/>
<p>GotdBot • License Lifecycle Manager</p>`,
						html.EscapeString(lic.Tier), html.EscapeString(databases.NormalizeTier(otherActive.Tier))),
					ParseMode: "HTML",
				})
			}
		}
		return
	}

	cfg.ActiveTokenSHA = ""
	cfg.Status = "Berhenti"
	cfg.MasaAktif = formatLicenseMasaAktif(lic)
	cfg.TindakanTerkini = "Sesi dicabut (token kedaluwarsa)"
	cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
	_ = h.db.SaveUserConfig(ctx, cfg)

	if h.broker != nil {
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
			ChatID:  lic.OwnerChatID,
			UserID:  lic.UserbotID,
			OwnerID: lic.OwnerChatID,
			Text:    lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
			Type:    "revoke_expired_session",
		})
	}

	if b != nil && lic.OwnerChatID != 0 {
		expStr := lic.ExpiresAt.Local().Format("02-01-2006 15:04:05")
		msgText := fmt.Sprintf(`<h1>⏳ Masa Aktif Token Berakhir</h1>
<blockquote expandable>Masa aktif lisensi userbot Anda telah habis pada <b>%s</b>.
Sesuai kebijakan sistem, sesi userbot Anda telah dicabut dan token lama tidak dapat digunakan kembali.</blockquote>
<hr/>
<p>Silakan ketik <b>/start</b> dan masukkan token baru dari Owner untuk mengaktifkan kembali userbot Anda (list, grup, catatan, dan pengaturan tetap tersimpan).</p>`, expStr)
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      lic.OwnerChatID,
			Text:        msgText,
			ParseMode:   "HTML",
			ReplyMarkup: assets.RemoveKeyboard,
		})
	}
}

// ensureActiveLicenseOrExpire checks if ownerID still has a valid active license.
// If the user only has an expired license, it marks it expired, revokes the session, and returns false.
func (h *Handler) ensureActiveLicenseOrExpire(ctx context.Context, b *botgotd.Bot, ownerID int64) (bool, string) {
	if h == nil || h.db == nil || ownerID == 0 {
		return true, ""
	}
	if active, _ := h.db.GetActiveUserLicense(ctx, ownerID); active != nil {
		return true, ""
	}
	latest, _ := h.db.GetLatestUserLicense(ctx, ownerID)
	if latest == nil {
		return true, ""
	}
	if strings.EqualFold(latest.Status, "active") && !latest.ExpiresAt.After(time.Now()) {
		h.expireSingleLicense(ctx, b, latest)
	}
	return false, latest.ExpiresAt.Local().Format("02-01-2006 15:04:05")
}
