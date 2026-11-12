package botgotd

import (
	"context"

	"github.com/gotd/td/tg"
)

type ParseMode string

const (
	ParseModeHTML       ParseMode = "HTML"
	ParseModeMarkdown   ParseMode = "Markdown"
	ParseModeMarkdownV2 ParseMode = "MarkdownV2"
)

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	IsBot     bool   `json:"is_bot,omitempty"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

type MessageEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	URL           string `json:"url,omitempty"`
	User          *User  `json:"user,omitempty"`
	Language      string `json:"language,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

type Contact struct {
	PhoneNumber string `json:"phone_number"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name,omitempty"`
	UserID      int64  `json:"user_id,omitempty"`
	VCard       string `json:"vcard,omitempty"`
}

type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size,omitempty"`
}

type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Video struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Duration int    `json:"duration"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Audio struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Message struct {
	ID              int             `json:"message_id"`
	From            *User           `json:"from,omitempty"`
	Chat            Chat            `json:"chat"`
	Date            int             `json:"date"`
	Text            string          `json:"text,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	Photo           []PhotoSize     `json:"photo,omitempty"`
	Video           *Video          `json:"video,omitempty"`
	Document        *Document       `json:"document,omitempty"`
	Audio           *Audio          `json:"audio,omitempty"`
	Voice           *Voice          `json:"voice,omitempty"`
	Animation       *Document       `json:"animation,omitempty"`
	VideoNote       *Video          `json:"video_note,omitempty"`
	Sticker         any             `json:"sticker,omitempty"`
	Contact         *Contact        `json:"contact,omitempty"`
	ReplyToMessage  *Message        `json:"reply_to_message,omitempty"`
	MediaGroupID    string          `json:"media_group_id,omitempty"`
}

type MaybeInaccessibleMessage struct {
	Message *Message
}

type CallbackQuery struct {
	ID                 string                          `json:"id"`
	From               User                            `json:"from"`
	Message            *MaybeInaccessibleMessage       `json:"message,omitempty"`
	InlineMessageID    string                          `json:"inline_message_id,omitempty"`
	RawInlineMessageID tg.InputBotInlineMessageIDClass `json:"-"`
	Data               string                          `json:"data,omitempty"`
}

type Update struct {
	ID            int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	ChannelPost   *Message       `json:"channel_post,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
	InlineQuery   *InlineQuery   `json:"inline_query,omitempty"`
}

type InlineQuery struct {
	ID     string `json:"id"`
	From   User   `json:"from"`
	Query  string `json:"query"`
	Offset string `json:"offset"`
}

type InlineQueryResult interface{}

type InputMessageContent interface{}

type InputTextMessageContent struct {
	MessageText string    `json:"message_text"`
	ParseMode   ParseMode `json:"parse_mode,omitempty"`
}

type InlineQueryResultArticle struct {
	Type                string                `json:"type"`
	ID                  string                `json:"id"`
	Title               string                `json:"title"`
	Description         string                `json:"description,omitempty"`
	InputMessageContent InputMessageContent   `json:"input_message_content"`
	ReplyMarkup         *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type AnswerInlineQueryParams struct {
	InlineQueryID string              `json:"inline_query_id"`
	Results       []InlineQueryResult `json:"results"`
	CacheTime     int                 `json:"cache_time,omitempty"`
	IsPersonal    bool                `json:"is_personal,omitempty"`
}

type InlineKeyboardButton struct {
	Text                         string `json:"text"`
	URL                          string `json:"url,omitempty"`
	CallbackData                 string `json:"callback_data,omitempty"`
	SwitchInlineQueryCurrentChat string `json:"switch_inline_query_current_chat,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type KeyboardButton struct {
	Text           string `json:"text"`
	RequestContact bool   `json:"request_contact,omitempty"`
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

type ReplyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

type ReplyParameters struct {
	MessageID int `json:"message_id"`
}

type ReplyMarkup interface{}

type MessageId struct {
	ID int `json:"message_id"`
}

type SendMessageParams struct {
	ChatID          any              `json:"chat_id"`
	Text            string           `json:"text"`
	ParseMode       ParseMode        `json:"parse_mode,omitempty"`
	Entities        []MessageEntity  `json:"entities,omitempty"`
	ReplyMarkup     ReplyMarkup      `json:"reply_markup,omitempty"`
	ReplyParameters *ReplyParameters `json:"reply_parameters,omitempty"`
}

type CopyMessageParams struct {
	ChatID      any         `json:"chat_id"`
	FromChatID  any         `json:"from_chat_id"`
	MessageID   int         `json:"message_id"`
	Caption     string      `json:"caption,omitempty"`
	ParseMode   ParseMode   `json:"parse_mode,omitempty"`
	ReplyMarkup ReplyMarkup `json:"reply_markup,omitempty"`
}

type CopyMessagesParams struct {
	ChatID     any   `json:"chat_id"`
	FromChatID any   `json:"from_chat_id"`
	MessageIDs []int `json:"message_ids"`
}

type EditMessageTextParams struct {
	ChatID             any                             `json:"chat_id,omitempty"`
	MessageID          int                             `json:"message_id,omitempty"`
	InlineMessageID    string                          `json:"inline_message_id,omitempty"`
	RawInlineMessageID tg.InputBotInlineMessageIDClass `json:"-"`
	Text               string                          `json:"text"`
	ParseMode          ParseMode                       `json:"parse_mode,omitempty"`
	Entities           []MessageEntity                 `json:"entities,omitempty"`
	ReplyMarkup        ReplyMarkup                     `json:"reply_markup,omitempty"`
}

type EditMessageReplyMarkupParams struct {
	ChatID             any                             `json:"chat_id,omitempty"`
	MessageID          int                             `json:"message_id,omitempty"`
	InlineMessageID    string                          `json:"inline_message_id,omitempty"`
	RawInlineMessageID tg.InputBotInlineMessageIDClass `json:"-"`
	ReplyMarkup        ReplyMarkup                     `json:"reply_markup,omitempty"`
}

type DeleteMessageParams struct {
	ChatID    any `json:"chat_id"`
	MessageID int `json:"message_id"`
}

type DeleteMessagesParams struct {
	ChatID     any   `json:"chat_id"`
	MessageIDs []int `json:"message_ids"`
}

type AnswerCallbackQueryParams struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
	URL             string `json:"url,omitempty"`
}

type HandlerType int

const (
	HandlerTypeMessageText HandlerType = iota
	HandlerTypeCallbackQueryData
	HandlerTypeInlineQuery
)

type MatchType int

const (
	MatchTypeExact MatchType = iota
	MatchTypePrefix
)

type HandlerFunc func(ctx context.Context, b *Bot, update *Update)
