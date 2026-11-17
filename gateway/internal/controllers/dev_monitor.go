package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
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
	"github.com/shirou/gopsutil/v3/mem"
)

type devMonitorMsg struct {
	topicID int
	text    string
}

type devMonitor struct {
	mu          sync.Mutex
	chatTarget  any
	fallbackTop int
	topics      map[string]int
	queue       chan devMonitorMsg
	dedup       map[string]time.Time
	started     bool
	b           *botgotd.Bot
	db          *databases.DataManager
	rdb         *redis.Client
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
		globalDevMonitor.queue = make(chan devMonitorMsg, 128)
		globalDevMonitor.started = true
		go globalDevMonitor.worker(ctx)
		if target != nil {
			go globalDevMonitor.runStartupAndDBHealthLoop(ctx)
		}
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
	if len(m.dedup) > 512 {
		for k, ts := range m.dedup {
			if now.After(ts) {
				delete(m.dedup, k)
			}
		}
	}
	if until, ok := m.dedup[dedupKey]; ok && now.Before(until) {
		return false
	}
	m.dedup[dedupKey] = now.Add(cooldown)
	return true
}

func (m *devMonitor) enqueue(topicKey, dedupKey string, cooldown time.Duration, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.mu.Lock()
	target := m.chatTarget
	q := m.queue
	m.mu.Unlock()
	if target == nil || q == nil {
		return
	}
	if !m.shouldSendDedup(dedupKey, cooldown) {
		return
	}
	topicID := m.resolveTopic(topicKey)
	select {
	case q <- devMonitorMsg{topicID: topicID, text: text}:
	default:
		log.Printf("⚠️ [DevMonitor] queue full, dropping event for %s", topicKey)
	}
}

func (m *devMonitor) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-m.queue:
			m.deliver(ctx, item)
		}
	}
}

func (m *devMonitor) deliver(ctx context.Context, item devMonitorMsg) {
	m.mu.Lock()
	target := m.chatTarget
	fallback := m.fallbackTop
	b := m.b
	m.mu.Unlock()
	if target == nil || b == nil {
		return
	}

	sendAttempt := func(threadID int) error {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := b.SendMessage(sendCtx, &botgotd.SendMessageParams{
			ChatID:          target,
			MessageThreadID: threadID,
			Text:            item.text,
			ParseMode:       botgotd.ParseModeHTML,
		})
		return err
	}

	err := sendAttempt(item.topicID)
	if err == nil {
		return
	}

	// Fallback berjenjang jika topic spesifik invalid/tertutup:
	// topic spesifik -> MONITOR_TOPIC -> GROUP_DEV utama (threadID=0)
	if item.topicID != 0 && fallback != 0 && item.topicID != fallback {
		if fbErr := sendAttempt(fallback); fbErr == nil {
			return
		}
	}
	if item.topicID != 0 {
		if rootErr := sendAttempt(0); rootErr == nil {
			return
		}
	}
	log.Printf("⚠️ [DevMonitor] gagal kirim ke GROUP_DEV (topic=%d): %v", item.topicID, err)
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

func (m *devMonitor) notifyStartupStatus(ctx context.Context) {
	mongoConfigured := strings.TrimSpace(os.Getenv("MONGO_URI")) != "" || strings.TrimSpace(os.Getenv("MONGO_BACKUP_URI")) != ""
	var dbHealth databases.DBHealth
	if m.db != nil {
		dbHealth = m.db.CheckHealth(ctx, mongoConfigured)
	}
	redisOK := false
	if m.rdb != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		redisOK = m.rdb.Ping(rCtx).Err() == nil
		cancel()
	}
	var memStr string
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		memStr = fmt.Sprintf("%.1f / %.1f MiB (%.1f%%)", float64(vm.Used)/(1024*1024), float64(vm.Total)/(1024*1024), vm.UsedPercent)
	} else {
		memStr = "N/A"
	}

	sqliteStatus := "❌ Error"
	if dbHealth.SQLiteOK {
		sqliteStatus = "🟢 OK (WAL)"
	}
	mongoStatus := "⚪ Nonaktif"
	if mongoConfigured {
		if dbHealth.MongoOK {
			mongoStatus = "🟢 Connected"
		} else {
			mongoStatus = "🟡 Fallback SQLite"
		}
	}
	redisStatus := "❌ Error"
	if redisOK {
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
		sqliteStatus, mongoStatus, redisStatus, html.EscapeString(memStr),
		runtime.NumCPU(), html.EscapeString(runtime.Version()),
		time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BOT_LOADER", "", 0, text)
}

