package botgotd

import (
	"database/sql"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/Kenboo14/GoBot/engine"
)

// Type aliases forwarding botgotd symbols to github.com/Kenboo14/GoBot.
type (
	ParseMode                    = gobot.ParseMode
	User                         = gobot.User
	Chat                         = gobot.Chat
	MessageEntity                = gobot.MessageEntity
	Contact                      = gobot.Contact
	PhotoSize                    = gobot.PhotoSize
	Document                     = gobot.Document
	Video                        = gobot.Video
	Audio                        = gobot.Audio
	Voice                        = gobot.Voice
	Message                      = gobot.Message
	MaybeInaccessibleMessage     = gobot.MaybeInaccessibleMessage
	CallbackQuery                = gobot.CallbackQuery
	Update                       = gobot.Update
	InlineQuery                  = gobot.InlineQuery
	InlineQueryResult            = gobot.InlineQueryResult
	InputMessageContent          = gobot.InputMessageContent
	InputTextMessageContent      = gobot.InputTextMessageContent
	InlineQueryResultArticle     = gobot.InlineQueryResultArticle
	AnswerInlineQueryParams      = gobot.AnswerInlineQueryParams
	InlineKeyboardButton         = gobot.InlineKeyboardButton
	InlineKeyboardMarkup         = gobot.InlineKeyboardMarkup
	KeyboardButton               = gobot.KeyboardButton
	ReplyKeyboardMarkup          = gobot.ReplyKeyboardMarkup
	ReplyKeyboardRemove          = gobot.ReplyKeyboardRemove
	ReplyParameters              = gobot.ReplyParameters
	ReplyMarkup                  = gobot.ReplyMarkup
	MessageId                    = gobot.MessageId
	SendMessageParams            = gobot.SendMessageParams
	CopyMessageParams            = gobot.CopyMessageParams
	CopyMessagesParams           = gobot.CopyMessagesParams
	EditMessageTextParams        = gobot.EditMessageTextParams
	EditMessageReplyMarkupParams = gobot.EditMessageReplyMarkupParams
	DeleteMessageParams          = gobot.DeleteMessageParams
	DeleteMessagesParams         = gobot.DeleteMessagesParams
	AnswerCallbackQueryParams    = gobot.AnswerCallbackQueryParams
	PeerStorage                  = gobot.PeerStorage
	HandlerType                  = gobot.HandlerType
	MatchType                    = gobot.MatchType
	HandlerFunc                  = gobot.HandlerFunc
	Bot                          = gobot.Bot
)

const (
	ParseModeHTML       = gobot.ParseModeHTML
	ParseModeMarkdown   = gobot.ParseModeMarkdown
	ParseModeMarkdownV2 = gobot.ParseModeMarkdownV2

	HandlerTypeMessageText       = gobot.HandlerTypeMessageText
	HandlerTypeCallbackQueryData = gobot.HandlerTypeCallbackQueryData
	HandlerTypeInlineQuery       = gobot.HandlerTypeInlineQuery

	MatchTypeExact  = gobot.MatchTypeExact
	MatchTypePrefix = gobot.MatchTypePrefix
)

var (
	FormatRichHTML = gobot.FormatRichHTML
	CleanHTML      = gobot.CleanHTML
	IsRichContent  = gobot.IsRichContent
	ParseHTML      = gobot.ParseHTML
)

func NewPeerStorage(sqliteDB *sql.DB) *PeerStorage {
	return gobot.NewPeerStorage(sqliteDB)
}

func NewEngineBot(conn *engine.Conn, token string, peersStorage *PeerStorage) *Bot {
	return gobot.NewEngineBot(conn, token, peersStorage)
}

func NewTestBot(serverURL, token string) *Bot {
	return gobot.NewTestBot(serverURL, token)
}
