package userbot

import (
	"context"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
)

func parseUserCommand(text, prefix string) (name, args string, ok bool) {
	text = strings.TrimSpace(text)
	if prefix != "" {
		if !strings.HasPrefix(text, prefix) {
			return "", "", false
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	parts := strings.SplitN(text, " ", 2)
	name = strings.ToLower(strings.TrimSpace(parts[0]))
	if name == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		args = strings.TrimSpace(parts[1])
	}
	if _, exists := userbotCommandNames[name]; !exists {
		return "", "", false
	}
	return name, args, true
}

func (s *Service) handleSetPrefix(ctx context.Context, client *telegram.Client, chatID int64, target tg.InputPeerClass, args string) error {
	value := strings.TrimSpace(strings.ToLower(args))
	if value == "none" {
		value = ""
	} else if value == "" || strings.ContainsAny(value, " \t\r\n") || len([]rune(value)) > 8 {
		_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, "Gunakan: !setp <prefix> atau !setp none")
		return err
	}
	store := &mongoStorage{collection: s.db.Collection("userbot_sessions"), chatID: chatID, key: s.key}
	if err := store.setPrefix(ctx, value, true); err != nil {
		return err
	}
	s.mu.Lock()
	s.prefixes[chatID] = value
	s.mu.Unlock()
	label := value
	if label == "" {
		label = "tanpa prefix"
	}
	_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, "Prefix disimpan: "+label)
	return err
}
