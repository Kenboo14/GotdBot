package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
)

func sendWorkerCard(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, htmlText string) {
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, htmlText)
}

func (h *Handler) WorkerCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	senderID := update.Message.From.ID
	threadID := update.Message.MessageThreadID
	globalDevMonitor.ObserveIncomingChat(update.Message.Chat)
	if !isOwner(senderID) {
		return
	}
	if h.lb == nil {
		sendWorkerCard(ctx, b, chatID, threadID, renderMonitorRichCard("⚙️ Worker Pool", []monitorRichRow{{Label: "📌 Status", Value: "Load balancer belum aktif"}, {Label: "🕒 Mode", Value: "Single-worker default"}}, "ℹ️ Hubungkan Redis dan aktifkan WORKER_ID pada worker ubot.", false))
		return
	}

	fields := strings.Fields(update.Message.Text)
	subCmd := "status"
	if len(fields) >= 2 {
		subCmd = strings.ToLower(strings.TrimSpace(fields[1]))
	}

	switch subCmd {
	case "status":
		h.sendWorkerStatusCard(ctx, b, chatID, threadID)
	case "move":
		h.handleWorkerMoveCmd(ctx, b, chatID, threadID, fields)
	case "drain":
		h.handleWorkerDrainCmd(ctx, b, chatID, threadID, fields, true)
	case "undrain":
		h.handleWorkerDrainCmd(ctx, b, chatID, threadID, fields, false)
	default:
		msg := renderMonitorRichCard("⚙️ Panduan Perintah /worker", []monitorRichRow{
			{Label: "📊 Status", Value: "<code>/worker status</code>"},
			{Label: "🔀 Pindah User", Value: "<code>/worker move &lt;uid&gt; &lt;worker_id|hell|normal&gt;</code>"},
			{Label: "🚧 Drain", Value: "<code>/worker drain &lt;worker_id&gt;</code>"},
			{Label: "✅ Undrain", Value: "<code>/worker undrain &lt;worker_id&gt;</code>"},
		}, "ℹ️ Kelola distribusi user antar worker dalam pool VPS secara real-time.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
	}
}

