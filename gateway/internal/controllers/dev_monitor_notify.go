package controllers

import (
	"assterisk/gateway/internal/databases"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

func (m *devMonitor) NotifyUserStart(userID int64, username, fullName string, hasActive bool) {
	uStr := "-"
	if username != "" {
		uStr = "@" + html.EscapeString(username)
	}
	status := "Belum terpasang"
	if hasActive {
		status = "Sudah aktif"
	}
	text := fmt.Sprintf(`<h1>👤 USER START</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🏷️ Nama</td><td>%s</td></tr>
  <tr><td>🔗 Username</td><td>%s</td></tr>
  <tr><td>🆔 User ID</td><td><code>%d</code></td></tr>
  <tr><td>📌 Status Sesi</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		html.EscapeString(fullName), uStr, userID, status, time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• %s (%s | <code>%d</code> | <code>%s</code>)", html.EscapeString(fullName), uStr, userID, status)
	m.enqueueBatched("MONITOR_USER_START", "👤 USER START DIGEST", fmt.Sprintf("start:%d", userID), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyUserbotConnected(chatID, userbotID int64, reason string) {
	text := fmt.Sprintf(`<h1>🟢 USERBOT CONNECTED</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>💬 Chat ID</td><td><code>%d</code></td></tr>
  <tr><td>🤖 Userbot ID</td><td><code>%d</code></td></tr>
  <tr><td>📝 Keterangan</td><td>%s</td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		chatID, userbotID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> — %s", chatID, userbotID, html.EscapeString(reason))
	m.enqueueBatched("MONITOR_USERBOT_CONNECTED", "🟢 USERBOT CONNECTED DIGEST", fmt.Sprintf("conn:%d:%d", chatID, userbotID), 30*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifySessionTrack(chatID, ownerID int64, state, reason string) {
	stateUpper := strings.ToUpper(state)
	icon := "🟡"
	switch stateUpper {
	case "SESSION_INVALID":
		icon = "🔴"
	case "RECONNECTING", "SUSPECTED":
		icon = "🟠"
	case "CONNECTED":
		icon = "🟢"
	}
	text := fmt.Sprintf(`<h1>%s SESSION TRACK · %s</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>💬 Chat ID</td><td><code>%d</code></td></tr>
  <tr><td>🤖 Userbot ID</td><td><code>%d</code></td></tr>
</table>
<blockquote expandable><code>%s</code></blockquote>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		icon, html.EscapeString(stateUpper), chatID, ownerID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• %s <b>%s</b> Chat <code>%d</code> (Ubot <code>%d</code>): <code>%s</code>", icon, html.EscapeString(stateUpper), chatID, ownerID, html.EscapeString(truncateUTF8(reason, 180)))
	m.enqueueBatched("MONITOR_SESSION_TRACK", "🔄 SESSION TRACK DIGEST", fmt.Sprintf("track:%d:%s", chatID, stateUpper), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyTokenCreated(creatorID int64, tier, token, durStr string, isShared bool) {
	mode := "Personal (1x)"
	if isShared {
		mode = "Shared Free"
	}
	text := fmt.Sprintf(`<h1>🎫 TOKEN CREATED</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>👑 Owner</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s (%s)</code></td></tr>
  <tr><td>⏳ Durasi</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		creatorID, html.EscapeString(tier), mode, html.EscapeString(durStr), html.EscapeString(token), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TOKEN_CREATED", "tok_created:"+token, 60*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyTokenCollected(chatID, userbotID int64, tier, masterToken, tokenSHA string, isRecovery bool) {
	kind := "Aktivasi Baru"
	if isRecovery {
		kind = "Pemulihan / Relink SHA"
	}
	userbotIDStr := "Belum terpasang"
	if userbotID != 0 {
		userbotIDStr = strconv.FormatInt(userbotID, 10)
	}
	text := fmt.Sprintf(`<h1>🎟️ TOKEN COLLECTED · %s</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>💬 Chat ID</td><td><code>%d</code></td></tr>
  <tr><td>🤖 Userbot ID</td><td><code>%s</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s</code></td></tr>
  <tr><td>🎫 Master</td><td><code>%s</code></td></tr>
  <tr><td>🔑 SHA</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		kind, chatID, userbotIDStr, html.EscapeString(tier), html.EscapeString(masterToken), html.EscapeString(tokenSHA), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TOKEN_COLLECTED", fmt.Sprintf("tok_collected:%d:%s", chatID, tokenSHA), 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyBackupToken(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	text := fmt.Sprintf(`<h1>🔐 BACKUP TOKEN VAULT</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>💬 Owner ChatID</td><td><code>%d</code></td></tr>
  <tr><td>🤖 Userbot ID</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Expires</td><td><code>%s</code></td></tr>
  <tr><td>🎫 Master</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier),
		lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
		html.EscapeString(lic.MasterToken), html.EscapeString(lic.TokenSHA),
		time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BACKUP_TOKEN", fmt.Sprintf("backup:%s:%d", lic.TokenSHA, lic.UserbotID), 60*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyTokenExpired(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	text := fmt.Sprintf(`<h1>⏳ TOKEN EXPIRED &amp; SESSION REVOKED</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>💬 Owner ChatID</td><td><code>%d</code></td></tr>
  <tr><td>🤖 Userbot ID</td><td><code>%d</code></td></tr>
  <tr><td>🎖️ Tier</td><td><code>%s</code></td></tr>
  <tr><td>📅 Expired At</td><td><code>%s</code></td></tr>
  <tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier),
		lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"),
		html.EscapeString(lic.TokenSHA),
		time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> | <code>%s</code> | Exp <code>%s</code> | <code>%s</code>", lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier), lic.ExpiresAt.Local().Format("02-01 15:04"), html.EscapeString(truncateUTF8(lic.TokenSHA, 24)))
	m.enqueueBatched("MONITOR_TOKEN_EXPIRED", "⏳ TOKEN EXPIRED DIGEST", "expired:"+lic.TokenSHA, 10*time.Minute, monitorPriorityHigh, entry, text)
}

func (m *devMonitor) NotifyTraceback(source, title, detail string) {
	fullDetail := truncateUTF8(detail, 2500)
	text := fmt.Sprintf(`<h1>🐞 TRACEBACK · %s</h1>
<table border="1">
  <tr><th>📌 Judul</th><td><b>%s</b></td></tr>
</table>
<blockquote expandable><pre>%s</pre></blockquote>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		html.EscapeString(source), html.EscapeString(title), html.EscapeString(fullDetail), time.Now().Format("15:04:05"),
	)
	concise := truncateUTF8(strings.ReplaceAll(strings.TrimSpace(detail), "\n", " "), 280)
	entry := fmt.Sprintf("• <b>[%s] %s</b>: <code>%s</code>", html.EscapeString(source), html.EscapeString(truncateUTF8(title, 80)), html.EscapeString(concise))
	m.enqueueBatched("MONITOR_TRACEBACK", "🐞 TRACEBACK DIGEST", fmt.Sprintf("tb:%s:%s:%s", source, title, hashDedup(detail)), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyBotLoader(source, detail string) {
	text := fmt.Sprintf(`<h1>⚙️ BOT LOADER · %s</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BOT_LOADER", "loader:"+source+":"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyDatabase(source, detail string) {
	text := fmt.Sprintf(`<h1>🗄️ DATABASE · %s</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_DATABASE", "db:"+source+":"+hashDedup(detail), 2*time.Minute, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyInvoice(detail string) {
	text := fmt.Sprintf(`<h1>🧾 INVOICE</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>🕒 %s WIB · GotdBot Monitor</p>`,
		detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_INVOICE", "invoice:"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}
