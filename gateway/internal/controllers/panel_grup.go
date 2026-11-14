package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"log"
	"strings"
)

// ShowGrupList renders the Grup & List menu
func (h *Handler) ShowGrupList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtGrupList,
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

// PromptTambahGrup prompts user for group adding method
func (h *Handler) PromptTambahGrup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	text := `<h1>➕ Tambah Grup Target</h1>
<blockquote expandable>📌 <b>Pilih metode penambahan grup tujuan siaran:</b>
1. <b>Otomatis :</b> Mengimpor semua grup tempat akun userbot bergabung saat ini.
2. <b>Manual :</b> Memasukkan username atau ID grup secara spesifik satu per satu atau multi-baris.</blockquote>
<p>Silakan pilih opsi dari keyboard navigasi di bawah.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.TambahGrupOptionsMarkup,
	})
}

// PromptTambahGrupManual prompts user to input group usernames/IDs
func (h *Handler) PromptTambahGrupManual(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_add_group_manual")

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtPromptTambahGrupManual,
		ParseMode:   "HTML",
		ReplyMarkup: assets.CancelOnlyMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

// CekGrup displays all registered broadcast groups (summary view)
func (h *Handler) CekGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	groups := h.db.GetGroups(ctx, userID)
	if len(groups) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Groups) > 0 {
			groups = refreshed.Groups
		}
	}

	if len(groups) == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtCekGrupKosong,
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}

	items := make([]string, 0, 25)
	truncated := 0
	for i, g := range groups {
		if i >= 25 {
			truncated = len(groups) - 25
			break
		}
		title := g.Title
		if title == "" {
			title = g.Target
		}
		items = append(items, fmt.Sprintf("%d. %s — <code>%s</code>", i+1, html.EscapeString(title), html.EscapeString(g.Target)))
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.RichGrupSummary(len(groups), items, truncated),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

func browseGroupMarkup(count int) *botgotd.ReplyKeyboardMarkup {
	return browseKeyboard(count, "🗑️ Hapus Semua Grup 🗑️")
}

func (h *Handler) DisplayGroup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	groups := h.db.GetGroups(ctx, userID)
	if len(groups) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Groups) > 0 {
			groups = refreshed.Groups
		}
	}
	if len(groups) == 0 {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>📚 Display Grup</h1><blockquote expandable>Belum ada grup yang tersimpan.</blockquote><hr/><p>GotdBot • Group Target Manager</p>",
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}
	h.setPanelState(chatID, "browse_group")
	var builder strings.Builder
	fmt.Fprintf(&builder, "<h1>📚 Display Grup</h1>\n<blockquote expandable>Total tersimpan: <b>%d grup</b>\n", len(groups))
	for i, group := range groups {
		title := strings.Join(strings.Fields(group.Title), " ")
		words := strings.Fields(title)
		if len(words) > 6 {
			title = strings.Join(words[:6], " ") + "…"
		}
		if title == "" {
			title = group.Target
		}
		fmt.Fprintf(&builder, "%d. <code>%s</code> | %s\n", i+1, html.EscapeString(group.Target), html.EscapeString(title))
	}
	builder.WriteString("</blockquote>\n<p>Pilih nomor di bawah untuk melihat detail atau menghapus grup.</p>")
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        builder.String(),
		ParseMode:   "HTML",
		ReplyMarkup: browseGroupMarkup(len(groups)),
	})
}

func (h *Handler) showGroupDetail(ctx context.Context, b *botgotd.Bot, chatID, userID int64, index int) {
	groups := h.db.GetGroups(ctx, userID)
	if index < 0 || index >= len(groups) {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Groups) > 0 {
			groups = refreshed.Groups
		}
	}
	if index < 0 || index >= len(groups) {
		h.DisplayGroup(ctx, b, chatID, userID)
		return
	}
	grp := groups[index]
	title := strings.TrimSpace(grp.Title)
	if title == "" {
		title = grp.Target
	}
	richGroupText := fmt.Sprintf(`<h1>📚 Detail Grup #%d</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🏷️ Nama</td><td>%s</td></tr>
  <tr><td>🎯 Target</td><td><code>%s</code></td></tr>
  <tr><td>🆔 Chat ID</td><td><code>%d</code></td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="panel:group:delete:%d" style="danger">🗑️ Hapus Grup</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Group Target Manager</p>`, index+1, html.EscapeString(title), html.EscapeString(grp.Target), grp.ChatID, index)

	sentMsg, err := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      richGroupText,
		ParseMode: "HTML",
	})
	if err == nil && sentMsg != nil {
		h.trackDetailMessageIDs(ctx, chatID, []int{sentMsg.ID})
	}
}

// PromptHapusGrup prompts for group deletion
func (h *Handler) PromptHapusGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_del_group")

	groups := h.db.GetGroups(ctx, userID)
	var sb strings.Builder
	sb.WriteString("<h1>🗑️ Hapus Grup Target</h1>\n")
	if len(groups) == 0 {
		sb.WriteString("<blockquote expandable>Tidak ada grup yang tersimpan saat ini.</blockquote>")
	} else {
		fmt.Fprintf(&sb, "<blockquote expandable>Total tersimpan: <b>%d grup</b>\n\nKetik nomor urut grup yang ingin dihapus, atau tekan tombol <b>🗑️ Hapus Semua Grup 🗑️</b>.</blockquote>", len(groups))
	}
	sb.WriteString("\n<hr/>\n<p>Pilih dari keyboard atau klik <b>⬅️ Batal ⬅️</b> di bawah.</p>")

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.HapusGrupMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

// CekTopic displays forum topics
func (h *Handler) CekTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	topics := h.db.GetTopics(ctx, userID)
	if len(topics) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Topics) > 0 {
			topics = refreshed.Topics
			cfg = refreshed
		}
	}

	topicStatus := "🔴 Nonaktif"
	if cfg.TopicEnabled {
		topicStatus = "🟢 Aktif"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>🧵 Topic Forum Telegram</h1>\n<blockquote expandable>Status Topic: <b>%s</b>\nTotal Terdaftar: <b>%d topic</b>\n\n", topicStatus, len(topics))

	if len(topics) == 0 {
		sb.WriteString("<i>Belum ada topic terdaftar.</i>")
	} else {
		for i, t := range topics {
			fmt.Fprintf(&sb, "%d. %s — Chat <code>%d</code> Thread <code>%d</code>\n", i+1, html.EscapeString(t.Title), t.ChatID, t.TopicID)
		}
	}
	sb.WriteString("</blockquote>\n<hr/>\n<p>Kelola pengaturan topik forum dari keyboard navigasi di bawah.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

// ToggleTopic toggles topic mode
func (h *Handler) ToggleTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.TopicEnabled = !cfg.TopicEnabled
	_ = h.db.SaveUserConfig(ctx, cfg)

	statusStr := "🔴 Nonaktif"
	if cfg.TopicEnabled {
		statusStr = "🟢 Aktif"
	}

	text := fmt.Sprintf(`<h1>🧵 Pengaturan Topic Forum</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Status</th></tr>
  <tr><td>🧵 Topic Forum Mode</td><td><b>%s</b></td></tr>
