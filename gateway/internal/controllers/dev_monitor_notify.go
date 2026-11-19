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
	text := fmt.Sprintf(`👤 <b>[USER START]</b>
• <b>Nama:</b> %s
• <b>Username:</b> %s
• <b>User ID:</b> <code>%d</code>
• <b>Status Sesi:</b> <code>%s</code>
• 🕒 %s WIB`,
		html.EscapeString(fullName), uStr, userID, status, time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• %s (%s | <code>%d</code> | <code>%s</code>)", html.EscapeString(fullName), uStr, userID, status)
	m.enqueueBatched("MONITOR_USER_START", "👤 <b>[USER START DIGEST]</b>", fmt.Sprintf("start:%d", userID), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyUserbotConnected(chatID, userbotID int64, reason string) {
	text := fmt.Sprintf(`🟢 <b>[USERBOT CONNECTED]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Keterangan:</b> %s
• 🕒 %s WIB`,
		chatID, userbotID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> — %s", chatID, userbotID, html.EscapeString(reason))
	m.enqueueBatched("MONITOR_USERBOT_CONNECTED", "🟢 <b>[USERBOT CONNECTED DIGEST]</b>", fmt.Sprintf("conn:%d:%d", chatID, userbotID), 30*time.Second, monitorPriorityNormal, entry, text)
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
	text := fmt.Sprintf(`%s <b>[SESSION TRACK • %s]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%d</code>
• <b>Detail:</b> <code>%s</code>
• 🕒 %s WIB`,
		icon, html.EscapeString(stateUpper), chatID, ownerID, html.EscapeString(reason), time.Now().Format("15:04:05"),
	)
	entry := fmt.Sprintf("• %s <b>%s</b> Chat <code>%d</code> (Ubot <code>%d</code>): <code>%s</code>", icon, html.EscapeString(stateUpper), chatID, ownerID, html.EscapeString(truncateUTF8(reason, 180)))
	m.enqueueBatched("MONITOR_SESSION_TRACK", "🔄 <b>[SESSION TRACK DIGEST]</b>", fmt.Sprintf("track:%d:%s", chatID, stateUpper), 60*time.Second, monitorPriorityNormal, entry, text)
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
	text := fmt.Sprintf(`🎟️ <b>[TOKEN COLLECTED • %s]</b>
• <b>Chat ID:</b> <code>%d</code>
• <b>Userbot ID:</b> <code>%s</code>
• <b>Tier:</b> <code>%s</code>
• <b>Master:</b> <code>%s</code>
• <b>SHA:</b> <code>%s</code>
• 🕒 %s WIB`,
		kind, chatID, userbotIDStr, html.EscapeString(tier), html.EscapeString(masterToken), html.EscapeString(tokenSHA), time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_TOKEN_COLLECTED", fmt.Sprintf("tok_collected:%d:%s", chatID, tokenSHA), 30*time.Second, monitorPriorityHigh, text)
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
	m.enqueue("MONITOR_BACKUP_TOKEN", fmt.Sprintf("backup:%s:%d", lic.TokenSHA, lic.UserbotID), 60*time.Second, monitorPriorityHigh, text)
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
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> | <code>%s</code> | Exp <code>%s</code> | <code>%s</code>", lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier), lic.ExpiresAt.Local().Format("02-01 15:04"), html.EscapeString(truncateUTF8(lic.TokenSHA, 24)))
	m.enqueueBatched("MONITOR_TOKEN_EXPIRED", "⏳ <b>[TOKEN EXPIRED DIGEST]</b>", "expired:"+lic.TokenSHA, 10*time.Minute, monitorPriorityHigh, entry, text)
}

func (m *devMonitor) NotifyTraceback(source, title, detail string) {
	fullDetail := truncateUTF8(detail, 2500)
	text := fmt.Sprintf(`🐞 <b>[TRACEBACK • %s]</b>
<b>%s</b>
<pre>%s</pre>
• 🕒 %s WIB`,
		html.EscapeString(source), html.EscapeString(title), html.EscapeString(fullDetail), time.Now().Format("15:04:05"),
	)
	concise := truncateUTF8(strings.ReplaceAll(strings.TrimSpace(detail), "\n", " "), 280)
	entry := fmt.Sprintf("• <b>[%s] %s</b>: <code>%s</code>", html.EscapeString(source), html.EscapeString(truncateUTF8(title, 80)), html.EscapeString(concise))
	m.enqueueBatched("MONITOR_TRACEBACK", "🐞 <b>[TRACEBACK DIGEST]</b>", fmt.Sprintf("tb:%s:%s:%s", source, title, hashDedup(detail)), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyBotLoader(source, detail string) {
	text := fmt.Sprintf(`⚙️ <b>[BOT LOADER • %s]</b>
%s
• 🕒 %s WIB`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_BOT_LOADER", "loader:"+source+":"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyDatabase(source, detail string) {
	text := fmt.Sprintf(`🗄️ <b>[DATABASE • %s]</b>
%s
• 🕒 %s WIB`,
		html.EscapeString(source), detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_DATABASE", "db:"+source+":"+hashDedup(detail), 2*time.Minute, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyInvoice(detail string) {
	text := fmt.Sprintf(`🧾 <b>[INVOICE]</b>
%s
• 🕒 %s WIB`,
		detail, time.Now().Format("15:04:05"),
	)
	m.enqueue("MONITOR_INVOICE", "invoice:"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}
