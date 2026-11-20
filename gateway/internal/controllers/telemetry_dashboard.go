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

	rows := []monitorRichRow{
		{Label: "🟢 Online", Value: fmt.Sprintf("<code>%d akun</code>", connected)},
		{Label: "🟡 Menghubungkan", Value: fmt.Sprintf("<code>%d akun</code>", connecting)},
		{Label: "🟠 Reconnecting", Value: fmt.Sprintf("<code>%d akun</code>", reconnecting)},
		{Label: "🔴 Revoked / Invalid", Value: fmt.Sprintf("<code>%d akun</code>", revoked)},
		{Label: "⚡ CPU All Image", Value: fmt.Sprintf("<code>%.2f%%</code>", cpuAll)},
		{Label: "♾️ RAM All Image", Value: fmt.Sprintf("<code>%.2f MiB</code>", ramAll)},
		{Label: "🧠 RAM Alloc / Sys", Value: fmt.Sprintf("<code>%d / %d MiB</code>", ramAllocMB, ramSysMB)},
		{Label: "💻 Concurrency", Value: fmt.Sprintf("<code>%d Goroutines</code>", goroutines)},
		{Label: "🚀 Redis Broker", Value: "<code>16 Workers (PEL Auto-Recovery)</code>"},
		{Label: "🛡️ GC Tuning", Value: "<code>GOMEMLIMIT=1100MiB | GOGC=50</code>"},
		{Label: "⏳ Uptime", Value: fmt.Sprintf("<code>%s</code>", time.Since(t.bootTime).Truncate(time.Second).String())},
	}
	if sim.Status == "RUNNING" {
		rows = append(rows,
			monitorRichRow{Label: "🧪 Simulasi", Value: fmt.Sprintf("🟢 Running (<code>%d / %d</code>)", sim.ActiveUsers, sim.TargetUsers)},
			monitorRichRow{Label: "📨 Updates", Value: fmt.Sprintf("<code>%d / %d / %d</code>", sim.GeneratedUpdates, sim.ProcessedUpdates, sim.FailedUpdates)},
			monitorRichRow{Label: "📦 Queue / Rate", Value: fmt.Sprintf("<code>%d queue · %.1f/detik</code>", sim.PendingQueue, sim.ThroughputSec)},
			monitorRichRow{Label: "⏱️ Latency", Value: fmt.Sprintf("<code>%.2f / %.2f / %.2f ms</code>", sim.LatencyP50Ms, sim.LatencyP95Ms, sim.LatencyP99Ms)},
			monitorRichRow{Label: "🧵 Sim Goroutines", Value: fmt.Sprintf("<code>%d</code>", sim.Goroutines)},
		)
	}
	rows = append(rows, monitorRichRow{Label: "🕒 Update", Value: fmt.Sprintf("<code>%s WIB</code>", time.Now().Format("15:04:05"))})

	return renderMonitorRichCard("📊 Telemetry Live GotdBot", rows, "ℹ️ 🟢 Semua sistem operasional · GotdBot Live Telemetry", false)
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

	rows := make([]monitorRichRow, 0, len(collapsed)+1)
	approxLen := 120
	omitted := 0
	for i, entry := range collapsed {
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
		row := monitorRichRow{
			Label: fmt.Sprintf("%s %s", icon, alert.Timestamp.Format("15:04:05")),
			Value: fmt.Sprintf("<code>%d</code> · %s%s", alert.ChatID, html.EscapeString(truncateUTF8(alert.Reason, 160)), suffix),
		}
		if i >= 15 || approxLen+len(row.Label)+len(row.Value)+40 > 3300 {
			omitted = len(collapsed) - i
			break
		}
		approxLen += len(row.Label) + len(row.Value) + 40
		rows = append(rows, row)
	}
	if len(rows) == 1 {
		rows = append(rows, monitorRichRow{
			Label: "🕒 Digest",
			Value: fmt.Sprintf("<code>%s WIB</code>", time.Now().Format("15:04:05")),
		})
	}
	quote := "ℹ️ Ringkasan kejadian sesi terbaru · GotdBot Alert Monitor"
	if omitted > 0 {
		quote = fmt.Sprintf("ℹ️ … dan <b>+%d akun</b> lainnya diringkas · GotdBot Alert Monitor", omitted)
	}
	globalDevMonitor.EnqueueAlertDigest(renderMonitorRichCard("🚨 Alert Digest GotdBot", rows, quote, false))
}
