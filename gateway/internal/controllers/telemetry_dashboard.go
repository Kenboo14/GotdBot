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
	mu                 sync.RWMutex
	sessionStates      map[int64]string
	lastEventIDs       map[int64]string
	bootTime           time.Time
	alerts             chan alertEventItem
	lastDashboardMsgID int
	simulasiMetrics    simulationMetricsDoc
	rdb                *redis.Client
	cpuAllImage        float64
	ramAllImage        float64
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

func startTelemetryDashboard(ctx context.Context, b *botgotd.Bot, r *broker.Redis) {
	if b == nil || r == nil {
		return
	}

	groupDev := strings.TrimSpace(os.Getenv("GROUP_DEV"))
	if groupDev == "" {
		log.Println("ℹ️ [Telemetry] GROUP_DEV is not set in .env. Live Telegram Dashboard is in headless mode.")
	}

	globalTelemetry.mu.Lock()
	globalTelemetry.rdb = r.Client
	globalTelemetry.mu.Unlock()

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

	go globalTelemetry.runDashboardLoop(ctx)
	go globalTelemetry.runAlertDigestLoop(ctx)
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
	if ms, _, ok := parseRedisStreamID(msgID); ok && ms > 0 && time.Since(time.UnixMilli(ms)) > 2*time.Minute {
		return
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
	accepted := true
	stateChanged := false
	t.mu.Lock()
	if event.ChatID != 0 && event.State != "" {
		if t.lastEventIDs == nil {
			t.lastEventIDs = make(map[int64]string)
		}
		if isNewerStreamID(t.lastEventIDs[event.ChatID], msgID) {
			prevState = t.sessionStates[event.ChatID]
			stateChanged = !strings.EqualFold(prevState, event.State)
			t.sessionStates[event.ChatID] = event.State
			if msgID != "" {
				t.lastEventIDs[event.ChatID] = msgID
			}
		} else {
			accepted = false
		}
	}
	t.mu.Unlock()

	if !accepted || event.Text == "periodic_sync" {
		return
	}

	stateUpper := strings.ToUpper(event.State)
	prevUpper := strings.ToUpper(prevState)
	if event.ChatID != 0 && stateChanged {
		switch stateUpper {
		case "CONNECTED":
			if prevUpper == "RECONNECTING" || prevUpper == "SUSPECTED" || prevUpper == "SESSION_INVALID" {
				globalDevMonitor.NotifySessionTrack(event.ChatID, event.OwnerID, stateUpper, event.Text)
			}
		case "RECONNECTING", "SUSPECTED", "SESSION_INVALID":
			globalDevMonitor.NotifySessionTrack(event.ChatID, event.OwnerID, stateUpper, event.Text)
		}
	}

	reasonLower := strings.ToLower(event.Text)
	isCritical := stateUpper == "SESSION_INVALID" ||
		containsAny(reasonLower, "revoked", "banned", "flood_wait", "floodwait") ||
		event.Type == "session_installed" ||
		stateUpper == "INSTALLED"

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
		}
	}
}

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

