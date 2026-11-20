package userbot

// Userbot command names are kept here so the dispatcher can be extended
// without editing the session lifecycle code.
var userbotCommandNames = map[string]struct{}{
	"help": {}, "server": {}, "eval": {}, "sh": {}, "ping": {}, "p": {},
	"code": {}, "setp": {}, "gcast": {}, "broadcast": {}, "cancel": {}, "cancelall": {},
	"fgcast": {}, "fucast": {}, "memcast": {}, "spam": {}, "spamcast": {}, "ucast": {},
	"addbl": {}, "rmbl": {}, "rmallbl": {}, "lsbl": {}, "addsudo": {}, "rmsudo": {},
	"lsudo": {}, "clearsudo": {},
	"save": {}, "get": {}, "cat": {}, "lsnote": {}, "notes": {}, "delnote": {},
	"clearnote": {}, "nofm": {}, "exportnote": {}, "importnote": {},
	"log": {}, "statuslog": {},
	"addblock": {}, "rmblock": {}, "lsblock": {}, "rmallblock": {},
	"alive": {}, "id": {}, "info": {}, "limit": {}, "sg": {},
	"ban": {}, "del": {}, "demote": {}, "dh": {}, "fullpromote": {}, "kick": {},
	"mute": {}, "pin": {}, "promote": {}, "purge": {}, "unban": {}, "unmute": {},
	"unpin": {}, "zombies": {},
	"gban": {}, "gkick": {}, "gmute": {}, "ungban": {}, "ungmute": {}, "lsgban": {}, "lsgmute": {},
	"ai": {}, "azan": {}, "font": {}, "fontlist": {}, "invite": {}, "join": {},
	"kickme": {}, "mirror": {}, "quotly": {}, "telegraph": {},
	"setbio": {}, "setname": {}, "setusername": {},
	"kang": {}, "strinfo": {},
	"joinvc": {}, "leavevc": {}, "play": {}, "setvc": {}, "startvc": {}, "stopvc": {},
	"video": {}, "ytmusic": {}, "ytvideo": {},
}

func isKnownUserbotCommand(name string) bool {
	if _, exists := userbotCommandNames[name]; exists {
		return true
	}
	_, mapped := commandPluginMap[name]
	return mapped
}
