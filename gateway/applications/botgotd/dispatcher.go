package botgotd

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func (b *Bot) SetupUpdateHandlers() {
	b.Dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		// Save peer access hashes from entities
		for _, user := range e.Users {
			if user != nil {
				b.Peers.SaveUser(user.ID, user.AccessHash)
			}
		}
		for _, ch := range e.Channels {
			if ch != nil {
				b.Peers.SaveChannel(ch.ID, ch.AccessHash)
			}
		}

		update := b.convertMessageToUpdate(msg, e)
		if update != nil {
			b.DispatchUpdate(ctx, update)
		}
		return nil
	})

	b.Dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		for _, user := range e.Users {
			if user != nil {
				b.Peers.SaveUser(user.ID, user.AccessHash)
			}
		}
		for _, ch := range e.Channels {
			if ch != nil {
				b.Peers.SaveChannel(ch.ID, ch.AccessHash)
			}
		}

		update := b.convertMessageToUpdate(msg, e)
		if update != nil {
			b.DispatchUpdate(ctx, update)
		}
		return nil
	})

	b.Dispatcher.OnBotCallbackQuery(func(ctx context.Context, e tg.Entities, u *tg.UpdateBotCallbackQuery) error {
		for _, user := range e.Users {
			if user != nil {
				b.Peers.SaveUser(user.ID, user.AccessHash)
			}
		}
		for _, ch := range e.Channels {
			if ch != nil {
				b.Peers.SaveChannel(ch.ID, ch.AccessHash)
			}
		}
		for _, chat := range e.Chats {
			if chat != nil {
				b.Peers.SaveChat(chat.ID)
			}
		}

		cb := &CallbackQuery{
			ID:   strconv.FormatInt(u.QueryID, 10),
			Data: string(u.Data),
			From: User{ID: u.UserID},
		}
		if user, ok := e.Users[u.UserID]; ok && user != nil {
			cb.From.FirstName = user.FirstName
			cb.From.LastName = user.LastName
			cb.From.Username = user.Username
		}
		// Attached message
		var chatID int64
		switch p := u.Peer.(type) {
		case *tg.PeerUser:
			chatID = p.UserID
		case *tg.PeerChat:
			chatID = -p.ChatID
		case *tg.PeerChannel:
			chatID = -1000000000000 - p.ChannelID
		}
		cb.Message = &MaybeInaccessibleMessage{
			Message: &Message{
				ID:   u.MsgID,
				Chat: Chat{ID: chatID},
				From: &cb.From,
			},
		}

		update := &Update{
			ID:            int(u.QueryID),
			CallbackQuery: cb,
		}
		b.DispatchUpdate(ctx, update)
		return nil
	})

	b.Dispatcher.OnInlineBotCallbackQuery(func(ctx context.Context, e tg.Entities, u *tg.UpdateInlineBotCallbackQuery) error {
		for _, user := range e.Users {
			if user != nil {
				b.Peers.SaveUser(user.ID, user.AccessHash)
			}
		}

		var inlineMsgID string
		if u.MsgID != nil {
			buf := &bin.Buffer{}
			if err := u.MsgID.Encode(buf); err == nil {
				inlineMsgID = base64.RawURLEncoding.EncodeToString(buf.Raw())
			}
		}

		cb := &CallbackQuery{
			ID:                 strconv.FormatInt(u.QueryID, 10),
			Data:               string(u.Data),
			InlineMessageID:    inlineMsgID,
			RawInlineMessageID: u.MsgID,
			From:               User{ID: u.UserID},
		}
		if user, ok := e.Users[u.UserID]; ok && user != nil {
			cb.From.FirstName = user.FirstName
			cb.From.LastName = user.LastName
			cb.From.Username = user.Username
		}

		update := &Update{
			ID:            int(u.QueryID),
			CallbackQuery: cb,
		}
		b.DispatchUpdate(ctx, update)
		return nil
	})

	b.Dispatcher.OnBotInlineQuery(func(ctx context.Context, e tg.Entities, u *tg.UpdateBotInlineQuery) error {
		for _, user := range e.Users {
			if user != nil {
				b.Peers.SaveUser(user.ID, user.AccessHash)
			}
		}

		iq := &InlineQuery{
			ID:     strconv.FormatInt(u.QueryID, 10),
			Query:  u.Query,
			Offset: u.Offset,
			From:   User{ID: u.UserID},
		}
		if user, ok := e.Users[u.UserID]; ok && user != nil {
			iq.From.FirstName = user.FirstName
			iq.From.LastName = user.LastName
			iq.From.Username = user.Username
		}

		update := &Update{
			ID:          int(u.QueryID),
			InlineQuery: iq,
		}
		b.DispatchUpdate(ctx, update)
		return nil
	})
}