func (t *telemetryTracker) runDashboardLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	for {
		t.mu.RLock()
		rdb := t.rdb
		t.mu.RUnlock()
		t.loadSimulasiState(rdb)
		t.updateSystemResourceStats(ctx)
		t.updateDashboardMessage()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (t *telemetryTracker) updateDashboardMessage() {
	t.mu.RLock()
	msgID := t.lastDashboardMsgID
	t.mu.RUnlock()

	globalDevMonitor.EnqueueDashboardEdit(msgID, t.formatDashboardHTML(), func(newID int) {
		t.mu.Lock()
		t.lastDashboardMsgID = newID
		t.mu.Unlock()
	})
}

func (t *telemetryTracker) formatDashboardHTML() string {
	t.mu.RLock()
	connected, connecting, reconnecting, revoked := 0, 0, 0, 0
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
	sim := t.simulasiMetrics
	cpuAll, ramAll := t.cpuAllImage, t.ramAllImage
	t.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	ramAllocMB, ramSysMB := m.Alloc/1024/1024, m.Sys/1024/1024
	goroutines := runtime.NumGoroutine()

	simulasiLine := ""
	if sim.Status == "RUNNING" {
		ramAllocMB += sim.RAMAllocMB
		ramSysMB += sim.RAMSysMB
		goroutines += sim.Goroutines
		simulasiLine = fmt.Sprintf("\n\n🧪 <b>Virtual Userbot Simulation:</b>\n• Status: 🟢 <b>RUNNING</b>\n• Target Users: <b>%d</b> | Active Users: <b>%d</b>\n• Generated Updates: <b>%d</b>\n• Processed Updates: <b>%d</b>\n• Failed Updates: <b>%d</b> | Pending Queue: <b>%d</b>\n• Throughput: <b>%.1f updates/sec</b>\n• Latency: P50: <code>%.2f ms</code> | P95: <code>%.2f ms</code> | P99: <code>%.2f ms</code>\n• Simulation Goroutines: <b>%d</b>",
			sim.TargetUsers, sim.ActiveUsers, sim.GeneratedUpdates, sim.ProcessedUpdates,
			sim.FailedUpdates, sim.PendingQueue, sim.ThroughputSec,
			sim.LatencyP50Ms, sim.LatencyP95Ms, sim.LatencyP99Ms, sim.Goroutines,
		)
	}

	return fmt.Sprintf("📊 <b>[GOTDBOT TELEMETRY LIVE]</b>\n🕒 <b>Waktu:</b> %s | ⏳ <b>Uptime:</b> %s\n────────────────────────\n👥 <b>MTProto Session Pool:</b>\n• 🟢 Online (Connected)  : <b>%d</b> akun\n• 🟡 Menghubungkan      : <b>%d</b> akun\n• 🟠 Reconnecting       : <b>%d</b> akun\n• 🔴 Revoked / Invalid  : <b>%d</b> akun%s\n\n⚙️ <b>Kesehatan Mesin & Runtime:</b>\n• ⚡ CPU All Image     : <b>%.2f%%</b>\n• ♾️ RAM All Image     : <b>%.2f MiB</b>\n• 🧠 RAM Alloc / Sys    : <b>%d MiB</b> / %d MiB\n• 💻 Concurrency        : <b>%d Goroutines</b>\n• 🚀 Redis Broker Pool  : <b>16 Workers</b> (PEL Auto-Recovery)\n• 🛡️ GC Tuning          : GOMEMLIMIT=1100MiB | GOGC=50\n────────────────────────\nStatus: 🟢 <b>ALL SYSTEMS OPERATIONAL</b>",
		time.Now().Format("15:04:05 WIB"), time.Since(t.bootTime).Truncate(time.Second).String(),
		connected, connecting, reconnecting, revoked, simulasiLine,
		cpuAll, ramAll, ramAllocMB, ramSysMB, goroutines,
	)
}

func (t *telemetryTracker) runAlertDigestLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.flushAlertDigest()
		}
	}
}

func (t *telemetryTracker) flushAlertDigest() {
	var batch []alertEventItem
drain:
	for i := 0; i < 50; i++ {
		select {
		case item := <-t.alerts:
			batch = append(batch, item)
		default:
			break drain
		}
	}
	if len(batch) == 0 {
		return
	}

	type alertKey struct {
		chatID int64
		state  string
	}
	type collapsedAlert struct {
		item  alertEventItem
		count int
	}
	indexMap := make(map[alertKey]int)
	collapsed := make([]collapsedAlert, 0, len(batch))
	for _, alert := range batch {
		k := alertKey{chatID: alert.ChatID, state: strings.ToUpper(alert.State)}
		if idx, exists := indexMap[k]; exists {
			collapsed[idx].count++
			collapsed[idx].item = alert
		} else {
			indexMap[k] = len(collapsed)
			collapsed = append(collapsed, collapsedAlert{item: alert, count: 1})
		}
	}

	var sb strings.Builder
	sb.WriteString("🚨 <b>[GotdBot Alert Digest]</b>\n────────────────────────\n")
	omitted := 0
	for i, entry := range collapsed {
		if i >= 20 || sb.Len() > 3500 {
			omitted = len(collapsed) - i
			break
		}
		alert := entry.item
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
		suffix := ""
		if entry.count > 1 {
			suffix = fmt.Sprintf(" (x%d)", entry.count)
		}
		sb.WriteString(fmt.Sprintf("• %s <code>[%s]</code> Akun <code>%d</code>: %s%s\n",
			icon, alert.Timestamp.Format("15:04:05"), alert.ChatID, html.EscapeString(truncateUTF8(alert.Reason, 160)), suffix))
	}
	if omitted > 0 {
		sb.WriteString(fmt.Sprintf("<i>… dan +%d akun lainnya diringkas</i>\n", omitted))
	}
	globalDevMonitor.EnqueueAlertDigest(sb.String())
}
