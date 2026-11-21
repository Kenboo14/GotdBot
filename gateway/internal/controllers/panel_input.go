package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) HandlePanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string, message *botgotd.Message) bool {
	state := h.getPanelState(chatID)
	if state == "" {
		return false
	}
	if h.handleOwnerPGPanelInput(ctx, b, chatID, userID, state, text, message) {
		return true
	}

	if strings.HasPrefix(state, "ctrl_add_list:") {
		h.setPanelState(chatID, "")
		parts := strings.Split(state, ":")
		targetID := userID
		parentMsgID := 0
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				targetID = id
			}
		}
		if len(parts) >= 3 {
			parentMsgID, _ = strconv.Atoi(parts[2])
		}

		if text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel" {
			if parentMsgID > 0 {
				h.RenderControlGrupList(ctx, b, chatID, parentMsgID, targetID)
			}
			if message != nil {
				_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
			}
			return true
		}

		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		payload := models.PToService{
			ChatID:   0,
			UserID:   targetID,
			OwnerID:  targetID,
			Type:     "cache_list",
			Text:     text,
			Entities: panelEntities(message),
		}
		_ = h.broker.To(brokerCtx, "to:ubot:command", payload)

		if message != nil {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
		}

		targetName := h.GetAccountName(targetID)
		if targetName == "" {
			targetName = fmt.Sprintf("Akun [%d]", targetID)
		}

		if parentMsgID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        assets.TxtControlListSentSuccess(targetName, targetID),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CtrlDoneIKB(targetID),
			})
		}
		return true
	}

	if strings.HasPrefix(state, "ctrl_add_grup:") {
		h.setPanelState(chatID, "")
		parts := strings.Split(state, ":")
		targetID := userID
		parentMsgID := 0
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				targetID = id
			}
		}
		if len(parts) >= 3 {
			parentMsgID, _ = strconv.Atoi(parts[2])
		}

		h.handleManualGroupPanelInput(ctx, b, chatID, targetID, text, message, true, parentMsgID)
		return true
	}

	if text == assets.BtnBatal || text == "🔙 Batal 🔙" || strings.EqualFold(text, "batal") || text == "/cancel" {
		h.clearPanelPrompt(ctx, b, chatID)
		h.clearProgress(ctx, b, chatID)
		h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
		h.setPanelState(chatID, "")
		hasActive := false
		if h.db != nil {
			hasActive = h.db.HasActiveSession(ctx, userID)
		}
		if hasActive {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>↩️ Proses Dibatalkan</h1>
<blockquote expandable>Tindakan dibatalkan dan Anda dikembalikan ke menu panel.</blockquote>
<hr/>
<p>GotdBot • Control Panel</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: mainMenuMarkup,
			})
		} else {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>↩️ Proses Dibatalkan</h1>
<blockquote expandable>Tindakan telah dibatalkan.</blockquote>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.RemoveKeyboard,
			})
		}
		return true
	}

	if h.handleTopicPanelInput(ctx, b, chatID, userID, state, text, message) {
		return true
	}

	if h.handleJedaTimerPanelInput(ctx, b, chatID, userID, state, text) {
		return true
	}

	if h.handleSettingsPanelInput(ctx, b, chatID, userID, state, text) {
		return true
	}

	if h.handleStatusPanelInput(ctx, b, chatID, userID, state, text, message) {
		return true
	}

	switch state {
	case "waiting_add_list":
		if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && policy.MaxLists > 0 {
			if _, lCount := h.db.CountGroupsAndLists(ctx, userID); lCount >= policy.MaxLists {
				h.clearPanelPrompt(ctx, b, chatID)
				h.setPanelState(chatID, "")
				h.denyTierFeature(ctx, b, chatID, policy.Tier, fmt.Sprintf("Tambah List (> %d list)", policy.MaxLists), "Pro", grupListMenuMarkup)
				return true
			}
		}
		h.clearPanelPrompt(ctx, b, chatID)
		if message == nil || message.MediaGroupID == "" {
			h.setPanelState(chatID, "")
		}
		payload := models.PToService{
			ChatID:   chatID,
			UserID:   userID,
			Type:     "cache_list",
			Text:     text,
			Entities: panelEntities(message),
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("cache_list broker error: %v", err)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "<h1>❌ Gagal Menyimpan List</h1><blockquote expandable>Broker userbot tidak merespons. Silakan coba beberapa saat lagi.</blockquote><hr/><p>GotdBot • Promotional Content Manager</p>",
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>⏳ Menyimpan List</h1><blockquote expandable>Mengamankan materi promosi ke saluran cache userbot...</blockquote><hr/><p>GotdBot • Promotional Content Manager</p>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "waiting_add_group_manual":
		h.clearPanelPrompt(ctx, b, chatID)
		h.setPanelState(chatID, "")
		h.handleManualGroupPanelInput(ctx, b, chatID, userID, text, message, false, 0)
		return true

	case "browse_list", "list_detail":
		if text == assets.BtnHapusSemuaList {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			_ = h.db.ClearLists(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
			h.DisplayList(ctx, b, chatID, userID)
			return true
		}
		if text == assets.BtnKembali || text == "Kembali" {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			h.ShowGrupList(ctx, b, chatID, userID)
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			if state == "list_detail" {
				h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			}
			h.showListDetail(ctx, b, chatID, userID, idx-1)
			return true
		}
		return true

	case "browse_group":
		if text == assets.BtnHapusSemuaGrup {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			_ = h.db.ClearGroups(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
			h.DisplayGroup(ctx, b, chatID, userID)
			return true
		}
		if text == assets.BtnKembali || text == "Kembali" {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			h.ShowGrupList(ctx, b, chatID, userID)
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			h.showGroupDetail(ctx, b, chatID, userID, idx-1)
			return true
		}
		return true

	case "waiting_del_list":
		h.clearPanelPrompt(ctx, b, chatID)
		h.setPanelState(chatID, "")
		if text == assets.BtnHapusSemuaList {
			_ = h.db.ClearLists(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Semua List Dihapus</h1>
<blockquote expandable>Seluruh list promosi berhasil dibersihkan dari database dan cache.</blockquote>
<hr/>
<p>GotdBot • Promotional Content Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			lists := h.db.GetLists(ctx, userID)
			if idx <= len(lists) {
				cacheID := lists[idx-1]
				_ = h.db.DeleteListByIndex(ctx, userID, idx-1)
				_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "delete_panel_list", Text: strconv.Itoa(cacheID)})
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: chatID,
					Text: fmt.Sprintf(`<h1>✅ List Dihapus</h1>
<blockquote expandable>List #%d berhasil dihapus dari database dan cache.</blockquote>
<hr/>
<p>GotdBot • Promotional Content Manager</p>`, idx),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>⚠️ Gagal Menghapus List</h1>
<blockquote expandable>Nomor urut list tidak valid. Silakan periksa kembali index list.</blockquote>
<hr/>
<p>GotdBot • Promotional Content Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "waiting_del_group":
		h.clearPanelPrompt(ctx, b, chatID)
		h.setPanelState(chatID, "")
		if text == "🗑️ Hapus Semua Grup 🗑️" {
			_ = h.db.ClearGroups(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Semua Grup Dihapus</h1>
<blockquote expandable>Seluruh grup target siaran berhasil dibersihkan dari database.</blockquote>
<hr/>
<p>GotdBot • Group Target Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			groups := h.db.GetGroups(ctx, userID)
			if idx <= len(groups) {
				removed := groups[idx-1]
				_ = h.db.DeleteGroupByIndex(ctx, userID, idx-1)
				groupKey := removed.Target
				if removed.ChatID != 0 {
					groupKey = strconv.FormatInt(removed.ChatID, 10)
				}
				_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "delete_panel_group", Text: groupKey})
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: chatID,
					Text: fmt.Sprintf(`<h1>✅ Grup Dihapus</h1>
<blockquote expandable>Grup #%d (<code>%s</code>) berhasil dihapus.</blockquote>
<hr/>
<p>GotdBot • Group Target Manager</p>`, idx, html.EscapeString(removed.Target)),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>⚠️ Gagal Menghapus Grup</h1>
<blockquote expandable>Nomor urut grup tidak valid. Silakan periksa kembali index grup.</blockquote>
<hr/>
<p>GotdBot • Group Target Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true
	}

	return false
}
