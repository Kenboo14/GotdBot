package controllers

import "assterisk/gateway/internal/botgotd"

// registerBotCommands is the single place for Bot command routing.
// Handler implementations remain in their feature files.
func registerBotCommands(b *botgotd.Bot, h *Handler) {
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/start", botgotd.MatchTypeExact, h.User)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/status", botgotd.MatchTypeExact, h.User)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/broadcast", botgotd.MatchTypePrefix, h.SimulateLoad)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/ping", botgotd.MatchTypeExact, h.PingCommand)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/help", botgotd.MatchTypeExact, h.HelpCommandMenu)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/server", botgotd.MatchTypeExact, h.ServerCommand)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "!server", botgotd.MatchTypeExact, h.ServerCommand)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "!code", botgotd.MatchTypePrefix, h.CodeCommand)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "/token", botgotd.MatchTypePrefix, h.TokenCommand)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "token_set_duration:", botgotd.MatchTypePrefix, h.TokenCallback)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "assist_help:", botgotd.MatchTypePrefix, h.HelpCallback)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "assist_server:", botgotd.MatchTypePrefix, h.HelpCallback)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "note:", botgotd.MatchTypePrefix, h.NoteCallback)
}