var (
	lastHealthMu      sync.Mutex
	lastHealthSummary string
)

func (m *devMonitor) checkAndReportHealth(ctx context.Context, forceStartup bool) {
	mongoConfigured := strings.TrimSpace(os.Getenv("MONGO_URI")) != "" || strings.TrimSpace(os.Getenv("MONGO_BACKUP_URI")) != ""
	var dbHealth databases.DBHealth
	if m.db != nil {
		dbHealth = m.db.CheckHealth(ctx, mongoConfigured)
	}
	redisOK := true
	redisErr := ""
	if m.rdb != nil {
		rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := m.rdb.Ping(rCtx).Err(); err != nil {
			redisOK = false
			redisErr = err.Error()
		}
		cancel()
	}

	ramPercent := 0.0
	ramDetail := "N/A"
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		ramPercent = vm.UsedPercent
		ramDetail = fmt.Sprintf("%.1f / %.1f MiB (%.1f%%)", float64(vm.Used)/(1024*1024), float64(vm.Total)/(1024*1024), vm.UsedPercent)
	}

	highRAM := ramPercent >= 85.0
	summary := fmt.Sprintf("sq=%v|mg=%v|rd=%v|hr=%v", dbHealth.SQLiteOK, dbHealth.MongoOK, redisOK, highRAM)

	lastHealthMu.Lock()
	changed := summary != lastHealthSummary
	lastHealthSummary = summary
	lastHealthMu.Unlock()

	if !forceStartup && !changed && dbHealth.SQLiteOK && redisOK && (!mongoConfigured || dbHealth.MongoOK) && !highRAM {
		return
	}

	icon := "🟢"
	title := "DATABASE & VPS HEALTH"
	if !dbHealth.SQLiteOK || !redisOK || highRAM {
		icon = "🔴"
		title = "DATABASE / VPS WARNING"
	} else if mongoConfigured && !dbHealth.MongoOK {
		icon = "🟡"
		title = "MONGODB FALLBACK ACTIVE"
	}

	extraErr := ""
	if dbHealth.SQLiteErr != "" {
		extraErr += "\n• SQLite: <code>" + html.EscapeString(dbHealth.SQLiteErr) + "</code>"
	}
	if dbHealth.MongoErr != "" {
		extraErr += "\n• MongoDB: <code>" + html.EscapeString(dbHealth.MongoErr) + "</code>"
	}
	if redisErr != "" {
		extraErr += "\n• Redis: <code>" + html.EscapeString(redisErr) + "</code>"
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
		dbHealth.SQLiteOK, dbHealth.MongoOK, mongoConfigured, redisOK,
		html.EscapeString(ramDetail), extraErr,
		time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_DATABASE", "dbhealth:"+summary, 5*time.Minute, text)
}

