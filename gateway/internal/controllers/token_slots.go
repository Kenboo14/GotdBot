package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"strings"
	"time"
)

func tierBadge(tier string) string {
	switch databases.NormalizeTier(tier) {
	case "Pro++":
		return "👑"
	case "Pro+":
		return "⚡"
	case "Pro":
		return "💎"
	default:
		return "🆓"
	}
}

func shortTokenSwitchKey(tokenSHA string) string {
	trimmed := strings.TrimSpace(tokenSHA)
	if strings.HasPrefix(trimmed, tokenPrefixSession) {
		sum := sha256.Sum256([]byte(trimmed))
		return hex.EncodeToString(sum[:8])
	}
	trimmed = strings.TrimPrefix(trimmed, "GOTD_SHA:")
	if len(trimmed) > 16 {
		trimmed = trimmed[:16]
	}
	return trimmed
}

func pickFallbackActiveLicense(licenses []*databases.UserLicense, excludeSHA string) *databases.UserLicense {
	var best *databases.UserLicense
	for _, lic := range licenses {
		if lic == nil || lic.Status != "active" || lic.TokenSHA == excludeSHA {
			continue
		}
		if databases.IsBetterActiveLicense(lic, best) {
			best = lic
		}
	}
	return best
}

func (h *Handler) snapshotCurrentTokenProfile(ctx context.Context, ownerID int64, excludeSHA string) {
	if h == nil || h.db == nil || ownerID == 0 {
		return
	}
	cfg := h.db.GetUserConfig(ctx, ownerID)
	snapshotSHA := strings.TrimSpace(cfg.ActiveTokenSHA)
	if snapshotSHA == "" {
		licenses, _ := h.db.ListActiveUserLicenses(ctx, ownerID)
		if fallback := pickFallbackActiveLicense(licenses, excludeSHA); fallback != nil {
			snapshotSHA = fallback.TokenSHA
		}
	}
	if snapshotSHA != "" && snapshotSHA != excludeSHA {
		_ = h.db.SaveTokenProfile(ctx, databases.TokenProfileFromConfig(snapshotSHA, cfg))
	}
}

func (h *Handler) switchUserActiveToken(ctx context.Context, ownerID int64, targetLic *databases.UserLicense) error {
	if h == nil || h.db == nil || ownerID == 0 || targetLic == nil || strings.TrimSpace(targetLic.TokenSHA) == "" {
		return fmt.Errorf("invalid target token slot")
	}
	h.snapshotCurrentTokenProfile(ctx, ownerID, targetLic.TokenSHA)

	cfg := h.db.GetUserConfig(ctx, ownerID)
	prof, _ := h.db.GetTokenProfile(ctx, targetLic.TokenSHA)
	if prof == nil {
		if cfg.ActiveTokenSHA == targetLic.TokenSHA {
			prof = databases.TokenProfileFromConfig(targetLic.TokenSHA, cfg)
		} else {
			prof = databases.NewDefaultTokenProfile(targetLic.TokenSHA, ownerID)
		}
		if err := h.db.SaveTokenProfile(ctx, prof); err != nil {
			return fmt.Errorf("init target token profile: %w", err)
		}
	}
	prof.OwnerChatID = ownerID
	databases.ApplyTokenProfileToConfig(prof, cfg)
	cfg.MasaAktif = formatLicenseMasaAktif(targetLic)
	if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
		return fmt.Errorf("save switched config: %w", err)
	}
	return nil
}

