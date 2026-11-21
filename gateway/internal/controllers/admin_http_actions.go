package controllers

import (
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) handleAdminTokensLookup(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "database tidak tersedia")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeAdminError(w, http.StatusBadRequest, "parameter q wajib diisi")
		return
	}
	if IsMasterTokenPrefix(q) {
		tm, err := h.db.GetTokenMaster(r.Context(), q)
		if err != nil || tm == nil {
			writeAdminError(w, http.StatusNotFound, "voucher master tidak ditemukan")
			return
		}
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"kind":   "master",
			"master": tm,
		})
		return
	}
	lic, targetID := h.lookupLicenseByQuery(r.Context(), q)
	if lic == nil {
		writeAdminError(w, http.StatusNotFound, "lisensi pengguna tidak ditemukan")
		return
	}
	gCount, lCount := h.db.CountGroupsAndLists(r.Context(), targetID)
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"kind":         "license",
		"license":      lic,
		"groups_count": gCount,
		"lists_count":  lCount,
	})
}

func (h *Handler) handleAdminTokensGenerate(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "database tidak tersedia")
		return
	}
	var req struct {
		Tier     string `json:"tier"`
		Duration string `json:"duration"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	tier, ok := parseManagedTier(req.Tier)
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "tier harus free, pro, pro++, atau vip")
		return
	}
	if tier == "Free" {
		existing, _ := h.db.GetActiveSharedFreeToken(r.Context())
		if existing != nil {
			writeAdminJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"master": existing,
			})
			return
		}
		dur := 7 * 24 * time.Hour
		if strings.TrimSpace(req.Duration) != "" {
			if parsed, err := ParseDurationStr(req.Duration); err == nil {
				dur = parsed
			}
		}
		tm := &databases.TokenMaster{
			Token:           GenerateMasterToken(),
			Tier:            "Free",
			DurationSeconds: int64(dur.Seconds()),
			IsShared:        true,
			CreatedBy:       h.getOwnerID(),
			CreatedAt:       time.Now(),
			Status:          "active",
		}
		if err := h.db.SaveTokenMaster(r.Context(), tm); err != nil {
			writeAdminError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if globalDevMonitor != nil {
			globalDevMonitor.NotifyTokenCreated(h.getOwnerID(), tm.Tier, tm.Token, FormatDurationID(dur), true)
		}
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"master": tm,
		})
		return
	}

	dur, err := ParseDurationStr(req.Duration)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	tm := &databases.TokenMaster{
		Token:           GenerateMasterToken(),
		Tier:            tier,
		DurationSeconds: int64(dur.Seconds()),
		IsShared:        false,
		CreatedBy:       h.getOwnerID(),
		CreatedAt:       time.Now(),
		Status:          "active",
	}
	if err := h.db.SaveTokenMaster(r.Context(), tm); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if globalDevMonitor != nil {
		globalDevMonitor.NotifyTokenCreated(h.getOwnerID(), tm.Tier, tm.Token, FormatDurationID(dur), false)
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"master": tm,
	})
}

func (h *Handler) handleAdminTokensUpgrade(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "database tidak tersedia")
		return
	}
	var req struct {
		UID      int64  `json:"uid"`
		Tier     string `json:"tier"`
		Duration string `json:"duration"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.UID <= 0 {
		writeAdminError(w, http.StatusBadRequest, "uid tidak valid")
		return
	}
	tier, ok := parseManagedTier(req.Tier)
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "tier tidak valid")
		return
	}
	dur, err := ParseDurationStr(req.Duration)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	curLic, _ := h.db.GetActiveUserLicense(r.Context(), req.UID)
	if curLic == nil {
		curLic, _ = h.db.GetLatestUserLicense(r.Context(), req.UID)
	}
	var tokenSHA, masterToken string
	userbotID := req.UID
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
		tokenSHA = ComputeTokenSHA(masterToken, req.UID)
		expiresAt = now.Add(dur)
	}
	newLic := &databases.UserLicense{
		TokenSHA:           tokenSHA,
		MasterToken:        masterToken,
		OwnerChatID:        req.UID,
		UserbotID:          userbotID,
		Tier:               tier,
		CanCustomWatermark: databases.GetTierPolicy(tier).AllowCustomWatermark,
		ExpiresAt:          expiresAt,
		CreatedAt:          now,
		Status:             "active",
	}
	if err := h.db.SaveUserLicense(r.Context(), newLic); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.switchUserActiveToken(r.Context(), req.UID, newLic); err != nil {
		cfg := h.db.GetUserConfig(r.Context(), req.UID)
		cfg.MasaAktif = formatLicenseMasaAktif(newLic)
		_ = h.db.SaveUserConfig(r.Context(), cfg)
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"license": newLic,
	})
}

