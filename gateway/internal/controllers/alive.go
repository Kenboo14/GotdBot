package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"

	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"

	"github.com/shirou/gopsutil/v3/host"
)

type aliveInlinePayload struct {
	OwnerID int64 `json:"owner_id"`
	ChatID  int64 `json:"chat_id"`
}

type alivePingResult struct {
	OK    bool    `json:"ok"`
	MS    float64 `json:"ms,omitempty"`
	Error string  `json:"error,omitempty"`
}

func jakartaLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil && loc != nil {
		return loc
	}
	return time.FixedZone("WIB", 7*3600)
}

func serverUptimeDuration() time.Duration {
	if hInfo, err := host.Info(); err == nil && hInfo != nil && hInfo.Uptime > 0 {
		return time.Duration(hInfo.Uptime) * time.Second
	}
	return time.Since(gatewayStartedAt)
}

func formatServerUptimeAlive(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int64(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if days > 0 {
		return fmt.Sprintf("%d hari, %d jam, %d menit", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%d jam, %d menit", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d menit", minutes)
	}
	return fmt.Sprintf("%d detik", seconds)
}

func formatAliveExpiredTime(t time.Time) string {
	return t.In(jakartaLocation()).Format("15:04, 2 January 2006")
}

func resolveAliveExpired(ctx context.Context, db *databases.DataManager, ownerID, chatID int64) string {
	if db == nil {
		return "Belum diatur"
	}
	var lic *databases.UserLicense
	for _, id := range []int64{ownerID, chatID} {
		if id == 0 {
			continue
		}
		if l, _ := db.GetActiveUserLicense(ctx, id); l != nil {
			lic = l
			break
		}
	}
	if lic == nil {
		for _, id := range []int64{ownerID, chatID} {
			if id == 0 {
				continue
			}
			if l, _ := db.GetLatestUserLicense(ctx, id); l != nil {
				lic = l
				break
			}
		}
	}
	if lic != nil && !lic.ExpiresAt.IsZero() {
		if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
			return fmt.Sprintf("Kedaluwarsa (%s)", formatAliveExpiredTime(lic.ExpiresAt))
		}
		return formatAliveExpiredTime(lic.ExpiresAt)
	}
	for _, id := range []int64{ownerID, chatID} {
		if id == 0 {
			continue
		}
		if cfg := db.GetUserConfig(ctx, id); cfg.MasaAktif != "" && cfg.MasaAktif != "Belum diatur" {
			return cfg.MasaAktif
		}
	}
	return "Belum diatur"
}

func buildAliveMessageText(userID int64, expiredStr, uptimeStr string) string {
	return fmt.Sprintf(
		"── <b><i>GO UserBot</i></b> ──\n"+
			"  • <b>UserID:</b> <code>%d</code>\n"+
			"  • <b>Expired:</b> <i>%s</i>\n"+
			"  • <b>Uptime:</b> <i>%s</i>",
		userID,
		html.EscapeString(expiredStr),
		html.EscapeString(uptimeStr),
	)
}

func buildAliveKeyboard(ownerID, chatID int64) *botgotd.InlineKeyboardMarkup {
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{{
				Text:         "Ping!",
				CallbackData: fmt.Sprintf("alive_ping:%d:%d", ownerID, chatID),
			}},
		},
	}
}

func handleAliveInline(ctx context.Context, b *botgotd.Bot, update *botgotd.Update, query string) {
	var ownerID, chatID int64
	if update.InlineQuery != nil && update.InlineQuery.From.ID > 0 {
		ownerID = update.InlineQuery.From.ID
		chatID = ownerID
	}

	var ref string
	for _, part := range strings.Fields(query) {
		if strings.HasPrefix(part, "ref:") {
			ref = strings.TrimPrefix(part, "ref:")
			break
		}
	}
	if ref != "" && inlineRedis != nil && inlineRedis.Client != nil {
		if raw, err := inlineRedis.Client.Get(ctx, "inline:alive:"+ref).Bytes(); err == nil {
			var p aliveInlinePayload
			if json.Unmarshal(raw, &p) == nil {
				if p.OwnerID > 0 {
					ownerID = p.OwnerID
				}
				if p.ChatID != 0 {
					chatID = p.ChatID
				}
			}
		}
	}
	if chatID == 0 {
		chatID = ownerID
	}

	expiredStr := resolveAliveExpired(ctx, inlineDB, ownerID, chatID)
	uptimeStr := formatServerUptimeAlive(serverUptimeDuration())
	text := buildAliveMessageText(ownerID, expiredStr, uptimeStr)

	result := &botgotd.InlineQueryResultArticle{
		ID:          fmt.Sprintf("assist_alive_%d", ownerID),
		Title:       "GO UserBot Alive",
		Description: "Informasi status userbot, masa aktif, dan uptime server",
		InputMessageContent: &botgotd.InputTextMessageContent{
			MessageText: text,
			ParseMode:   "HTML",
		},
		ReplyMarkup: buildAliveKeyboard(ownerID, chatID),
	}
	_, err := b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{
		InlineQueryID: update.InlineQuery.ID,
		Results:       []botgotd.InlineQueryResult{result},
		CacheTime:     0,
		IsPersonal:    true,
	})
	if err != nil {
		log.Printf("⚠️ [inline-alive] AnswerInlineQuery error: %v", err)
	}
}

func formatAlivePingAlert(res alivePingResult) string {
	if !res.OK {
		return "Userbot sedang offline atau tidak aktif."
	}
	return fmt.Sprintf("Pong..!!\n%.3fms", res.MS)
}

func (h *Handler) invokeUserbotPing(ctx context.Context, ownerID, chatID int64) alivePingResult {
	if h == nil || h.broker == nil || h.broker.Client == nil {
		return alivePingResult{OK: false, Error: "broker_unavailable"}
	}
	taskID := fmt.Sprintf("%d_%d", ownerID, time.Now().UnixNano())
	key := "alive:ping:" + taskID

	waitCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer cancel()

	sub := h.broker.Client.Subscribe(waitCtx, key)
	defer sub.Close()
	if _, err := sub.Receive(waitCtx); err != nil {
		log.Printf("⚠️ [alive_ping] subscribe err: %v", err)
	}

	_ = h.broker.To(waitCtx, "to:ubot:command", models.PToService{
		ChatID:  chatID,
		UserID:  ownerID,
		OwnerID: ownerID,
		TaskID:  taskID,
		Type:    "alive_ping",
	})

	ch := sub.Channel()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return alivePingResult{OK: false, Error: "channel_closed"}
			}
			var res alivePingResult
			if json.Unmarshal([]byte(msg.Payload), &res) == nil {
				return res
			}
		case <-waitCtx.Done():
			if raw, err := h.broker.Client.Get(context.Background(), key).Bytes(); err == nil {
				var res alivePingResult
				if json.Unmarshal(raw, &res) == nil {
					return res
				}
			}
			return alivePingResult{OK: false, Error: "timeout"}
		}
	}
}

func (h *Handler) AlivePingCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	parts := strings.Split(update.CallbackQuery.Data, ":")
	var ownerID, chatID int64
	if len(parts) >= 2 {
		ownerID, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) >= 3 {
		chatID, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	if ownerID == 0 {
		ownerID = update.CallbackQuery.From.ID
	}
	if chatID == 0 {
		chatID = ownerID
	}

	res := h.invokeUserbotPing(ctx, ownerID, chatID)
	alertText := formatAlivePingAlert(res)

	_, err := b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:            alertText,
		ShowAlert:       true,
	})
	if err != nil {
		log.Printf("⚠️ [alive_ping] AnswerCallbackQuery error: %v", err)
	}
}
