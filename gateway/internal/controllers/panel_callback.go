package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PanelCallback handles the inline delete button attached to a detail view.
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

	// 1. Control remote callbacks
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

			// Validasi otorisasi kontrol akun
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
	chatID = cb.Message.Message.Chat.ID
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

func (h *Handler) handleControlCallback(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, cb *botgotd.CallbackQuery) {
	data := cb.Data
	if data == "ctrl:list" || data == "ctrl:list_akun" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlAccountList(ctx, b, chatID, messageID, cb.From.ID, cb.From.Username)
		return
	}

	if data == "ctrl:close" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.setPanelState(chatID, "")
		if messageID > 0 {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{messageID}})
		}
		return
	}

	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		return
	}

	action := parts[1]
	targetID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || targetID == 0 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		return
	}

	// Validasi hak akses
	if !h.isAuthorizedAdmin(ctx, cb.From.ID, cb.From.Username, targetID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⛔ Anda tidak memiliki wewenang untuk mengontrol akun ini.",
			ShowAlert:       true,
		})
		return
	}

	switch action {
	case "select", "menu":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlAdminMenu(ctx, b, chatID, messageID, cb.From.ID, cb.From.FirstName, targetID)

	case "status":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlStatus(ctx, b, chatID, messageID, targetID)

	case "status_refresh":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "✅ Status diperbarui",
		})
		h.RenderControlStatus(ctx, b, chatID, messageID, targetID)

	case "gruplist":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlGrupList(ctx, b, chatID, messageID, targetID)

	case "cek_list":
		page := 0
		if len(parts) >= 4 {
			page, _ = strconv.Atoi(parts[3])
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlCekList(ctx, b, chatID, messageID, targetID, page)

	case "cek_grup":
		page := 0
		if len(parts) >= 4 {
			page, _ = strconv.Atoi(parts[3])
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlCekGrup(ctx, b, chatID, messageID, targetID, page)

	case "tambah_list":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlTambahList(ctx, b, chatID, messageID, targetID)

	case "tambah_grup":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlTambahGrupOptions(ctx, b, chatID, messageID, targetID)

	case "fetch_all", "fetch_all_groups":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⏳ Memulai import semua grup...",
		})
		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		payload := models.PToService{
			ChatID:  0,
			UserID:  targetID,
			OwnerID: targetID,
			Type:    "fetch_groups",
		}
		_ = h.broker.To(brokerCtx, "to:ubot:command", payload)
		h.RenderControlImportGroupsStarted(ctx, b, chatID, messageID, targetID)

	case "tambah_grup_manual":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlTambahGrupManual(ctx, b, chatID, messageID, targetID)

	case "cancel":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "↩️ Tindakan dibatalkan",
		})
		h.setPanelState(chatID, "")
		h.RenderControlGrupList(ctx, b, chatID, messageID, targetID)

	case "hapus_list_confirm":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlHapusListConfirm(ctx, b, chatID, messageID, targetID)

	case "hapus_list_do":
		_ = h.db.ClearLists(ctx, targetID)
		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_ = h.broker.To(brokerCtx, "to:ubot:command", models.PToService{ChatID: 0, UserID: targetID, OwnerID: targetID, Type: "clear_panel_lists"})
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "🗑️ Semua list promosi berhasil dihapus.",
		})
		h.RenderControlHapusListDone(ctx, b, chatID, messageID, targetID)

	case "hapus_grup_confirm":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlHapusGrupConfirm(ctx, b, chatID, messageID, targetID)

	case "hapus_grup_do":
		_ = h.db.ClearGroups(ctx, targetID)
		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_ = h.broker.To(brokerCtx, "to:ubot:command", models.PToService{ChatID: 0, UserID: targetID, OwnerID: targetID, Type: "clear_panel_groups"})
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "🗑️ Semua target grup berhasil dihapus.",
		})
		h.RenderControlHapusGrupDone(ctx, b, chatID, messageID, targetID)

	case "jeda":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.RenderControlJeda(ctx, b, chatID, messageID, targetID)

	case "set_jeda":
		if len(parts) >= 4 {
			sec, _ := strconv.Atoi(parts[3])
			if sec > 0 {
				cfg := h.db.GetUserConfig(ctx, targetID)
				cfg.Delay = sec
				_ = h.db.SaveUserConfig(ctx, cfg)
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            fmt.Sprintf("✅ Jeda diatur ke %d detik", sec),
				})
				h.RenderControlJeda(ctx, b, chatID, messageID, targetID)
				return
			}
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})

	default:
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	}
}