func (h *Handler) sendWorkerStatusCard(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int) {
	knownIDs, err := h.lb.GetKnownWorkerIDs(ctx)
	live, liveErr := h.lb.ListWorkers(ctx)
	if err != nil || liveErr != nil {
		errObj := err
		if errObj == nil {
			errObj = liveErr
		}
		msg := renderMonitorRichCard("⚠️ Gagal Membaca Worker Pool", []monitorRichRow{
			{Label: "📌 Sumber", Value: "Redis Registry"},
			{Label: "❌ Detail", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(errObj.Error()))},
		}, "ℹ️ Periksa koneksi Redis sebelum mengulangi perintah.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}
	if len(knownIDs) == 0 && len(live) == 0 {
		msg := renderMonitorRichCard("⚙️ Status Worker Pool", []monitorRichRow{
			{Label: "📌 Mode", Value: "Single-worker legacy (<code>to:ubot:command</code>)"},
			{Label: "📊 Worker Aktif", Value: "<code>0 worker sharded</code>"},
		}, "ℹ️ Belum ada worker sharded yang mendaftarkan heartbeat di Redis.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}

	liveMap := make(map[string]WorkerStatus, len(live))
	for _, w := range live {
		liveMap[w.WorkerID] = w
	}
	rows := make([]monitorRichRow, 0, len(knownIDs)+1)
	totalActive := 0
	totalCap := 0
	for _, wid := range knownIDs {
		w, online := liveMap[wid]
		if !online {
			rows = append(rows, monitorRichRow{
				Label: "🔴 " + html.EscapeString(wid),
				Value: "Offline (heartbeat tidak terdeteksi)",
			})
			continue
		}
		totalActive += w.ActiveSessions
		totalCap += w.MaxSessions
		badge := "🟢 Ready"
		if w.Status != "ready" {
			badge = "🟡 " + html.EscapeString(w.Status)
		}
		poolTag := "normal"
		if w.Pool == "hell" {
			poolTag = "hell"
		}
		rows = append(rows, monitorRichRow{
			Label: "⚙️ " + html.EscapeString(wid),
			Value: fmt.Sprintf("%s · <code>%s</code> · <code>%d/%d</code> user · <code>%d MiB</code>", badge, poolTag, w.ActiveSessions, w.MaxSessions, w.RAMAllocMB),
		})
	}
	if len(rows) == 1 {
		rows = append(rows, monitorRichRow{
			Label: "📊 Total Kapasitas",
			Value: fmt.Sprintf("<code>%d / %d user</code>", totalActive, totalCap),
		})
	}
	quote := fmt.Sprintf("ℹ️ Total aktif pool: <b>%d / %d</b> user terdaftar · Warm Pool 1 VPS.", totalActive, totalCap)
	msg := renderMonitorRichCard("⚙️ Status Worker Pool", rows, quote, false)
	sendWorkerCard(ctx, b, chatID, threadID, msg)
}

func (h *Handler) handleWorkerMoveCmd(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, fields []string) {
	if len(fields) < 4 {
		msg := renderMonitorRichCard("⚠️ Format Perintah Kurang", []monitorRichRow{
			{Label: "📌 Format", Value: "<code>/worker move &lt;uid&gt; &lt;worker_id|hell|normal&gt;</code>"},
			{Label: "💡 Contoh", Value: "<code>/worker move 1935806583 ubot-hell</code>"},
		}, "ℹ️ Masukkan User ID target dan worker tujuan.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}
	uid, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil || uid <= 0 {
		msg := renderMonitorRichCard("❌ User ID Tidak Valid", []monitorRichRow{
			{Label: "🆔 Input", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(fields[2]))},
			{Label: "📌 Syarat", Value: "Angka ID Telegram positif"},
		}, "ℹ️ Periksa kembali UID pengguna.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}

	targetArg := strings.TrimSpace(fields[3])
	targetWorkerID := targetArg
	targetPool := ""
	if strings.EqualFold(targetArg, "hell") || strings.EqualFold(targetArg, "normal") {
		live, listErr := h.lb.ListWorkers(ctx)
		if listErr != nil {
			sendWorkerCard(ctx, b, chatID, threadID, fmt.Sprintf("<code>%s</code>", html.EscapeString(listErr.Error())))
			return
		}
		chosen, ok := SelectLeastLoaded(live, strings.ToLower(targetArg))
		if !ok {
			msg := renderMonitorRichCard("❌ Pool Tujuan Penuh / Offline", []monitorRichRow{
				{Label: "🆔 User ID", Value: fmt.Sprintf("<code>%d</code>", uid)},
				{Label: "📦 Pool", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(strings.ToLower(targetArg)))},
			}, "ℹ️ Tidak ada worker siap pakai dengan slot kosong pada pool tujuan.", false)
			sendWorkerCard(ctx, b, chatID, threadID, msg)
			return
		}
		targetWorkerID = chosen.WorkerID
		targetPool = chosen.Pool
	}

	if err := h.lb.TriggerMigration(ctx, uid, targetWorkerID, targetPool, "owner_move"); err != nil {
		msg := renderMonitorRichCard("❌ Gagal Memindahkan User", []monitorRichRow{
			{Label: "🆔 User ID", Value: fmt.Sprintf("<code>%d</code>", uid)},
			{Label: "🎯 Tujuan", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(targetWorkerID))},
			{Label: "⚠️ Error", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(err.Error()))},
		}, "ℹ️ Pastikan worker tujuan aktif.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}

	msg := renderMonitorRichCard("🔀 Perintah Migrasi Dikirim", []monitorRichRow{
		{Label: "🆔 User ID", Value: fmt.Sprintf("<code>%d</code>", uid)},
		{Label: "🎯 Worker Tujuan", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(targetWorkerID))},
	}, "ℹ️ Sesi akan berpindah tanpa login ulang dan siaran aktif dilanjutkan otomatis.", false)
	sendWorkerCard(ctx, b, chatID, threadID, msg)
}

func (h *Handler) handleWorkerDrainCmd(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, fields []string, drain bool) {
	if len(fields) < 3 {
		msg := renderMonitorRichCard("⚠️ Nama Worker Diperlukan", []monitorRichRow{
			{Label: "📌 Format Drain", Value: "<code>/worker drain &lt;worker_id&gt;</code>"},
			{Label: "📌 Format Undrain", Value: "<code>/worker undrain &lt;worker_id&gt;</code>"},
		}, "ℹ️ Tentukan ID worker misalnya ubot-1 atau ubot-2.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}
	wid := strings.TrimSpace(fields[2])
	if !drain {
		if err := h.lb.SetWorkerDrain(ctx, wid, false); err != nil {
			msg := renderMonitorRichCard("❌ Gagal Undrain", []monitorRichRow{
				{Label: "⚙️ Worker", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(wid))},
				{Label: "⚠️ Error", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(err.Error()))},
			}, "ℹ️ Periksa koneksi Redis.", false)
			sendWorkerCard(ctx, b, chatID, threadID, msg)
			return
		}
		msg := renderMonitorRichCard("✅ Worker Siap Menerima User", []monitorRichRow{
			{Label: "⚙️ Worker", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(wid))},
			{Label: "📌 Status", Value: "🟢 Ready"},
		}, "ℹ️ Flag drain dilepas dan worker kembali masuk rotasi load balancer.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}

	moved, err := h.lb.DrainWorker(ctx, wid)
	if err != nil {
		msg := renderMonitorRichCard("❌ Gagal Drain Worker", []monitorRichRow{
			{Label: "⚙️ Worker", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(wid))},
			{Label: "⚠️ Error", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(err.Error()))},
		}, "ℹ️ Periksa status worker dan slot pool lain.", false)
		sendWorkerCard(ctx, b, chatID, threadID, msg)
		return
	}
	msg := renderMonitorRichCard("🚧 Worker Mode Drain Aktif", []monitorRichRow{
		{Label: "⚙️ Worker", Value: fmt.Sprintf("<code>%s</code>", html.EscapeString(wid))},
		{Label: "🔀 User Dipindah", Value: fmt.Sprintf("<code>%d user</code>", moved)},
	}, "ℹ️ Worker dikeluarkan dari penerimaan user baru dan sesi dialihkan bertahap.", false)
	sendWorkerCard(ctx, b, chatID, threadID, msg)
}
