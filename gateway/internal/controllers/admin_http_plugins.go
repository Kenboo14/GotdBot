package controllers

import (
	"net/http"
	"strconv"
	"strings"
)

type adminPluginSubPayload struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type adminPluginModulePayload struct {
	Key         string                  `json:"key"`
	Title       string                  `json:"title"`
	Commands    string                  `json:"commands"`
	Enabled     bool                    `json:"enabled"`
	ActiveSubs  int                     `json:"active_subs"`
	TotalSubs   int                     `json:"total_subs"`
	SubCommands []adminPluginSubPayload `json:"sub_commands"`
}

func (h *Handler) buildPluginPayload(r *http.Request, ownerID int64) []adminPluginModulePayload {
	states := h.getEffectivePluginStates(r.Context(), ownerID)
	modules := make([]adminPluginModulePayload, 0, len(ownerPluginSpecs))
	for _, sp := range ownerPluginSpecs {
		modOn := states[sp.Key]
		activeSub, totalSub := h.moduleSubActiveCount(states, sp.Key)
		subs := pluginSubCommandsForModule(sp.Key)
		subList := make([]adminPluginSubPayload, 0, len(subs))
		for _, sub := range subs {
			subOn := modOn
			if subOn {
				if val, exists := states["cmd:"+sub.Key]; exists {
					subOn = val
				}
			}
			subList = append(subList, adminPluginSubPayload{
				Key:         sub.Key,
				Label:       sub.Label,
				Description: sub.Description,
				Enabled:     subOn,
			})
		}
		modules = append(modules, adminPluginModulePayload{
			Key:         sp.Key,
			Title:       sp.Title,
			Commands:    sp.Commands,
			Enabled:     modOn && activeSub > 0,
			ActiveSubs:  activeSub,
			TotalSubs:   totalSub,
			SubCommands: subList,
		})
	}
	return modules
}

func (h *Handler) handleAdminPluginsGet(w http.ResponseWriter, r *http.Request) {
	var ownerID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("uid")); raw != "" {
		ownerID, _ = strconv.ParseInt(raw, 10, 64)
	}
	if ownerID < 0 {
		ownerID = 0
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"uid":     ownerID,
		"modules": h.buildPluginPayload(r, ownerID),
	})
}

func (h *Handler) handleAdminPluginsToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID     int64  `json:"uid"`
		Action  string `json:"action"`
		Module  string `json:"module"`
		Cmd     string `json:"cmd"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.UID < 0 {
		req.UID = 0
	}
	ctx := r.Context()
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "module":
		if _, _, ok := h.togglePluginModule(ctx, req.UID, req.Module); !ok {
			writeAdminError(w, http.StatusBadRequest, "modul plugin tidak dikenal")
			return
		}
	case "cmd":
		if _, _, ok := h.togglePluginSubCommand(ctx, req.UID, req.Module, req.Cmd); !ok {
			writeAdminError(w, http.StatusBadRequest, "modul/command tidak dikenal")
			return
		}
	case "suball":
		if !h.setAllModuleSubCommands(ctx, req.UID, req.Module, req.Enabled) {
			writeAdminError(w, http.StatusBadRequest, "modul plugin tidak dikenal")
			return
		}
	case "all":
		h.setAllPluginStatesForTarget(ctx, req.UID, req.Enabled)
	case "reset":
		if req.UID > 0 {
			h.resetUserPluginStates(ctx, req.UID)
		}
	default:
		writeAdminError(w, http.StatusBadRequest, "action plugin tidak valid")
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"uid":     req.UID,
		"modules": h.buildPluginPayload(r, req.UID),
	})
}

func (h *Handler) handleAdminShopPricingGet(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"packages": h.listShopPackages(),
	})
}

func (h *Handler) handleAdminShopPricingPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target      string `json:"target"`
		PriceIDR    *int64 `json:"price_idr"`
		DiscountPct *int   `json:"discount_pct"`
	}
	if err := decodeAdminJSON(w, r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := strings.ToLower(strings.TrimSpace(req.Target))
	if req.PriceIDR != nil {
		baseID, ok := normalizeShopBaseID(target)
		if !ok {
			writeAdminError(w, http.StatusBadRequest, "target harga harus pro atau proplus")
			return
		}
		if err := h.shop.setItemPrice(baseID, *req.PriceIDR); err != nil {
			writeAdminError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.DiscountPct != nil {
		discTarget := target
		if discTarget != "all" {
			if baseID, ok := normalizeShopBaseID(target); ok {
				discTarget = baseID
			}
		}
		if err := h.shop.setDiscount(discTarget, *req.DiscountPct); err != nil {
			writeAdminError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"packages": h.listShopPackages(),
	})
}
