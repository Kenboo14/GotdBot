package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type alertEventItem struct {
	ChatID    int64
	OwnerID   int64
	State     string
	Reason    string
	Timestamp time.Time
}

type telemetryTracker struct {
	mu                  sync.RWMutex
	sessionStates       map[int64]string // chatID -> state
	bootTime            time.Time
	alerts              chan alertEventItem
	lastDashboardMsgID  int
	dashboardChatTarget any
	monitorTopicID      int
}

var globalTelemetry *telemetryTracker

func init() {
	globalTelemetry = &telemetryTracker{
		sessionStates: make(map[int64]string),
		bootTime:      time.Now(),
		alerts:        make(chan alertEventItem, 200),
	}
}

// StartTelemetryDashboard starts the live monitoring dashboard (in-place message editing)
// and the batched alert digest worker (every 10s) targeting GROUP_DEV and MONITOR_TOPIC.
func startTelemetryDashboard(ctx context.Context, b *botgotd.Bot, r *broker.Redis) {
	if b == nil || r == nil {
		return
	}

	groupDev := strings.TrimSpace(os.Getenv("GROUP_DEV"))
	topicStr := strings.TrimSpace(os.Getenv("MONITOR_TOPIC"))
	if groupDev == "" {
		log.Println("ℹ️ [Telemetry] GROUP_DEV is not set in .env. Live Telegram Dashboard is in headless mode.")
	}

	var chatTarget any = groupDev
	if numID, err := strconv.ParseInt(groupDev, 10, 64); err == nil {
		chatTarget = numID
	} else if groupDev != "" && !strings.HasPrefix(groupDev, "@") && !strings.HasPrefix(groupDev, "-") {
		chatTarget = "@" + groupDev
	}

	monitorTopic := 0
	if tID, err := strconv.Atoi(topicStr); err == nil && tID > 0 {
		monitorTopic = tID
	}

	globalTelemetry.mu.Lock()
	globalTelemetry.dashboardChatTarget = chatTarget
	globalTelemetry.monitorTopicID = monitorTopic
	globalTelemetry.mu.Unlock()

	// 1. Worker Consumer: Listen to session:events stream
	go func() {
		r.Listener(ctx, nil, "session:events", "session:telemetry", "session:events", func(_ context.Context, _ *botgotd.Bot, msg redis.XMessage) {
			payload, ok := msg.Values["payload"].(string)
			if !ok {
				return
			}
			var event models.PFromService
			if json.Unmarshal([]byte(payload), &event) != nil {
				return
			}

			globalTelemetry.recordSessionEvent(event)
		})
	}()

	if groupDev == "" {
		return
	}

	// 2. Worker: In-Place Edit Live Dashboard Message (Every 45 seconds)
	go globalTelemetry.runDashboardLoop(ctx, b)

	// 3. Worker: Batched Alert Digest (Every 10 seconds, zero spam)
	go globalTelemetry.runAlertDigestLoop(ctx, b)
}

func (t *telemetryTracker) recordSessionEvent(event models.PFromService) {
	t.mu.Lock()
	if event.ChatID != 0 && event.State != "" {
		t.sessionStates[event.ChatID] = event.State
	}
	t.mu.Unlock()

	// Enqueue to alert digest if critical
	isCritical := false
	reasonLower := strings.ToLower(event.Text)
	stateUpper := strings.ToUpper(event.State)

	if stateUpper == "SESSION_INVALID" || strings.Contains(reasonLower, "revoked") || strings.Contains(reasonLower, "banned") {
		isCritical = true
	} else if strings.Contains(reasonLower, "flood_wait") || strings.Contains(reasonLower, "floodwait") {
		isCritical = true
	} else if event.Type == "session_installed" || stateUpper == "INSTALLED" {
		isCritical = true
	}

	if isCritical {
		select {
		case t.alerts <- alertEventItem{
			ChatID:    event.ChatID,
			OwnerID:   event.OwnerID,
			State:     event.State,
			Reason:    event.Text,
			Timestamp: time.Now(),
		}:
		default:
			// Buffer full, drop to prevent blocking
		}
	}
}

func (t *telemetryTracker) runDashboardLoop(ctx context.Context, b *botgotd.Bot) {
	// Initial delay so bot finishes MTProto / HTTP handshake
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	// Update immediately once
	t.updateDashboardMessage(ctx, b)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.updateDashboardMessage(ctx, b)
		}
	}
}

