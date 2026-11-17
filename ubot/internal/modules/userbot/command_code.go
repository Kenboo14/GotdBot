package userbot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) handleCodeCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	language := "go"
	if fields := strings.Fields(args); len(fields) > 0 {
		language = fields[0]
	}
	source, err := s.replySource(ctx, client, msg, target)
	if err != nil || strings.TrimSpace(source) == "" {
		sendErr := sendUserText(ctx, client, target, "Reply pesan yang mau di-code terlebih dahulu.", threadID)
		return sendErr
	}
	refBytes := make([]byte, 12)
	if _, err := rand.Read(refBytes); err != nil {
		return err
	}
	ref := base64.RawURLEncoding.EncodeToString(refBytes)
	if err := s.redis.Client.Set(ctx, "inline:code:"+ref, source, 10*time.Minute).Err(); err != nil {
		return err
	}
	query := "code " + language + " ref:" + ref
	if err := s.sendInlineQuery(ctx, client, target, query, threadID); err != nil {
		log.Printf("[userbot %d] !code inline gagal: %v", msg.ID, err)
		sendErr := sendUserText(ctx, client, target, "Code formatter tidak bisa dibuka. Pastikan inline mode bot aktif.", threadID)
		return sendErr
	}
	return nil
}

func (s *Service) replySource(ctx context.Context, client *gobot.Client, msg *tg.Message, target tg.InputPeerClass) (string, error) {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return "", err
	}
	return reply.Message, nil
}

func (s *Service) replyMessage(ctx context.Context, client *gobot.Client, msg *tg.Message, target tg.InputPeerClass) (*tg.Message, error) {
	header, ok := msg.GetReplyTo()
	if !ok {
		return nil, nil
	}
	rh, ok := header.(*tg.MessageReplyHeader)
	if !ok {
		return nil, nil
	}
	if rh.ReplyToMsgID == 0 || (rh.ForumTopic && rh.ReplyToTopID == 0) {
		if rh.QuoteText != "" {
			return &tg.Message{Message: rh.QuoteText}, nil
		}
		return nil, nil
	}

	var messages []tg.MessageClass

	// 1. Ambil pesan spesifik berdasarkan ReplyToMsgID
	if ch, ok := target.(*tg.InputPeerChannel); ok {
		res, err := client.API().ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: rh.ReplyToMsgID}},
		})
		if err == nil {
			switch val := res.(type) {
			case *tg.MessagesMessages:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			case *tg.MessagesMessagesSlice:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			case *tg.MessagesChannelMessages:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			}
		}
	} else {
		res, err := client.API().MessagesGetMessages(ctx, []tg.InputMessageClass{
			&tg.InputMessageID{ID: rh.ReplyToMsgID},
		})
		if err == nil {
			switch val := res.(type) {
			case *tg.MessagesMessages:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			case *tg.MessagesMessagesSlice:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			case *tg.MessagesChannelMessages:
				s.peerCache().ApplyUsersChats(val.Users, val.Chats)
				messages = val.Messages
			}
		}
	}

	// Pastikan ID pesan yang dikembalikan benar-benar cocok dengan ReplyToMsgID (bukan pesan perintah itu sendiri)
	for _, item := range messages {
		if text, ok := item.(*tg.Message); ok && text.ID == rh.ReplyToMsgID {
			return text, nil
		}
	}

	// 2. Fallback via MessagesGetHistory jika GetMessages tidak mengembalikan hasil
	result, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:      target,
		OffsetID:  rh.ReplyToMsgID + 1,
		AddOffset: 0,
		Limit:     5,
	})
	if err != nil {
		return nil, err
	}
	switch value := result.(type) {
	case *tg.MessagesMessages:
		s.peerCache().ApplyUsersChats(value.Users, value.Chats)
		messages = value.Messages
	case *tg.MessagesMessagesSlice:
		s.peerCache().ApplyUsersChats(value.Users, value.Chats)
		messages = value.Messages
	case *tg.MessagesChannelMessages:
		s.peerCache().ApplyUsersChats(value.Users, value.Chats)
		messages = value.Messages
	}
	for _, item := range messages {
		if text, ok := item.(*tg.Message); ok && text.ID == rh.ReplyToMsgID {
			return text, nil
		}
	}
	return nil, nil
}
