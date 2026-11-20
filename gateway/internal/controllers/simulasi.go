package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"
	"time"
)

type simulationControlMsg struct {
	Action string `json:"action"`
	Target int    `json:"target"`
}

func (h *Handler) SimulasiCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil {
		return
	}
	globalDevMonitor.ObserveIncomingChat(update.Message.Chat)
	threadID := update.Message.MessageThreadID

	var callerID int64
	if update.Message.From != nil {
		callerID = update.Message.From.ID
	}
	allowed := callerID != 0 && (isOwner(callerID) || (h.getOwnerID() != 0 && callerID == h.getOwnerID()))
	if !allowed {
		msg := renderMonitorRichCard("❌ Akses Ditolak", []monitorRichRow{
			{Label: "📌 Perintah", Value: "<code>/simulasi</code>"},
			{Label: "🔐 Wewenang", Value: "Khusus OWNER"},
		}, "ℹ️ Perintah ini hanya dapat dijalankan oleh Owner sistem.", false)
		_, _ = sendMonitorRichMessage(ctx, b, update.Message.Chat.ID, threadID, msg)
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/simulasi"))
	fields := strings.Fields(args)

	if len(fields) == 0 {
		h.sendSimulasiHelp(ctx, b, update.Message.Chat.ID, threadID)
		return
	}

	subCmd := strings.ToLower(fields[0])
	switch subCmd {
	case "off", "stop":
		h.handleSimulasiStop(ctx, b, update.Message.Chat.ID, threadID)
	case "status":
		h.handleSimulasiStatus(ctx, b, update.Message.Chat.ID, threadID)
	default:
		target, err := strconv.Atoi(subCmd)
		if err != nil || target <= 0 {
			msg := renderMonitorRichCard("⚠️ Input Simulasi Tidak Valid", []monitorRichRow{
				{Label: "⌨️ Input", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(subCmd))},
				{Label: "📌 Format", Value: "<code>/simulasi &lt;jumlah&gt;</code> atau <code>/simulasi off</code>"},
			}, "ℹ️ Masukkan angka positif untuk memulai simulasi virtual.", false)
			_, _ = sendMonitorRichMessage(ctx, b, update.Message.Chat.ID, threadID, msg)
			return
		}
		h.handleSimulasiScale(ctx, b, update.Message.Chat.ID, threadID, target)
	}
}

