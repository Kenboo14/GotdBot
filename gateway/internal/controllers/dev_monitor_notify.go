package controllers

import (
	"assterisk/gateway/internal/databases"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

type monitorRichRow struct {
	Label string
	Value string
}

func renderMonitorRichCard(title string, rows []monitorRichRow, quote string, expandable bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>%s</h1>\n<table>\n", title)
	for _, row := range rows {
		fmt.Fprintf(&sb, "  <tr><td><b>%s</b></td><td>%s</td></tr>\n", row.Label, row.Value)
	}
	sb.WriteString("</table>")
	if strings.TrimSpace(quote) != "" {
		tag := "<blockquote>"
		if expandable {
			tag = "<blockquote expandable>"
		}
		fmt.Fprintf(&sb, "\n%s%s</blockquote>", tag, quote)
	}
	return sb.String()
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
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("👤 User Start", []monitorRichRow{
		{Label: "🏷️ Nama", Value: html.EscapeString(fullName)},
		{Label: "🔗 Username", Value: uStr},
		{Label: "🆔 User ID", Value: fmt.Sprintf("<code>%d</code>", userID)},
		{Label: "📌 Status Sesi", Value: html.EscapeString(status)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Pengguna menekan perintah /start di bot panel.", false)
	entry := fmt.Sprintf("• %s (%s | <code>%d</code> | <code>%s</code>)", html.EscapeString(fullName), uStr, userID, status)
	m.enqueueBatched("MONITOR_USER_START", "👤 User Start Digest", fmt.Sprintf("start:%d", userID), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyUserbotConnected(chatID, userbotID int64, reason string) {
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🟢 Userbot Connected", []monitorRichRow{
		{Label: "💬 Chat ID", Value: fmt.Sprintf("<code>%d</code>", chatID)},
		{Label: "🤖 Userbot ID", Value: fmt.Sprintf("<code>%d</code>", userbotID)},
		{Label: "📝 Keterangan", Value: html.EscapeString(reason)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Koneksi MTProto userbot aktif dan siap menerima tugas.", false)
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> — %s", chatID, userbotID, html.EscapeString(reason))
	m.enqueueBatched("MONITOR_USERBOT_CONNECTED", "🟢 Userbot Connected Digest", fmt.Sprintf("conn:%d:%d", chatID, userbotID), 30*time.Second, monitorPriorityNormal, entry, text)
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
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard(icon+" Session Track", []monitorRichRow{
		{Label: "📌 Status", Value: html.EscapeString(stateUpper)},
		{Label: "💬 Chat ID", Value: fmt.Sprintf("<code>%d</code>", chatID)},
		{Label: "🤖 Userbot ID", Value: fmt.Sprintf("<code>%d</code>", ownerID)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, fmt.Sprintf("ℹ️ <b>Detail Sesi:</b>\n<code>%s</code>", html.EscapeString(reason)), true)
	entry := fmt.Sprintf("• %s <b>%s</b> Chat <code>%d</code> (Ubot <code>%d</code>): <code>%s</code>", icon, html.EscapeString(stateUpper), chatID, ownerID, html.EscapeString(truncateUTF8(reason, 180)))
	m.enqueueBatched("MONITOR_SESSION_TRACK", "🔄 Session Track Digest", fmt.Sprintf("track:%d:%s", chatID, stateUpper), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyTokenCreated(creatorID int64, tier, token, durStr string, isShared bool) {
	mode := "Personal (1x)"
	if isShared {
		mode = "Shared Free"
	}
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🎫 Token Created", []monitorRichRow{
		{Label: "👑 Owner", Value: fmt.Sprintf("<code>%d</code>", creatorID)},
		{Label: "🎖️ Tier", Value: fmt.Sprintf("%s (%s)", html.EscapeString(tier), mode)},
		{Label: "⏳ Durasi", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(durStr))},
		{Label: "🔑 Token", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(token))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Voucher lisensi baru tercatat ke sistem.", false)
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
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🎟️ Token Collected", []monitorRichRow{
		{Label: "📌 Jenis", Value: kind},
		{Label: "💬 Chat ID", Value: fmt.Sprintf("<code>%d</code>", chatID)},
		{Label: "🤖 Userbot ID", Value: fmt.Sprintf("<code>%s</code>", userbotIDStr)},
		{Label: "🎖️ Tier", Value: html.EscapeString(tier)},
		{Label: "🎫 Master", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(masterToken))},
		{Label: "🔑 SHA", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(tokenSHA))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Klaim dan penautan token pengguna selesai diproses.", false)
	m.enqueue("MONITOR_TOKEN_COLLECTED", fmt.Sprintf("tok_collected:%d:%s", chatID, tokenSHA), 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyBackupToken(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🔐 Backup Token Vault", []monitorRichRow{
		{Label: "💬 Owner ChatID", Value: fmt.Sprintf("<code>%d</code>", lic.OwnerChatID)},
		{Label: "🤖 Userbot ID", Value: fmt.Sprintf("<code>%d</code>", lic.UserbotID)},
		{Label: "🎖️ Tier", Value: html.EscapeString(lic.Tier)},
		{Label: "⏳ Expired", Value: fmt.Sprintf("<code>%s</code>", lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"))},
		{Label: "🎫 Master", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(lic.MasterToken))},
		{Label: "🔑 Token SHA", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(lic.TokenSHA))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Brankas lisensi dicadangkan untuk pemulihan akun.", false)
	m.enqueue("MONITOR_BACKUP_TOKEN", fmt.Sprintf("backup:%s:%d", lic.TokenSHA, lic.UserbotID), 60*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyTokenExpired(lic *databases.UserLicense) {
	if lic == nil {
		return
	}
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("⏳ Token Expired", []monitorRichRow{
		{Label: "💬 Owner ChatID", Value: fmt.Sprintf("<code>%d</code>", lic.OwnerChatID)},
		{Label: "🤖 Userbot ID", Value: fmt.Sprintf("<code>%d</code>", lic.UserbotID)},
		{Label: "🎖️ Tier", Value: html.EscapeString(lic.Tier)},
		{Label: "📅 Expired At", Value: fmt.Sprintf("<code>%s</code>", lic.ExpiresAt.Local().Format("02-01-2006 15:04:05"))},
		{Label: "🔑 Token SHA", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(lic.TokenSHA))},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, "ℹ️ Masa aktif lisensi habis dan sesi userbot otomatis dicabut.", false)
	entry := fmt.Sprintf("• Chat <code>%d</code> | Ubot <code>%d</code> | <code>%s</code> | Exp <code>%s</code> | <code>%s</code>", lic.OwnerChatID, lic.UserbotID, html.EscapeString(lic.Tier), lic.ExpiresAt.Local().Format("02-01 15:04"), html.EscapeString(truncateUTF8(lic.TokenSHA, 24)))
	m.enqueueBatched("MONITOR_TOKEN_EXPIRED", "⏳ Token Expired Digest", "expired:"+lic.TokenSHA, 10*time.Minute, monitorPriorityHigh, entry, text)
}

func (m *devMonitor) NotifyTraceback(source, title, detail string) {
	fullDetail := truncateUTF8(detail, 2500)
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🐞 Traceback Error", []monitorRichRow{
		{Label: "⚙️ Sumber", Value: html.EscapeString(source)},
		{Label: "📌 Judul", Value: html.EscapeString(title)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, fmt.Sprintf("<pre>%s</pre>", html.EscapeString(fullDetail)), true)
	concise := truncateUTF8(strings.ReplaceAll(strings.TrimSpace(detail), "\n", " "), 280)
	entry := fmt.Sprintf("• <b>[%s] %s</b>: <code>%s</code>", html.EscapeString(source), html.EscapeString(truncateUTF8(title, 80)), html.EscapeString(concise))
	m.enqueueBatched("MONITOR_TRACEBACK", "🐞 Traceback Digest", fmt.Sprintf("tb:%s:%s:%s", source, title, hashDedup(detail)), 60*time.Second, monitorPriorityNormal, entry, text)
}

func (m *devMonitor) NotifyBotLoader(source, detail string) {
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("⚙️ Bot Loader Status", []monitorRichRow{
		{Label: "⚙️ Sumber", Value: html.EscapeString(source)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, fmt.Sprintf("ℹ️ %s", detail), true)
	m.enqueue("MONITOR_BOT_LOADER", "loader:"+source+":"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyDatabase(source, detail string) {
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🗄️ Database Status", []monitorRichRow{
		{Label: "⚙️ Sumber", Value: html.EscapeString(source)},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, fmt.Sprintf("ℹ️ %s", detail), true)
	m.enqueue("MONITOR_DATABASE", "db:"+source+":"+hashDedup(detail), 2*time.Minute, monitorPriorityHigh, text)
}

func (m *devMonitor) NotifyInvoice(detail string) {
	nowStr := time.Now().Format("15:04:05")
	text := renderMonitorRichCard("🧾 Invoice Transaksi", []monitorRichRow{
		{Label: "📌 Kategori", Value: "Pembayaran &amp; Lisensi"},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", nowStr)},
	}, fmt.Sprintf("ℹ️ %s", detail), true)
	m.enqueue("MONITOR_INVOICE", "invoice:"+hashDedup(detail), 30*time.Second, monitorPriorityHigh, text)
}
