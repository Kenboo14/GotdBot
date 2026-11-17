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

type simulationMetricsDoc struct {
	Status           string  `json:"status"`
	TargetUsers      int     `json:"target_users"`
	ActiveUsers      int     `json:"active_users"`
	LowProfile       int     `json:"low_profile"`
	MedProfile       int     `json:"med_profile"`
	HighProfile      int     `json:"high_profile"`
	GeneratedUpdates int64   `json:"generated_updates"`
	ProcessedUpdates int64   `json:"processed_updates"`
	FailedUpdates    int64   `json:"failed_updates"`
	PendingQueue     int     `json:"pending_queue"`
	ThroughputSec    float64 `json:"throughput_sec"`
	LatencyP50Ms     float64 `json:"latency_p50_ms"`
	LatencyP95Ms     float64 `json:"latency_p95_ms"`
	LatencyP99Ms     float64 `json:"latency_p99_ms"`
	UptimeSec        int64   `json:"uptime_sec"`
	Goroutines       int     `json:"goroutines"`
	RAMAllocMB       uint64  `json:"ram_alloc_mb"`
	RAMSysMB         uint64  `json:"ram_sys_mb"`
	ErrorsCount      int64   `json:"errors_count"`
	UpdatedAt        int64   `json:"updated_at"`
}

type telemetryTracker struct {
	mu                  sync.RWMutex
	sessionStates       map[int64]string // chatID -> state
	lastEventIDs        map[int64]string // chatID -> latest Redis stream ID
	bootTime            time.Time
	alerts              chan alertEventItem
	lastDashboardMsgID  int
	dashboardChatTarget any
	monitorTopicID      int
	simulasiMetrics     simulationMetricsDoc
	rdb                 *redis.Client
	cpuAllImage         float64
	ramAllImage         float64
}

var globalTelemetry *telemetryTracker

