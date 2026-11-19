package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"hash/fnv"
	"html"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v3/mem"
)

const (
	maxDedupEntries = 512
	dedupPruneTo    = 384
)

type devMonitor struct {
	mu                sync.Mutex
	chatTarget        any
	fallbackTop       int
	topics            map[string]int
	dispatcher        *monitorDispatcher
	dedup             map[string]time.Time
	lastHealthSummary string
	started           bool
	b                 *botgotd.Bot
	db                *databases.DataManager
	rdb               *redis.Client
}

type infraHealthSnapshot struct {
	dbHealth        databases.DBHealth
	mongoConfigured bool
	redisOK         bool
	redisErr        string
	memDetail       string
	ramPercent      float64
}

var globalDevMonitor = &devMonitor{
	topics: make(map[string]int),
	dedup:  make(map[string]time.Time),
}

func parseChatTargetEnv(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if numID, err := strconv.ParseInt(raw, 10, 64); err == nil && numID != 0 {
		return numID
	}
	if !strings.HasPrefix(raw, "@") && !strings.HasPrefix(raw, "-") {
		return "@" + raw
	}
	return raw
}

func parseTopicEnv(key string) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func hashDedup(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 36)
}

func startDevMonitor(ctx context.Context, b *botgotd.Bot, rdb *redis.Client, db *databases.DataManager) {
	if b == nil {
		return
	}
	target := parseChatTargetEnv(os.Getenv("GROUP_DEV"))
	fallback := parseTopicEnv("MONITOR_TOPIC")

	topicKeys := []string{
		"MONITOR_TOPIC",
		"MONITOR_USERBOT_CONNECTED",
		"MONITOR_TOKEN_COLLECTED",
		"MONITOR_USER_START",
		"MONITOR_TOKEN_EXPIRED",
		"MONITOR_BACKUP_TOKEN",
		"MONITOR_BOT_LOADER",
		"MONITOR_TOKEN_CREATED",
		"MONITOR_SESSION_TRACK",
		"MONITOR_TRACEBACK",
		"MONITOR_INVOICE",
		"MONITOR_DATABASE",
	}

	globalDevMonitor.mu.Lock()
	globalDevMonitor.chatTarget = target
	globalDevMonitor.fallbackTop = fallback
	globalDevMonitor.b = b
	globalDevMonitor.db = db
	globalDevMonitor.rdb = rdb
	for _, k := range topicKeys {
		globalDevMonitor.topics[k] = parseTopicEnv(k)
	}
	if !globalDevMonitor.started {
		disp := newMonitorDispatcher(defaultMonitorDispatcherConfig(), b, target, fallback)
		globalDevMonitor.dispatcher = disp
		globalDevMonitor.started = true
		go disp.run(ctx, globalDevMonitor.resolveTopic)
		if target != nil {
			go globalDevMonitor.runStartupAndDBHealthLoop(ctx)
		}
	} else if globalDevMonitor.dispatcher != nil {
		globalDevMonitor.dispatcher.updateTarget(b, target, fallback)
	}
	globalDevMonitor.mu.Unlock()
}

func (m *devMonitor) resolveTopic(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id := m.topics[key]; id > 0 {
		return id
	}
	return m.fallbackTop
}

func (m *devMonitor) shouldSendDedup(dedupKey string, cooldown time.Duration) bool {
	if dedupKey == "" || cooldown <= 0 {
		return true
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dedup == nil {
		m.dedup = make(map[string]time.Time)
	}
	if until, ok := m.dedup[dedupKey]; ok && now.Before(until) {
		return false
	}
	if len(m.dedup) >= maxDedupEntries {
		for k, ts := range m.dedup {
			if !now.Before(ts) {
				delete(m.dedup, k)
			}
		}
		if len(m.dedup) >= maxDedupEntries {
			type dedupEntry struct {
				k  string
				ts time.Time
			}
			entries := make([]dedupEntry, 0, len(m.dedup))
			for k, ts := range m.dedup {
				entries = append(entries, dedupEntry{k: k, ts: ts})
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].ts.Before(entries[j].ts)
			})
			excess := len(m.dedup) - dedupPruneTo
			for i := 0; i < excess && i < len(entries); i++ {
				delete(m.dedup, entries[i].k)
			}
		}
	}
	m.dedup[dedupKey] = now.Add(cooldown)
	return true
}

