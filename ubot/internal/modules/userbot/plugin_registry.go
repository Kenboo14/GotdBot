package userbot

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	redisKeyPluginState      = "ubot:plugins:state"
	redisKeyUserPluginPrefix = "ubot:plugins:user:"
)

type PluginModule uint8

const (
	PluginAdmin PluginModule = iota
	PluginNotes
	PluginBroadcast
	PluginBlacklist
	PluginExtra
	PluginGlobals
	PluginInfo
	PluginPMPermit
	PluginProfile
	PluginSticker
	PluginSudo
	PluginVCTools
	PluginLogger
	pluginModuleCount
)

var pluginModuleNames = [pluginModuleCount]string{
	PluginAdmin:     "admin",
	PluginNotes:     "notes",
	PluginBroadcast: "broadcast",
	PluginBlacklist: "blacklist",
	PluginExtra:     "extra",
	PluginGlobals:   "globals",
	PluginInfo:      "info",
	PluginPMPermit:  "pmpermit",
	PluginProfile:   "profile",
	PluginSticker:   "sticker",
	PluginSudo:      "sudo",
	PluginVCTools:   "vctools",
	PluginLogger:    "logger",
}

// Default status plugin saat ini: modul yang sudah terimplementasi aktif (true),
// modul yang belum diimplementasikan nonaktif (false) agar zero-overhead.
var defaultPluginEnabled = [pluginModuleCount]bool{
	PluginAdmin:     true,
	PluginNotes:     true,
	PluginBroadcast: true,
	PluginBlacklist: true,
	PluginExtra:     true,
	PluginGlobals:   true,
	PluginInfo:      true,
	PluginPMPermit:  false,
	PluginProfile:   true,
	PluginSticker:   true,
	PluginSudo:      true,
	PluginVCTools:   true,
	PluginLogger:    true,
}

var commandPluginMap = map[string]PluginModule{
	"ban": PluginAdmin, "del": PluginAdmin, "demote": PluginAdmin,
	"dh": PluginAdmin, "fullpromote": PluginAdmin, "kick": PluginAdmin,
	"mute": PluginAdmin, "pin": PluginAdmin, "promote": PluginAdmin,
	"purge": PluginAdmin, "unban": PluginAdmin, "unmute": PluginAdmin,
	"unpin": PluginAdmin, "zombies": PluginAdmin,

	"save": PluginNotes, "get": PluginNotes, "cat": PluginNotes,
	"lsnote": PluginNotes, "notes": PluginNotes, "delnote": PluginNotes,
	"clearnote": PluginNotes, "nofm": PluginNotes, "exportnote": PluginNotes,
	"importnote": PluginNotes,

	"gcast": PluginBroadcast, "broadcast": PluginBroadcast, "cancel": PluginBroadcast,
	"cancelall": PluginBroadcast, "fgcast": PluginBroadcast, "fucast": PluginBroadcast,
	"memcast": PluginBroadcast, "spam": PluginBroadcast, "spamcast": PluginBroadcast,
	"ucast": PluginBroadcast,

	"addbl": PluginBlacklist, "rmbl": PluginBlacklist, "rmallbl": PluginBlacklist, "lsbl": PluginBlacklist,

	"alive": PluginInfo, "ping": PluginInfo, "p": PluginInfo, "server": PluginInfo,
	"id": PluginInfo, "info": PluginInfo, "limit": PluginInfo, "sg": PluginInfo,

	"help": PluginExtra, "setp": PluginExtra, "code": PluginExtra, "eval": PluginExtra, "sh": PluginExtra,
	"ai": PluginExtra, "azan": PluginExtra, "font": PluginExtra, "fontlist": PluginExtra,
	"invite": PluginExtra, "join": PluginExtra, "kickme": PluginExtra,
	"mirror": PluginExtra, "quotly": PluginExtra, "telegraph": PluginExtra,

	"gban": PluginGlobals, "gkick": PluginGlobals, "gmute": PluginGlobals,
	"ungban": PluginGlobals, "ungmute": PluginGlobals, "lsgban": PluginGlobals, "lsgmute": PluginGlobals,

	"setbio": PluginProfile, "setname": PluginProfile, "setusername": PluginProfile,

	"kang": PluginSticker, "strinfo": PluginSticker,

	"log": PluginLogger, "statuslog": PluginLogger,
	"addblock": PluginLogger, "rmblock": PluginLogger, "lsblock": PluginLogger, "rmallblock": PluginLogger,

	"addsudo": PluginSudo, "rmsudo": PluginSudo, "lsudo": PluginSudo, "clearsudo": PluginSudo,

	"joinvc": PluginVCTools, "leavevc": PluginVCTools, "play": PluginVCTools,
	"setvc": PluginVCTools, "startvc": PluginVCTools, "stopvc": PluginVCTools,
	"video": PluginVCTools, "ytmusic": PluginVCTools, "ytvideo": PluginVCTools,
}

