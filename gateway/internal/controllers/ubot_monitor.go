package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func (h *Handler) handleBroadcastMonitor(ctx context.Context, b *botgotd.Bot, data models.PFromService) {
	if data.ChatID == 0 || data.TaskID == "" {
		return
	}
	markup := broadcastStopKeyboard(data.OwnerID, data.TaskID)
	if data.Type == "broadcast_start" {
		msg, err := sendMonitorRichMessageWithMarkup(ctx, b, data.ChatID, data.ThreadID, broadcastMonitorText(data), markup)
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
	text := broadcastMonitorText(data)
	if data.Type == "broadcast_done" {
		text = broadcastDetailText(data)
	}
	if _, err := editMonitorRichMessageWithMarkup(ctx, b, monitor.ChatID, monitor.MessageID, text, markup); err != nil {
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
	builder.WriteString("<h1>📊 Laporan Akhir Broadcast</h1>\n<table>\n")
	fmt.Fprintf(&builder, "  <tr><td><b>✅ Terkirim</b></td><td><code>%d</code></td></tr>\n", data.Success)
	fmt.Fprintf(&builder, "  <tr><td><b>❌ Gagal</b></td><td><code>%d</code></td></tr>\n", data.Failed)
	fmt.Fprintf(&builder, "  <tr><td><b>🎯 Total Target</b></td><td><code>%d</code></td></tr>\n", data.Total)
	builder.WriteString("</table>\n")
	if len(data.Errors) == 0 {
		builder.WriteString("<blockquote>ℹ️ Seluruh pesan siaran berhasil dikirim tanpa kendala.</blockquote>")
	} else {
		builder.WriteString("<blockquote expandable><b>Catatan Kendala:</b>\n<pre>")
		for index, item := range data.Errors {
			if index >= 30 || builder.Len() > 3750 {
				fmt.Fprintf(&builder, "... dan %d kendala lainnya\n", len(data.Errors)-index)
				break
			}
			line := fmt.Sprintf("%d. %s\n", index+1, html.EscapeString(item))
			if builder.Len()+len(line) > 3850 {
				fmt.Fprintf(&builder, "... dan %d kendala lainnya\n", len(data.Errors)-index)
				break
			}
			builder.WriteString(line)
		}
		builder.WriteString("</pre></blockquote>")
	}
	return builder.String()
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
	bar := strings.Repeat("■", filled) + strings.Repeat("□", 10-filled)
	status := fmt.Sprintf("%d%%", progress)
	title := "📡 Monitor Broadcast"
	if data.Type == "broadcast_done" {
		if progress == 100 {
			title = "✅ Broadcast Selesai"
			status = "100% selesai"
		} else {
			title = "🛑 Broadcast Dibatalkan"
			status = fmt.Sprintf("%d%% dibatalkan", progress)
		}
	}
	return fmt.Sprintf(`<h1>%s</h1>
<table>
  <tr><td><b>🚀 Progress</b></td><td><code>[%s] %s</code></td></tr>
  <tr><td><b>✅ Terkirim</b></td><td><code>%d</code></td></tr>
  <tr><td><b>❌ Gagal</b></td><td><code>%d</code></td></tr>
  <tr><td><b>🎯 Total</b></td><td><code>%d</code></td></tr>
</table>
<blockquote>ℹ️ Pantau progres pengiriman userbot secara berkala.</blockquote>`, title, bar, status, data.Success, data.Failed, data.Total)
}

func broadcastStopKeyboard(ownerID int64, taskID string) *botgotd.InlineKeyboardMarkup {
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{
		Text: "🛑 Hentikan Broadcast", SwitchInlineQueryCurrentChat: fmt.Sprintf("gcancel %d %s", ownerID, taskID),
	}}}}
}

func (h *Handler) SimulateLoad(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/broadcast"))
	if message == "" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Gunakan: /broadcast <pesan>"})
		return
	}
	update.Message.Text = message
	h.Ubot(ctx, b, update)
}

var gatewayHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	},
}

func sendRichMessage(ctx context.Context, token string, chatID int64, htmlContent string, markup any) (int, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendRichMessage", token)
	payload := map[string]any{
		"chat_id": chatID,
		"rich_message": map[string]any{
			"html": botgotd.FormatRichHTML(htmlContent),
		},
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	bodyData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := gatewayHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(respBody, &res)
	return res.Result.MessageID, nil
}

func stripRichTags(s string) string {
	s = strings.ReplaceAll(s, "<h1>", "<b>")
	s = strings.ReplaceAll(s, "</h1>", "</b>\n")
	s = strings.ReplaceAll(s, "<h2>", "<b>")
	s = strings.ReplaceAll(s, "</h2>", "</b>\n")
	if idx := strings.Index(s, "<tg-button-row>"); idx != -1 {
		if endIdx := strings.Index(s, "</tg-button-row>"); endIdx != -1 {
			s = s[:idx] + s[endIdx+len("</tg-button-row>"):]
		}
	}
	s = strings.ReplaceAll(s, "<tr><th>", "• <b>")
	s = strings.ReplaceAll(s, "</th><th>", " | ")
	s = strings.ReplaceAll(s, "</th></tr>", "</b>\n")
	s = strings.ReplaceAll(s, "<tr><td>", "• ")
	s = strings.ReplaceAll(s, "</td><td>", ": ")
	s = strings.ReplaceAll(s, "</td></tr>", "\n")
	tableTags := []string{
		"<table border=\"1\">", "<table>", "</table>",
		"<tr>", "</tr>", "<th>", "</th>", "<td>", "</td>",
		"<thead>", "</thead>", "<tbody>", "</tbody>",
	}
	for _, tag := range tableTags {
		s = strings.ReplaceAll(s, tag, "")
	}
	s = strings.ReplaceAll(s, "<hr/>", "")
	s = strings.ReplaceAll(s, "<hr />", "")
	s = strings.ReplaceAll(s, "<hr>", "")
	return strings.TrimSpace(s)
}
