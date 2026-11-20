package controllers

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ubotPluginsRedisKey      = "ubot:plugins:state"
	ubotPluginsUserKeyPrefix = "ubot:plugins:user:"
	ubotPluginInvalidate     = "cache:inv"
)

type ownerPluginSpec struct {
	Key      string
	Title    string
	Commands string
}

type pluginSubSpec struct {
	Key         string
	Label       string
	Description string
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

var loggerPluginSubSpecs = []pluginSubSpec{
	{Key: "log", Label: "Log", Description: "Aktifkan atau matikan Log Bot PM & Mention (!log on|off)."},
	{Key: "statuslog", Label: "Statuslog", Description: "Periksa status logger aktif/mati."},
	{Key: "addblock", Label: "Addblock", Description: "Blokir pengirim agar tidak diteruskan ke Log Bot."},
	{Key: "rmblock", Label: "Rmblock", Description: "Hapus pengguna dari daftar blokir Log Bot."},
	{Key: "lsblock", Label: "Lsblock", Description: "Tampilkan daftar pengguna terblokir di Log Bot."},
	{Key: "rmallblock", Label: "Rmallblock", Description: "Kosongkan seluruh daftar blokir Log Bot."},
}

func canonPluginCmdKey(cmd string) string {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "notes":
		return "lsnote"
	case "broadcast":
		return "gcast"
	case "p":
		return "ping"
	case "setprefix":
		return "setp"
	default:
		return strings.ToLower(strings.TrimSpace(cmd))
	}
}

func pluginSubCommandsForModule(modKey string) []pluginSubSpec {
	norm := strings.ToLower(strings.TrimSpace(modKey))
	if norm == "logger" {
		return loggerPluginSubSpecs
	}
	for _, cat := range helpCatalog {
		if cat.Key != norm {
			continue
		}
		out := make([]pluginSubSpec, 0, len(cat.Commands))
		for _, c := range cat.Commands {
			if norm == "extra" && c.Key == "logger" {
				continue
			}
			out = append(out, pluginSubSpec{
				Key:         canonPluginCmdKey(c.Key),
				Label:       c.Label,
				Description: c.Description,
			})
		}
		return out
	}
	return nil
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

func defaultPluginStateMap() map[string]bool {
	states := make(map[string]bool, len(ownerPluginSpecs)*2)
	for _, sp := range ownerPluginSpecs {
		states[sp.Key] = true
	}
	if raw := strings.TrimSpace(os.Getenv("DISABLED_PLUGINS")); raw != "" {
		for _, tok := range strings.Split(raw, ",") {
			k := strings.ToLower(strings.TrimSpace(tok))
			if _, ok := findOwnerPluginSpec(k); ok {
				states[k] = false
			}
		}
	}
	return states
}

func pluginHashRedisKey(ownerID int64) string {
	if ownerID > 0 {
		return ubotPluginsUserKeyPrefix + strconv.FormatInt(ownerID, 10)
	}
	return ubotPluginsRedisKey
}

func (h *Handler) fetchHashStatesFromRedis(ctx context.Context, ownerID int64) {
	if h == nil || h.broker == nil || h.broker.Client == nil {
		return
	}
	rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	vals, err := h.broker.Client.HGetAll(rCtx, pluginHashRedisKey(ownerID)).Result()
	cancel()
	if err != nil {
		return
	}
	h.pluginMu.Lock()
	defer h.pluginMu.Unlock()
	if ownerID > 0 {
		if h.userPluginStates == nil {
			h.userPluginStates = make(map[int64]map[string]bool)
		}
		if len(vals) == 0 {
			return
		}
		m := make(map[string]bool, len(vals))
		for k, raw := range vals {
			m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(raw) != "0"
		}
		h.userPluginStates[ownerID] = m
		return
	}
	if h.pluginStates == nil {
		h.pluginStates = defaultPluginStateMap()
	}
	for k, raw := range vals {
		h.pluginStates[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(raw) != "0"
	}
}

func (h *Handler) getEffectivePluginStates(ctx context.Context, ownerID int64) map[string]bool {
	states := defaultPluginStateMap()
	if h == nil {
		return states
	}
	h.fetchHashStatesFromRedis(ctx, 0)
	if ownerID > 0 {
		h.fetchHashStatesFromRedis(ctx, ownerID)
	}
	h.pluginMu.RLock()
	for k, v := range h.pluginStates {
		states[k] = v
	}
	if ownerID > 0 && h.userPluginStates != nil {
		for k, v := range h.userPluginStates[ownerID] {
			states[k] = v
		}
	}
	h.pluginMu.RUnlock()
	return states
}

func (h *Handler) isEffectiveModuleEnabled(ctx context.Context, ownerID int64, modKey string) bool {
	norm := strings.ToLower(strings.TrimSpace(modKey))
	if norm == "pmpermit" {
		return false
	}
	states := h.getEffectivePluginStates(ctx, ownerID)
	v, ok := states[norm]
	if !ok {
		return true
	}
	return v
}

func (h *Handler) isEffectiveCommandEnabled(ctx context.Context, ownerID int64, modKey, cmdKey string) bool {
	if strings.EqualFold(strings.TrimSpace(modKey), "pmpermit") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(modKey), "extra") && strings.EqualFold(strings.TrimSpace(cmdKey), "logger") {
		return h.isEffectiveModuleEnabled(ctx, ownerID, "logger")
	}
	states := h.getEffectivePluginStates(ctx, ownerID)
	if modOn, ok := states[strings.ToLower(strings.TrimSpace(modKey))]; ok && !modOn {
		return false
	}
	ck := "cmd:" + canonPluginCmdKey(cmdKey)
	if cmdOn, ok := states[ck]; ok {
		return cmdOn
	}
	return true
}

func (h *Handler) moduleSubActiveCount(states map[string]bool, modKey string) (int, int) {
	subs := pluginSubCommandsForModule(modKey)
	if len(subs) == 0 {
		if states[modKey] {
			return 1, 1
		}
		return 0, 1
	}
	modOn := true
	if v, ok := states[modKey]; ok {
		modOn = v
	}
	if !modOn {
		return 0, len(subs)
	}
	active := 0
	for _, sub := range subs {
		if cmdOn, exists := states["cmd:"+sub.Key]; !exists || cmdOn {
			active++
		}
	}
	return active, len(subs)
}

func (h *Handler) writePluginFields(ctx context.Context, ownerID int64, fields map[string]bool) {
	if h == nil || len(fields) == 0 {
		return
	}
	redisFields := make(map[string]any, len(fields))
	h.pluginMu.Lock()
	var targetMap map[string]bool
	if ownerID > 0 {
		if h.userPluginStates == nil {
			h.userPluginStates = make(map[int64]map[string]bool)
		}
		if h.userPluginStates[ownerID] == nil {
			h.userPluginStates[ownerID] = make(map[string]bool)
		}
		targetMap = h.userPluginStates[ownerID]
	} else {
		if h.pluginStates == nil {
			h.pluginStates = defaultPluginStateMap()
		}
		targetMap = h.pluginStates
	}
	for k, v := range fields {
		norm := strings.ToLower(strings.TrimSpace(k))
		targetMap[norm] = v
		if v {
			redisFields[norm] = "1"
		} else {
			redisFields[norm] = "0"
		}
	}
	h.pluginMu.Unlock()

	if h.broker != nil && h.broker.Client != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = h.broker.Client.HSet(rCtx, pluginHashRedisKey(ownerID), redisFields).Err()
		_ = h.broker.Client.Publish(rCtx, ubotPluginInvalidate, fmt.Sprintf(`{"kind":"plugin","owner_id":%d}`, ownerID)).Err()
	}
}