func (m *devMonitor) activeDispatcher() (*monitorDispatcher, bool) {
	m.mu.Lock()
	target := m.chatTarget
	disp := m.dispatcher
	m.mu.Unlock()
	if target == nil || disp == nil {
		return nil, false
	}
	return disp, true
}

func (m *devMonitor) enqueue(topicKey, dedupKey string, cooldown time.Duration, priority monitorPriority, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	disp, ok := m.activeDispatcher()
	if !ok || !m.shouldSendDedup(dedupKey, cooldown) {
		return
	}
	disp.Enqueue(monitorJob{
		topicKey:   topicKey,
		topicID:    m.resolveTopic(topicKey),
		text:       text,
		priority:   priority,
		allowRetry: priority == monitorPriorityHigh,
	})
}

func (m *devMonitor) enqueueBatched(topicKey, header, dedupKey string, cooldown time.Duration, priority monitorPriority, entry, singleText string) {
	if strings.TrimSpace(entry) == "" {
		return
	}
	disp, ok := m.activeDispatcher()
	if !ok || !m.shouldSendDedup(dedupKey, cooldown) {
		return
	}
	disp.EnqueueBatchEntry(topicKey, header, priority, entry, singleText)
}

func (m *devMonitor) EnqueueDashboardEdit(msgID int, htmlContent string, onMsgID func(int)) {
	if strings.TrimSpace(htmlContent) == "" {
		return
	}
	disp, ok := m.activeDispatcher()
	if !ok {
		return
	}
	disp.EnqueueDashboard(dashboardEditJob{
		topicID: m.resolveTopic("MONITOR_TOPIC"),
		msgID:   msgID,
		text:    htmlContent,
		onMsgID: onMsgID,
	})
}

func (m *devMonitor) EnqueueAlertDigest(text string) {
	m.enqueue("MONITOR_TOPIC", "alertdigest:"+hashDedup(text), 10*time.Second, monitorPriorityNormal, text)
}

func (m *devMonitor) runStartupAndDBHealthLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(4 * time.Second):
	}

	m.notifyStartupStatus(ctx)
	m.checkAndReportHealth(ctx, true)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAndReportHealth(ctx, false)
		}
	}
}

func (m *devMonitor) collectInfraHealth(ctx context.Context) infraHealthSnapshot {
	m.mu.Lock()
	db := m.db
	rdb := m.rdb
	m.mu.Unlock()

	snap := infraHealthSnapshot{
		mongoConfigured: strings.TrimSpace(os.Getenv("MONGO_URI")) != "" || strings.TrimSpace(os.Getenv("MONGO_BACKUP_URI")) != "",
		redisOK:         true,
		memDetail:       "N/A",
	}
	if db != nil {
		snap.dbHealth = db.CheckHealth(ctx, snap.mongoConfigured)
	}
	if rdb != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := rdb.Ping(rCtx).Err(); err != nil {
			snap.redisOK = false
			snap.redisErr = err.Error()
		}
		cancel()
	}
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		snap.ramPercent = vm.UsedPercent
		snap.memDetail = fmt.Sprintf("%.1f / %.1f MiB (%.1f%%)", float64(vm.Used)/(1024*1024), float64(vm.Total)/(1024*1024), vm.UsedPercent)
	}
	return snap
}

