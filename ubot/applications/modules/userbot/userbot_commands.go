package userbot

// Userbot command names are kept here so the dispatcher can be extended
// without editing the session lifecycle code.
var userbotCommandNames = map[string]struct{}{
	"help": {}, "server": {}, "eval": {}, "sh": {}, "ping": {}, "p": {},
	"code": {}, "setp": {}, "gcast": {}, "broadcast": {}, "cancel": {}, "addbl": {},
	"rmbl": {}, "rmallbl": {}, "lsbl": {}, "addsudo": {}, "rmsudo": {},
	"save": {}, "get": {}, "cat": {}, "lsnote": {}, "notes": {}, "delnote": {},
	"clearnote": {}, "nofm": {}, "exportnote": {}, "importnote": {},
}
