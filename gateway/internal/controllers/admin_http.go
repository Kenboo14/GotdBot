package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
)

const maxInternalAdminBody = 32 << 10

func resolveInternalAdminKey() string {
	if k := strings.TrimSpace(os.Getenv("DASHBOARD_ADMIN_KEY")); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("PG_ADMIN_KEY"))
}

func constantTimeKeyMatch(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	pSum := sha256.Sum256([]byte(provided))
	eSum := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(pSum[:], eSum[:]) == 1
}

func (h *Handler) requireInternalAdminKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := resolveInternalAdminKey()
		if expected == "" {
			writeAdminError(w, http.StatusServiceUnavailable, "DASHBOARD_ADMIN_KEY belum dikonfigurasi")
			return
		}
		if !constantTimeKeyMatch(strings.TrimSpace(r.Header.Get("X-Internal-Admin-Key")), expected) {
			writeAdminError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func writeAdminJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAdminError(w http.ResponseWriter, status int, msg string) {
	writeAdminJSON(w, status, map[string]any{
		"ok":    false,
		"error": msg,
	})
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxInternalAdminBody)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode request json: %w", err)
	}
	return nil
}

func (h *Handler) registerAdminHTTPRoutes(mux *http.ServeMux, b *botgotd.Bot) {
	mux.HandleFunc("POST /internal/admin/auth/verify-magic", h.requireInternalAdminKey(h.handleAdminVerifyMagic))
	mux.HandleFunc("POST /internal/admin/security-alert", h.requireInternalAdminKey(func(w http.ResponseWriter, r *http.Request) {
		h.handleAdminSecurityAlert(w, r, b)
	}))
	mux.HandleFunc("GET /internal/admin/overview", h.requireInternalAdminKey(h.handleAdminOverview))
	mux.HandleFunc("GET /internal/admin/workers", h.requireInternalAdminKey(h.handleAdminWorkersList))
	mux.HandleFunc("POST /internal/admin/workers/move", h.requireInternalAdminKey(h.handleAdminWorkersMove))
	mux.HandleFunc("POST /internal/admin/workers/{id}/drain", h.requireInternalAdminKey(h.handleAdminWorkersDrain))
	mux.HandleFunc("GET /internal/admin/tokens/lookup", h.requireInternalAdminKey(h.handleAdminTokensLookup))
	mux.HandleFunc("POST /internal/admin/tokens/generate", h.requireInternalAdminKey(h.handleAdminTokensGenerate))
	mux.HandleFunc("POST /internal/admin/tokens/upgrade", h.requireInternalAdminKey(h.handleAdminTokensUpgrade))
	mux.HandleFunc("POST /internal/admin/tokens/extend", h.requireInternalAdminKey(h.handleAdminTokensExtend))
	mux.HandleFunc("POST /internal/admin/tokens/revoke", h.requireInternalAdminKey(h.handleAdminTokensRevoke))
	mux.HandleFunc("GET /internal/admin/plugins", h.requireInternalAdminKey(h.handleAdminPluginsGet))
	mux.HandleFunc("POST /internal/admin/plugins/toggle", h.requireInternalAdminKey(h.handleAdminPluginsToggle))
	mux.HandleFunc("GET /internal/admin/shop/pricing", h.requireInternalAdminKey(h.handleAdminShopPricingGet))
	mux.HandleFunc("PUT /internal/admin/shop/pricing", h.requireInternalAdminKey(h.handleAdminShopPricingPut))
	mux.HandleFunc("POST /internal/admin/simulasi", h.requireInternalAdminKey(h.handleAdminSimulasi))
	mux.HandleFunc("POST /internal/admin/broadcast", h.requireInternalAdminKey(h.handleAdminBroadcast))
}

func (h *Handler) handleAdminVerifyMagic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	ownerID, ok := globalDashMagic.Verify(req.Code)
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "kode login tidak valid atau kedaluwarsa")
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"owner_id": ownerID,
	})
}

