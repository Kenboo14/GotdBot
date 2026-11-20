package userbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

type ubotInlineCardButton struct {
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`
	Alert string `json:"alert,omitempty"`
}

type ubotInlineCardPayload struct {
	Title   string                   `json:"title"`
	HTML    string                   `json:"html"`
	Buttons [][]ubotInlineCardButton `json:"buttons,omitempty"`
}

func randomInlineCardToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func deleteUserStatusMessage(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msgID int) {
	if client == nil || target == nil || msgID <= 0 {
		return
	}
	if ch, ok := target.(*tg.InputPeerChannel); ok {
		_, _ = client.API().ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      []int{msgID},
		})
		return
	}
	_, _ = client.API().MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
		Revoke: true,
		ID:     []int{msgID},
	})
}

func (s *Service) saveInlineCardToRedis(ctx context.Context, card ubotInlineCardPayload) (string, error) {
	if s == nil || s.redis == nil || s.redis.Client == nil {
		return "", fmt.Errorf("redis inline card tidak tersedia")
	}
	token := randomInlineCardToken()
	for rIdx := range card.Buttons {
		for cIdx := range card.Buttons[rIdx] {
			alertText := card.Buttons[rIdx][cIdx].Alert
			if alertText != "" {
				_ = s.redis.Client.Set(ctx, "inline:bio:"+token, alertText, 24*time.Hour).Err()
				card.Buttons[rIdx][cIdx].Alert = token
			}
		}
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal inline card: %w", err)
	}
	if err := s.redis.Client.Set(ctx, "inline:card:"+token, raw, 5*time.Minute).Err(); err != nil {
		return "", fmt.Errorf("simpan inline card ke redis: %w", err)
	}
	return token, nil
}

func (s *Service) trySendInlineCard(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, card ubotInlineCardPayload, threadID int) error {
	token, err := s.saveInlineCardToRedis(ctx, card)
	if err != nil {
		return err
	}
	return s.sendInlineQuery(ctx, client, target, "ucard "+token, threadID)
}

func (s *Service) sendInlineCardWithFallback(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, card ubotInlineCardPayload, fallbackText string, threadID int) error {
	if err := s.trySendInlineCard(ctx, client, target, card, threadID); err == nil {
		return nil
	}
	return sendUserText(ctx, client, target, fallbackText, threadID)
}