func (h *Handler) transferTokenSlotBetweenAccounts(ctx context.Context, oldOwnerID, newChatID, newUserbotID int64, lic *databases.UserLicense) (*databases.UserLicense, error) {
	if h == nil || h.db == nil || lic == nil {
		return nil, fmt.Errorf("invalid transfer license")
	}
	oldSHA := lic.TokenSHA
	if oldOwnerID != 0 {
		oldCfg := h.db.GetUserConfig(ctx, oldOwnerID)
		if oldCfg.ActiveTokenSHA == oldSHA || oldCfg.ActiveTokenSHA == "" {
			_ = h.db.SaveTokenProfile(ctx, databases.TokenProfileFromConfig(oldSHA, oldCfg))
		}
	}

	oldProf, _ := h.db.GetTokenProfile(ctx, oldSHA)
	newSHA := ComputeRotatedTokenSHA(lic.MasterToken, newChatID, time.Now().UnixNano())
	if newUserbotID == 0 {
		newUserbotID = newChatID
	}

	var newProf *databases.TokenProfile
	if oldProf != nil {
		cp := *oldProf
		cp.TokenSHA = newSHA
		cp.OwnerChatID = newChatID
		cp.UpdatedAt = time.Now()
		newProf = &cp
	} else {
		newProf = databases.NewDefaultTokenProfile(newSHA, newChatID)
	}
	if err := h.db.SaveTokenProfile(ctx, newProf); err != nil {
		return nil, fmt.Errorf("save transferred profile: %w", err)
	}

	_ = h.db.MarkUserLicenseStatus(ctx, oldSHA, "transferred")

	newLic := &databases.UserLicense{
		TokenSHA:           newSHA,
		MasterToken:        lic.MasterToken,
		OwnerChatID:        newChatID,
		UserbotID:          newUserbotID,
		Tier:               lic.Tier,
		CanCustomWatermark: lic.CanCustomWatermark,
		ExpiresAt:          lic.ExpiresAt,
		CreatedAt:          lic.CreatedAt,
		Status:             "active",
	}
	if err := h.db.SaveUserLicense(ctx, newLic); err != nil {
		return nil, fmt.Errorf("save transferred license: %w", err)
	}
	if err := h.switchUserActiveToken(ctx, newChatID, newLic); err != nil {
		return nil, fmt.Errorf("activate transferred slot: %w", err)
	}

	if oldOwnerID != 0 && oldOwnerID != newChatID {
		remaining, _ := h.db.ListActiveUserLicenses(ctx, oldOwnerID)
		nextBest := pickFallbackActiveLicense(remaining, oldSHA)
		if nextBest != nil {
			oldCfg := h.db.GetUserConfig(ctx, oldOwnerID)
			if oldCfg.ActiveTokenSHA == oldSHA || oldCfg.ActiveTokenSHA == "" {
				_ = h.switchUserActiveToken(ctx, oldOwnerID, nextBest)
			}
			oldCfg = h.db.GetUserConfig(ctx, oldOwnerID)
			oldCfg.Status = "Berhenti"
			oldCfg.TindakanTerkini = "Token dipindahkan ke akun lain"
			oldCfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
			_ = h.db.SaveUserConfig(ctx, oldCfg)
		} else {
			oldCfg := h.db.GetUserConfig(ctx, oldOwnerID)
			oldCfg.ActiveTokenSHA = ""
			oldCfg.Status = "Berhenti"
			oldCfg.MasaAktif = "Masa aktif dialihkan"
			oldCfg.TindakanTerkini = "Token dipindahkan ke akun lain"
			oldCfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
			_ = h.db.SaveUserConfig(ctx, oldCfg)
		}

		if sq := h.db.GetSQLiteDB(); sq != nil {
			_, _ = sq.ExecContext(ctx, `DELETE FROM sessions WHERE chat_id = ? OR owner_id = ? OR account_id = ?`, oldOwnerID, oldOwnerID, oldOwnerID)
			if lic.UserbotID != 0 && lic.UserbotID != oldOwnerID {
				_, _ = sq.ExecContext(ctx, `DELETE FROM sessions WHERE chat_id = ? OR owner_id = ? OR account_id = ?`, lic.UserbotID, lic.UserbotID, lic.UserbotID)
			}
		}

		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
				ChatID:  oldOwnerID,
				UserID:  lic.UserbotID,
				OwnerID: oldOwnerID,
				Text:    "Akses token dipindahkan ke akun Telegram lain",
				Type:    "revoke_transferred_session",
			})
		}
	}
	return newLic, nil
}

