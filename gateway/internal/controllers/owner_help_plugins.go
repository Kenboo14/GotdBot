package controllers

import (
	"context"
	"os"
	"strings"
	"time"
)

const (
	ubotPluginsRedisKey  = "ubot:plugins:state"
	ubotPluginInvalidate = "cache:inv"
)

type ownerPluginSpec struct {
	Key      string
	Title    string
	Commands string
}

var ownerPluginSpecs = []ownerPluginSpec{
	{Key: "admin", Title: "Admin", Commands: "!ban !kick !mute !promote !pin !purge !zombies"},
	{Key: "notes", Title: "Notes", Commands: "!save !get !cat !lsnote !delnote !nofm #note"},
	{Key: "broadcast", Title: "Broadcast", Commands: "!gcast !ucast !fgcast !fucast !spam !memcast"},
	{Key: "blacklist", Title: "Blacklist", Commands: "!addbl !rmbl !rmallbl !lsbl"},
	{Key: "info", Title: "Info", Commands: "!alive !ping !server !id !info !limit !sg"},
	{Key: "extra", Title: "Extra", Commands: "!help !setp !ai !azan !font !telegraph !quotly"},
	{Key: "globals", Title: "Globals", Commands: "!gban !ungban !gmute !ungmute !gkick !lsgban"},
	{Key: "profile", Title: "Profile", Commands: "!setbio !setname !setusername"},
	{Key: "sticker", Title: "Sticker", Commands: "!kang !strinfo"},
	{Key: "logger", Title: "Logger", Commands: "!log !statuslog !addblock !rmblock !lsblock"},
	{Key: "sudo", Title: "Sudo", Commands: "!addsudo !rmsudo !lsudo !clearsudo"},
	{Key: "vctools", Title: "VCTools", Commands: "!startvc !stopvc !setvc !joinvc !leavevc !play"},
}

func defaultPluginStateMap() map[string]bool {
	states := make(map[string]bool, len(ownerPluginSpecs))
	for _, sp := range ownerPluginSpecs {
		states[sp.Key] = true
	}
	if raw := strings.TrimSpace(os.Getenv("DISABLED_PLUGINS")); raw != "" {
		for _, tok := range strings.Split(raw, ",") {
			k := strings.ToLower(strings.TrimSpace(tok))
			if _, ok := states[k]; ok {
				states[k] = false
			}
		}
	}
	return states
}

func (h *Handler) getPluginStates(ctx context.Context) map[string]bool {
	states := defaultPluginStateMap()
	if h == nil {
		return states
	}
	h.pluginMu.RLock()
	for k, v := range h.pluginStates {
		if _, ok := states[k]; ok {
			states[k] = v
		}
	}
	h.pluginMu.RUnlock()
	if h.broker != nil && h.broker.Client != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		vals, err := h.broker.Client.HGetAll(rCtx, ubotPluginsRedisKey).Result()
		cancel()
		if err == nil && len(vals) > 0 {
			h.pluginMu.Lock()
			if h.pluginStates == nil {
				h.pluginStates = make(map[string]bool, len(ownerPluginSpecs))
			}
			for k, raw := range vals {
				norm := strings.ToLower(strings.TrimSpace(k))
				if _, ok := states[norm]; ok {
					enabled := strings.TrimSpace(raw) != "0"
					states[norm] = enabled
					h.pluginStates[norm] = enabled
				}
			}
			h.pluginMu.Unlock()
		}
	}
	return states
}

func findOwnerPluginSpec(key string) (ownerPluginSpec, bool) {
	norm := strings.ToLower(strings.TrimSpace(key))
	for _, sp := range ownerPluginSpecs {
		if sp.Key == norm {
			return sp, true
		}
	}
	return ownerPluginSpec{}, false
}

func (h *Handler) publishPluginSync(ctx context.Context, fields map[string]any) {
	if h == nil || h.broker == nil || h.broker.Client == nil || len(fields) == 0 {
		return
	}
	rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = h.broker.Client.HSet(rCtx, ubotPluginsRedisKey, fields).Err()
	_ = h.broker.Client.Publish(rCtx, ubotPluginInvalidate, `{"kind":"plugin"}`).Err()
}

func (h *Handler) togglePluginState(ctx context.Context, modKey string) (ownerPluginSpec, bool, bool) {
	spec, ok := findOwnerPluginSpec(modKey)
	if !ok {
		return ownerPluginSpec{}, false, false
	}
	states := h.getPluginStates(ctx)
	next := !states[spec.Key]
	if h != nil {
		h.pluginMu.Lock()
		if h.pluginStates == nil {
			h.pluginStates = make(map[string]bool, len(ownerPluginSpecs))
		}
		h.pluginStates[spec.Key] = next
		h.pluginMu.Unlock()
	}
	val := "1"
	if !next {
		val = "0"
	}
	h.publishPluginSync(ctx, map[string]any{spec.Key: val})
	return spec, next, true
}

func (h *Handler) setAllPluginStates(ctx context.Context, enabled bool) {
	val := "1"
	if !enabled {
		val = "0"
	}
	fields := make(map[string]any, len(ownerPluginSpecs))
	if h != nil {
		h.pluginMu.Lock()
		if h.pluginStates == nil {
			h.pluginStates = make(map[string]bool, len(ownerPluginSpecs))
		}
		for _, sp := range ownerPluginSpecs {
			h.pluginStates[sp.Key] = enabled
			fields[sp.Key] = val
		}
		h.pluginMu.Unlock()
	}
	h.publishPluginSync(ctx, fields)
}