func init() {
	globalTelemetry = &telemetryTracker{
		sessionStates: make(map[int64]string),
		lastEventIDs:  make(map[int64]string),
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
	globalTelemetry.rdb = r.Client
	globalTelemetry.mu.Unlock()

	// 1. Worker Consumer: Listen to session:events stream with dedicated group & 1 ordered worker
	go func() {
		r.ListenerWithWorkers(ctx, nil, "session:events:telemetry", "session:telemetry", "session:events", 1, func(_ context.Context, _ *botgotd.Bot, msg redis.XMessage) {
			payload, ok := msg.Values["payload"].(string)
			if !ok {
				return
			}
			var event models.PFromService
			if json.Unmarshal([]byte(payload), &event) != nil {
				return
			}

			globalTelemetry.recordSessionEvent(event, msg.ID)
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

func parseRedisStreamID(id string) (int64, int64, bool) {
	msStr, seqStr, ok := strings.Cut(id, "-")
	if !ok {
		return 0, 0, false
	}
	ms, err1 := strconv.ParseInt(msStr, 10, 64)
	seq, err2 := strconv.ParseInt(seqStr, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return ms, seq, true
}

func isNewerStreamID(prev, next string) bool {
	if prev == "" || next == "" {
		return true
	}
	pMs, pSeq, pOk := parseRedisStreamID(prev)
	nMs, nSeq, nOk := parseRedisStreamID(next)
	if !pOk || !nOk {
		return true
	}
	if nMs != pMs {
		return nMs > pMs
	}
	return nSeq > pSeq
}

func (t *telemetryTracker) recordSessionEvent(event models.PFromService, streamID ...string) {
	msgID := ""
	if len(streamID) > 0 {
		msgID = streamID[0]
	}
	if ms, _, ok := parseRedisStreamID(msgID); ok && ms > 0 {
		if time.Since(time.UnixMilli(ms)) > 2*time.Minute {
			return
		}
	}

	switch event.Type {
	case "bot_loader":
		globalDevMonitor.NotifyBotLoader("ubot", event.Text)
		return
	case "traceback":
		globalDevMonitor.NotifyTraceback("ubot", fmt.Sprintf("Chat %d (Owner %d)", event.ChatID, event.OwnerID), event.Text)
		return
	case "db_health":
		globalDevMonitor.NotifyDatabase("ubot", event.Text)
		return
	}

	var prevState string
	t.mu.Lock()
	if event.ChatID != 0 && event.State != "" {
		if t.lastEventIDs == nil {
			t.lastEventIDs = make(map[int64]string)
		}
		if isNewerStreamID(t.lastEventIDs[event.ChatID], msgID) {
			prevState = t.sessionStates[event.ChatID]
			t.sessionStates[event.ChatID] = event.State
			if msgID != "" {
				t.lastEventIDs[event.ChatID] = msgID
			}
		}
	}
	t.mu.Unlock()

	stateUpper := strings.ToUpper(event.State)
	if event.ChatID != 0 {
		switch stateUpper {
		case "CONNECTED":
			if prevState != "CONNECTED" {
				globalDevMonitor.NotifyUserbotConnected(event.ChatID, event.OwnerID, event.Text)
			}
		case "RECONNECTING", "SUSPECTED", "SESSION_INVALID":
			globalDevMonitor.NotifySessionTrack(event.ChatID, event.OwnerID, stateUpper, event.Text)
		}
	}

	// Enqueue to alert digest if critical
	isCritical := false
	reasonLower := strings.ToLower(event.Text)

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

// loadSimulasiState reads the simulasi state from Redis key "simulasi:state"
// so the dashboard can display it alongside live telemetry.
func (t *telemetryTracker) loadSimulasiState(rdb *redis.Client) {
	if rdb == nil {
		return
	}
	data, err := rdb.Get(context.Background(), "simulasi:metrics").Result()
	if err != nil {
		return
	}
	var doc simulationMetricsDoc
	if json.Unmarshal([]byte(data), &doc) != nil {
		return
	}
	t.mu.Lock()
	t.simulasiMetrics = doc
	t.mu.Unlock()
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
	t.loadSimulasiState(t.rdb)
	t.updateSystemResourceStats(ctx)
	t.updateDashboardMessage(ctx, b)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.loadSimulasiState(t.rdb)
			t.updateSystemResourceStats(ctx)
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

	if t.simulasiMetrics.Status == "RUNNING" {
		ramAllocMB += t.simulasiMetrics.RAMAllocMB
		ramSysMB += t.simulasiMetrics.RAMSysMB
		goroutines += t.simulasiMetrics.Goroutines
	}

	uptime := time.Since(t.bootTime).Truncate(time.Second)
	nowStr := time.Now().Format("15:04:05 WIB")

	t.mu.RLock()
	cpuAll := t.cpuAllImage
	ramAll := t.ramAllImage
	t.mu.RUnlock()

	simulasiLine := ""
	if t.simulasiMetrics.Status == "RUNNING" {
		sim := t.simulasiMetrics
		simulasiLine = fmt.Sprintf(`

🧪 <b>Virtual Userbot Simulation:</b>
• Status: 🟢 <b>RUNNING</b>
• Target Users: <b>%d</b> | Active Users: <b>%d</b>
• Generated Updates: <b>%d</b>
• Processed Updates: <b>%d</b>
• Failed Updates: <b>%d</b> | Pending Queue: <b>%d</b>
• Throughput: <b>%.1f updates/sec</b>
• Latency: P50: <code>%.2f ms</code> | P95: <code>%.2f ms</code> | P99: <code>%.2f ms</code>
• Simulation Goroutines: <b>%d</b>`,
			sim.TargetUsers, sim.ActiveUsers,
			sim.GeneratedUpdates,
			sim.ProcessedUpdates,
			sim.FailedUpdates, sim.PendingQueue,
			sim.ThroughputSec,
			sim.LatencyP50Ms, sim.LatencyP95Ms, sim.LatencyP99Ms,
			sim.Goroutines,
		)
	}

	return fmt.Sprintf(`📊 <b>[GOTDBOT TELEMETRY LIVE]</b>
🕒 <b>Waktu:</b> %s | ⏳ <b>Uptime:</b> %s
────────────────────────
👥 <b>MTProto Session Pool:</b>
• 🟢 Online (Connected)  : <b>%d</b> akun
• 🟡 Menghubungkan      : <b>%d</b> akun
• 🟠 Reconnecting       : <b>%d</b> akun
• 🔴 Revoked / Invalid  : <b>%d</b> akun%s

⚙️ <b>Kesehatan Mesin & Runtime:</b>
• ⚡ CPU All Image     : <b>%.2f%%</b>
• ♾️ RAM All Image     : <b>%.2f MiB</b>
• 🧠 RAM Alloc / Sys    : <b>%d MiB</b> / %d MiB
• 💻 Concurrency        : <b>%d Goroutines</b>
• 🚀 Redis Broker Pool  : <b>16 Workers</b> (PEL Auto-Recovery)
• 🛡️ GC Tuning          : GOMEMLIMIT=1100MiB | GOGC=50
────────────────────────
Status: 🟢 <b>ALL SYSTEMS OPERATIONAL</b>`,
		nowStr, uptime.String(),
		connected, connecting, reconnecting, revoked, simulasiLine,
		cpuAll, ramAll,
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
