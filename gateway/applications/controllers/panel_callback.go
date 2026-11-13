package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/models"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PanelCallback handles the inline delete button attached to a detail view.
func (h *Handler) PanelCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	if cb.Data == "panel:control" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		var chatID int64
		if cb.Message != nil && cb.Message.Message != nil {
			chatID = cb.Message.Message.Chat.ID
		}
		if chatID == 0 {
			chatID = cb.From.ID
		}
		h.ShowControlMenu(ctx, b, chatID, cb.From.ID, cb.From.Username)
		return
	}
	if strings.HasPrefix(cb.Data, "panel:control_select:") {
		parts := strings.Split(cb.Data, ":")
		if len(parts) >= 3 {
			var chatID int64
			if cb.Message != nil && cb.Message.Message != nil {
				chatID = cb.Message.Message.Chat.ID
			}
			if chatID == 0 {
				chatID = cb.From.ID
			}
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			if targetID == 0 {
				h.SetActiveRemoteTarget(chatID, 0)
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            "✅ Remote akun dinonaktifkan.",
				})
				hasActive := false
				if h.db != nil {
					hasActive = h.db.HasActiveSession(ctx, cb.From.ID)
				}
				if hasActive {
					h.ShowMainMenu(ctx, b, chatID, cb.From.ID, cb.From.FirstName)
				} else {
					b.SendMessage(ctx, &botgotd.SendMessageParams{
						ChatID:      chatID,
						Text:        "✅ Remote akun dilepas. Anda kembali ke menu awal.",
						ReplyMarkup: cachedCreateChoiceMarkup,
					})
				}
				return
			}

			// Validasi otorisasi kontrol akun
			authorized := false
			if targetID == cb.From.ID {
				authorized = true
			} else if h.db != nil {
				accounts := h.db.GetAccessibleAccounts(ctx, cb.From.ID, cb.From.Username)
				for _, acc := range accounts {
					if acc.OwnerID == targetID {
						authorized = true
						break
					}
				}
			}

			if !authorized {
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            "⛔ Anda tidak memiliki wewenang untuk mengontrol akun ini.",
					ShowAlert:       true,
				})
				return
			}

			h.SetActiveRemoteTarget(chatID, targetID)
			targetName := h.GetAccountName(targetID)
			if targetName == "" {
				targetName = fmt.Sprintf("Akun [%d]", targetID)
			}

			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
				CallbackQueryID: cb.ID,
				Text:            fmt.Sprintf("✅ Mengontrol: %s", targetName),
			})

			h.ShowMainMenu(ctx, b, chatID, targetID, targetName)
			return
		}
	}
	if cb.Data == "panel:status" || cb.Data == "panel:status:refresh" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Status diperbarui"})
		var chatID int64
		if cb.Message != nil && cb.Message.Message != nil {
			chatID = cb.Message.Message.Chat.ID
		}
		if chatID == 0 {
			chatID = cb.From.ID
		}
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.ShowStatus(ctx, b, chatID, effectiveID)
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) >= 3 && parts[0] == "panel" && parts[1] == "list" && parts[2] == "back" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		var chatID int64
		if cb.Message != nil && cb.Message.Message != nil {
			chatID = cb.Message.Message.Chat.ID
		}
		if chatID == 0 {
			chatID = cb.From.ID
		}
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		found := false
		if cb.Message != nil && cb.Message.Message != nil {
			for _, mid := range msgIDs {
				if mid == cb.Message.Message.ID {
					found = true
					break
				}
			}
			if !found {
				msgIDs = append(msgIDs, cb.Message.Message.ID)
			}
		}
		if len(msgIDs) > 0 {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
		}
		h.DisplayList(ctx, b, chatID, cb.From.ID)
		return
	}
	if len(parts) >= 3 && parts[0] == "panel" && parts[1] == "group" && parts[2] == "back" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		if cb.Message != nil && cb.Message.Message != nil {
			_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: cb.Message.Message.Chat.ID, MessageID: cb.Message.Message.ID})
			h.DisplayGroup(ctx, b, cb.Message.Message.Chat.ID, cb.From.ID)
		}
		return
	}
	if len(parts) != 4 || parts[0] != "panel" || parts[3] == "" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil || index < 0 || cb.Message.Message == nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	chatID := cb.Message.Message.Chat.ID
	userID := cb.From.ID
	switch parts[1] {
	case "list":
		lists := h.db.GetLists(ctx, userID)
		if index >= len(lists) {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		cacheID := lists[index]
		if err := h.db.DeleteListByIndex(ctx, userID, index); err != nil {
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("❌ Gagal menghapus list: %v", err)})
			return
		}
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "delete_panel_list", TaskID: strconv.Itoa(index), Text: strconv.Itoa(cacheID)})
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		found := false
		if cb.Message != nil && cb.Message.Message != nil {
			for _, mid := range msgIDs {
				if mid == cb.Message.Message.ID {
					found = true
					break
				}
			}
			if !found {
				msgIDs = append(msgIDs, cb.Message.Message.ID)
			}
		}
		if len(msgIDs) > 0 {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
		}
		h.DisplayList(ctx, b, chatID, userID)
	case "group":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Grup berhasil dihapus"})
		groups := h.db.GetGroups(ctx, userID)
		if index < 0 || index >= len(groups) {
			return
		}
		removedGroup := groups[index]
		if err := h.db.DeleteGroupByIndex(ctx, userID, index); err != nil {
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("❌ Gagal menghapus grup: %v", err), ParseMode: "HTML"})
			return
		}
		groupKey := removedGroup.Target
		if removedGroup.ChatID != 0 {
			groupKey = strconv.FormatInt(removedGroup.ChatID, 10)
		}
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "delete_panel_group", Text: groupKey})
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		found := false
		if cb.Message != nil && cb.Message.Message != nil {
			for _, mid := range msgIDs {
				if mid == cb.Message.Message.ID {
					found = true
					break
				}
			}
			if !found {
				msgIDs = append(msgIDs, cb.Message.Message.ID)
			}
		}
		if len(msgIDs) > 0 {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
		}
		h.DisplayGroup(ctx, b, chatID, userID)
	}
}
