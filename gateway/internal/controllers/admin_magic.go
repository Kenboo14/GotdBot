package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"html"
	"os"
	"strings"
	"sync"
	"time"
)

const dashMagicTTL = 60 * time.Second

type dashMagicEntry struct {
	OwnerID   int64
	ExpiresAt time.Time
}

type dashMagicStore struct {
	mu      sync.Mutex
	entries map[string]dashMagicEntry
}

var globalDashMagic = &dashMagicStore{
	entries: make(map[string]dashMagicEntry),
}

func randomDashCodeSegment(length int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, length)
	raw := make([]byte, length)
	_, _ = rand.Read(raw)
	for i := 0; i < length; i++ {
		buf[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(buf)
}

func (s *dashMagicStore) pruneLocked(now time.Time) {
	for code, entry := range s.entries {
		if !entry.ExpiresAt.After(now) {
			delete(s.entries, code)
		}
	}
}

func (s *dashMagicStore) Issue(ownerID int64) (string, time.Time) {
	now := time.Now()
	exp := now.Add(dashMagicTTL)
	code := "GOTD-" + randomDashCodeSegment(4) + "-" + randomDashCodeSegment(4)
	s.mu.Lock()
	s.pruneLocked(now)
	s.entries[code] = dashMagicEntry{
		OwnerID:   ownerID,
		ExpiresAt: exp,
	}
	s.mu.Unlock()
	return code, exp
}

func (s *dashMagicStore) Verify(code string) (int64, bool) {
	norm := strings.ToUpper(strings.TrimSpace(code))
	if norm == "" {
		return 0, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	var matchedKey string
	var matchedOwner int64
	for k, entry := range s.entries {
		if len(k) == len(norm) && subtle.ConstantTimeCompare([]byte(k), []byte(norm)) == 1 {
			matchedKey = k
			matchedOwner = entry.OwnerID
		}
	}
	if matchedKey == "" {
		return 0, false
	}
	delete(s.entries, matchedKey)
	return matchedOwner, true
}

func (h *Handler) DashLoginCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	senderID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	threadID := update.Message.MessageThreadID
	allowed := isOwner(senderID) || (h != nil && h.getOwnerID() != 0 && senderID == h.getOwnerID())
	if !allowed {
		msg := renderMonitorRichCard("❌ Akses Ditolak", []monitorRichRow{
			{Label: "📌 Perintah", Value: "<code>/dashlogin</code>"},
			{Label: "🔐 Wewenang", Value: "Khusus OWNER"},
		}, "ℹ️ Perintah login WebAppDashboard hanya tersedia untuk Owner.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	code, exp := globalDashMagic.Issue(senderID)
	rows := []monitorRichRow{
		{Label: "🔑 Magic Token", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(code))},
		{Label: "⏳ Masa Berlaku", Value: fmt.Sprintf("<code>60 detik (%s WIB)</code>", exp.Local().Format("15:04:05"))},
		{Label: "🛡️ Sifat", Value: "Sekali pakai (auto-hangus setelah dipakai)"},
	}
	if pubURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DASHBOARD_PUBLIC_URL")), "/"); pubURL != "" {
		rows = append(rows, monitorRichRow{
			Label: "🌐 Link Cepat",
			Value: fmt.Sprintf("<code>%s/?magic=%s</code>", html.EscapeString(pubURL), html.EscapeString(code)),
		})
	}
	card := renderMonitorRichCard("🔐 Login WebAppDashboard Owner", rows, "ℹ️ Tempel kode di tab <b>Telegram /dashlogin</b> pada halaman login dashboard.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, card)
}
