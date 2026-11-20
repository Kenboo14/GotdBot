package userbot

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

// pluginRegistry menyimpan status aktif tiap plugin menggunakan atomic.Bool agar
// pengecekan di hot-path pesan bersifat lock-free dan tidak membebani CPU/RAM saat plugin Off.
type pluginRegistry struct {
	initOnce sync.Once
	flags    [pluginModuleCount]atomic.Bool
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
