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
	rawTargetUsername string
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

	rawUsername := ""
	if targetStr, ok := target.(string); ok {
		rawUsername = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(targetStr), "@"))
	}

	globalDevMonitor.mu.Lock()
	globalDevMonitor.chatTarget = target
	globalDevMonitor.rawTargetUsername = rawUsername
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

func (m *devMonitor) ObserveIncomingChat(chat botgotd.Chat) {
	if m == nil || chat.ID == 0 || strings.TrimSpace(chat.Username) == "" {
		return
	}
	m.mu.Lock()
	match := m.rawTargetUsername != "" &&
		strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(chat.Username), "@"), m.rawTargetUsername) &&
		parseMonitorChatID(m.chatTarget) == 0
	var disp *monitorDispatcher
	var b *botgotd.Bot
	var fb int
	if match {
		m.chatTarget = chat.ID
		disp = m.dispatcher
		b = m.b
		fb = m.fallbackTop
	}
	m.mu.Unlock()
	if match && disp != nil {
		disp.updateTarget(b, chat.ID, fb)
	}
}

func (m *devMonitor) ResolveChatTarget(ctx context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	b := m.b
	target := m.chatTarget
	disp := m.dispatcher
	fb := m.fallbackTop
	m.mu.Unlock()
	if target == nil || parseMonitorChatID(target) != 0 || b == nil {
		return
	}
	resolved := resolveMonitorChatTarget(ctx, b, target)
	resolvedID := parseMonitorChatID(resolved)
	if resolvedID == 0 {
		return
	}
	m.mu.Lock()
	if parseMonitorChatID(m.chatTarget) == 0 {
		m.chatTarget = resolvedID
		disp = m.dispatcher
		b = m.b
		fb = m.fallbackTop
	} else {
		disp = nil
	}
	m.mu.Unlock()
	if disp != nil {
		disp.updateTarget(b, resolvedID, fb)
	}
}

func (m *devMonitor) runStartupAndDBHealthLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(4 * time.Second):
	}
	for i := 0; i < 16; i++ {
		m.mu.Lock()
		ready := m.b != nil && m.b.API != nil
		m.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
	m.ResolveChatTarget(ctx)

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

	text := renderMonitorRichCard("🚀 Bot Loader · Gateway Ready", []monitorRichRow{
		{Label: "📂 SQLite", Value: sqliteStatus},
		{Label: "🍃 MongoDB", Value: mongoStatus},
		{Label: "⚡ Redis", Value: redisStatus},
		{Label: "🧠 RAM VPS", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(snap.memDetail))},
		{Label: "💻 CPU / Go", Value: fmt.Sprintf("<code>%d vCPU / %s</code>", runtime.NumCPU(), html.EscapeString(runtime.Version()))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", time.Now().Format("15:04:05"))},
	}, "ℹ️ Layanan Gateway MTProto aktif dan siap memproses panel serta broker Redis.", false)
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
	title := "Database &amp; VPS Health"
	if !snap.dbHealth.SQLiteOK || !snap.redisOK || highRAM {
		icon = "🔴"
		title = "Database / VPS Warning"
	} else if snap.mongoConfigured && !snap.dbHealth.MongoOK {
		icon = "🟡"
		title = "MongoDB Fallback Active"
	}

	sqliteBadge := "❌ Error"
	if snap.dbHealth.SQLiteOK {
		sqliteBadge = "🟢 OK"
	}
	mongoBadge := "⚪ Nonaktif"
	if snap.mongoConfigured {
		if snap.dbHealth.MongoOK {
			mongoBadge = "🟢 OK"
		} else {
			mongoBadge = "🟡 Fallback SQLite"
		}
	}
	redisBadge := "❌ Error"
	if snap.redisOK {
		redisBadge = "🟢 OK"
	}

	var errs []string
	if snap.dbHealth.SQLiteErr != "" {
		errs = append(errs, "• SQLite: <code>"+html.EscapeString(snap.dbHealth.SQLiteErr)+"</code>")
	}
	if snap.dbHealth.MongoErr != "" {
		errs = append(errs, "• MongoDB: <code>"+html.EscapeString(snap.dbHealth.MongoErr)+"</code>")
	}
	if snap.redisErr != "" {
		errs = append(errs, "• Redis: <code>"+html.EscapeString(snap.redisErr)+"</code>")
	}
	quote := "ℹ️ Seluruh komponen database dan VPS dalam kondisi sehat."
	expandable := false
	if len(errs) > 0 {
		quote = strings.Join(errs, "\n")
		expandable = true
	}

	text := renderMonitorRichCard(icon+" "+title, []monitorRichRow{
		{Label: "📂 SQLite", Value: sqliteBadge},
		{Label: "🍃 MongoDB", Value: mongoBadge},
		{Label: "⚡ Redis", Value: redisBadge},
		{Label: "🧠 RAM VPS", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(snap.memDetail))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", time.Now().Format("15:04:05"))},
	}, quote, expandable)
	m.enqueue("MONITOR_DATABASE", "dbhealth:"+summary, 5*time.Minute, monitorPriorityHigh, text)
}
