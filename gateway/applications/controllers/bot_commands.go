package controllers

import "github.com/go-telegram/bot"

// registerBotCommands is the single place for Bot API command routing.
// Handler implementations remain in their feature files.
func registerBotCommands(b *bot.Bot, h *Handler) {
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, h.User)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/status", bot.MatchTypeExact, h.User)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/create", bot.MatchTypeExact, h.Create)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/broadcast", bot.MatchTypePrefix, h.SimulateLoad)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/ping", bot.MatchTypeExact, h.PingCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, h.HelpCommandMenu)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/server", bot.MatchTypeExact, h.ServerCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "!server", bot.MatchTypeExact, h.ServerCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "!code", bot.MatchTypePrefix, h.CodeCommand)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "assist_help:", bot.MatchTypePrefix, h.HelpCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "assist_server:", bot.MatchTypePrefix, h.HelpCallback)
}