</table>
<blockquote expandable>Ketika aktif, broadcast ke grup forum Telegram akan dikirimkan ke topik spesifik yang telah didaftarkan.</blockquote>
<hr/>
<p>Konfigurasi berhasil diperbarui.</p>`, statusStr)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

// SebarKeRC sends test broadcast to Saved Messages (RC)
func (h *Handler) SebarKeRC(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	cfg := h.db.GetUserConfig(ctx, userID)

	if len(lists) == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <b>Peringatan:</b> Belum ada list materi. Tambahkan list terlebih dahulu.",
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}

	payload := models.PToService{
		ChatID: chatID,
		UserID: userID,
		Type:   "test_rc_broadcast",
	}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		log.Printf("test_rc_broadcast broker error: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("<h1>📦 Uji Coba Sebar ke RC</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>📌 Properti</th><th>Nilai</th></tr>\n")
	fmt.Fprintf(&sb, "  <tr><td>💾 Cache ID</td><td><code>#%d</code></td></tr>\n", lists[0])
	wm := "<i>(Tanpa Watermark)</i>"
	if cfg.Watermark != "" {
		wm = html.EscapeString(cfg.Watermark)
	}
	fmt.Fprintf(&sb, "  <tr><td>🏷️ Watermark</td><td>%s</td></tr>\n", wm)
	sb.WriteString("</table>\n")
	sb.WriteString("<blockquote expandable>Materi uji coba berhasil dikirim ke <b>Saved Messages</b> akun Telegram Anda.</blockquote>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Quality Assurance Check</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

// FetchAllGroupsFromAccount communicates with ubot via Redis to import all groups
func (h *Handler) FetchAllGroupsFromAccount(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⏳ <b>Mengimpor semua grup akun...</b>\n\n<i>Proses berjalan di latar belakang. Laporan akan dikirim otomatis setelah selesai.</i>",
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})

	payload := models.PToService{
		ChatID: chatID,
		UserID: userID,
		Type:   "fetch_groups",
	}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		log.Printf("fetch_groups broker error: %v", err)
	}
}