func (b *Bot) convertMessageToUpdate(msg *tg.Message, e tg.Entities) *Update {
	var chatID int64
	var fromUser *User

	switch p := msg.PeerID.(type) {
	case *tg.PeerUser:
		chatID = p.UserID
	case *tg.PeerChat:
		chatID = -p.ChatID
	case *tg.PeerChannel:
		chatID = -1000000000000 - p.ChannelID
	}

	if fromPeer, ok := msg.FromID.(*tg.PeerUser); ok {
		fromUser = &User{ID: fromPeer.UserID}
		if u, ok := e.Users[fromPeer.UserID]; ok && u != nil {
			fromUser.FirstName = u.FirstName
			fromUser.LastName = u.LastName
			fromUser.Username = u.Username
			fromUser.IsBot = u.Bot
		}
	} else if chatID > 0 {
		fromUser = &User{ID: chatID}
		if u, ok := e.Users[chatID]; ok && u != nil {
			fromUser.FirstName = u.FirstName
			fromUser.LastName = u.LastName
			fromUser.Username = u.Username
		}
	}

	m := &Message{
		ID:   msg.ID,
		Date: msg.Date,
		Text: msg.Message,
		Chat: Chat{ID: chatID},
		From: fromUser,
	}

	if gid, ok := msg.GetGroupedID(); ok && gid != 0 {
		m.MediaGroupID = strconv.FormatInt(gid, 10)
	}

	// Check media
	if msg.Media != nil {
		if msg.Message != "" {
			m.Caption = msg.Message
		}
		switch media := msg.Media.(type) {
		case *tg.MessageMediaPhoto:
			if photo, ok := media.Photo.(*tg.Photo); ok {
				m.Photo = []PhotoSize{{FileID: fmt.Sprintf("%d", photo.ID), Width: 100, Height: 100}}
			}
		case *tg.MessageMediaDocument:
			if doc, ok := media.Document.(*tg.Document); ok {
				m.Document = &Document{FileID: fmt.Sprintf("%d", doc.ID), MimeType: doc.MimeType, FileSize: doc.Size}
				for _, attr := range doc.Attributes {
					switch a := attr.(type) {
					case *tg.DocumentAttributeVideo:
						m.Video = &Video{FileID: fmt.Sprintf("%d", doc.ID), Duration: int(a.Duration), Width: a.W, Height: a.H, FileSize: doc.Size}
					case *tg.DocumentAttributeAudio:
						if a.Voice {
							m.Voice = &Voice{FileID: fmt.Sprintf("%d", doc.ID), Duration: a.Duration, FileSize: doc.Size}
						} else {
							m.Audio = &Audio{FileID: fmt.Sprintf("%d", doc.ID), Duration: a.Duration, FileSize: doc.Size}
						}
					case *tg.DocumentAttributeAnimated:
						m.Animation = &Document{FileID: fmt.Sprintf("%d", doc.ID), MimeType: doc.MimeType, FileSize: doc.Size}
					}
				}
			}
		}
	}

	entities := convertTgEntitiesToBot(msg.Entities)
	if msg.Media != nil {
		m.CaptionEntities = entities
	} else {
		m.Entities = entities
	}

	return &Update{
		ID:      msg.ID,
		Message: m,
	}
}

func convertTgEntitiesToBot(source []tg.MessageEntityClass) []MessageEntity {
	if len(source) == 0 {
		return nil
	}
	result := make([]MessageEntity, 0, len(source))
	for _, item := range source {
		var e MessageEntity
		switch val := item.(type) {
		case *tg.MessageEntityBold:
			e.Type, e.Offset, e.Length = "bold", val.Offset, val.Length
		case *tg.MessageEntityItalic:
			e.Type, e.Offset, e.Length = "italic", val.Offset, val.Length
		case *tg.MessageEntityUnderline:
			e.Type, e.Offset, e.Length = "underline", val.Offset, val.Length
		case *tg.MessageEntityStrike:
			e.Type, e.Offset, e.Length = "strikethrough", val.Offset, val.Length
		case *tg.MessageEntityCode:
			e.Type, e.Offset, e.Length = "code", val.Offset, val.Length
		case *tg.MessageEntityPre:
			e.Type, e.Offset, e.Length, e.Language = "pre", val.Offset, val.Length, val.Language
		case *tg.MessageEntityTextURL:
			e.Type, e.Offset, e.Length, e.URL = "text_link", val.Offset, val.Length, val.URL
		case *tg.MessageEntityMention:
			e.Type, e.Offset, e.Length = "mention", val.Offset, val.Length
		case *tg.MessageEntityHashtag:
			e.Type, e.Offset, e.Length = "hashtag", val.Offset, val.Length
		case *tg.MessageEntityURL:
			e.Type, e.Offset, e.Length = "url", val.Offset, val.Length
		case *tg.MessageEntityBlockquote:
			e.Type, e.Offset, e.Length = "blockquote", val.Offset, val.Length
		case *tg.MessageEntitySpoiler:
			e.Type, e.Offset, e.Length = "spoiler", val.Offset, val.Length
		case *tg.MessageEntityCustomEmoji:
			e.Type, e.Offset, e.Length, e.CustomEmojiID = "custom_emoji", val.Offset, val.Length, strconv.FormatInt(val.DocumentID, 10)
		default:
			continue
		}
		result = append(result, e)
	}
	return result
}
