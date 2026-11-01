package userbot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

func (s *Service) handleCodeCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, args string) error {
	language := "go"
	if fields := strings.Fields(args); len(fields) > 0 {
		language = fields[0]
	}
	source, err := s.replySource(ctx, client, msg, target)
	if err != nil || strings.TrimSpace(source) == "" {
		_, sendErr := s.getSender(client).To(target).Text(ctx, "Reply pesan yang mau di-code terlebih dahulu.")
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
	if err := s.sendInlineQuery(ctx, client, target, query); err != nil {
		log.Printf("[userbot %d] !code inline gagal: %v", msg.ID, err)
		_, sendErr := s.getSender(client).To(target).Text(ctx, "Code formatter tidak bisa dibuka. Pastikan inline mode bot aktif.")
		return sendErr
	}
	return nil
}

func (s *Service) replySource(ctx context.Context, client *telegram.Client, msg *tg.Message, target tg.InputPeerClass) (string, error) {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return "", err
	}
	return reply.Message, nil
}

func (s *Service) replyMessage(ctx context.Context, client *telegram.Client, msg *tg.Message, target tg.InputPeerClass) (*tg.Message, error) {
	header, ok := msg.GetReplyTo()
	if !ok {
		return nil, nil
	}
	rh, ok := header.(*tg.MessageReplyHeader)
	if !ok {
		return nil, nil
	}
	if rh.ReplyToMsgID == 0 {
		return &tg.Message{Message: rh.QuoteText}, nil
	}
	result, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: target, OffsetID: rh.ReplyToMsgID + 1, AddOffset: -1, Limit: 1})
	if err != nil {
		return nil, err
	}
	var messages []tg.MessageClass
	switch value := result.(type) {
	case *tg.MessagesMessages:
		messages = value.Messages
	case *tg.MessagesMessagesSlice:
		messages = value.Messages
	}
	for _, item := range messages {
		if text, ok := item.(*tg.Message); ok {
			return text, nil
		}
	}
	return nil, nil
}
