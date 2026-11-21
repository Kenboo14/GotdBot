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
			h.deleteBotMessages(ctx, b, chatID, []int{messageID})
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
			rawVal := parts[3]
			cfg := h.db.GetUserConfig(ctx, targetID)
			gCount, _ := h.db.CountGroupsAndLists(ctx, targetID)
			updated := false
			switch rawVal {
			case "aman":
				cfg.DelayMode = "aman"
				cfg.Delay = 0
				cfg.DelayAllGroups = 0
				updated = true
			case "otomatis":
				cfg.DelayMode = "otomatis"
				cfg.Delay = 0
				cfg.DelayAllGroups = 0
				updated = true
			case "0":
				cfg.DelayMode = "manual"
				cfg.Delay = 0
				cfg.DelayAllGroups = 0
				updated = true
			default:
				if sec, err := strconv.Atoi(rawVal); err == nil && sec > 0 {
					cfg.DelayMode = "manual"
					cfg.Delay = sec
					updated = true
				}
			}
			if updated {
				_ = h.db.SaveUserConfig(ctx, cfg)
				_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
					CallbackQueryID: cb.ID,
					Text:            fmt.Sprintf("✅ Jeda: %s", cfg.FormatJedaSummary(gCount)),
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
