package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (h *Handler) PanelCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	var chatID int64
	var messageID int
	if cb.Message != nil && cb.Message.Message != nil {
		chatID = cb.Message.Message.Chat.ID
		messageID = cb.Message.Message.ID
	}
	if chatID == 0 {
		chatID = cb.From.ID
	}

	if strings.HasPrefix(cb.Data, "ctrl:") {
		h.handleControlCallback(ctx, b, chatID, messageID, cb)
		return
	}

	if cb.Data == "panel:control" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlAccountList(ctx, b, chatID, messageID, cb.From.ID, cb.From.Username)
		return
	}
	if strings.HasPrefix(cb.Data, "panel:control_select:") {
		parts := strings.Split(cb.Data, ":")
		if len(parts) >= 3 {
			targetID, _ := strconv.ParseInt(parts[2], 10, 64)
			if targetID == 0 {
				h.SetActiveRemoteTarget(chatID, 0)
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            "✅ Remote akun dinonaktifkan.",
				})
				h.RenderControlAccountList(ctx, b, chatID, messageID, cb.From.ID, cb.From.Username)
				return
			}

			if !h.isAuthorizedAdmin(ctx, cb.From.ID, cb.From.Username, targetID) {
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            "⛔ Anda tidak memiliki wewenang untuk mengontrol akun ini.",
					ShowAlert:       true,
				})
				return
			}

			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
				CallbackQueryID: cb.ID,
				Text:            "✅ Membuka Menu Admin",
			})
			h.RenderControlAdminMenu(ctx, b, chatID, messageID, cb.From.ID, cb.From.FirstName, targetID)
			return
		}
	}
	if cb.Data == "panel:status" || cb.Data == "panel:status:refresh" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ Status diperbarui"})
		if chatID == 0 {
			chatID = cb.From.ID
		}
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.renderStatusDashboard(ctx, b, chatID, effectiveID, messageID)
		return
	}
	if cb.Data == "panel:status:toggle" {
		if chatID == 0 {
			chatID = cb.From.ID
		}
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		ok, expAt, newStatus := h.executeToggleStatus(ctx, b, chatID, effectiveID)
		if !ok {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
				CallbackQueryID: cb.ID,
				Text:            fmt.Sprintf("❌ Masa aktif token habis (%s).", expAt),
				ShowAlert:       true,
			})
			return
		}
		toast := "🛑 Siaran dihentikan"
		if newStatus == "Berjalan" {
			toast = "🚀 Siaran dimulai"
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: toast})
		h.renderStatusDashboard(ctx, b, chatID, effectiveID, messageID)
		return
	}
	if strings.HasPrefix(cb.Data, "panel:notif:set:") {
		h.handleNotifikasiCallback(ctx, b, chatID, messageID, cb)
		return
	}
	if cb.Data == "panel:token:menu" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.ShowSwitchTokenMenu(ctx, b, chatID, cb.From.ID, messageID)
		return
	}
	if cb.Data == "panel:settings:home" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.renderSettingsView(ctx, b, chatID, effectiveID, messageID)
		return
	}
	if cb.Data == "panel:settings:backup" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ Kode backup dibuat"})
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.createAndRenderDataBackup(ctx, b, chatID, effectiveID, messageID)
		return
	}
	if cb.Data == "panel:settings:transfer" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.HandlePindahAkses(ctx, b, chatID, effectiveID, messageID)
		return
	}
	if cb.Data == "panel:transfer:claim" {
		h.handleTransferClaimCallback(ctx, b, chatID, cb)
		return
	}
	if strings.HasPrefix(cb.Data, "panel:tlist:") {
		h.handleTopicListCallback(ctx, b, chatID, messageID, cb)
		return
	}
	if strings.HasPrefix(cb.Data, "panel:token:switch:") {
		h.handleTokenSwitchCallback(ctx, b, chatID, int64(messageID), cb)
		return
	}
	if cb.Data == "panel:watermark:delete" {
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		cfg := h.db.GetUserConfig(ctx, effectiveID)
		cfg.Watermark = ""
		cfg.WatermarkCacheID = 0
		if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
				CallbackQueryID: cb.ID,
				Text:            fmt.Sprintf("❌ Gagal menghapus watermark: %v", err),
				ShowAlert:       true,
			})
			return
		}
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveID, OwnerID: effectiveID, Type: "clear_watermark"})
		}
		h.setPanelState(chatID, "")
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "✅ Watermark berhasil dihapus",
		})
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		if messageID > 0 {
			msgIDs = append(msgIDs, messageID)
		}
		h.clearPanelPrompt(ctx, b, chatID)
		h.deleteBotMessages(ctx, b, chatID, msgIDs)
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>✅ Watermark Berhasil Dihapus</h1>
<blockquote expandable>Custom watermark telah dihapus dari konfigurasi siaran Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.StatusMenuMarkup,
		})
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) >= 3 && parts[0] == "panel" && parts[1] == "list" && parts[2] == "back" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		if messageID > 0 {
			msgIDs = append(msgIDs, messageID)
		}
		h.deleteBotMessages(ctx, b, chatID, msgIDs)
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.DisplayList(ctx, b, chatID, effectiveID)
		return
	}
	if len(parts) >= 3 && parts[0] == "panel" && parts[1] == "group" && parts[2] == "back" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		if messageID > 0 {
			msgIDs = append(msgIDs, messageID)
		}
		h.deleteBotMessages(ctx, b, chatID, msgIDs)
		effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
		h.DisplayGroup(ctx, b, chatID, effectiveID)
		return
	}
	if len(parts) != 4 || parts[0] != "panel" || parts[3] == "" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil || index < 0 || cb.Message == nil || cb.Message.Message == nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
		return
	}
	chatID = cb.Message.Message.Chat.ID
	userID := h.GetEffectiveUserID(chatID, cb.From.ID)
	switch parts[1] {
	case "list":
		lists := h.db.GetLists(ctx, userID)
		if index >= len(lists) {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Mohon pilih kembali list yang ingin dihapus.", ShowAlert: true})
			return
		}
		cacheID := lists[index]
		if err := h.db.DeleteListByIndex(ctx, userID, index); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: fmt.Sprintf("❌ Gagal menghapus list: %v", err), ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ List berhasil dihapus"})
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "delete_panel_list", TaskID: strconv.Itoa(index), Text: strconv.Itoa(cacheID)})
		}
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		if messageID > 0 {
			msgIDs = append(msgIDs, messageID)
		}
		h.deleteBotMessages(ctx, b, chatID, msgIDs)
		h.DisplayList(ctx, b, chatID, userID)
	case "group":
		groups := h.db.GetGroups(ctx, userID)
		if index < 0 || index >= len(groups) {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Grup sudah tidak tersedia.", ShowAlert: true})
			return
		}
		removedGroup := groups[index]
		if err := h.db.DeleteGroupByIndex(ctx, userID, index); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: fmt.Sprintf("❌ Gagal menghapus grup: %v", err), ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ Grup berhasil dihapus"})
		groupKey := removedGroup.Target
		if removedGroup.ChatID != 0 {
			groupKey = strconv.FormatInt(removedGroup.ChatID, 10)
		}
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "delete_panel_group", Text: groupKey})
		}
		msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
		if messageID > 0 {
			msgIDs = append(msgIDs, messageID)
		}
		h.deleteBotMessages(ctx, b, chatID, msgIDs)
		h.DisplayGroup(ctx, b, chatID, userID)
	}
}
