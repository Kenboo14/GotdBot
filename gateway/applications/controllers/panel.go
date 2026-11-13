package controllers

import (
	"assterisk/gateway/applications/assets"
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/databases"
	"assterisk/gateway/applications/models"
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	mainMenuMarkup          = assets.MainMenuMarkup
	statusMenuMarkup        = assets.StatusMenuMarkup
	grupListMenuMarkup      = assets.GrupListMenuMarkup
	tambahGrupOptionsMarkup = assets.TambahGrupOptionsMarkup
	cancelOnlyMarkup        = assets.CancelOnlyMarkup
	aturJedaMarkup          = assets.AturJedaMarkup
	ubahModeMarkup          = assets.UbahModeMarkup
	timerMarkup             = assets.TimerMarkup
	resetMarkup             = assets.ResetMarkup
	hapusListMarkup         = assets.HapusListMarkup
	hapusGrupMarkup         = assets.HapusGrupMarkup
)

// ShowMainMenu renders the main control panel
func (h *Handler) ShowMainMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, userName string) {
	h.setPanelState(chatID, "")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtMainMenu(userID, userName),
		ParseMode:   "HTML",
		ReplyMarkup: assets.MainMenuMarkup,
	})
}

// ShowStatus renders the userbot status dashboard
func (h *Handler) ShowStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")

	cfg := h.db.GetUserConfig(ctx, userID)
	gCount, lCount := h.db.CountGroupsAndLists(ctx, userID)

	skipFw := "Mati"
	if cfg.SkipFloodWait {
		skipFw = "Aktif"
	}

	safeMode := "Nonaktif"
	if cfg.Safemode {
		safeMode = "Aktif"
	}

	rows := []assets.StatusRow{
		{Label: "🏷️ UserID", Value: fmt.Sprintf("%d", cfg.OwnerID)},
		{Label: "🧩 Tipe", Value: html.EscapeString(cfg.Type)},
		{Label: "👑 Mode", Value: html.EscapeString(cfg.Mode)},
		{Label: "🕒 Jeda", Value: fmt.Sprintf("%d detik", cfg.Delay)},
		{Label: "🌐 Target Grup", Value: fmt.Sprintf("%d grup", gCount)},
		{Label: "📚 Materi List", Value: fmt.Sprintf("%d list", lCount)},
		{Label: "⚠️ Safemode", Value: safeMode},
		{Label: "⏭️ Skip Flood", Value: skipFw},
		{Label: "⏱️ Timer", Value: html.EscapeString(cfg.Timer)},
		{Label: "🚀 Status", Value: html.EscapeString(cfg.Status)},
		{Label: "🔥 Masa Aktif", Value: html.EscapeString(cfg.MasaAktif)},
		{Label: "🔔 Notifikasi", Value: html.EscapeString(cfg.Notifikasi)},
		{Label: "✉️ Email", Value: html.EscapeString(cfg.Email)},
		{Label: "👤 Admin", Value: html.EscapeString(cfg.Admin)},
	}

	tbl := assets.RichStatusTable(rows)
	text := assets.RichStatusDashboard(tbl, html.EscapeString(cfg.TindakanTerkini), html.EscapeString(cfg.TindakanTerakhir))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: assets.StatusMenuMarkup,
	})
}

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
func browseGroupMarkup(count int) *botgotd.ReplyKeyboardMarkup {
	return browseKeyboard(count, "🗑️ Hapus Semua Grup 🗑️")
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
			ReplyMarkup: grupListMenuMarkup,
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
			ReplyMarkup: grupListMenuMarkup,
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
		ReplyMarkup: hapusListMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
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
		ReplyMarkup: hapusGrupMarkup,
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
		ReplyMarkup: grupListMenuMarkup,
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
		ReplyMarkup: grupListMenuMarkup,
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
			ReplyMarkup: grupListMenuMarkup,
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
		ReplyMarkup: grupListMenuMarkup,
	})
}

