package userbot

import (
	"context"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

func parseUserCommand(text, prefix string) (name, args string, ok bool) {
	if text == "" {
		return "", "", false
	}
	if prefix != "" {
		if !strings.HasPrefix(text, prefix) {
			return "", "", false
		}
		text = text[len(prefix):]
	}
	// Zero-allocation whitespace trim di awal
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r') {
		text = text[1:]
	}
	if text == "" {
		return "", "", false
	}
	idx := strings.IndexByte(text, ' ')
	if idx < 0 {
		name = strings.ToLower(text)
		if _, exists := userbotCommandNames[name]; !exists {
			return "", "", false
		}
		return name, "", true
	}
	name = strings.ToLower(text[:idx])
	if _, exists := userbotCommandNames[name]; !exists {
		return "", "", false
	}
	args = strings.TrimSpace(text[idx+1:])
	return name, args, true
}

func (s *Service) handleSetPrefix(ctx context.Context, client *telegram.Client, chatID int64, target tg.InputPeerClass, args string) error {
	value := strings.TrimSpace(strings.ToLower(args))
	if value == "none" {
		value = ""
	} else if value == "" || strings.ContainsAny(value, " \t\r\n") || len([]rune(value)) > 8 {
		_, err := s.getSender(client).To(target).Text(ctx, "Gunakan: !setp <prefix> atau !setp none")
		return err
	}
	store := &sqliteStorage{sqlite: s.sqlite, db: s.db, chatID: chatID, key: s.key}
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
	_, err := s.getSender(client).To(target).Text(ctx, "Prefix disimpan: "+label)
	return err
}