func (m *devMonitor) notifyStartupStatus(ctx context.Context) {
	snap := m.collectInfraHealth(ctx)

	sqliteStatus := "❌ Error"
	if snap.dbHealth.SQLiteOK {
		sqliteStatus = "🟢 OK (WAL)"
	}
	mongoStatus := "⚪ Nonaktif"
	if snap.mongoConfigured {
		if snap.dbHealth.MongoOK {
			mongoStatus = "🟢 Connected"
		} else {
			mongoStatus = "🟡 Fallback SQLite"
		}
	}
	redisStatus := "❌ Error"
	if snap.redisOK {
		redisStatus = "🟢 Connected"
	}

	text := fmt.Sprintf(`🚀 <b>[BOT LOADER • GATEWAY READY]</b>
<blockquote expandable>Layanan Gateway MTProto telah aktif dan siap memproses panel &amp; broker Redis.</blockquote>
<table border="1">
  <tr><th>Komponen</th><th>Status</th></tr>
  <tr><td>📂 SQLite</td><td>%s</td></tr>
  <tr><td>🍃 MongoDB</td><td>%s</td></tr>
  <tr><td>⚡ Redis</td><td>%s</td></tr>
  <tr><td>🧠 RAM VPS</td><td><code>%s</code></td></tr>
  <tr><td>💻 CPU / Go</td><td><code>%d vCPU / %s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB</p>`,
		sqliteStatus, mongoStatus, redisStatus, html.EscapeString(snap.memDetail),
		runtime.NumCPU(), html.EscapeString(runtime.Version()),
		time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BOT_LOADER", "loader:gateway:startup", 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) checkAndReportHealth(ctx context.Context, forceStartup bool) {
	snap := m.collectInfraHealth(ctx)

	highRAM := snap.ramPercent >= 85.0
	summary := fmt.Sprintf("sq=%v|mg=%v|rd=%v|hr=%v", snap.dbHealth.SQLiteOK, snap.dbHealth.MongoOK, snap.redisOK, highRAM)

	m.mu.Lock()
	changed := summary != m.lastHealthSummary
	m.lastHealthSummary = summary
	m.mu.Unlock()

	if !forceStartup && !changed && snap.dbHealth.SQLiteOK && snap.redisOK && (!snap.mongoConfigured || snap.dbHealth.MongoOK) && !highRAM {
		return
	}

	icon := "🟢"
	title := "DATABASE & VPS HEALTH"
	if !snap.dbHealth.SQLiteOK || !snap.redisOK || highRAM {
		icon = "🔴"
		title = "DATABASE / VPS WARNING"
	} else if snap.mongoConfigured && !snap.dbHealth.MongoOK {
		icon = "🟡"
		title = "MONGODB FALLBACK ACTIVE"
	}

	extraErr := ""
	if snap.dbHealth.SQLiteErr != "" {
		extraErr += "\n• SQLite: <code>" + html.EscapeString(snap.dbHealth.SQLiteErr) + "</code>"
	}
	if snap.dbHealth.MongoErr != "" {
		extraErr += "\n• MongoDB: <code>" + html.EscapeString(snap.dbHealth.MongoErr) + "</code>"
	}
	if snap.redisErr != "" {
		extraErr += "\n• Redis: <code>" + html.EscapeString(snap.redisErr) + "</code>"
	}

	text := fmt.Sprintf(`%s <b>[%s]</b>
<table border="1">
  <tr><th>Resource</th><th>Status</th></tr>
  <tr><td>📂 SQLite</td><td>%v</td></tr>
  <tr><td>🍃 MongoDB</td><td>%v (cfg=%v)</td></tr>
  <tr><td>⚡ Redis</td><td>%v</td></tr>
  <tr><td>🧠 RAM VPS</td><td><code>%s</code></td></tr>
</table>%s
<hr/>
<p>🕒 %s WIB</p>`,
		icon, title,
		snap.dbHealth.SQLiteOK, snap.dbHealth.MongoOK, snap.mongoConfigured, snap.redisOK,
		html.EscapeString(snap.memDetail), extraErr,
		time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_DATABASE", "dbhealth:"+summary, 5*time.Minute, monitorPriorityHigh, text)
}
