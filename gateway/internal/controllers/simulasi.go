package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"assterisk/gateway/internal/botgotd"
)

type simulationControlMsg struct {
	Action string `json:"action"`
	Target int    `json:"target"`
}

func (h *Handler) SimulasiCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil {
		return
	}

	callerID := update.Message.From.ID
	if callerID == 0 || callerID != h.getOwnerID() {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "<b>❌ Akses Ditolak</b>\nPerintah /simulasi hanya bisa digunakan oleh OWNER.",
		})
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/simulasi"))
	fields := strings.Fields(args)

	if len(fields) == 0 {
		h.sendSimulasiHelp(ctx, b, update.Message.Chat.ID)
		return
	}

	subCmd := strings.ToLower(fields[0])

	switch subCmd {
	case "off", "stop":
		h.handleSimulasiStop(ctx, b, update.Message.Chat.ID)
	case "status":
		h.handleSimulasiStatus(ctx, b, update.Message.Chat.ID)
	default:
		target, err := strconv.Atoi(subCmd)
		if err != nil || target <= 0 {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "<b>⚠️ Input tidak valid.</b> Masukkan angka target positif atau ketik <code>/simulasi off</code>.",
			})
			return
		}
		if target > 2000 {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "<b>⚠️ Target terlalu besar.</b> Maksimal 2.000 virtual userbot untuk mencegah beban berlebih.",
			})
			return
		}
		h.handleSimulasiScale(ctx, b, update.Message.Chat.ID, target)
	}
}

func (h *Handler) handleSimulasiScale(ctx context.Context, b *botgotd.Bot, chatID any, target int) {
	if h.broker == nil || h.broker.Client == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   "<b>❌ Broker Redis tidak tersedia.</b>",
		})
		return
	}

	cmd := simulationControlMsg{
		Action: "scale",
		Target: target,
	}
	payload, _ := json.Marshal(cmd)
	if err := h.broker.Client.Publish(ctx, "simulasi:control", payload).Err(); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("<b>❌ Gagal mengirim perintah ke engine simulasi:</b> %v", err),
		})
		return
	}

	report := fmt.Sprintf(`🧪 <b>Virtual Userbot Simulation Dikonfigurasi</b>
<blockquote expandable>Target beban diatur ke <b>%d</b> virtual userbot tanpa koneksi MTProto sungguhan.
Sistem melakukan dynamic scaling otomatis tanpa mengganggu akun produksi.</blockquote>
<hr/>
• Pantau metrik aktual secara live pada pesan <b>[GOTDBOT TELEMETRY LIVE]</b>.
• Ketik <code>/simulasi status</code> untuk cek statistik instan.
• Ketik <code>/simulasi off</code> untuk menghentikan.`, target)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      report,
		ParseMode: botgotd.ParseModeHTML,
	})
}

func (h *Handler) handleSimulasiStop(ctx context.Context, b *botgotd.Bot, chatID any) {
	if h.broker == nil || h.broker.Client == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   "<b>❌ Broker Redis tidak tersedia.</b>",
		})
		return
	}

	cmd := simulationControlMsg{Action: "stop"}
	payload, _ := json.Marshal(cmd)
	_ = h.broker.Client.Publish(ctx, "simulasi:control", payload).Err()

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      "🛑 <b>Simulasi Dihentikan.</b> Seluruh virtual userbot dimatikan dan dibersihkan dari memori.\nDasbor Telemetry akan kembali normal pada siklus refresh berikutnya.",
		ParseMode: botgotd.ParseModeHTML,
	})
}

func (h *Handler) handleSimulasiStatus(ctx context.Context, b *botgotd.Bot, chatID any) {
	if h.broker == nil || h.broker.Client == nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   "<b>❌ Broker Redis tidak tersedia.</b>",
		})
		return
	}

	data, err := h.broker.Client.Get(ctx, "simulasi:metrics").Result()
	if err != nil || data == "" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   "ℹ️ <b>Belum ada metrik simulasi aktif.</b> Jalankan perintah seperti <code>/simulasi 100</code> untuk memulai.",
		})
		return
	}

	var m simulationMetricsDoc
	if json.Unmarshal([]byte(data), &m) != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text:   "⚠️ Gagal membaca data metrik simulasi.",
		})
		return
	}

	statusIcon := "🟢"
	if m.Status != "RUNNING" {
		statusIcon = "🔴"
	}

	report := fmt.Sprintf(`🧪 <b>Statistik Virtual Userbot Simulation</b>
• Status: %s <b>%s</b>
• Target Users: <b>%d</b> | Aktif: <b>%d</b>
• Profil: <code>%d low / %d med / %d high</code>
• Generated Updates: <b>%d</b>
• Processed Updates: <b>%d</b>
• Failed Updates: <b>%d</b>
• Pending Queue: <b>%d</b>
• Throughput: <b>%.1f updates/sec</b>
• Latency: P50: <code>%.2f ms</code> | P95: <code>%.2f ms</code> | P99: <code>%.2f ms</code>
• Uptime Simulasi: <b>%ds</b>
• Concurrency: <b>%d Goroutines</b>
• Diperbarui: <code>%s</code>`,
		statusIcon, m.Status,
		m.TargetUsers, m.ActiveUsers,
		m.LowProfile, m.MedProfile, m.HighProfile,
		m.GeneratedUpdates,
		m.ProcessedUpdates,
		m.FailedUpdates,
		m.PendingQueue,
		m.ThroughputSec,
		m.LatencyP50Ms, m.LatencyP95Ms, m.LatencyP99Ms,
		m.UptimeSec,
		m.Goroutines,
		time.Unix(m.UpdatedAt, 0).Format("15:04:05 WIB"),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      report,
		ParseMode: botgotd.ParseModeHTML,
	})
}

func (h *Handler) sendSimulasiHelp(ctx context.Context, b *botgotd.Bot, chatID any) {
	help := `ℹ️ <b>Petunjuk Virtual Userbot Simulation:</b>
• <code>/simulasi 10</code> : Jalankan 10 virtual userbot
• <code>/simulasi 100</code> : Jalankan 100 virtual userbot
• <code>/simulasi 500</code> : Jalankan 500 virtual userbot
• <code>/simulasi 1000</code> : Jalankan 1.000 virtual userbot
• <code>/simulasi status</code> : Tampilkan statistik & throughput
• <code>/simulasi off</code> : Hentikan simulasi secara bersih

<i>Simulasi berjalan 100% tanpa koneksi Telegram asli & tidak memengaruhi akun produksi.</i>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      help,
		ParseMode: botgotd.ParseModeHTML,
	})
}

func (h *Handler) getOwnerID() int64 {
	ownerStr := strings.TrimSpace(os.Getenv("OWNER"))
	if ownerStr == "" {
		ownerStr = strings.TrimSpace(os.Getenv("OWNER_ID"))
	}
	var id int64
	fmt.Sscanf(ownerStr, "%d", &id)
	return id
}