func (h *Handler) resetUserPluginStates(ctx context.Context, ownerID int64) {
	if h == nil || ownerID <= 0 {
		return
	}
	h.pluginMu.Lock()
	if h.userPluginStates != nil {
		delete(h.userPluginStates, ownerID)
	}
	h.pluginMu.Unlock()
	if h.broker != nil && h.broker.Client != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = h.broker.Client.Del(rCtx, pluginHashRedisKey(ownerID)).Err()
		_ = h.broker.Client.Publish(rCtx, ubotPluginInvalidate, fmt.Sprintf(`{"kind":"plugin","owner_id":%d}`, ownerID)).Err()
	}
}

func (h *Handler) togglePluginModule(ctx context.Context, ownerID int64, modKey string) (ownerPluginSpec, bool, bool) {
	sp, ok := findOwnerPluginSpec(modKey)
	if !ok {
		return ownerPluginSpec{}, false, false
	}
	states := h.getEffectivePluginStates(ctx, ownerID)
	next := !states[sp.Key]
	h.writePluginFields(ctx, ownerID, map[string]bool{sp.Key: next})
	return sp, next, true
}

func (h *Handler) togglePluginSubCommand(ctx context.Context, ownerID int64, modKey, cmdKey string) (string, bool, bool) {
	if _, ok := findOwnerPluginSpec(modKey); !ok {
		return "", false, false
	}
	canon := canonPluginCmdKey(cmdKey)
	states := h.getEffectivePluginStates(ctx, ownerID)
	cur := true
	if v, exists := states["cmd:"+canon]; exists {
		cur = v
	}
	next := !cur
	fields := map[string]bool{"cmd:" + canon: next}
	if next && !states[modKey] {
		fields[modKey] = true
	}
	h.writePluginFields(ctx, ownerID, fields)
	return canon, next, true
}

func (h *Handler) setAllModuleSubCommands(ctx context.Context, ownerID int64, modKey string, enabled bool) bool {
	if _, ok := findOwnerPluginSpec(modKey); !ok {
		return false
	}
	subs := pluginSubCommandsForModule(modKey)
	fields := make(map[string]bool, len(subs)+1)
	fields[modKey] = enabled
	for _, sub := range subs {
		fields["cmd:"+sub.Key] = enabled
	}
	h.writePluginFields(ctx, ownerID, fields)
	return true
}

func (h *Handler) setAllPluginStatesForTarget(ctx context.Context, ownerID int64, enabled bool) {
	fields := make(map[string]bool, len(ownerPluginSpecs)*10)
	for _, sp := range ownerPluginSpecs {
		fields[sp.Key] = enabled
		for _, sub := range pluginSubCommandsForModule(sp.Key) {
			fields["cmd:"+sub.Key] = enabled
		}
	}
	h.writePluginFields(ctx, ownerID, fields)
}