// PromptAturJeda displays delay settings
func (h *Handler) PromptAturJeda(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_jeda")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>⏱️ Atur Jeda Broadcast</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>⏳ Jeda Saat Ini</td><td><code>%d detik</code></td></tr>
</table>
<blockquote expandable>Pilih interval cepat dari tombol keyboard di bawah, atau ketik angka detik secara langsung.</blockquote>
<hr/>
<p>Silakan tentukan interval jeda siaran.</p>`, cfg.Delay)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: aturJedaMarkup,
	})
}

// ToggleSafemode toggles safemode
func (h *Handler) ToggleSafemode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	cfg.Safemode = !cfg.Safemode
	_ = h.db.SaveUserConfig(ctx, cfg)

	statusStr := "🔴 Nonaktif"
	if cfg.Safemode {
		statusStr = "🟢 Aktif"
	}

	text := fmt.Sprintf(`<h1>🛡️ Pengaturan Safemode</h1>
<table border="1">
  <tr><th>📌 Fitur</th><th>Status</th></tr>
  <tr><td>🛡️ Safemode Anti-Flood</td><td><b>%s</b></td></tr>
</table>
<blockquote expandable>Safemode secara otomatis mengontrol delay dinamis untuk melindungi akun Telegram dari batasan limitasi spam.</blockquote>
<hr/>
<p>Konfigurasi berhasil disimpan.</p>`, statusStr)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// PromptUbahMode displays mode options
func (h *Handler) PromptUbahMode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_mode")
	cfg := h.db.GetUserConfig(ctx, userID)

	text := fmt.Sprintf(`<h1>👑 Ubah Mode Siaran</h1>
<table border="1">
  <tr><th>📌 Mode</th><th>Karakteristik</th></tr>
  <tr><td>👑 Normal</td><td>Kecepatan seimbang &amp; interval stabil</td></tr>
  <tr><td>⚡ Fast</td><td>Kecepatan tinggi, minim jeda waktu</td></tr>
  <tr><td>🔕 Silent</td><td>Tanpa notifikasi suara di grup target</td></tr>
</table>
<blockquote expandable>Mode aktif saat ini: <b>%s</b></blockquote>
<hr/>
<p>Silakan pilih mode siaran dari tombol keyboard di bawah.</p>`, html.EscapeString(cfg.Mode))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: ubahModeMarkup,
	})
}

// ToggleStatus toggles userbot broadcast status
func (h *Handler) ToggleStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	var text string
	if strings.HasPrefix(cfg.Status, "Berhenti") {
		cfg.Status = "Berjalan"
		cfg.TindakanTerkini = "Siaran otomatis diaktifkan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "start_panel_broadcast"}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("start_panel_broadcast broker error: %v", err)
		}
		text = "🚀 <b>Siaran dimulai.</b>\n\nStatus: <code>Berjalan</code>\n\n<i>Userbot aktif mengirim materi ke seluruh grup target.</i>"
	} else {
		cfg.Status = "Berhenti"
		cfg.TindakanTerkini = "Siaran otomatis dimatikan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, OwnerID: userID, Type: "stop_panel_broadcast"}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("stop_panel_broadcast broker error: %v", err)
		}
		text = "🛑 <b>Siaran dihentikan.</b>\n\nStatus: <code>Berhenti</code>\n\n<i>Seluruh antrean pengiriman dinonaktifkan.</i>"
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// ToggleNotifikasi toggles notification setting
func (h *Handler) ToggleNotifikasi(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	if strings.HasPrefix(cfg.Notifikasi, "Aktif") {
		cfg.Notifikasi = "Nonaktif"
	} else {
		cfg.Notifikasi = "Aktif · Kirim ke Saya"
	}
	_ = h.db.SaveUserConfig(ctx, cfg)

	text := fmt.Sprintf(
		"✅ <b>Notifikasi diperbarui.</b>\n\nStatus: <code>%s</code>",
		html.EscapeString(cfg.Notifikasi),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// PromptWatermark prompts user for watermark text
func (h *Handler) PromptWatermark(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lic, _ := h.db.GetActiveUserLicense(ctx, chatID)
	tier := "Free"
	if lic != nil && lic.Tier != "" {
		tier = lic.Tier
	}
	if tier == "Free" || tier == "Pro" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("⚠️ Lisensi Anda (<b>%s</b>) tidak mendukung Custom Watermark. Silakan upgrade ke lisensi <b>PRO++</b> atau <b>VIP</b>.", tier),
			ParseMode: "HTML",
		})
		return
	}

	h.setPanelState(chatID, "waiting_set_watermark")
	cfg := h.db.GetUserConfig(ctx, userID)

	wm := cfg.Watermark
	if wm == "" {
		wm = "<i>(Belum diatur)</i>"
	} else {
		wm = html.EscapeString(wm)
	}

	text := fmt.Sprintf(`<h1>📌 Custom Watermark</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>🏷️ Watermark Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan teks watermark baru. Watermark akan disematkan pada baris paling akhir setiap pesan siaran.</blockquote>
<hr/>
<p>Kirimkan teks sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, wm)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptEmail prompts user for email
func (h *Handler) PromptEmail(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_email")
	cfg := h.db.GetUserConfig(ctx, userID)

	em := cfg.Email
	if em == "" {
		em = "<i>(Belum diatur)</i>"
	} else {
		em = html.EscapeString(em)
	}

	text := fmt.Sprintf(`<h1>✉️ Email Notifikasi</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>📧 Email Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan alamat email valid untuk menerima laporan siaran berkala dan notifikasi alert sistem.</blockquote>
<hr/>
<p>Kirimkan alamat email sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, em)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptAdmin prompts user for admin ID
func (h *Handler) PromptAdmin(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_admin")
	cfg := h.db.GetUserConfig(ctx, userID)

	adm := cfg.Admin
	if adm == "" {
		adm = "<i>(Belum diatur)</i>"
	} else {
		adm = html.EscapeString(adm)
	}

	text := fmt.Sprintf(`<h1>👤 Pengaturan Admin / Sudo</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>👑 Admin ID Saat Ini</td><td>%s</td></tr>
</table>
<blockquote expandable>Kirimkan Telegram User ID atau Username yang diberikan akses wewenang pengelolaan userbot.</blockquote>
<hr/>
<p>Kirimkan User ID sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, adm)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTimer prompts user for timer
func (h *Handler) PromptTimer(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_timer")
	cfg := h.db.GetUserConfig(ctx, userID)

	curr := "🔴 Nonaktif"
	if cfg.Timer != "" {
		curr = html.EscapeString(cfg.Timer)
	}

	text := fmt.Sprintf(`<h1>⏱️ Timer Siaran Otomatis</h1>
<table border="1">
  <tr><th>📌 Parameter</th><th>Nilai</th></tr>
  <tr><td>⏰ Jadwal Saat Ini</td><td><b>%s</b></td></tr>
</table>
<blockquote expandable>Pilih interval pengulangan siaran berkala otomatis dari tombol keyboard di bawah.</blockquote>
<hr/>
<p>Pilih interval timer yang diinginkan.</p>`, curr)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: timerMarkup,
	})
}