type userPluginOverride struct {
	mods map[PluginModule]bool
	cmds map[string]bool
}

// pluginRegistry menyimpan status aktif tiap plugin menggunakan atomic.Bool agar
// pengecekan di hot-path pesan bersifat lock-free dan tidak membebani CPU/RAM saat plugin Off.
type pluginRegistry struct {
	initOnce      sync.Once
	flags         [pluginModuleCount]atomic.Bool
	mu            sync.RWMutex
	globalCmdOff  map[string]bool
	userOverrides map[int64]*userPluginOverride
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

func (r *pluginRegistry) ensureInit() {
	r.initOnce.Do(func() {
		for i := PluginModule(0); i < pluginModuleCount; i++ {
			r.flags[i].Store(defaultPluginEnabled[i])
		}
		if disabledRaw := strings.TrimSpace(os.Getenv("DISABLED_PLUGINS")); disabledRaw != "" {
			for _, token := range strings.Split(disabledRaw, ",") {
				if mod, ok := parsePluginModule(token); ok {
					r.flags[mod].Store(false)
				}
			}
		}
	})
}

func parsePluginModule(name string) (PluginModule, bool) {
	norm := strings.ToLower(strings.TrimSpace(name))
	for i := PluginModule(0); i < pluginModuleCount; i++ {
		if pluginModuleNames[i] == norm {
			return i, true
		}
	}
	return 0, false
}

func pluginForCommand(name string) (PluginModule, bool) {
	mod, ok := commandPluginMap[strings.ToLower(name)]
	return mod, ok
}

func (s *Service) IsPluginEnabled(mod PluginModule) bool {
	if mod >= pluginModuleCount {
		return false
	}
	if s == nil {
		return defaultPluginEnabled[mod]
	}
	s.pluginState.ensureInit()
	return s.pluginState.flags[mod].Load()
}

func (s *Service) SetPluginEnabled(mod PluginModule, enabled bool) {
	if s == nil || mod >= pluginModuleCount {
		return
	}
	s.pluginState.ensureInit()
	s.pluginState.flags[mod].Store(enabled)
}

func (s *Service) IsPluginEnabledForOwner(ownerID int64, mod PluginModule) bool {
	if mod >= pluginModuleCount || mod == PluginPMPermit {
		return false
	}
	if s == nil {
		return defaultPluginEnabled[mod]
	}
	s.pluginState.ensureInit()
	if ownerID > 0 {
		s.pluginState.mu.RLock()
		ov := s.pluginState.userOverrides[ownerID]
		var found, val bool
		if ov != nil && ov.mods != nil {
			val, found = ov.mods[mod]
		}
		s.pluginState.mu.RUnlock()
		if found {
			return val
		}
	}
	return s.pluginState.flags[mod].Load()
}

func (s *Service) IsCommandEnabledForOwner(ownerID int64, cmdName string) bool {
	norm := canonPluginCmdKey(cmdName)
	if mod, mapped := pluginForCommand(strings.ToLower(strings.TrimSpace(cmdName))); mapped {
		if !s.IsPluginEnabledForOwner(ownerID, mod) {
			return false
		}
	}
	if s == nil {
		return true
	}
	s.pluginState.ensureInit()
	s.pluginState.mu.RLock()
	defer s.pluginState.mu.RUnlock()
	if ownerID > 0 {
		if ov := s.pluginState.userOverrides[ownerID]; ov != nil && ov.cmds != nil {
			if val, ok := ov.cmds[norm]; ok {
				return val
			}
		}
	}
	if s.pluginState.globalCmdOff != nil && s.pluginState.globalCmdOff[norm] {
		return false
	}
	return true
}

func applyPluginHashToMaps(vals map[string]string) (map[PluginModule]bool, map[string]bool) {
	mods := make(map[PluginModule]bool)
	cmds := make(map[string]bool)
	for rawKey, rawVal := range vals {
		enabled := strings.TrimSpace(rawVal) != "0"
		low := strings.ToLower(strings.TrimSpace(rawKey))
		if strings.HasPrefix(low, "cmd:") {
			cmdKey := canonPluginCmdKey(strings.TrimPrefix(low, "cmd:"))
			if cmdKey != "" {
				cmds[cmdKey] = enabled
			}
			continue
		}
		if mod, ok := parsePluginModule(low); ok && mod != PluginPMPermit {
			mods[mod] = enabled
		}
	}
	return mods, cmds
}

func (s *Service) SyncPluginsFromRedis(ctx context.Context) {
	if s == nil || s.redis == nil || s.redis.Client == nil {
		return
	}
	rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	vals, err := s.redis.Client.HGetAll(rCtx, redisKeyPluginState).Result()
	if err == nil && len(vals) > 0 {
		s.pluginState.ensureInit()
		mods, cmds := applyPluginHashToMaps(vals)
		for mod, enabled := range mods {
			s.pluginState.flags[mod].Store(enabled)
		}
		s.pluginState.mu.Lock()
		if s.pluginState.globalCmdOff == nil {
			s.pluginState.globalCmdOff = make(map[string]bool)
		}
		for cmdKey, enabled := range cmds {
			s.pluginState.globalCmdOff[cmdKey] = !enabled
		}
		s.pluginState.mu.Unlock()
	}
	var cursor uint64
	for {
		keys, nextCursor, scanErr := s.redis.Client.Scan(rCtx, cursor, redisKeyUserPluginPrefix+"*", 50).Result()
		if scanErr != nil {
			break
		}
		for _, fullKey := range keys {
			idPart := strings.TrimPrefix(fullKey, redisKeyUserPluginPrefix)
			if ownerID, pErr := strconv.ParseInt(idPart, 10, 64); pErr == nil && ownerID > 0 {
				s.SyncUserPluginsFromRedis(ctx, ownerID)
			}
		}
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}
}

func (s *Service) SyncUserPluginsFromRedis(ctx context.Context, ownerID int64) {
	if s == nil || s.redis == nil || s.redis.Client == nil || ownerID <= 0 {
		return
	}
	rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	vals, err := s.redis.Client.HGetAll(rCtx, redisKeyUserPluginPrefix+strconv.FormatInt(ownerID, 10)).Result()
	if err != nil {
		return
	}
	s.pluginState.ensureInit()
	s.pluginState.mu.Lock()
	defer s.pluginState.mu.Unlock()
	if s.pluginState.userOverrides == nil {
		s.pluginState.userOverrides = make(map[int64]*userPluginOverride)
	}
	if len(vals) == 0 {
		delete(s.pluginState.userOverrides, ownerID)
		return
	}
	mods, cmds := applyPluginHashToMaps(vals)
	s.pluginState.userOverrides[ownerID] = &userPluginOverride{
		mods: mods,
		cmds: cmds,
	}
}
