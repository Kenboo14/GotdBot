package controllers

import (
	"assterisk/gateway/applications/models"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

// PanelCallback handles the inline delete button attached to a detail view.
func (h *Handler) PanelCallback(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 4 || parts[0] != "panel" || parts[3] == "" {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil || index < 0 || cb.Message.Message == nil {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	chatID := cb.Message.Message.Chat.ID
	userID := cb.From.ID
	switch parts[1] {
	case "list":
		lists := h.db.GetLists(ctx, userID)
		if index >= len(lists) {
			_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		cacheID := lists[index]
		if err := h.db.DeleteListByIndex(ctx, userID, index); err != nil {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("❌ Gagal menghapus list: %v", err)})
			return
		}
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "delete_panel_list", TaskID: strconv.Itoa(index), Text: strconv.Itoa(cacheID)})
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: cb.Message.Message.ID})
		h.DisplayList(ctx, b, chatID, userID)
	case "group":
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		if err := h.db.DeleteGroupByIndex(ctx, userID, index); err != nil {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("❌ Gagal menghapus grup: %v", err)})
			return
		}
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: cb.Message.Message.ID})
		h.DisplayGroup(ctx, b, chatID, userID)
	}
}