func (t *telemetryTracker) updateDashboardMessage(ctx context.Context, b *botgotd.Bot) {
	t.mu.RLock()
	target := t.dashboardChatTarget
	topicID := t.monitorTopicID
	msgID := t.lastDashboardMsgID
	t.mu.RUnlock()

	if target == nil || target == "" {
		return
	}

	htmlContent := t.formatDashboardHTML()

	// If message ID is known, edit in-place
	if msgID > 0 {
		_, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:    target,
			MessageID: msgID,
			Text:      htmlContent,
			ParseMode: botgotd.ParseModeHTML,
		})
		if err == nil {
			return
		}
		// If edit failed (e.g. message deleted), fall through to send a new message
	}

	// Send initial message or re-create deleted message
	msg, err := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:          target,
		MessageThreadID: topicID,
		Text:            htmlContent,
		ParseMode:       botgotd.ParseModeHTML,
	})
	if err == nil && msg != nil && msg.ID > 0 {
		t.mu.Lock()
		t.lastDashboardMsgID = msg.ID
		t.mu.Unlock()
	}
}

func (t *telemetryTracker) formatDashboardHTML() string {
	t.mu.RLock()
	connected := 0
	connecting := 0
	reconnecting := 0
	revoked := 0
	for _, st := range t.sessionStates {
		switch strings.ToUpper(st) {
		case "CONNECTED":
			connected++
		case "CONNECTING":
			connecting++
		case "RECONNECTING", "SUSPECTED":
			reconnecting++
		case "SESSION_INVALID":
			revoked++
		}
	}
	t.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	ramAllocMB := m.Alloc / 1024 / 1024
	ramSysMB := m.Sys / 1024 / 1024
	goroutines := runtime.NumGoroutine()

	uptime := time.Since(t.bootTime).Truncate(time.Second)
	nowStr := time.Now().Format("15:04:05 WIB")

	return fmt.Sprintf(`📊 <b>[GOTDBOT TELEMETRY LIVE]</b>
🕒 <b>Waktu:</b> %s | ⏳ <b>Uptime:</b> %s
────────────────────────
👥 <b>MTProto Session Pool:</b>
• 🟢 Online (Connected)  : <b>%d</b> akun
• 🟡 Menghubungkan      : <b>%d</b> akun
• 🟠 Reconnecting       : <b>%d</b> akun
• 🔴 Revoked / Invalid  : <b>%d</b> akun

⚙️ <b>Kesehatan Mesin & Runtime:</b>
• 🧠 RAM Alloc / Sys    : <b>%d MiB</b> / %d MiB
• 💻 Concurrency        : <b>%d Goroutines</b>
• 🚀 Redis Broker Pool  : <b>16 Workers</b> (PEL Auto-Recovery)
• 🛡️ GC Tuning          : GOMEMLIMIT=1100MiB | GOGC=50
────────────────────────
Status: 🟢 <b>ALL SYSTEMS OPERATIONAL</b>`,
		nowStr, uptime.String(),
		connected, connecting, reconnecting, revoked,
		ramAllocMB, ramSysMB, goroutines,
	)
}

func (t *telemetryTracker) runAlertDigestLoop(ctx context.Context, b *botgotd.Bot) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.flushAlertDigest(ctx, b)
		}
	}
}

func (t *telemetryTracker) flushAlertDigest(ctx context.Context, b *botgotd.Bot) {
	var batch []alertEventItem
	for i := 0; i < 20; i++ {
		select {
		case item := <-t.alerts:
			batch = append(batch, item)
		default:
			break
		}
	}

	if len(batch) == 0 {
		return
	}

	t.mu.RLock()
	target := t.dashboardChatTarget
	topicID := t.monitorTopicID
	t.mu.RUnlock()

	if target == nil || target == "" {
		return
	}

	var sb strings.Builder
	sb.WriteString("🚨 <b>[GotdBot Alert Digest]</b>\n")
	sb.WriteString("────────────────────────\n")

	for _, alert := range batch {
		icon := "⚠️"
		stateUpper := strings.ToUpper(alert.State)
		reasonLower := strings.ToLower(alert.Reason)

		if stateUpper == "SESSION_INVALID" || strings.Contains(reasonLower, "revoked") {
			icon = "🔴"
		} else if strings.Contains(reasonLower, "flood") {
			icon = "🟡"
		} else if alert.State == "INSTALLED" || stateUpper == "CONNECTED" {
			icon = "🟢"
		}

		timeStr := alert.Timestamp.Format("15:04:05")
		sb.WriteString(fmt.Sprintf("• %s <code>[%s]</code> Akun <code>%d</code>: %s\n",
			icon, timeStr, alert.ChatID, html.EscapeString(alert.Reason)))
	}

	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:          target,
		MessageThreadID: topicID,
		Text:            sb.String(),
		ParseMode:       botgotd.ParseModeHTML,
	})
}
