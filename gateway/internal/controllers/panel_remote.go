package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"html"
)

// ShowControlMenu delegates to RenderControlAccountList
func (h *Handler) ShowControlMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, username string) {
	h.RenderControlAccountList(ctx, b, chatID, 0, userID, username)
}

func (h *Handler) isAuthorizedAdmin(ctx context.Context, callerID int64, callerUsername string, targetID int64) bool {
	if targetID == callerID {
		return true
	}
	if h.db != nil {
		accounts := h.db.GetAccessibleAccounts(ctx, callerID, callerUsername)
		for _, acc := range accounts {
			if acc.OwnerID == targetID {
				return true
			}
		}
	}
	return false
}

// RenderControlAccountList renders the remote account picker (via send or edit)
func (h *Handler) RenderControlAccountList(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, userID int64, username string) {
	h.setPanelState(chatID, "")

	var accounts []databases.AccountControlInfo
	if h.db != nil {
		accounts = h.db.GetAccessibleAccounts(ctx, userID, username)
	}

	for i := range accounts {
		if cached := h.GetAccountName(accounts[i].OwnerID); cached != "" {
			if accounts[i].IsOwn {
				accounts[i].Name = "[Saya] " + cached
			} else {
				accounts[i].Name = cached
			}
		}
	}

	if len(accounts) == 0 {
		text := assets.TxtControlAccountListEmpty(userID)
		if messageID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: messageID,
				Text:      text,
				ParseMode: "HTML",
			})
		} else {
			effectiveID := h.GetEffectiveUserID(chatID, userID)
			hasActive := false
			if h.db != nil {
				hasActive = h.db.HasActiveSession(ctx, effectiveID)
			}
			markup := any(assets.CreateChoiceMarkup)
			if hasActive {
				markup = assets.MainMenuMarkup
			}
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        text,
				ParseMode:   "HTML",
				ReplyMarkup: markup,
			})
		}
		return
	}

	items := make([]assets.AccountItem, 0, len(accounts))
	for _, acc := range accounts {
		items = append(items, assets.AccountItem{
			ID:   acc.OwnerID,
			Name: acc.Name,
		})
	}
	inlineMarkup := assets.CtrlAccountListIKB(items)
	text := assets.TxtControlAccountList(len(accounts))

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlAdminMenu renders the main admin remote dashboard
func (h *Handler) RenderControlAdminMenu(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, adminID int64, adminName string, targetID int64) {
	h.setPanelState(chatID, "")

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	if adminName == "" {
		adminName = "Admin"
	}

	text := assets.TxtControlAdminMenu(targetName, targetID, adminName)
	inlineMarkup := assets.CtrlAdminMenuIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlStatus renders the status dashboard of the remote account
func (h *Handler) RenderControlStatus(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.setPanelState(chatID, "")

	cfg := h.db.GetUserConfig(ctx, targetID)
	gCount, lCount := h.db.CountGroupsAndLists(ctx, targetID)

	skipFw := "Mati"
	if cfg.SkipFloodWait {
		skipFw = "Aktif"
	}

	safeMode := "Nonaktif"
	if cfg.Safemode {
		safeMode = "Aktif"
	}

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	rows := []assets.StatusRow{
		{Label: "🏷️ UserID", Value: fmt.Sprintf("%d", targetID)},
		{Label: "🚀 Status", Value: html.EscapeString(cfg.Status)},
		{Label: "👑 Mode", Value: html.EscapeString(cfg.Mode)},
		{Label: "🕒 Jeda", Value: fmt.Sprintf("%d detik", cfg.Delay)},
		{Label: "🌐 Target Grup", Value: fmt.Sprintf("%d grup", gCount)},
		{Label: "📚 Materi List", Value: fmt.Sprintf("%d list", lCount)},
		{Label: "⚠️ Safemode", Value: safeMode},
		{Label: "⏭️ Skip Flood", Value: skipFw},
		{Label: "⏱️ Timer", Value: html.EscapeString(cfg.Timer)},
		{Label: "🔥 Masa Aktif", Value: html.EscapeString(cfg.MasaAktif)},
	}
	tbl := assets.RichStatusTable(rows)
	text := assets.RichControlStatusDashboard(html.EscapeString(targetName), targetID, tbl, html.EscapeString(cfg.TindakanTerkini), html.EscapeString(cfg.TindakanTerakhir))
	inlineMarkup := assets.CtrlStatusIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlGrupList renders the remote group and list menu
func (h *Handler) RenderControlGrupList(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.setPanelState(chatID, "")

	gCount, lCount := h.db.CountGroupsAndLists(ctx, targetID)
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlGrupList(targetName, targetID, lCount, gCount)
	inlineMarkup := assets.CtrlGrupListIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlCekList renders the promotional lists view of target account with pagination
func (h *Handler) RenderControlCekList(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64, page int) {
	lists := h.db.GetLists(ctx, targetID)
	if len(lists) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, targetID); refreshed != nil && len(refreshed.Lists) > 0 {
			lists = refreshed.Lists
		}
	}

	const pageSize = 10
	total := len(lists)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	items := make([]string, 0, pageSize)
	if total > 0 {
		start := page * pageSize
		end := start + pageSize
		if end > total {
			end = total
		}
		for i := start; i < end; i++ {
			items = append(items, fmt.Sprintf("%d. <b>List #%d</b> — <code>Cache ID: %d</code>", i+1, i+1, lists[i]))
		}
	}

	text := assets.TxtControlCekList(targetName, targetID, page, totalPages, total, items)
	inlineMarkup := assets.CtrlCekListIKB(targetID, page, totalPages)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlCekGrup renders the target groups view of target account with pagination
func (h *Handler) RenderControlCekGrup(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64, page int) {
	groups := h.db.GetGroups(ctx, targetID)
	if len(groups) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, targetID); refreshed != nil && len(refreshed.Groups) > 0 {
			groups = refreshed.Groups
		}
	}

	const pageSize = 15
	total := len(groups)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	items := make([]string, 0, pageSize)
	if total > 0 {
		start := page * pageSize
		end := start + pageSize
		if end > total {
			end = total
		}
		for i := start; i < end; i++ {
			g := groups[i]
			title := g.Title
			if title == "" {
				title = g.Target
			}
			items = append(items, fmt.Sprintf("%d. %s — <code>%s</code>", i+1, html.EscapeString(title), html.EscapeString(g.Target)))
		}
	}

	text := assets.TxtControlCekGrup(targetName, targetID, page, totalPages, total, items)
	inlineMarkup := assets.CtrlCekGrupIKB(targetID, page, totalPages)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlTambahList prompts user to send promotion list for target account
func (h *Handler) RenderControlTambahList(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, fmt.Sprintf("ctrl_add_list:%d:%d", targetID, messageID))

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlTambahList(targetName, targetID)
	inlineMarkup := assets.CtrlTambahListIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlTambahGrupOptions renders options to add groups to target account
func (h *Handler) RenderControlTambahGrupOptions(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.setPanelState(chatID, "")

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlTambahGrupOptions(targetName, targetID)
	inlineMarkup := assets.CtrlTambahGrupOptionsIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlTambahGrupManual prompts user to input group targets manually
func (h *Handler) RenderControlTambahGrupManual(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, fmt.Sprintf("ctrl_add_grup:%d:%d", targetID, messageID))

	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlTambahGrupManual(targetName, targetID)
	inlineMarkup := assets.CtrlTambahGrupManualIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlImportGroupsStarted informs user that background group import is running
func (h *Handler) RenderControlImportGroupsStarted(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlImportGroupsStarted(targetName, targetID)
	inlineMarkup := assets.CtrlDoneIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlHapusListConfirm confirms clearing all lists
func (h *Handler) RenderControlHapusListConfirm(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlHapusListConfirm(targetName, targetID)
	inlineMarkup := assets.CtrlHapusListConfirmIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlHapusListDone displays success message after clearing lists
func (h *Handler) RenderControlHapusListDone(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlHapusListDone(targetName, targetID)
	inlineMarkup := assets.CtrlDoneIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlHapusGrupConfirm confirms clearing all groups
func (h *Handler) RenderControlHapusGrupConfirm(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlHapusGrupConfirm(targetName, targetID)
	inlineMarkup := assets.CtrlHapusGrupConfirmIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlHapusGrupDone displays success message after clearing groups
func (h *Handler) RenderControlHapusGrupDone(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlHapusGrupDone(targetName, targetID)
	inlineMarkup := assets.CtrlDoneIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlJeda renders the broadcast delay options for target account
func (h *Handler) RenderControlJeda(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.setPanelState(chatID, "")

	cfg := h.db.GetUserConfig(ctx, targetID)
	targetName := h.GetAccountName(targetID)
	if targetName == "" {
		targetName = fmt.Sprintf("Akun [%d]", targetID)
	}

	text := assets.TxtControlJeda(targetName, targetID, cfg.Delay)
	inlineMarkup := assets.CtrlJedaIKB(targetID)

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}