func (h *Handler) handleSimulasiScale(ctx context.Context, b *botgotd.Bot, chatID any, threadID int, target int) {
	if h.broker == nil || h.broker.Client == nil {
		msg := renderMonitorRichCard("❌ Broker Tidak Tersedia", []monitorRichRow{
			{Label: "📌 Komponen", Value: "Redis Broker"},
			{Label: "🔴 Status", Value: "Offline"},
		}, "ℹ️ Pastikan koneksi Redis aktif.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	payload, _ := json.Marshal(simulationControlMsg{Action: "scale", Target: target})
	if err := h.broker.Client.Publish(ctx, "simulasi:control", payload).Err(); err != nil {
		msg := renderMonitorRichCard("❌ Gagal Mengirim Perintah Simulasi", []monitorRichRow{
			{Label: "⚠️ Error", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(err.Error()))},
		}, "ℹ️ Gagal mem-publish kontrol simulasi ke Redis.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	msg := renderMonitorRichCard("🧪 Simulasi Virtual Dikonfigurasi", []monitorRichRow{
		{Label: "👥 Target Virtual", Value: fmt.Sprintf("<code>%d userbot</code>", target)},
		{Label: "🛡️ Mode Isolasi", Value: "Tanpa koneksi MTProto asli"},
		{Label: "📊 Statistik", Value: "<code>/simulasi status</code>"},
		{Label: "🛑 Hentikan", Value: "<code>/simulasi off</code>"},
	}, "ℹ️ Sistem melakukan dynamic scaling tanpa mengganggu sesi produksi.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
}

func (h *Handler) handleSimulasiStop(ctx context.Context, b *botgotd.Bot, chatID any, threadID int) {
	if h.broker == nil || h.broker.Client == nil {
		msg := renderMonitorRichCard("❌ Broker Tidak Tersedia", []monitorRichRow{
			{Label: "📌 Komponen", Value: "Redis Broker"},
			{Label: "🔴 Status", Value: "Offline"},
		}, "ℹ️ Pastikan koneksi Redis aktif.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	payload, _ := json.Marshal(simulationControlMsg{Action: "stop"})
	_ = h.broker.Client.Publish(ctx, "simulasi:control", payload).Err()

	msg := renderMonitorRichCard("🛑 Simulasi Dihentikan", []monitorRichRow{
		{Label: "📌 Status", Value: "🟢 Bersih &amp; Dimatikan"},
		{Label: "🕒 Waktu", Value: fmt.Sprintf("<code>%s WIB</code>", time.Now().Format("15:04:05"))},
	}, "ℹ️ Seluruh virtual userbot dimatikan dan dibersihkan dari memori.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
}

func (h *Handler) handleSimulasiStatus(ctx context.Context, b *botgotd.Bot, chatID any, threadID int) {
	if h.broker == nil || h.broker.Client == nil {
		msg := renderMonitorRichCard("❌ Broker Tidak Tersedia", []monitorRichRow{
			{Label: "📌 Komponen", Value: "Redis Broker"},
			{Label: "🔴 Status", Value: "Offline"},
		}, "ℹ️ Pastikan koneksi Redis aktif.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	data, err := h.broker.Client.Get(ctx, "simulasi:metrics").Result()
	if err != nil || data == "" {
		msg := renderMonitorRichCard("ℹ️ Belum Ada Simulasi Aktif", []monitorRichRow{
			{Label: "📌 Status", Value: "Idle"},
			{Label: "💡 Mulai", Value: "<code>/simulasi 100</code>"},
		}, "ℹ️ Jalankan /simulasi &lt;jumlah&gt; untuk melihat metrik real-time.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	var m simulationMetricsDoc
	if json.Unmarshal([]byte(data), &m) != nil {
		msg := renderMonitorRichCard("⚠️ Gagal Membaca Metrik", []monitorRichRow{
			{Label: "📌 Sumber", Value: "<code>simulasi:metrics</code>"},
		}, "ℹ️ Format data metrik simulasi tidak valid.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
		return
	}

	statusBadge := "🟢 " + html.EscapeString(m.Status)
	if m.Status != "RUNNING" {
		statusBadge = "🔴 " + html.EscapeString(m.Status)
	}

	msg := renderMonitorRichCard("🧪 Statistik Simulasi Virtual", []monitorRichRow{
		{Label: "📌 Status", Value: statusBadge},
		{Label: "👥 Target / Aktif", Value: fmt.Sprintf("<code>%d / %d user</code>", m.TargetUsers, m.ActiveUsers)},
		{Label: "⚖️ Profil", Value: fmt.Sprintf("<code>%d low / %d med / %d high</code>", m.LowProfile, m.MedProfile, m.HighProfile)},
		{Label: "📨 Updates (Gen/Proc/Fail)", Value: fmt.Sprintf("<code>%d / %d / %d</code>", m.GeneratedUpdates, m.ProcessedUpdates, m.FailedUpdates)},
		{Label: "📦 Pending Queue", Value: fmt.Sprintf("<code>%d</code>", m.PendingQueue)},
		{Label: "🚀 Throughput", Value: fmt.Sprintf("<code>%.1f updates/sec</code>", m.ThroughputSec)},
		{Label: "⏱️ Latensi P50/P95/P99", Value: fmt.Sprintf("<code>%.2f / %.2f / %.2f ms</code>", m.LatencyP50Ms, m.LatencyP95Ms, m.LatencyP99Ms)},
		{Label: "🧵 Concurrency", Value: fmt.Sprintf("<code>%d Goroutines</code>", m.Goroutines)},
		{Label: "⏳ Uptime", Value: fmt.Sprintf("<code>%ds</code>", m.UptimeSec)},
		{Label: "🕒 Update", Value: fmt.Sprintf("<code>%s WIB</code>", time.Unix(m.UpdatedAt, 0).Format("15:04:05"))},
	}, "ℹ️ GotdBot Virtual Load Simulation Monitor.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
}

func (h *Handler) sendSimulasiHelp(ctx context.Context, b *botgotd.Bot, chatID any, threadID int) {
	msg := renderMonitorRichCard("🧪 Petunjuk Perintah /simulasi", []monitorRichRow{
		{Label: "🔹 Skala Kecil", Value: "<code>/simulasi 10</code> atau <code>/simulasi 100</code>"},
		{Label: "🔸 Skala Besar", Value: "<code>/simulasi 500</code> atau <code>/simulasi 1000</code>"},
		{Label: "📊 Cek Metrik", Value: "<code>/simulasi status</code>"},
		{Label: "🛑 Hentikan", Value: "<code>/simulasi off</code>"},
	}, "ℹ️ Simulasi berjalan 100% tanpa koneksi Telegram asli dan tidak mengganggu akun produksi.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, msg)
}

func (h *Handler) getOwnerID() int64 {
	ownerStr := strings.TrimSpace(os.Getenv("OWNER"))
	if ownerStr == "" {
		ownerStr = strings.TrimSpace(os.Getenv("OWNER_ID"))
	}
	var id int64
	_, _ = fmt.Sscanf(ownerStr, "%d", &id)
	return id
}