func (h *Handler) handleAdminSecurityAlert(w http.ResponseWriter, r *http.Request, b *botgotd.Bot) {
	var req struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		IP     string `json:"ip"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Dashboard Security Alert"
	}
	detail := strings.TrimSpace(req.Detail)
	if req.IP != "" {
		detail = fmt.Sprintf("IP: %s | %s", req.IP, detail)
	}
	if globalDevMonitor != nil {
		globalDevMonitor.NotifyTraceback("dashboard", title, detail)
	}
	if b != nil {
		if ownerID := h.getOwnerID(); ownerID != 0 {
			card := renderMonitorRichCard("🛡️ Security Alert · WebAppDashboard", []monitorRichRow{
				{Label: "📌 Event", Value: html.EscapeString(title)},
				{Label: "🌐 IP", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(req.IP))},
				{Label: "📝 Detail", Value: html.EscapeString(req.Detail)},
			}, "ℹ️ Periksa akses dashboard owner Anda segera bila aktivitas ini tidak dikenali.", false)
			_, _ = sendMonitorRichMessage(r.Context(), b, ownerID, 0, card)
		}
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	snap := h.buildAdminOverviewSnapshot(r.Context())
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"overview": snap,
	})
}

func (h *Handler) handleAdminWorkersList(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.lb == nil {
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"workers": []WorkerStatus{},
			"known":   []string{},
		})
		return
	}
	known, err := h.lb.GetKnownWorkerIDs(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, err.Error())
		return
	}
	live, err := h.lb.ListWorkers(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"known":   known,
		"workers": live,
	})
}

func (h *Handler) handleAdminWorkersMove(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.lb == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "load balancer tidak tersedia")
		return
	}
	var req struct {
		UID    int64  `json:"uid"`
		Target string `json:"target"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := strings.TrimSpace(req.Target)
	if req.UID <= 0 || target == "" {
		writeAdminError(w, http.StatusBadRequest, "uid dan target wajib diisi")
		return
	}

	targetWorkerID := target
	targetPool := ""
	if strings.EqualFold(target, "hell") || strings.EqualFold(target, "normal") {
		live, err := h.lb.ListWorkers(r.Context())
		if err != nil {
			writeAdminError(w, http.StatusBadGateway, err.Error())
			return
		}
		chosen, ok := SelectLeastLoaded(live, strings.ToLower(target))
		if !ok {
			writeAdminError(w, http.StatusConflict, "tidak ada worker tersedia pada pool tujuan")
			return
		}
		targetWorkerID = chosen.WorkerID
		targetPool = chosen.Pool
	}

	if err := h.lb.TriggerMigration(r.Context(), req.UID, targetWorkerID, targetPool, "owner_dashboard_move"); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"uid":       req.UID,
		"worker_id": targetWorkerID,
		"pool":      targetPool,
	})
}

func (h *Handler) handleAdminWorkersDrain(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.lb == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "load balancer tidak tersedia")
		return
	}
	wid := strings.TrimSpace(r.PathValue("id"))
	if wid == "" {
		writeAdminError(w, http.StatusBadRequest, "worker id kosong")
		return
	}
	var req struct {
		Drain bool `json:"drain"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !req.Drain {
		if err := h.lb.SetWorkerDrain(r.Context(), wid, false); err != nil {
			writeAdminError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"worker_id": wid,
			"drain":     false,
			"moved":     0,
		})
		return
	}
	moved, err := h.lb.DrainWorker(r.Context(), wid)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"worker_id": wid,
		"drain":     true,
		"moved":     moved,
	})
}

func (h *Handler) handleAdminSimulasi(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.broker == nil || h.broker.Client == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "redis broker tidak tersedia")
		return
	}
	var req simulationControlMsg
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "scale" && action != "stop" {
		writeAdminError(w, http.StatusBadRequest, "action harus scale atau stop")
		return
	}
	if action == "scale" && req.Target <= 0 {
		writeAdminError(w, http.StatusBadRequest, "target harus lebih dari 0")
		return
	}
	payload, _ := json.Marshal(simulationControlMsg{Action: action, Target: req.Target})
	if err := h.broker.Client.Publish(r.Context(), "simulasi:control", payload).Err(); err != nil {
		writeAdminError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": action,
		"target": req.Target,
	})
}

func (h *Handler) handleAdminBroadcast(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.broker == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "redis broker tidak tersedia")
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		writeAdminError(w, http.StatusBadRequest, "pesan broadcast tidak boleh kosong")
		return
	}
	ownerID := h.getOwnerID()
	payload := models.PToService{
		ChatID:   ownerID,
		UserID:   ownerID,
		Username: "owner_dashboard",
		Text:     msg,
		Type:     "broadcast",
	}
	if err := h.broker.To(r.Context(), "to:ubot:broadcast", payload); err != nil {
		writeAdminError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"queued": true,
	})
}
