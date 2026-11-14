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

// PromptTambahList prompts user to enter broadcast list text
func (h *Handler) PromptTambahList(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_add_list")

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtPromptTambahList,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

// CekList displays all saved promotional lists (summary view)
func (h *Handler) CekList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	if len(lists) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Lists) > 0 {
			lists = refreshed.Lists
		}
	}

	if len(lists) == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtCekListKosong,
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}

	items := make([]string, 0, 20)
	truncated := 0
	for i, msgID := range lists {
		if i >= 20 {
			truncated = len(lists) - 20
			break
		}
		items = append(items, fmt.Sprintf("%d. <b>List #%d</b> — <code>Cache ID: %d</code>", i+1, i+1, msgID))
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.RichListSummary(len(lists), items, truncated),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

func browseKeyboard(count int, allText string) *botgotd.ReplyKeyboardMarkup {
	rows := [][]botgotd.KeyboardButton{{{Text: allText}}}
	for start := 0; start < count; start += 5 {
		row := make([]botgotd.KeyboardButton, 0, 5)
		for i := start; i < start+5 && i < count; i++ {
			row = append(row, botgotd.KeyboardButton{Text: strconv.Itoa(i + 1)})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []botgotd.KeyboardButton{{Text: "Kembali"}})
	return &botgotd.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true}
}

func browseListMarkup(count int) *botgotd.ReplyKeyboardMarkup {
	return browseKeyboard(count, "🗑️ Hapus Semua List 🗑️")
}

func (h *Handler) DisplayList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	if len(lists) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Lists) > 0 {
			lists = refreshed.Lists
		}
	}
	if len(lists) == 0 {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>📋 Daftar List Promosi</h1><blockquote expandable>Belum ada materi promosi yang tersimpan.</blockquote><hr/><p>GotdBot • Promotional Content Manager</p>",
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}
	h.setPanelState(chatID, "browse_list")
	payload := models.PToService{ChatID: chatID, UserID: userID, Type: "get_list_previews", TaskID: strconv.Itoa(len(lists))}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ <b>Gagal.</b> Preview list tidak dapat diminta dari userbot.",
			ParseMode:   "HTML",
			ReplyMarkup: browseListMarkup(len(lists)),
		})
	}
}

func (h *Handler) showListDetail(ctx context.Context, b *botgotd.Bot, chatID, userID int64, index int) {
	lists := h.db.GetLists(ctx, userID)
	if index < 0 || index >= len(lists) {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Lists) > 0 {
			lists = refreshed.Lists
		}
	}
	if index < 0 || index >= len(lists) {
		h.DisplayList(ctx, b, chatID, userID)
		return
	}
	h.setPanelState(chatID, "list_detail")
	payload := models.PToService{ChatID: chatID, UserID: userID, Type: "get_list_detail", TaskID: strconv.Itoa(index), Text: strconv.Itoa(lists[index])}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ <b>Gagal.</b> Detail list tidak dapat diminta dari userbot.",
			ParseMode:   "HTML",
			ReplyMarkup: browseListMarkup(len(lists)),
		})
	}
}

// PromptHapusList prompts for list deletion
func (h *Handler) PromptHapusList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_del_list")

	lists := h.db.GetLists(ctx, userID)
	var sb strings.Builder
	sb.WriteString("<h1>🗑️ Hapus List Promosi</h1>\n")
	if len(lists) == 0 {
		sb.WriteString("<blockquote expandable>Tidak ada list yang tersimpan saat ini.</blockquote>")
	} else {
		fmt.Fprintf(&sb, "<blockquote expandable>Total tersimpan: <b>%d materi</b>\n\nKetik nomor urut list yang ingin dihapus, atau tekan tombol <b>🗑️ Hapus Semua List 🗑️</b>.</blockquote>", len(lists))
	}
	sb.WriteString("\n<hr/>\n<p>Pilih dari keyboard atau klik <b>⬅️ Batal ⬅️</b> di bawah.</p>")

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.HapusListMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

func isRestartBrowseButton(text string) bool {
	switch text {
	case assets.BtnCekList, assets.BtnCekGrup, assets.BtnTambahList, assets.BtnTambahGrup:
		return true
	default:
		return false
	}
}