// PromptReset prompts user for reset options
func (h *Handler) PromptReset(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	text := `<h1>🗑️ Pusat Reset Data</h1>
<blockquote expandable>⚠️ <b>PERINGATAN:</b> Tindakan reset akan menghapus data yang tersimpan di database permanen. Pastikan Anda telah melakukan konfirmasi sebelum melanjutkan.</blockquote>
<table border="1">
  <tr><th>Opsi Reset</th><th>Cakupan</th></tr>
  <tr><td>🗑️ Reset Grup</td><td>Menghapus seluruh daftar grup target</td></tr>
  <tr><td>🗑️ Reset List</td><td>Menghapus seluruh materi promosi tersimpan</td></tr>
  <tr><td>🗑️ Reset Semua</td><td>Mengembalikan semua konfigurasi ke default</td></tr>
</table>
<hr/>
<p>Pilih jenis reset dari tombol keyboard navigasi di bawah.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: resetMarkup,
	})
}

// ShowShop displays Shop info
func (h *Handler) ShowShop(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("<h1>🛒 GotdBot Store &amp; Layanan Premium</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>💎 Layanan</th><th>⚡ Keunggulan</th></tr>\n")
	sb.WriteString("  <tr><td>👑 VIP Userbot</td><td>Masa aktif tanpa batas &amp; prioritas MTProto</td></tr>\n")
	sb.WriteString("  <tr><td>🌐 Multi-Account Hub</td><td>Koneksikan puluhan nomor paralel</td></tr>\n")
	sb.WriteString("  <tr><td>🚀 Dedicated Instance</td><td>Bot gateway privat eksklusif</td></tr>\n")
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n<p>Untuk konsultasi atau upgrade paket, silakan hubungi admin botgotd.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowExpertExtra displays Expert Extra info
func (h *Handler) ShowExpertExtra(ctx context.Context, b *botgotd.Bot, chatID int64) {
	text := `<h1>⚡ Expert Extra Features</h1>
<table border="1">
  <tr><th>🛠️ Fitur</th><th>📌 Keterangan</th></tr>
  <tr><td>🛡️ FloodWait Bypass</td><td>Antrean adaptif saat sinyal flood Telegram</td></tr>
  <tr><td>🔍 Anti-Duplicate</td><td>Cegah pesan ganda ke dialog sama</td></tr>
  <tr><td>⚡ Pure-Go MTProto</td><td>High performance gotd/td + SQLite WAL</td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="assist_server:refresh" style="primary">🔄 Server Status</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Multi-Configuration VPS Control Panel</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowBantuan displays Bantuan info
func (h *Handler) ShowBantuan(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	effectiveID := h.GetEffectiveUserID(chatID, userID)
	hasActive := false
	if h.db != nil && effectiveID > 0 {
		hasActive = h.db.HasActiveSession(ctx, effectiveID)
	}

	markup := any(assets.CreateChoiceMarkup)
	if hasActive {
		markup = assets.MainMenuMarkup
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtBantuan,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
}

// ShowControlMenu displays the remote account selection menu
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

// ShowSettings delegates to ShowStatus
func (h *Handler) ShowSettings(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.ShowStatus(ctx, b, chatID, userID)
}

// ShowFeed displays updates feed
func (h *Handler) ShowFeed(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("<h1>📰 Informasi &amp; Berita GotdBot</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>🚀 Versi</th><th><code>v2.5.0-HighPerformance</code></th></tr>\n")
	sb.WriteString("  <tr><td>🔄 Sinkronisasi</td><td>MongoDB Cloud Atlas &amp; SQLite WAL</td></tr>\n")
	sb.WriteString("  <tr><td>⚡ Messaging</td><td>Pure Gotd MTProto Layer 227</td></tr>\n")
	sb.WriteString("  <tr><td>📦 Manajemen</td><td>Grup &amp; List Terintegrasi</td></tr>\n")
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/><p>Dapatkan pembaruan rilis berkala di channel resmi GotdBot.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowWebapp displays Webapp info
func (h *Handler) ShowWebapp(ctx context.Context, b *botgotd.Bot, chatID int64) {
	text := `<h1>🌐 GotdBot Web Dashboard</h1>
<blockquote expandable>Akses panel kendali visual untuk memantau performa siaran secara langsung melalui browser atau Telegram Mini App.</blockquote>
<hr/>
<p>Integrasi Web Dashboard sedang dalam tahap penyempurnaan cloud.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

func (h *Handler) HandlePanelMediaUpdate(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if h.captureCacheMedia(update) {
		return
	}
	h.HandlePanelMedia(ctx, b, update)
}

func (h *Handler) captureCacheMedia(update *botgotd.Update) bool {
	if update == nil {
		return false
	}
	message := update.Message
	if message == nil {
		message = update.ChannelPost
	}
	if message == nil {
		return false
	}
	cache := strings.TrimPrefix(strings.TrimSpace(os.Getenv("GROUP_CACHE")), "@")
	if cache == "" {
		cache = "AiotdCaches"
	}
	chatID := strconv.FormatInt(message.Chat.ID, 10)
	chatMatches := strings.EqualFold(strings.TrimSpace(message.Chat.Username), cache) || chatID == cache || "-100"+chatID == cache
	if !chatMatches {
		return false
	}
	item := cachedMediaFile{}
	if len(message.Photo) > 0 {
		item.Kind, item.FileID = "photo", message.Photo[len(message.Photo)-1].FileID
	} else if message.Video != nil {
		item.Kind, item.FileID = "video", message.Video.FileID
	} else if message.Document != nil {
		item.Kind, item.FileID = "document", message.Document.FileID
	} else if message.Audio != nil {
		item.Kind, item.FileID = "audio", message.Audio.FileID
	} else if message.Animation != nil {
		item.Kind, item.FileID = "animation", message.Animation.FileID
	} else if message.Voice != nil {
		item.Kind, item.FileID = "voice_note", message.Voice.FileID
	} else {
		return true
	}
	h.cacheMediaMu.Lock()
	h.cacheMedia[message.ID] = item
	h.cacheMediaMu.Unlock()
	return true
}

// HandlePanelMedia routes media/caption messages into the same panel state flow.
func (h *Handler) HandlePanelMedia(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	message := update.Message
	state := h.getPanelState(message.Chat.ID)
	if state != "waiting_add_list" && state != "waiting_add_watermark" && !strings.HasPrefix(state, "ctrl_add_list:") {
		return
	}
	if message.MediaGroupID != "" {
		effectiveUID := message.From.ID
		if strings.HasPrefix(state, "ctrl_add_list:") {
			parts := strings.Split(state, ":")
			if len(parts) >= 2 {
				if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					effectiveUID = id
				}
			}
		}
		key := fmt.Sprintf("%d:%s:%s", message.Chat.ID, state, message.MediaGroupID)
		h.mediaMu.Lock()
		batch := h.mediaBatches[key]
		if batch == nil {
			batch = &panelMediaBatch{ChatID: message.Chat.ID, UserID: effectiveUID, State: state}
			h.mediaBatches[key] = batch
		}
		batch.SourceIDs = append(batch.SourceIDs, message.ID)
		// Ambil caption dari foto/video pertama yang membawa teks
		if batch.Caption == "" {
			if message.Caption != "" {
				batch.Caption = message.Caption
				batch.Entities = panelEntities(message)
			} else if message.Text != "" {
				batch.Caption = message.Text
				batch.Entities = panelEntities(message)
			}
		}
		if batch.Timer != nil {
			batch.Timer.Stop()
		}
		batch.Timer = time.AfterFunc(1500*time.Millisecond, func() { h.flushPanelMediaGroup(context.Background(), b, key) })
		h.mediaMu.Unlock()
		return
	}
	caption := message.Caption
	if caption == "" {
		caption = message.Text
	}
	effectiveUID := message.From.ID
	if strings.HasPrefix(state, "ctrl_add_list:") {
		parts := strings.Split(state, ":")
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				effectiveUID = id
			}
		}
	}
	h.stagePanelMediaWithCaption(ctx, b, message.Chat.ID, effectiveUID, state, []int{message.ID}, caption, panelEntities(message))
}

func (h *Handler) flushPanelMediaGroup(ctx context.Context, b *botgotd.Bot, key string) {
	h.mediaMu.Lock()
	batch := h.mediaBatches[key]
	delete(h.mediaBatches, key)
	h.mediaMu.Unlock()
	if batch == nil {
		return
	}
	h.stagePanelMediaWithCaption(ctx, b, batch.ChatID, batch.UserID, batch.State, batch.SourceIDs, batch.Caption, batch.Entities)
}

func (h *Handler) stagePanelMedia(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state string, sourceIDs []int) {
	h.stagePanelMediaWithCaption(ctx, b, chatID, userID, state, sourceIDs, "", nil)
}

func (h *Handler) stagePanelMediaWithCaption(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state string, sourceIDs []int, caption string, entities []models.MessageEntity) {
	h.clearPanelPrompt(ctx, b, chatID)
	if strings.HasPrefix(state, "ctrl_add_list:") {
		parts := strings.Split(state, ":")
		parentMsgID := 0
		targetID := userID
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				targetID = id
			}
		}
		if len(parts) >= 3 {
			parentMsgID, _ = strconv.Atoi(parts[2])
		}
		h.setPanelState(chatID, "")
		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		payload := models.PToService{
			ChatID:           0, // 0 agar ubot tidak membalas via SendMessage + ReplyKeyboard
			UserID:           targetID,
			OwnerID:          targetID,
			Type:             "cache_list",
			MediaSourceCount: len(sourceIDs),
			Text:             caption,
			Entities:         entities,
		}
		_ = h.broker.To(brokerCtx, "to:ubot:command", payload)

		if parentMsgID > 0 {
			targetName := h.GetAccountName(targetID)
			if targetName == "" {
				targetName = fmt.Sprintf("Akun [%d]", targetID)
			}
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        assets.TxtControlMediaSaving(targetName, targetID),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CtrlDoneIKB(targetID),
			})
		}
		return
	}

	jobType := "cache_list"
	if state == "waiting_add_watermark" {
		jobType = "cache_watermark"
	}
	h.setPanelState(chatID, "")
	payload := models.PToService{
		ChatID:           chatID,
		UserID:           userID,
		Type:             jobType,
		MediaSourceCount: len(sourceIDs),
		Text:             caption,
		Entities:         entities,
	}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		log.Printf("media cache broker error: %v", err)
		return
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⏳ <b>Mengamankan media ke cache...</b>",
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

func panelEntities(message *botgotd.Message) []models.MessageEntity {
	if message == nil {
		return nil
	}
	entities := message.Entities
	if message.Caption != "" {
		entities = message.CaptionEntities
	}
	result := make([]models.MessageEntity, 0, len(entities))
	for _, entity := range entities {
		item := models.MessageEntity{Type: string(entity.Type), Offset: entity.Offset, Length: entity.Length, URL: entity.URL, Language: entity.Language, CustomEmojiID: entity.CustomEmojiID}
		if entity.User != nil {
			item.UserID = entity.User.ID
		}
		result = append(result, item)
	}
	return result
}

// HandlePanelInput processes text inputs when user is in a state
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

	if text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel" {
		h.clearPanelPrompt(ctx, b, chatID)
		h.clearProgress(ctx, b, chatID)
		h.setPanelState(chatID, "")
		hasActive := false
		if h.db != nil {
			hasActive = h.db.HasActiveSession(ctx, userID)
		}
		if hasActive {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "<p>↩️ <b>Tindakan dibatalkan.</b></p>",
				ParseMode:   "HTML",
				ReplyMarkup: mainMenuMarkup,
			})
		} else {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "<p>↩️ <b>Tindakan dibatalkan.</b></p>",
				ParseMode:   "HTML",
				ReplyMarkup: assets.RemoveKeyboard,
			})
		}
		return true
	}

	switch state {
	case "waiting_add_list":
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
		lines := strings.Split(text, "\n")
		addedCount := 0
		for _, l := range lines {
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
				var chatIDVal int64
				if id, parseErr := strconv.ParseInt(p, 10, 64); parseErr == nil {
					chatIDVal = id
				}
				err := h.db.AddGroup(ctx, userID, p, p, chatIDVal)
				if err == nil {
					addedCount++
				}
			}
		}
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "sync_panel_config"})

		gCount, _ := h.db.CountGroupsAndLists(ctx, userID)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Grup Berhasil Ditambahkan</h1>
<blockquote expandable>Ditambahkan: <code>%d</code> grup baru.<br/>Total grup tersimpan: <code>%d</code> grup.</blockquote>
<table border="1">
  <tr><th>📌 Status</th><th>Total Grup</th></tr>
  <tr><td>Aktif</td><td><code>%d grup</code></td></tr>
</table>
<hr/>
<p>GotdBot • Group Target Manager</p>`, addedCount, gCount, gCount),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "browse_list", "list_detail":
		if text == "🗑️ Hapus Semua List 🗑️" {
			_ = h.db.ClearLists(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
			h.DisplayList(ctx, b, chatID, userID)
			return true
		}
		if text == "Kembali" {
			msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
			if len(msgIDs) > 0 {
				_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
			}
			h.ShowGrupList(ctx, b, chatID, userID)
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			if state == "list_detail" {
				msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
				if len(msgIDs) > 0 {
					_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
				}
			}
			h.showListDetail(ctx, b, chatID, userID, idx-1)
			return true
		}
		return true

	case "browse_group":
		if text == "🗑️ Hapus Semua Grup 🗑️" {
			_ = h.db.ClearGroups(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
			h.DisplayGroup(ctx, b, chatID, userID)
			return true
		}
		if text == "Kembali" {
			msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
			if len(msgIDs) > 0 {
				_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
			}
			h.ShowGrupList(ctx, b, chatID, userID)
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			msgIDs := h.clearAndGetDetailMessageIDs(ctx, chatID)
			if len(msgIDs) > 0 {
				_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: msgIDs})
			}
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

	case "waiting_set_jeda":
		h.setPanelState(chatID, "")
		clean := strings.TrimPrefix(text, "⏱️ ")
		clean = strings.TrimSuffix(clean, " Detik")
		clean = strings.TrimSpace(clean)
		if sec, err := strconv.Atoi(clean); err == nil && sec > 0 {
			cfg := h.db.GetUserConfig(ctx, userID)
			cfg.Delay = sec
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("✅ <b>Jeda diatur ke <code>%d detik</code>.</b>", sec),
				ParseMode:   "HTML",
				ReplyMarkup: mainMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Nilai jeda tidak valid. Masukkan angka detik yang benar.",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "waiting_set_mode":
		h.setPanelState(chatID, "")
		mode := strings.TrimPrefix(text, "👑 ")
		mode = strings.TrimPrefix(mode, "⚡ ")
		mode = strings.TrimPrefix(mode, "🔕 ")
		mode = strings.TrimSpace(mode)
		if mode == "Normal" || mode == "Fast" || mode == "Silent" {
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
		if message == nil || message.MediaGroupID == "" {
			h.setPanelState(chatID, "")
		}
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == assets.BtnHapusWatermark || strings.EqualFold(text, "hapus") {
			cfg.Watermark = ""
			cfg.WatermarkCacheID = 0
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Watermark dihapus.</b>",
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

	case "waiting_set_timer":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		clean := strings.TrimPrefix(text, "⏱️ ")
		if clean == "Nonaktifkan Timer" {
			cfg.Timer = "Belum diatur"
		} else {
			cfg.Timer = clean
		}
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>✅ Timer Siaran Diperbarui</h1>
<blockquote expandable>Jadwal timer siaran otomatis telah diatur ke: <code>%s</code>.</blockquote>
<hr/>
<p>GotdBot • Status Configuration</p>`, html.EscapeString(cfg.Timer)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true
	}

	return false
}

// FetchAllGroupsFromAccount communicates with ubot via Redis to import all groups
func (h *Handler) FetchAllGroupsFromAccount(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⏳ <b>Mengimpor semua grup akun...</b>\n\n<i>Proses berjalan di latar belakang. Laporan akan dikirim otomatis setelah selesai.</i>",
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
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

func isRestartBrowseButton(text string) bool {
	if text == "Kembali" || text == assets.BtnHapusSemuaList || text == assets.BtnHapusSemuaGrup {
		return true
	}
	if n, err := strconv.Atoi(text); err == nil && n > 0 && n <= 100 {
		return true
	}
	return false
}

func isKnownPanelButton(text string) bool {
	switch text {
	case assets.BtnHome, "/menu",
		assets.BtnStatus, "/status",
		assets.BtnShop,
		assets.BtnGrupList,
		assets.BtnExpert,
		assets.BtnReset,
		assets.BtnAturJeda,
		assets.BtnSettings,
		assets.BtnFeed,
		assets.BtnWebapp,
		assets.BtnCustomWatermark,
		assets.BtnAturEmail,
		assets.BtnSafemode,
		assets.BtnTimer,
		assets.BtnUbahMode,
		assets.BtnAdmin,
		assets.BtnUbahStatus,
		assets.BtnNotifikasi,
		assets.BtnCekList,
		assets.BtnCekGrup,
		assets.BtnTambahList,
		assets.BtnTambahGrup,
		assets.BtnTambahkanSemuaGrup,
		assets.BtnTambahGrupManual,
		assets.BtnCekTopic, assets.BtnListTopic,
		assets.BtnTopic,
		assets.BtnSebarKeRC,
		assets.BtnHapusSemuaGrup,
		assets.BtnHapusSemuaList,
		assets.BtnResetKonfigurasi,
		assets.BtnBatal:
		return true
	default:
		return isRestartBrowseButton(text)
	}
}

// RoutePanelButton handles any ReplyKeyboard button click
func (h *Handler) RoutePanelButton(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) bool {
	if update.Message == nil || update.Message.From == nil {
		return false
	}
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	h.SetAccountName(userID, userName)
	text := strings.TrimSpace(update.Message.Text)

	// Intersep tombol Control
	if text == assets.BtnControl || text == assets.BtnControlAlt || strings.EqualFold(text, "control") || text == "/control" {
		username := ""
		if update.Message.From != nil {
			username = update.Message.From.Username
		}
		h.ShowControlMenu(ctx, b, chatID, userID, username)
		return true
	}

	// Intersep tombol Bantuan
	if text == assets.BtnBantuan || strings.EqualFold(text, "bantuan") || text == "/help" {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true
	}

	effectiveUserID := h.GetEffectiveUserID(chatID, userID)
	effectiveUserName := userName
	if effectiveUserID != userID {
		if tName := h.GetAccountName(effectiveUserID); tName != "" {
			effectiveUserName = tName
		}
	}

	// Cek apakah user sedang berada dalam input state
	if h.HandlePanelInput(ctx, b, chatID, effectiveUserID, text, update.Message) {
		return true
	}

	// Hanya tangani jika teks benar-benar salah satu tombol ReplyKeyboard panel yang dikenali
	if !isKnownPanelButton(text) {
		return false
	}

	// Periksa apakah user memiliki sesi userbot yang aktif
	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, effectiveUserID)
	}

	// Jika belum memasang userbot dan tidak sedang me-remote akun aktif, tolak akses tombol kontrol panel
	if !hasActive {
		h.setPanelState(chatID, "")
		if text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel" {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "↩️ Tindakan dibatalkan.",
				ReplyMarkup: assets.CreateChoiceMarkup,
			})
			return true
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("<h1>👋 Halo, %s</h1>\n", html.EscapeString(userName)))
		sb.WriteString("<blockquote expandable>⚠️ <b>Sesi Userbot Belum Terpasang</b>\n")
		sb.WriteString("<i>Menu kontrol panel (Status, Grup &amp; List, Atur Jeda, dll.) hanya untuk akun yang sudah memasang userbot aktif atau akun admin yang sedang me-remote.</i></blockquote>\n\n")
		sb.WriteString("<p>Silakan gunakan <b>🔑 Input Token 🔑</b> atau <b>🚀 Buat Userbot 🚀</b> untuk memasang userbot Anda, atau gunakan <b>🎯 Control 🎯</b> jika Anda admin dari akun lain.</p>")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: assets.CreateChoiceMarkup,
		})
		return true
	}

	// Jika server baru restart (state kosong di RAM) dan user mengklik tombol dari Cek List / Cek Grup
	if isRestartBrowseButton(text) {
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true
	}

	switch text {
	// Main Menu Navigation
	case assets.BtnHome, "/menu":
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	case assets.BtnStatus, "/status":
		h.ShowStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnShop:
		h.ShowShop(ctx, b, chatID)
		return true

	case assets.BtnGrupList:
		h.ShowGrupList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnExpert:
		h.ShowExpertExtra(ctx, b, chatID)
		return true

	case assets.BtnReset:
		h.PromptReset(ctx, b, chatID)
		return true

	case assets.BtnAturJeda:
		h.PromptAturJeda(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBantuan:
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSettings:
		h.ShowSettings(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnFeed:
		h.ShowFeed(ctx, b, chatID)
		return true

	case assets.BtnWebapp:
		h.ShowWebapp(ctx, b, chatID)
		return true

	// Status Menu Actions
	case assets.BtnCustomWatermark:
		h.PromptWatermark(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAturEmail:
		h.PromptEmail(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSafemode:
		h.ToggleSafemode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTimer:
		h.PromptTimer(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahMode:
		h.PromptUbahMode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAdmin:
		h.PromptAdmin(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahStatus:
		h.ToggleStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnNotifikasi:
		h.ToggleNotifikasi(ctx, b, chatID, effectiveUserID)
		return true

	// Grup & List Actions
	case assets.BtnCekList:
		h.DisplayList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnCekGrup:
		h.DisplayGroup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahList:
		h.PromptTambahList(ctx, b, chatID)
		return true

	case assets.BtnTambahGrup:
		h.PromptTambahGrup(ctx, b, chatID)
		return true

	case assets.BtnTambahkanSemuaGrup:
		h.FetchAllGroupsFromAccount(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahGrupManual:
		h.PromptTambahGrupManual(ctx, b, chatID)
		return true

	case assets.BtnCekTopic, assets.BtnListTopic:
		h.CekTopic(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTopic:
		h.ToggleTopic(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSebarKeRC:
		h.SebarKeRC(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBatal:
		h.setPanelState(chatID, "")
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	// Reset Menu Sub-Actions
	case assets.BtnHapusSemuaGrup:
		_ = h.db.ClearGroups(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_groups"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaGrupSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnHapusSemuaList:
		_ = h.db.ClearLists(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaListSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnResetKonfigurasi:
		defaultCfg := &databases.UserConfig{
			OwnerID:          effectiveUserID,
			Type:             "Broadcast",
			Delay:            5,
			SkipFloodWait:    false,
			Timer:            "Belum diatur",
			Extra:            0,
			Mode:             "Normal",
			Safemode:         false,
			Status:           "Berhenti",
			MasaAktif:        "November 29, 2026 at 14:18",
			Notifikasi:       "Aktif · Kirim ke Saya",
			Email:            "Belum diatur",
			Admin:            "Belum diatur",
			Watermark:        "",
			TopicEnabled:     false,
			TindakanTerkini:  "Siaran otomatis dimatikan",
			TindakanTerakhir: "-",
			UpdatedAt:        time.Now(),
		}
		_ = h.db.SaveUserConfig(ctx, defaultCfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetKonfigurasiSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true
	}

	return false
}