func (m *devMonitor) NotifyUserStart(userID int64, username, fullName string, hasActive bool) {
	uStr := "-"
	if username != "" {
		uStr = "@" + html.EscapeString(username)
	}
	status := "Belum terpasang"
	if hasActive {
		status = "Sudah aktif"
	}
	text := fmt.Sprintf(`👤 <b>[USER START]</b>
• <b>Nama:</b> %s
• <b>Username:</b> %s
• <b>User ID:</b> <code>%d</code>
• <b>Status Sesi:</b> <code>%s</code>
• 🕒 %s WIB`,
		html.EscapeString(fullName), uStr, userID, status, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_USER_START", fmt.Sprintf("start:%d", userID), 30*time.Second, text)
}

func (m *devMonitor) NotifyUserbotConnected(chatID, userbotID int64, reason string) {
	text := fmt.Sprintf(`🟢 <b>[USERBOT CONNECTED]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Keterangan:</b> %s
• 🕒 %s WIB`,
		chatID, userbotID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_USERBOT_CONNECTED", fmt.Sprintf("conn:%d:%s", chatID, reason), 20*time.Second, text)
}

func (m *devMonitor) NotifySessionTrack(chatID, ownerID int64, state, reason string) {
	icon := "🟡"
	switch strings.ToUpper(state) {
	case "SESSION_INVALID":
		icon = "🔴"
	case "RECONNECTING", "SUSPECTED":
		icon = "🟠"
	case "CONNECTED":
		icon = "🟢"
	}
	text := fmt.Sprintf(`%s <b>[SESSION TRACK • %s]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Detail:</b> <code>%s</code>
• 🕒 %s WIB`,
		icon, html.EscapeString(strings.ToUpper(state)), chatID, ownerID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_SESSION_TRACK", fmt.Sprintf("track:%d:%s", chatID, state), 45*time.Second, text)
}

func (m *devMonitor) NotifyTokenCreated(creatorID int64, tier, token, durStr string, isShared bool) {
	mode := "Personal (1x)"
	if isShared {
		mode = "Shared Free"
	}
	text := fmt.Sprintf(`🎫 <b>[TOKEN CREATED]</b>
• <b>Owner:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code> (%s)
• <b>Durasi:</b> <code>%s</code>
• <b>Token:</b> <code>%s</code>
• 🕒 %s WIB`,
		creatorID, html.EscapeString(tier), mode, html.EscapeString(durStr), html.EscapeString(token), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TOKEN_CREATED", "", 0, text)
}

func (m *devMonitor) NotifyTokenCollected(chatID, userbotID int64, tier, masterToken, tokenSHA string, isRecovery bool) {
	kind := "Aktivasi Baru"
	if isRecovery {
		kind = "Pemulihan / Relink SHA"
	}
	text := fmt.Sprintf(`🎟️ <b>[TOKEN COLLECTED • %s]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code>
• <b>Master:</b> <code>%s</code>
• <b>SHA:</b> <code>%s</code>
• 🕒 %s WIB`,
		kind, chatID, userbotID, html.EscapeString(tier), html.EscapeString(masterToken), html.EscapeString(tokenSHA), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TOKEN_COLLECTED", "", 0, text)
}

func (m *devMonitor) NotifyBackupToken(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	text := fmt.Sprintf(`🔐 <b>[BACKUP TOKEN VAULT]</b>
• <b>Owner ChatID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code>
• <b>Expires:</b> <code>%s</code>
• <b>Master:</b> <code>%s</code>
• <b>Token SHA:</b> <code>%s</code>`,
		lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier),
		lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
		html.EscapeString(lic.MasterToken), html.EscapeString(lic.TokenSHA),
	)
	m.enqueue("MONITOR_BACKUP_TOKEN", "", 0, text)
}

func (m *devMonitor) NotifyTokenExpired(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	text := fmt.Sprintf(`⏳ <b>[TOKEN EXPIRED &amp; SESSION REVOKED]</b>
• <b>Owner ChatID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Tier:</b> <code>%s</code>
• <b>Expired At:</b> <code>%s</code>
• <b>Token SHA:</b> <code>%s</code>`,
		lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier),
		lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
		html.EscapeString(lic.TokenSHA),
	)
	m.enqueue("MONITOR_TOKEN_EXPIRED", "", 0, text)
}

func (m *devMonitor) NotifyTraceback(source, title, detail string) {
	if len(detail) > 2500 {
		detail = detail[:2500] + "..."
	}
	text := fmt.Sprintf(`🐞 <b>[TRACEBACK • %s]</b>
<b>%s</b>
<pre>%s</pre>
• 🕒 %s WIB`,
		html.EscapeString(source), html.EscapeString(title), html.EscapeString(detail), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TRACEBACK", "tb:"+source+":"+title, 30*time.Second, text)
}

func (m *devMonitor) NotifyBotLoader(source, detail string) {
	text := fmt.Sprintf(`⚙️ <b>[BOT LOADER • %s]</b>
%s
• 🕒 %s WIB`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BOT_LOADER", "", 0, text)
}

func (m *devMonitor) NotifyDatabase(source, detail string) {
	text := fmt.Sprintf(`🗄️ <b>[DATABASE • %s]</b>
%s
• 🕒 %s WIB`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_DATABASE", "db:"+source+":"+detail, 2*time.Minute, text)
}

func (m *devMonitor) NotifyInvoice(detail string) {
	text := fmt.Sprintf(`🧾 <b>[INVOICE]</b>
%s
• 🕒 %s WIB`,
		detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_INVOICE", "", 0, text)
}
