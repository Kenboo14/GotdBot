package controllers

import (
	"assterisk/gateway/internal/databases"
	"context"
	"crypto/rand"
	"html"
	"strings"
	"time"
)

const backupChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func GenerateBackupCode() string {
	out := make([]byte, 24)
	n := len(backupChars)
	lim := 256 - (256 % n)
	buf := make([]byte, 1)
	for i := 0; i < len(out); {
		_, _ = rand.Read(buf)
		if int(buf[0]) < lim {
			out[i] = backupChars[int(buf[0])%n]
			i++
		}
	}
	return "GOTD_BKP:" + string(out)
}

func (h *Handler) applyVoucherUpgrade(ctx context.Context, userID int64, rawToken string) (*databases.UserLicense, string, string) {
	token := strings.TrimSpace(rawToken)
	if !IsMasterTokenFormat(token) {
		return nil, "", "❌ Format token tidak valid. Pastikan token voucher yang Anda masukkan benar."
	}

	tm, err := h.db.GetTokenMaster(ctx, token)
	if err != nil || tm == nil {
		return nil, "", "❌ Token voucher tidak ditemukan dalam database sistem."
	}
	if tm.Status != "active" {
		return nil, "", "❌ Token voucher ini sudah kedaluwarsa atau dicabut oleh Owner."
	}
	if !tm.IsShared {
		used, _ := h.db.HasUsedMasterToken(ctx, tm.Token)
		if tm.ClaimCount >= 1 || used {
			return nil, "", "❌ Token voucher personal ini sudah pernah diklaim dan hanya bisa digunakan satu kali."
		}
	}

	newTier := databases.NormalizeTier(tm.Tier)
	dur := time.Duration(tm.DurationSeconds) * time.Second
	if dur <= 0 {
		dur = 7 * 24 * time.Hour
	}

	now := time.Now()
	if tm.IsShared {
		activeSlots, _ := h.db.ListActiveUserLicenses(ctx, userID)
		for _, slot := range activeSlots {
			if slot != nil && (slot.MasterToken == tm.Token || (newTier == "Free" && databases.NormalizeTier(slot.Tier) == "Free")) {
				if swErr := h.switchUserActiveToken(ctx, userID, slot); swErr != nil {
					return nil, "", "❌ Gagal beralih ke slot Free: " + html.EscapeString(swErr.Error())
				}
				return slot, "Beralih ke Slot " + databases.NormalizeTier(slot.Tier), ""
			}
		}
	}

	prevLic, _ := h.db.GetUserLicenseByOwnerAndMaster(ctx, userID, tm.Token)
	if prevLic == nil {
		prevLic, _ = h.db.GetUserLicenseBySHA(ctx, ComputeLegacyTokenSHA(tm.Token, userID))
	}
	if prevLic != nil {
		if prevLic.Status == "active" && prevLic.ExpiresAt.After(now) {
			if swErr := h.switchUserActiveToken(ctx, userID, prevLic); swErr != nil {
				return nil, "", "❌ Gagal mengaktifkan slot token: " + html.EscapeString(swErr.Error())
			}
			return prevLic, "Beralih ke Slot " + databases.NormalizeTier(prevLic.Tier), ""
		}
		return nil, "", "❌ Token ini sudah pernah digunakan pada akun Anda dan masa aktifnya telah berakhir."
	}
	tokenSHA := ComputeTokenSHA(tm.Token, userID)

	resultLic := &databases.UserLicense{
		TokenSHA:           tokenSHA,
		MasterToken:        tm.Token,
		OwnerChatID:        userID,
		UserbotID:          userID,
		Tier:               newTier,
		CanCustomWatermark: databases.GetTierPolicy(newTier).AllowCustomWatermark,
		ExpiresAt:          now.Add(dur),
		CreatedAt:          now,
		Status:             "active",
	}
	if err := h.db.SaveUserLicense(ctx, resultLic); err != nil {
		return nil, "", "❌ Gagal menyimpan lisensi baru: " + html.EscapeString(err.Error())
	}

	_ = h.db.IncrementTokenMasterClaim(ctx, tm.Token)
	if !tm.IsShared {
		_ = h.db.MarkTokenMasterExpired(ctx, tm.Token)
	}
	if swErr := h.switchUserActiveToken(ctx, userID, resultLic); swErr != nil {
		return nil, "", "❌ Gagal mengaktifkan slot token baru: " + html.EscapeString(swErr.Error())
	}
	return resultLic, "Slot Baru " + newTier, ""
}