func (h *Handler) ShowSwitchTokenMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID ...int) {
	effectiveID := h.GetEffectiveUserID(chatID, userID)
	licenses, _ := h.db.ListActiveUserLicenses(ctx, effectiveID)
	activeLic, _ := h.db.GetActiveUserLicense(ctx, effectiveID)
	cfg := h.db.GetUserConfig(ctx, effectiveID)

	if activeLic != nil && cfg.ActiveTokenSHA == "" {
		cfg.ActiveTokenSHA = activeLic.TokenSHA
		_ = h.db.SaveUserConfig(ctx, cfg)
	}

	var sb strings.Builder
	sb.WriteString("<h1>🔄 Kelola &amp; Ganti Slot Token</h1>\n")
	if len(licenses) == 0 {
		sb.WriteString("<blockquote expandable>Anda belum memiliki slot token aktif.\nKetuk <b>🔑 Tambah Token 🔑</b> di menu Settings untuk mengaktifkan atau menambah slot token baru.</blockquote>\n")
	} else {
		sb.WriteString("<blockquote expandable>Setiap token menyimpan konfigurasi tersendiri (List, Grup, Topic, Jeda, Timer, Watermark, Admin, dan Email). Pilih salah satu slot di bawah untuk mengganti token aktif saat ini.</blockquote>\n")
		sb.WriteString("<table border=\"1\">\n")
		sb.WriteString("  <tr><th>#</th><th>🎖️ Slot</th><th>📦 Data</th><th>⏳ Berlaku</th><th>📌 Status</th></tr>\n")
		for idx, lic := range licenses {
			isActive := activeLic != nil && activeLic.TokenSHA == lic.TokenSHA
			statusTag := "Nonaktif"
			if isActive {
				statusTag = "✅ AKTIF"
			}
			var prof *databases.TokenProfile
			if isActive {
				prof = databases.TokenProfileFromConfig(lic.TokenSHA, cfg)
			} else {
				prof, _ = h.db.GetTokenProfile(ctx, lic.TokenSHA)
			}
			listCount := 0
			groupCount := 0
			if prof != nil {
				listCount = len(prof.Lists)
				groupCount = len(prof.Groups)
			}
			key := shortTokenSwitchKey(lic.TokenSHA)
			expStr := lic.ExpiresAt.Local().Format("02 Jan 06 15:04")
			fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td>%s <b>%s</b> (<code>...%s</code>)</td><td><code>%dL · %dG</code></td><td><code>%s</code></td><td><b>%s</b></td></tr>\n",
				idx+1, tierBadge(lic.Tier), html.EscapeString(databases.NormalizeTier(lic.Tier)),
				html.EscapeString(key), listCount, groupCount, html.EscapeString(expStr), statusTag,
			)
		}
		sb.WriteString("</table>\n")
		for idx, lic := range licenses {
			isActive := activeLic != nil && activeLic.TokenSHA == lic.TokenSHA
			key := shortTokenSwitchKey(lic.TokenSHA)
			tierName := databases.NormalizeTier(lic.Tier)
			sb.WriteString("<tg-button-row>\n")
			if isActive {
				fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="panel:token:switch:%s" style="primary">✅ #%d %s (Aktif)</tg-button>`+"\n",
					html.EscapeString(key), idx+1, html.EscapeString(tierName))
			} else {
				fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="panel:token:switch:%s">🔄 Pilih #%d %s</tg-button>`+"\n",
					html.EscapeString(key), idx+1, html.EscapeString(tierName))
			}
			sb.WriteString("</tg-button-row>\n")
		}
	}
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:token:menu\">🔄 Refresh</tg-button>\n  <tg-button type=\"callback_data\" data=\"panel:settings:home\">◀️ Kembali</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Multi-Slot Token Manager</p>")

	msgText := sb.String()
	if len(editMsgID) > 0 && editMsgID[0] > 0 {
		_, _ = editMonitorRichMessageWithMarkup(ctx, b, chatID, editMsgID[0], msgText, nil)
		return
	}
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        msgText,
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
}

func (h *Handler) handleTokenSwitchCallback(ctx context.Context, b *botgotd.Bot, chatID, messageID int64, cb *botgotd.CallbackQuery) {
	key := strings.TrimPrefix(cb.Data, "panel:token:switch:")
	effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
	licenses, _ := h.db.ListActiveUserLicenses(ctx, effectiveID)

	var target *databases.UserLicense
	for _, lic := range licenses {
		if shortTokenSwitchKey(lic.TokenSHA) == key || lic.TokenSHA == key {
			target = lic
			break
		}
	}
	if target == nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "❌ Slot token tidak ditemukan atau sudah kedaluwarsa.",
			ShowAlert:       true,
		})
		return
	}

	cfg := h.db.GetUserConfig(ctx, effectiveID)
	if cfg.ActiveTokenSHA == target.TokenSHA {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            fmt.Sprintf("ℹ️ Slot %s sudah aktif.", databases.NormalizeTier(target.Tier)),
		})
		return
	}

	if err := h.switchUserActiveToken(ctx, effectiveID, target); err != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "❌ Gagal mengganti slot token.",
			ShowAlert:       true,
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            fmt.Sprintf("✅ Beralih ke %s", databases.NormalizeTier(target.Tier)),
	})
	h.ShowSwitchTokenMenu(ctx, b, chatID, cb.From.ID, int(messageID))
}