func (h *Handler) handleAdminTokensExtend(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "database tidak tersedia")
		return
	}
	var req struct {
		UID      int64  `json:"uid"`
		Duration string `json:"duration"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.UID <= 0 {
		writeAdminError(w, http.StatusBadRequest, "uid tidak valid")
		return
	}
	dur, err := ParseDurationStr(req.Duration)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	lic, _ := h.lookupLicenseByQuery(r.Context(), strconv.FormatInt(req.UID, 10))
	if lic == nil {
		writeAdminError(w, http.StatusNotFound, "riwayat lisensi tidak ditemukan")
		return
	}
	base := lic.ExpiresAt
	if base.Before(time.Now()) {
		base = time.Now()
	}
	lic.ExpiresAt = base.Add(dur)
	lic.Status = "active"
	lic.CanCustomWatermark = databases.GetTierPolicy(lic.Tier).AllowCustomWatermark
	if err := h.db.SaveUserLicense(r.Context(), lic); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg := h.db.GetUserConfig(r.Context(), req.UID)
	if cfg.ActiveTokenSHA == "" {
		cfg.ActiveTokenSHA = lic.TokenSHA
	}
	if cfg.ActiveTokenSHA == lic.TokenSHA {
		cfg.MasaAktif = formatLicenseMasaAktif(lic)
		_ = h.db.SaveUserConfig(r.Context(), cfg)
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"license": lic,
	})
}

func (h *Handler) handleAdminTokensRevoke(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "database tidak tersedia")
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		writeAdminError(w, http.StatusBadRequest, "query wajib diisi")
		return
	}
	if IsMasterTokenPrefix(q) {
		if err := h.db.RevokeTokenMaster(r.Context(), q); err != nil {
			writeAdminError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAdminJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": q, "kind": "master"})
		return
	}
	lic, _ := h.lookupLicenseByQuery(r.Context(), q)
	if lic == nil {
		writeAdminError(w, http.StatusNotFound, "lisensi tidak ditemukan")
		return
	}
	_ = h.db.MarkUserLicenseStatus(r.Context(), lic.TokenSHA, "revoked")
	cfg := h.db.GetUserConfig(r.Context(), lic.OwnerChatID)
	wasActive := cfg.ActiveTokenSHA == lic.TokenSHA
	if wasActive {
		h.snapshotCurrentTokenProfile(r.Context(), lic.OwnerChatID, "")
	}
	remaining, _ := h.db.ListActiveUserLicenses(r.Context(), lic.OwnerChatID)
	nextBest := pickFallbackActiveLicense(remaining, lic.TokenSHA)
	if nextBest != nil {
		if wasActive || cfg.ActiveTokenSHA == "" {
			_ = h.switchUserActiveToken(r.Context(), lic.OwnerChatID, nextBest)
		}
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"revoked":       lic.TokenSHA,
			"fallback_tier": nextBest.Tier,
		})
		return
	}
	cfg.ActiveTokenSHA = ""
	cfg.Status = "Berhenti"
	cfg.MasaAktif = "Dicabut"
	cfg.TindakanTerkini = "Lisensi dicabut oleh Owner"
	cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
	_ = h.db.SaveUserConfig(r.Context(), cfg)
	if h.broker != nil {
		_ = h.broker.To(r.Context(), "to:ubot:command", models.PToService{
			ChatID:  lic.OwnerChatID,
			UserID:  lic.UserbotID,
			OwnerID: lic.OwnerChatID,
			Text:    "Lisensi dicabut oleh Owner",
			Type:    "revoke_transferred_session",
		})
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"revoked": lic.TokenSHA,
		"stopped": true,
	})
}
