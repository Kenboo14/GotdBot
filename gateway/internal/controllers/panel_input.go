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

// HandlePanelInput processes text inputs when user is in a state machine wizard flow
func (h *Handler) HandlePanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string, message *botgotd.Message) bool {
	state := h.getPanelState(chatID)
	if state == "" {
		return false
	}

	// 1. Isolated Remote Control Add List
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
			ChatID:   0, // 0 agar ubot tidak membalas via SendMessage + ReplyKeyboard
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

	// 2. Isolated Remote Control Add Group Manual
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

		if text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel" {
			if parentMsgID > 0 {
				h.RenderControlGrupList(ctx, b, chatID, parentMsgID, targetID)
			}
			if message != nil {
				_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
			}
			return true
		}

		lines := strings.Split(text, "\n")
		addedCount := 0
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			fields := strings.Fields(l)
			for _, p := range fields {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				var chatIDVal int64
				if id, parseErr := strconv.ParseInt(p, 10, 64); parseErr == nil {
					chatIDVal = id
				}
				if err := h.db.AddGroup(ctx, targetID, p, p, chatIDVal); err == nil {
					addedCount++
				}
			}
		}

		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_ = h.broker.To(brokerCtx, "to:ubot:command", models.PToService{
			ChatID:  0,
			UserID:  targetID,
			OwnerID: targetID,
			Type:    "sync_panel_config",
		})

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
				Text:        assets.TxtControlGrupSentSuccess(targetName, targetID, addedCount),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CtrlDoneIKB(targetID),
			})
		}
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
				Text:        "❌ <b>Gagal menyimpan list.</b> Broker userbot tidak merespons.",
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⏳ <b>Menyimpan list ke cache...</b>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "waiting_add_group_manual":
		h.clearPanelPrompt(ctx, b, chatID)
		h.setPanelState(chatID, "")
		policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
		currentCount := 0
		if enforced && policy.MaxGroups > 0 {
			currentCount, _ = h.db.CountGroupsAndLists(ctx, userID)
		}
		lines := strings.Split(text, "\n")
		addedCount := 0
		capReached := false
		for _, l := range lines {
			if capReached {
				break
			}
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			parts := strings.Fields(l)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if enforced && policy.MaxGroups > 0 && currentCount >= policy.MaxGroups {
					capReached = true
					break
				}
				var chatIDVal int64
				if id, parseErr := strconv.ParseInt(p, 10, 64); parseErr == nil {
					chatIDVal = id
				}
				err := h.db.AddGroup(ctx, userID, p, p, chatIDVal)
				if err == nil {
					addedCount++
					currentCount++
				}
			}
		}
		if addedCount > 0 {
			h.resetTopicForumScan(chatID)
		}
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "sync_panel_config"})

		gCount, _ := h.db.CountGroupsAndLists(ctx, userID)
		limitNote := ""
		if capReached {
			limitNote = fmt.Sprintf("<br/>⚠️ <i>Batas paket %s maksimal <b>%d grup</b> telah tercapai. Gunakan ⚙️ Settings → 🔑 Upgrade untuk menambah lebih banyak grup.</i>", html.EscapeString(policy.Tier), policy.MaxGroups)
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Grup Berhasil Ditambahkan</h1>
<blockquote expandable>Ditambahkan: <code>%d</code> grup baru.<br/>Total grup tersimpan: <code>%d</code> grup.%s</blockquote>
<table border="1">
  <tr><th>📌 Status</th><th>Total Grup</th></tr>
  <tr><td>Aktif</td><td><code>%d grup</code></td></tr>
</table>
<hr/>
<p>GotdBot • Group Target Manager</p>`, addedCount, gCount, limitNote, gCount),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "browse_list", "list_detail":
		if text == "🗑️ Hapus Semua List 🗑️" {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			_ = h.db.ClearLists(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
			h.DisplayList(ctx, b, chatID, userID)
			return true
		}
		if text == "Kembali" {
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
		if text == "🗑️ Hapus Semua Grup 🗑️" {
			h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
			_ = h.db.ClearGroups(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
			h.DisplayGroup(ctx, b, chatID, userID)
			return true
		}
		if text == "Kembali" {
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
		if text == "🗑️ Hapus Semua List 🗑️" {
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

	case "waiting_set_mode":
		h.setPanelState(chatID, "")
		mode := strings.TrimPrefix(text, "👑 ")
		mode = strings.TrimPrefix(mode, "⚡ ")
		mode = strings.TrimPrefix(mode, "🔕 ")
		mode = strings.TrimPrefix(mode, "📨 ")
		mode = strings.TrimSpace(mode)
		if mode == "Forward" {
			if policy, enforced := h.getEffectiveTierPolicy(ctx, userID); enforced && !policy.AllowForwardMode {
				h.denyTierFeature(ctx, b, chatID, policy.Tier, "Mode Forward", "Pro++", statusMenuMarkup)
				return true
			}
		}
		if mode == "Normal" || mode == "Fast" || mode == "Silent" || mode == "Forward" {
			cfg := h.db.GetUserConfig(ctx, userID)
			cfg.Mode = mode
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("✅ <b>Mode diubah ke <code>%s</code>.</b>", mode),
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Pilihan mode tidak dikenali.",
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_watermark":
		h.clearPanelPrompt(ctx, b, chatID)
		h.deleteBotMessages(ctx, b, chatID, h.clearAndGetDetailMessageIDs(ctx, chatID))
		if message == nil || message.MediaGroupID == "" {
			h.setPanelState(chatID, "")
		}
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == assets.BtnHapusWatermark || strings.EqualFold(text, "hapus") {
			cfg.Watermark = ""
			cfg.WatermarkCacheID = 0
			_ = h.db.SaveUserConfig(ctx, cfg)
			if h.broker != nil {
				_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "clear_watermark"})
			}
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Watermark Berhasil Dihapus</h1>
<blockquote expandable>Custom watermark telah dihapus dari konfigurasi siaran Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		payload := models.PToService{
			ChatID:   chatID,
			UserID:   userID,
			Type:     "cache_watermark",
			Text:     text,
			Entities: panelEntities(message),
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("cache_watermark broker error: %v", err)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ <b>Gagal menyimpan watermark.</b> Broker tidak merespons.",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⏳ <b>Menyimpan watermark ke cache...</b>",
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_email":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == "🗑️ Hapus Email 🗑️" || strings.EqualFold(text, "hapus") {
			cfg.Email = "Belum diatur"
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Email Berhasil Dihapus</h1>
<blockquote expandable>Alamat email notifikasi telah dinonaktifkan dari konfigurasi akun Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Email = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Email Berhasil Diperbarui</h1>
<blockquote expandable>Alamat email notifikasi akun Anda telah diatur ke: <code>%s</code>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(text)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_admin":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == "🗑️ Hapus Admin 🗑️" || strings.EqualFold(text, "hapus") {
			cfg.Admin = "Belum diatur"
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>✅ Admin Berhasil Dihapus</h1>
<blockquote expandable>Wewenang admin tambahan telah dihapus dari konfigurasi akun Anda.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Admin = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Admin Berhasil Diperbarui</h1>
<blockquote expandable>Wewenang admin userbot telah diserahkan ke: <code>%s</code>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(text)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	}

	return false
}
