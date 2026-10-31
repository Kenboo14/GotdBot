package userbot

import (
	"context"
	"strings"

	"github.com/gotd/td/tg"
)

type EvalChat struct {
	ID    int64
	Type  string
	Title string
}

// EvalUser is the small, stable user view exposed to !eval code.
type EvalUser struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

// EvalMessage provides Pyrogram-like convenience fields while keeping the
// underlying gotd message available through Raw.
type EvalMessage struct {
	ID             int
	Text           string
	Caption        string
	Arguments      string
	Chat           *EvalChat
	FromUser       *EvalUser
	ReplyToMessage *EvalMessage
	Raw            *tg.Message
	replyFunc      func(context.Context, string) error
}

// Reply sends text to the same peer as the command.  It intentionally
// returns an error so evaluated code can handle Telegram/network failures.
func (m *EvalMessage) Reply(text string) error {
	if m == nil || m.replyFunc == nil {
		return nil
	}
	return m.replyFunc(nil, text)
}

// ReplyText is an explicit alias for Reply, matching the naming used by
// common Telegram wrappers.
func (m *EvalMessage) ReplyText(text string) error { return m.Reply(text) }

// GetArg mirrors the useful part of Pyrogram's get_arg helper.
// It returns the replied message text when no command argument is present,
// otherwise it returns the text after the command.
func (m *EvalMessage) GetArg() string {
	if m == nil {
		return ""
	}
	if m.ReplyToMessage != nil && strings.TrimSpace(m.Arguments) == "" {
		return strings.TrimSpace(m.ReplyToMessage.Text)
	}
	return strings.TrimSpace(m.Arguments)
}

// GetMessage mirrors get_message: it returns the reply when present, or the
// command argument text otherwise.
func (m *EvalMessage) GetMessage() any {
	if m == nil {
		return ""
	}
	if m.ReplyToMessage != nil {
		return m.ReplyToMessage
	}
	return m.Arguments
}

func newEvalMessage(msg, reply *tg.Message, entities tg.Entities, replyFunc func(context.Context, string) error, args ...string) *EvalMessage {
	// Yaegi cannot safely bind a typed nil pointer as an exported variable;
	// when no reply exists, Replit is therefore an empty wrapper while
	// m.ReplyToMessage remains nil. This keeps expressions panic-free.
	result := &EvalMessage{Raw: msg, Chat: evalChat(msg, entities), FromUser: evalUser(msg, entities)}
	result.replyFunc = replyFunc
	if msg == nil {
		return result
	}
	result.ID = msg.ID
	result.Text = msg.Message
	// gotd stores textual captions in Message.Message as well; keep Caption
	// as an API-compatible alias for callers.
	result.Caption = msg.Message
	if len(args) > 0 {
		result.Arguments = args[0]
	}
	if reply != nil {
		result.ReplyToMessage = newEvalMessage(reply, nil, entities, replyFunc)
	}
	return result
}

func evalUser(msg *tg.Message, entities tg.Entities) *EvalUser {
	result := &EvalUser{}
	if msg == nil {
		return result
	}
	user, ok := msg.FromID.(*tg.PeerUser)
	if !ok {
		return result
	}
	result.ID = user.UserID
	if value := entities.Users[user.UserID]; value != nil {
		result.Username = value.Username
		result.FirstName = value.FirstName
		result.LastName = value.LastName
	}
	return result
}

func evalChat(msg *tg.Message, entities tg.Entities) *EvalChat {
	result := &EvalChat{}
	if msg == nil {
		return result
	}
	switch chat := msg.PeerID.(type) {
	case *tg.PeerUser:
		result.ID, result.Type = chat.UserID, "private"
	case *tg.PeerChat:
		result.ID, result.Type = chat.ChatID, "group"
		if value := entities.Chats[chat.ChatID]; value != nil {
			result.Title = value.Title
		}
	case *tg.PeerChannel:
		result.ID, result.Type = chat.ChannelID, "supergroup"
		if value := entities.Channels[chat.ChannelID]; value != nil {
			result.Title = value.Title
			if value.Broadcast {
				result.Type = "channel"
			}
		}
	}
	return result
}
