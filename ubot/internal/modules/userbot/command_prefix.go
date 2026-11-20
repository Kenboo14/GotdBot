package userbot

import (
	"context"
	"strings"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func matchUserCommand(text string) (string, string, bool) {
	// Zero-allocation whitespace trim di awal
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r') {
		text = text[1:]
	}
	if text == "" {
		return "", "", false
	}
	idx := strings.IndexAny(text, " \t\n\r")
	if idx < 0 {
		name := strings.ToLower(text)
		if !isKnownUserbotCommand(name) {
			return "", "", false
		}
		return name, "", true
	}
	name := strings.ToLower(text[:idx])
	if !isKnownUserbotCommand(name) {
		return "", "", false
	}
	return name, strings.TrimSpace(text[idx+1:]), true
}

func parseUserCommand(text, prefix string) (name, args string, ok bool) {
	if text == "" {
		return "", "", false
	}
	if prefix != "" {
		if !strings.HasPrefix(text, prefix) {
			return "", "", false
		}
		return matchUserCommand(text[len(prefix):])
	}
	if name, args, ok := matchUserCommand(text); ok {
		return name, args, true
	}
	if strings.HasPrefix(text, "!") {
		return matchUserCommand(text[1:])
	}
	return "", "", false
}

func (s *Service) handleSetPrefix(ctx context.Context, client *gobot.Client, chatID int64, target tg.InputPeerClass, args string, threadID int) error {
	value := strings.TrimSpace(strings.ToLower(args))
	if value == "none" {
		value = ""
	} else if value == "" || strings.ContainsAny(value, " \t\r\n") || len([]rune(value)) > 8 {
		err := sendUserText(ctx, client, target, "Gunakan: !setp <prefix> atau !setp none", threadID)
		return err
	}
	s.mu.RLock()
	accountID := s.owners[chatID]
	s.mu.RUnlock()
	store := &sqliteStorage{sqlite: s.sqlite, db: s.db, chatID: chatID, ownerID: chatID, accountID: accountID, key: s.key}
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
	err := sendUserText(ctx, client, target, "Prefix disimpan: "+label, threadID)
	return err
}
