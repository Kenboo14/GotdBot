package controllers

import (
	"assterisk/gateway/applications/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
	"github.com/redis/go-redis/v9"
)

// Ubot submits exactly one job to the worker. It deliberately never generates
// background load from a Telegram command.
func (h *Handler) Ubot(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	allowed, estimated, err := h.broker.CheckLimit(ctx, fmt.Sprintf("ubot:broadcast:%d", update.Message.From.ID), 1, 5*time.Second)
	if err != nil {
		log.Printf("rate limiter error: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Layanan sementara tidak tersedia."})
		return
	}
	if !allowed {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Tunggu " + estimated.Round(time.Second).String() + " sebelum mengirim lagi."})
		return
	}
	payload := models.PToService{ChatID: update.Message.Chat.ID, UserID: update.Message.From.ID, Username: update.Message.From.Username, Text: update.Message.Text, Type: "broadcast"}
	if err := h.broker.To(ctx, "to:ubot:broadcast", payload); err != nil {
		log.Printf("Redis stream: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Worker tidak dapat menerima pesan. Cek Redis."})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Pesan sedang diproses."})
}

func (h *Handler) SendMessage(ctx context.Context, b *bot.Bot, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		log.Printf("invalid Redis payload for %s", msg.ID)
		return
	}
	var data models.PFromService
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		log.Printf("invalid Redis JSON for %s: %v", msg.ID, err)
		return
	}
	if strings.HasPrefix(data.Type, "broadcast_") {
		h.handleBroadcastMonitor(ctx, b, data)
		return
	}
	if data.ChatID == 0 || strings.TrimSpace(data.Text) == "" {
		log.Printf("incomplete Redis message %s", msg.ID)
		return
	}
	if data.State != "" {
		h.setInstallState(data.ChatID, data.State)
	}
	params := &bot.SendMessageParams{ChatID: data.ChatID, Text: data.Text}
	if strings.Contains(data.Text, "<") && strings.Contains(data.Text, ">") {
		params.ParseMode = "HTML"
	}
	switch data.State {
	case "waiting_phone":
		params.ReplyMarkup = installPhoneKeyboard()
	case "waiting_otp", "waiting_password", "waiting_worker":
		params.ReplyMarkup = cancelKeyboard()
	case "installed":
		params.ReplyMarkup = mainMenuMarkup
	case "failed":
		params.ReplyMarkup = cachedRemoveKeyboard
	case "session_revoked":
		params.ParseMode = "HTML"
		params.ReplyMarkup = cachedRemoveKeyboard
	case "panel_notice":
		params.ParseMode = "HTML"
		params.ReplyMarkup = grupListMenuMarkup
	}
	if _, err := b.SendMessage(ctx, params); err != nil {
		log.Printf("Telegram send failed: %v", err)
	}
}

func (h *Handler) handleBroadcastMonitor(ctx context.Context, b *bot.Bot, data models.PFromService) {
	if data.ChatID == 0 || data.TaskID == "" {
		return
	}
	if data.Type == "broadcast_start" {
		msg, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: data.ChatID, Text: broadcastMonitorText(data), ParseMode: "HTML",
			ReplyMarkup: broadcastStopKeyboard(data.OwnerID, data.TaskID),
		})
		if err != nil {
			log.Printf("broadcast monitor start failed: %v", err)
			return
		}
		h.broadcastMu.Lock()
		h.broadcastTasks[data.TaskID] = broadcastMonitor{ChatID: data.ChatID, MessageID: msg.ID}
		h.broadcastMu.Unlock()
		return
	}
	h.broadcastMu.Lock()
	monitor, ok := h.broadcastTasks[data.TaskID]
	h.broadcastMu.Unlock()
	if !ok {
		return
	}
	params := &bot.EditMessageTextParams{
		ChatID: monitor.ChatID, MessageID: monitor.MessageID,
		Text: broadcastMonitorText(data), ParseMode: "HTML",
	}
	if data.Type == "broadcast_done" {
		// The final report replaces the progress message instead of creating a
		// second message in the owner's private chat.
		params.Text = broadcastDetailText(data)
	}
	// Keep the stop button visible on the same monitor message, including the
	// final report. Pressing it after completion is harmless because the worker
	// has already removed the task.
	params.ReplyMarkup = broadcastStopKeyboard(data.OwnerID, data.TaskID)
	if _, err := b.EditMessageText(ctx, params); err != nil {
		log.Printf("broadcast monitor update failed: %v", err)
	}
	if data.Type == "broadcast_done" {
		h.broadcastMu.Lock()
		delete(h.broadcastTasks, data.TaskID)
		h.broadcastMu.Unlock()
	}
}

func broadcastDetailText(data models.PFromService) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "<pre>Laporan Broadcast\n\nTerkirim : %d\nGagal    : %d\nTotal    : %d\n\nDetail:\n", data.Success, data.Failed, data.Total)
	if len(data.Errors) == 0 {
		builder.WriteString("Tidak ada error pengiriman.")
	} else {
		for index, item := range data.Errors {
			if index >= 100 {
				break
			}
			fmt.Fprintf(&builder, "%d. %s\n", index+1, html.EscapeString(item))
		}
	}
	builder.WriteString("</pre>")
	text := builder.String()
	if len(text) > 4000 {
		return text[:4000] + "</pre>"
	}
	return text
}

func broadcastMonitorText(data models.PFromService) string {
	progress := data.Progress
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	filled := progress / 10
	bar := strings.Repeat("■ ", filled) + strings.Repeat("□ ", 10-filled)
	status := fmt.Sprintf("%d%%", progress)
	if data.Type == "broadcast_done" && progress == 100 {
		status = "100% selesai"
	} else if data.Type == "broadcast_done" {
		status = fmt.Sprintf("%d%% dibatalkan", progress)
	}
	return fmt.Sprintf("<pre>Proses Pengiriman\n\nTerkirim : %d\nGagal    : %d\nTotal    : %d\nStatus   : %s%s</pre>", data.Success, data.Failed, data.Total, bar, status)
}

func broadcastStopKeyboard(ownerID int64, taskID string) *t_models.InlineKeyboardMarkup {
	return &t_models.InlineKeyboardMarkup{InlineKeyboard: [][]t_models.InlineKeyboardButton{{{
		Text: "Hentikan Broadcast", SwitchInlineQueryCurrentChat: fmt.Sprintf("gcancel %d %s", ownerID, taskID),
	}}}}
}

// SimulateLoad is retained for compatibility. /broadcast now submits one job.
func (h *Handler) SimulateLoad(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/broadcast"))
	if message == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Gunakan: /broadcast <pesan>"})
		return
	}
	update.Message.Text = message
	h.Ubot(ctx, b, update)
}
