package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"encoding/json"
	"strings"
)

type ubotInlineCardButton struct {
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`
	Alert string `json:"alert,omitempty"`
}

type ubotInlineCardPayload struct {
	Title   string                   `json:"title"`
	HTML    string                   `json:"html"`
	Buttons [][]ubotInlineCardButton `json:"buttons,omitempty"`
}

func buildUCardMarkup(rows [][]ubotInlineCardButton) *botgotd.InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	kb := make([][]botgotd.InlineKeyboardButton, 0, len(rows))
	for _, r := range rows {
		btnRow := make([]botgotd.InlineKeyboardButton, 0, len(r))
		for _, btn := range r {
			if strings.TrimSpace(btn.Text) == "" {
				continue
			}
			b := botgotd.InlineKeyboardButton{Text: btn.Text}
			if btn.URL != "" {
				b.URL = btn.URL
			} else if btn.Alert != "" {
				b.CallbackData = "ucard_alert:" + btn.Alert
			} else {
				continue
			}
			btnRow = append(btnRow, b)
		}
		if len(btnRow) > 0 {
			kb = append(kb, btnRow)
		}
	}
	if len(kb) == 0 {
		return nil
	}
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: kb}
}

func handleUCardInline(ctx context.Context, b *botgotd.Bot, update *botgotd.Update, query string) {
	if update == nil || update.InlineQuery == nil {
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(query, "ucard "))
	if token != "" && inlineRedis != nil && inlineRedis.Client != nil {
		raw, err := inlineRedis.Client.Get(ctx, "inline:card:"+token).Bytes()
		if err == nil {
			var card ubotInlineCardPayload
			if json.Unmarshal(raw, &card) == nil && strings.TrimSpace(card.HTML) != "" {
				title := strings.TrimSpace(card.Title)
				if title == "" {
					title = "GotdBot Info"
				}
				result := &botgotd.InlineQueryResultArticle{
					ID:          "ucard_" + token,
					Title:       title,
					Description: "Kartu informasi GotdBot",
					InputMessageContent: &botgotd.InputTextMessageContent{
						MessageText: card.HTML,
						ParseMode:   "HTML",
					},
					ReplyMarkup: buildUCardMarkup(card.Buttons),
				}
				_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{
					InlineQueryID: update.InlineQuery.ID,
					Results:       []botgotd.InlineQueryResult{result},
					CacheTime:     0,
					IsPersonal:    true,
				})
				return
			}
		}
	}
	_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{
		InlineQueryID: update.InlineQuery.ID,
		Results:       []botgotd.InlineQueryResult{},
		CacheTime:     0,
		IsPersonal:    true,
	})
}

func truncateAlertRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func (h *Handler) UCardAlertCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	token := strings.TrimSpace(strings.TrimPrefix(cb.Data, "ucard_alert:"))
	alertMsg := "Bio pengguna kosong atau tidak tersedia."
	if token != "" && h != nil && h.broker != nil && h.broker.Client != nil {
		if val, err := h.broker.Client.Get(ctx, "inline:bio:"+token).Result(); err == nil && strings.TrimSpace(val) != "" {
			alertMsg = val
		}
	} else if token != "" && inlineRedis != nil && inlineRedis.Client != nil {
		if val, err := inlineRedis.Client.Get(ctx, "inline:bio:"+token).Result(); err == nil && strings.TrimSpace(val) != "" {
			alertMsg = val
		}
	}
	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            truncateAlertRunes(alertMsg, 200),
		ShowAlert:       true,
	})
}
