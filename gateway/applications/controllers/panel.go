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

	if userName == "" {
		userName = "User"
	}

	text := fmt.Sprintf(`<h1>🤖 GotdBot Control Panel</h1>
<blockquote expandable>👋 <b>Halo, <a href="tg://user?id=%d">%s</a>!</b><br/>
<i>Sesi userbot Anda sedang berjalan dan terhubung dengan aman via MTProto. Seluruh data disinkronkan secara real-time.</i></blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:status" style="primary">💎 Cek Status</tg-button>
</tg-button-row>
<hr/>
<p>Silakan gunakan menu navigasi keyboard di bawah untuk mengontrol bot:</p>`, userID, html.EscapeString(userName))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
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

	var sb strings.Builder
	sb.WriteString("<h1>💎 Status Userbot</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>⚙️ Parameter</th><th>📌 Nilai</th></tr>\n")
	sb.WriteString(fmt.Sprintf("  <tr><td>🏷️ UserID</td><td><code>%d</code></td></tr>\n", cfg.OwnerID))
	sb.WriteString(fmt.Sprintf("  <tr><td>🧩 Tipe</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Type)))
	sb.WriteString(fmt.Sprintf("  <tr><td>👑 Mode</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Mode)))
	sb.WriteString(fmt.Sprintf("  <tr><td>🕒 Jeda</td><td><code>%d detik</code></td></tr>\n", cfg.Delay))
	sb.WriteString(fmt.Sprintf("  <tr><td>🌐 Target Grup</td><td><code>%d grup</code></td></tr>\n", gCount))
	sb.WriteString(fmt.Sprintf("  <tr><td>📚 Materi List</td><td><code>%d list</code></td></tr>\n", lCount))
	sb.WriteString(fmt.Sprintf("  <tr><td>⚠️ Safemode</td><td><code>%s</code></td></tr>\n", safeMode))
	sb.WriteString(fmt.Sprintf("  <tr><td>⏭️ Skip Flood</td><td><code>%s</code></td></tr>\n", skipFw))
	sb.WriteString(fmt.Sprintf("  <tr><td>⏱️ Timer</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Timer)))
	sb.WriteString(fmt.Sprintf("  <tr><td>🚀 Status</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Status)))
	sb.WriteString(fmt.Sprintf("  <tr><td>🔥 Masa Aktif</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.MasaAktif)))
	sb.WriteString(fmt.Sprintf("  <tr><td>🔔 Notifikasi</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Notifikasi)))
	sb.WriteString(fmt.Sprintf("  <tr><td>✉️ Email</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Email)))
	sb.WriteString(fmt.Sprintf("  <tr><td>👤 Admin</td><td><code>%s</code></td></tr>\n", html.EscapeString(cfg.Admin)))
	sb.WriteString("</table>\n")
	sb.WriteString("<blockquote expandable>\n")
	sb.WriteString(fmt.Sprintf("📋 <b>Tindakan Terkini :</b> <i>%s</i><br/>\n", html.EscapeString(cfg.TindakanTerkini)))
	sb.WriteString(fmt.Sprintf("📄 <b>Tindakan Terakhir :</b> <i>%s</i>\n", html.EscapeString(cfg.TindakanTerakhir)))
	sb.WriteString("</blockquote>\n")
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:status\" style=\"primary\">🔄 Refresh Status</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Userbot Status Dashboard</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// ShowGrupList renders the Grup & List menu
func (h *Handler) ShowGrupList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")

	text := `<h1>🔥 Grup &amp; List</h1>
<blockquote expandable>💡 <b>Petunjuk Singkat:</b>
1. <b>List :</b> <i>Materi promosi (teks, media single, media grup) yang akan disiarkan.</i>
2. <b>Grup :</b> <i>Daftar grup atau supergroup tujuan siaran.</i>
3. <b>Topic :</b> <i>Thread forum grup Telegram untuk penargetan topik khusus.</i></blockquote>
<p>Atur target grup dan materi siaran Anda lewat tombol keyboard navigasi di bawah.</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// PromptTambahList prompts user to enter broadcast list text
func (h *Handler) PromptTambahList(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_add_list")

	text := `<h1>📝 Tambah List Promosi</h1>
<blockquote expandable>📌 <b>Format materi promosi yang didukung:</b>
1. <b>Teks :</b> Pesan teks biasa atau berformat HTML / Markdown.
2. <b>Media Single :</b> Foto, video, dokumen, audio, atau animasi / GIF (+ caption).
3. <b>Media Grup :</b> Album kumpulan foto atau video (+ caption).</blockquote>
<hr/>
<p>Kirimkan materi promosi sekarang atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
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
		ReplyMarkup: tambahGrupOptionsMarkup,
	})
}

// PromptTambahGrupManual prompts user to input group usernames/IDs
func (h *Handler) PromptTambahGrupManual(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "waiting_add_group_manual")

	text := `<h1>📝 Tambah Grup Manual</h1>
<blockquote expandable>📌 <b>Format target yang didukung:</b>
1. <b>Username grup</b> (contoh: <code>@grupchat</code>)
2. <b>Chat ID grup</b> (contoh: <code>-1001234567890</code>)
3. <b>Tautan grup</b> (contoh: <code>t.me/namagrup</code>)

<i>Jika lebih dari satu grup, pisahkan masing-masing target dengan baris baru.</i></blockquote>
<hr/>
<p>Kirimkan target grup sekarang atau klik tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

// CekList displays all saved promotional lists (summary view)
func (h *Handler) CekList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	if len(lists) == 0 {
		// Sinkronisasi on-demand jika cache SQLite lokal masih kosong (misal baru disimpan via ubot)
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Lists) > 0 {
			lists = refreshed.Lists
		}
	}

	if len(lists) == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<h1>📋 Ringkasan List Promosi</h1><blockquote expandable>Belum ada list materi promosi tersimpan.</blockquote><hr/><p>Gunakan tombol <b>📝 Tambah List 📝</b> untuk menambahkan materi baru.</p>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>📋 Ringkasan List Promosi</h1>\n<blockquote expandable>Total tersimpan: <b>%d materi</b>\n", len(lists))
	for i, msgID := range lists {
		fmt.Fprintf(&sb, "%d. <b>List #%d</b> — <code>Cache ID: %d</code>\n", i+1, i+1, msgID)
		if i >= 19 {
			fmt.Fprintf(&sb, "<i>... dan <code>%d</code> list lainnya.</i>", len(lists)-20)
			break
		}
	}
	sb.WriteString("</blockquote>\n<p>Gunakan menu <b>📋 Cek List</b> untuk melihat preview detail interaktif.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
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
			Text:        "<h1>📚 Ringkasan Grup Target</h1><blockquote expandable>Belum ada grup target siaran tersimpan.</blockquote><hr/><p>Gunakan tombol <b>📝 Tambah Grup 📝</b> untuk menambahkan grup target.</p>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>📚 Ringkasan Grup Target</h1>\n<blockquote expandable>Total tersimpan: <b>%d grup</b>\n", len(groups))
	for i, g := range groups {
		title := g.Title
		if title == "" {
			title = g.Target
		}
		fmt.Fprintf(&sb, "%d. %s — <code>%s</code>\n", i+1, html.EscapeString(title), html.EscapeString(g.Target))
		if i >= 24 {
			fmt.Fprintf(&sb, "<i>... dan <code>%d</code> grup lainnya.</i>", len(groups)-25)
			break
		}
	}
	sb.WriteString("</blockquote>\n<p>Gunakan menu <b>📚 Cek Grup</b> untuk melihat daftar lengkap tiap grup.</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
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
	text := `<h1>⚠️ Panduan &amp; Bantuan Penggunaan</h1>
<table border="1">
  <tr><th>🔘 Tombol</th><th>📘 Fungsi</th></tr>
  <tr><td>💎 Status</td><td>Melihat ringkasan lengkap data dan konfigurasi</td></tr>
  <tr><td>🧩 Grup &amp; List</td><td>Mengatur materi teks promosi &amp; target grup</td></tr>
  <tr><td>⌛ Atur Jeda</td><td>Mengubah interval jeda pengiriman antar grup</td></tr>
  <tr><td>⚠️ Safemode</td><td>Melindungi akun dari risiko limitasi Telegram</td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="assist_help:home" style="primary">📖 Menu Bantuan Lengkap</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Multi-Configuration VPS Control Panel</p>`

	effectiveID := h.GetEffectiveUserID(chatID, userID)
	hasActive := false
	if h.db != nil && effectiveID > 0 {
		hasActive = h.db.HasActiveSession(ctx, effectiveID)
	}

	markup := any(cachedCreateChoiceMarkup)
	if hasActive {
		markup = mainMenuMarkup
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
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
		var sb strings.Builder
		sb.Grow(512)
		sb.WriteString("<b>🎯 Panel Kontrol Akun</b>\n")
		sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
		sb.WriteString("👤 <b>Anda memiliki 0 akses</b>\n")
		sb.WriteString("Anda belum memiliki akses untuk mengontrol akun userbot manapun.\n\n")
		fmt.Fprintf(&sb, "<blockquote>User ID Anda: <code>%d</code></blockquote>\n\n", userID)
		sb.WriteString("Pastikan Anda telah memasang userbot sendiri via <b>/start</b>, atau minta pemilik userbot lain menambahkan User ID Anda sebagai <b>ADMIN</b> di menu Pengaturan Admin.")

		if messageID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:    chatID,
				MessageID: messageID,
				Text:      sb.String(),
				ParseMode: "HTML",
			})
		} else {
			effectiveID := h.GetEffectiveUserID(chatID, userID)
			hasActive := false
			if h.db != nil {
				hasActive = h.db.HasActiveSession(ctx, effectiveID)
			}
			markup := any(cachedCreateChoiceMarkup)
			if hasActive {
				markup = mainMenuMarkup
			}
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        sb.String(),
				ParseMode:   "HTML",
				ReplyMarkup: markup,
			})
		}
		return
	}

	rows := make([][]botgotd.InlineKeyboardButton, 0, len(accounts)+1)
	for _, acc := range accounts {
		btnText := acc.Name
		cbData := fmt.Sprintf("ctrl:select:%d", acc.OwnerID)
		rows = append(rows, []botgotd.InlineKeyboardButton{
			{Text: btnText, CallbackData: cbData},
		})
	}
	rows = append(rows, []botgotd.InlineKeyboardButton{
		{Text: "❌ Tutup Panel ❌", CallbackData: "ctrl:close"},
	})

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>🎯 Panel Kontrol Akun</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👤 <b>Anda memiliki %d akses akun</b>\n", len(accounts))
	sb.WriteString("📝 <i>Silakan pilih akun userbot yang ingin Anda kontrol di bawah:</i>\n\n")
	sb.WriteString("<blockquote>💡 <b>Tips:</b> Untuk mengubah teks / grup secara optimal, pastikan userbot dalam posisi Status Nonaktif.</blockquote>")

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	fmt.Fprintf(&sb, "👋 <b>Hai!, %s,</b>\n\n", html.EscapeString(adminName))
	sb.WriteString("Selamat datang di menu admins. Anda dapat mengkontrol beberapa akun ubot sekaligus.\n\n")
	fmt.Fprintf(&sb, "👑 <b>Mengkontrol:</b> %s (<code>%d</code>)\n\n", html.EscapeString(targetName), targetID)
	sb.WriteString("<b>Ads:</b> <a href=\"https://t.me/GotdBot\">Channel Resmi GotdBot</a>")

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "📚 Status 📚", CallbackData: fmt.Sprintf("ctrl:status:%d", targetID)}},
		{{Text: "🔮 Grup & List 🔮", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
		{{Text: "🕦 Atur Jeda 🕦", CallbackData: fmt.Sprintf("ctrl:jeda:%d", targetID)}},
		{{Text: "💎 List Akun 💎", CallbackData: "ctrl:list"}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>📚 Status Userbot (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> %s (<code>%d</code>)\n", html.EscapeString(targetName), targetID)
	fmt.Fprintf(&sb, "🚀 <b>Status:</b> <code>%s</code>\n", html.EscapeString(cfg.Status))
	fmt.Fprintf(&sb, "👑 <b>Mode:</b> <code>%s</code>\n", html.EscapeString(cfg.Mode))
	fmt.Fprintf(&sb, "🕒 <b>Jeda:</b> <code>%d detik</code>\n", cfg.Delay)
	fmt.Fprintf(&sb, "🌐 <b>Target Grup:</b> <code>%d grup</code>\n", gCount)
	fmt.Fprintf(&sb, "📚 <b>Materi List:</b> <code>%d list</code>\n", lCount)
	fmt.Fprintf(&sb, "⚠️ <b>Safemode:</b> <code>%s</code>\n", safeMode)
	fmt.Fprintf(&sb, "⏭️ <b>Skip Flood:</b> <code>%s</code>\n", skipFw)
	fmt.Fprintf(&sb, "⏱️ <b>Timer:</b> <code>%s</code>\n", html.EscapeString(cfg.Timer))
	fmt.Fprintf(&sb, "🔥 <b>Masa Aktif:</b> <code>%s</code>\n\n", html.EscapeString(cfg.MasaAktif))
	sb.WriteString("<blockquote>")
	fmt.Fprintf(&sb, "📋 <b>Tindakan Terkini:</b> <i>%s</i>\n", html.EscapeString(cfg.TindakanTerkini))
	fmt.Fprintf(&sb, "📄 <b>Tindakan Terakhir:</b> <i>%s</i>", html.EscapeString(cfg.TindakanTerakhir))
	sb.WriteString("</blockquote>")

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔄 Refresh Status 🔄", CallbackData: fmt.Sprintf("ctrl:status_refresh:%d", targetID)}},
		{{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:menu:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>🔮 Grup &amp; List (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> %s (<code>%d</code>)\n", html.EscapeString(targetName), targetID)
	fmt.Fprintf(&sb, "📚 <b>Total List Promosi:</b> <code>%d materi</code>\n", lCount)
	fmt.Fprintf(&sb, "🌐 <b>Total Target Grup:</b> <code>%d grup</code>\n\n", gCount)
	sb.WriteString("<blockquote>💡 <i>Gunakan tombol di bawah untuk melihat, menambah, atau mereset list &amp; grup akun ini secara remote.</i></blockquote>")

	rows := [][]botgotd.InlineKeyboardButton{
		{
			{Text: "📁 Cek List 📁", CallbackData: fmt.Sprintf("ctrl:cek_list:%d:0", targetID)},
			{Text: "📚 Cek Grup 📚", CallbackData: fmt.Sprintf("ctrl:cek_grup:%d:0", targetID)},
		},
		{
			{Text: "📝 Tambah List 📝", CallbackData: fmt.Sprintf("ctrl:tambah_list:%d", targetID)},
			{Text: "➕ Tambah Grup ➕", CallbackData: fmt.Sprintf("ctrl:tambah_grup:%d", targetID)},
		},
		{
			{Text: "🗑️ Hapus List 🗑️", CallbackData: fmt.Sprintf("ctrl:hapus_list_confirm:%d", targetID)},
			{Text: "🗑️ Hapus Grup 🗑️", CallbackData: fmt.Sprintf("ctrl:hapus_grup_confirm:%d", targetID)},
		},
		{
			{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:menu:%d", targetID)},
		},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}

	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>📋 Ringkasan List Promosi (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")

	if total == 0 {
		sb.WriteString("<blockquote>Belum ada list materi promosi tersimpan pada akun ini.</blockquote>\n\n")
		sb.WriteString("Tekan <b>📝 Tambah List 📝</b> untuk menambahkan materi baru.")
	} else {
		start := page * pageSize
		end := start + pageSize
		if end > total {
			end = total
		}
		fmt.Fprintf(&sb, "<blockquote>Total tersimpan: <b>%d materi</b> (Hal %d/%d)\n\n", total, page+1, totalPages)
		for i := start; i < end; i++ {
			if i > start {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "%d. <b>List #%d</b> — <code>Cache ID: %d</code>", i+1, i+1, lists[i])
		}
		sb.WriteString("</blockquote>")
	}

	rows := make([][]botgotd.InlineKeyboardButton, 0, 2)
	if totalPages > 1 {
		var navRow []botgotd.InlineKeyboardButton
		if page > 0 {
			navRow = append(navRow, botgotd.InlineKeyboardButton{
				Text:         "⬅️ Prev",
				CallbackData: fmt.Sprintf("ctrl:cek_list:%d:%d", targetID, page-1),
			})
		}
		if page < totalPages-1 {
			navRow = append(navRow, botgotd.InlineKeyboardButton{
				Text:         "Next ➡️",
				CallbackData: fmt.Sprintf("ctrl:cek_list:%d:%d", targetID, page+1),
			})
		}
		if len(navRow) > 0 {
			rows = append(rows, navRow)
		}
	}

	rows = append(rows, []botgotd.InlineKeyboardButton{
		{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)},
	})

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>📚 Ringkasan Grup Target (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")

	if total == 0 {
		sb.WriteString("<blockquote>Belum ada grup target siaran tersimpan pada akun ini.</blockquote>\n\n")
		sb.WriteString("Tekan <b>➕ Tambah Grup ➕</b> untuk menambahkan grup target.")
	} else {
		start := page * pageSize
		end := start + pageSize
		if end > total {
			end = total
		}
		fmt.Fprintf(&sb, "<blockquote>Total tersimpan: <b>%d grup</b> (Hal %d/%d)\n\n", total, page+1, totalPages)
		for i := start; i < end; i++ {
			if i > start {
				sb.WriteString("\n")
			}
			g := groups[i]
			title := g.Title
			if title == "" {
				title = g.Target
			}
			fmt.Fprintf(&sb, "%d. %s — <code>%s</code>", i+1, html.EscapeString(title), html.EscapeString(g.Target))
		}
		sb.WriteString("</blockquote>")
	}

	rows := make([][]botgotd.InlineKeyboardButton, 0, 2)
	if totalPages > 1 {
		var navRow []botgotd.InlineKeyboardButton
		if page > 0 {
			navRow = append(navRow, botgotd.InlineKeyboardButton{
				Text:         "⬅️ Prev",
				CallbackData: fmt.Sprintf("ctrl:cek_grup:%d:%d", targetID, page-1),
			})
		}
		if page < totalPages-1 {
			navRow = append(navRow, botgotd.InlineKeyboardButton{
				Text:         "Next ➡️",
				CallbackData: fmt.Sprintf("ctrl:cek_grup:%d:%d", targetID, page+1),
			})
		}
		if len(navRow) > 0 {
			rows = append(rows, navRow)
		}
	}

	rows = append(rows, []botgotd.InlineKeyboardButton{
		{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)},
	})

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>📝 Tambah List Promosi (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> %s (<code>%d</code>)\n\n", html.EscapeString(targetName), targetID)
	sb.WriteString("<blockquote>📌 <b>Format materi promosi yang didukung:</b>\n")
	sb.WriteString("1. <b>Teks:</b> Pesan teks biasa / berformat HTML.\n")
	sb.WriteString("2. <b>Media Single:</b> Foto, video, dokumen, audio, animasi (+ caption).\n")
	sb.WriteString("3. <b>Media Grup:</b> Album kumpulan foto atau video (+ caption).</blockquote>\n\n")
	sb.WriteString("Kirimkan materi promosi sekarang via pesan chat. Materi akan disimpan langsung ke sesi akun target.")

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔙 Batal 🔙", CallbackData: fmt.Sprintf("ctrl:cancel:%d:%d", targetID, messageID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	}
}

// RenderControlTambahGrupOptions renders options to add groups to target account
func (h *Handler) RenderControlTambahGrupOptions(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, targetID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>➕ Tambah Grup Target (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> <code>%d</code>\n\n", targetID)
	sb.WriteString("Pilih metode penambahan grup untuk akun ini:\n")
	sb.WriteString("1. <b>Ambil Semua Grup :</b> Mengimpor otomatis semua grup/channel yang sedang diikuti akun userbot target.\n")
	sb.WriteString("2. <b>Input Manual :</b> Menambahkan username/link grup secara spesifik.")

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔥 Ambil Semua Grup Akun 🔥", CallbackData: fmt.Sprintf("ctrl:fetch_all:%d", targetID)}},
		{{Text: "📝 Input Grup Manual 📝", CallbackData: fmt.Sprintf("ctrl:tambah_grup_manual:%d", targetID)}},
		{{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>📝 Tambah Grup Manual (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> %s (<code>%d</code>)\n\n", html.EscapeString(targetName), targetID)
	sb.WriteString("<blockquote>📌 <b>Format target yang didukung:</b>\n")
	sb.WriteString("1. <b>Username grup:</b> (contoh: <code>@grupchat</code>)\n")
	sb.WriteString("2. <b>Chat ID grup:</b> (contoh: <code>-1001234567890</code>)\n")
	sb.WriteString("3. <b>Tautan grup:</b> (contoh: <code>t.me/namagrup</code>)\n\n")
	sb.WriteString("<i>Jika lebih dari satu grup, pisahkan tiap grup dengan baris baru.</i></blockquote>\n\n")
	sb.WriteString("Kirimkan target grup sekarang via pesan chat, atau tekan tombol batal di bawah:")

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔙 Batal 🔙", CallbackData: fmt.Sprintf("ctrl:cancel:%d:%d", targetID, messageID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>⏳ Mengimpor Semua Grup Akun...</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "Sesi userbot target <b>%s</b> (<code>%d</code>) sedang memindai dan mengimpor seluruh grup &amp; channel di latar belakang. Laporan akan dikirim otomatis setelah proses selesai.", html.EscapeString(targetName), targetID)

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>⚠️ Konfirmasi Hapus Seluruh List</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "Apakah Anda yakin ingin menghapus <b>SEMUA</b> materi promosi pada akun target <b>%s</b> (<code>%d</code>)? Tindakan ini tidak dapat dibatalkan.", html.EscapeString(targetName), targetID)

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🗑️ Ya, Hapus Semua List 🗑️", CallbackData: fmt.Sprintf("ctrl:hapus_list_do:%d", targetID)}},
		{{Text: "🔙 Batal 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>🗑️ Seluruh List Berhasil Dihapus</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "Semua materi list promosi milik akun target <b>%s</b> (<code>%d</code>) telah dihapus secara permanen dari database.", html.EscapeString(targetName), targetID)

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>⚠️ Konfirmasi Hapus Seluruh Grup</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "Apakah Anda yakin ingin menghapus <b>SEMUA</b> grup target pada akun <b>%s</b> (<code>%d</code>)? Tindakan ini tidak dapat dibatalkan.", html.EscapeString(targetName), targetID)

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🗑️ Ya, Hapus Semua Grup 🗑️", CallbackData: fmt.Sprintf("ctrl:hapus_grup_do:%d", targetID)}},
		{{Text: "🔙 Batal 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>🗑️ Seluruh Grup Berhasil Dihapus</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "Semua grup target broadcast milik akun <b>%s</b> (<code>%d</code>) telah dihapus secara permanen dari database.", html.EscapeString(targetName), targetID)

	rows := [][]botgotd.InlineKeyboardButton{
		{{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
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

	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString("<b>🕦 Atur Jeda Broadcast (Remote)</b>\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&sb, "👑 <b>Akun Target:</b> %s (<code>%d</code>)\n", html.EscapeString(targetName), targetID)
	fmt.Fprintf(&sb, "⏳ <b>Jeda Saat Ini:</b> <code>%d detik</code>\n\n", cfg.Delay)
	sb.WriteString("<blockquote>Pilih interval jeda broadcast dari tombol di bawah:</blockquote>")

	rows := [][]botgotd.InlineKeyboardButton{
		{
			{Text: "⏱️ 3 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:3", targetID)},
			{Text: "⏱️ 5 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:5", targetID)},
			{Text: "⏱️ 10 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:10", targetID)},
		},
		{
			{Text: "⏱️ 15 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:15", targetID)},
			{Text: "⏱️ 30 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:30", targetID)},
			{Text: "⏱️ 60 Detik ⏱️", CallbackData: fmt.Sprintf("ctrl:set_jeda:%d:60", targetID)},
		},
		{
			{Text: "🔙 Kembali 🔙", CallbackData: fmt.Sprintf("ctrl:menu:%d", targetID)},
		},
	}

	inlineMarkup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID > 0 {
		_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
	} else {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
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
			text := fmt.Sprintf(`<b>⏳ Media Sedang Disimpan ke Cache (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

👑 <b>Akun Target:</b> %s (<code>%d</code>)

<blockquote>Media materi promosi sedang diproses oleh sesi userbot target.</blockquote>`, html.EscapeString(targetName), targetID)
			rows := [][]botgotd.InlineKeyboardButton{
				{{Text: "🔙 Kembali ke Grup & List 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
			}
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        text,
				ParseMode:   "HTML",
				ReplyMarkup: &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows},
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

		if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
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

		successText := fmt.Sprintf(`<b>✅ List Promosi Berhasil Dikirim (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

👑 <b>Akun Target:</b> %s (<code>%d</code>)

<blockquote>Materi promosi telah dikirimkan ke worker userbot untuk disimpan ke database target.</blockquote>`, html.EscapeString(targetName), targetID)

		rows := [][]botgotd.InlineKeyboardButton{
			{{Text: "🔙 Kembali ke Grup & List 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
		}
		if parentMsgID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        successText,
				ParseMode:   "HTML",
				ReplyMarkup: &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows},
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

		if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
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

		if message != nil {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
		}

		targetName := h.GetAccountName(targetID)
		if targetName == "" {
			targetName = fmt.Sprintf("Akun [%d]", targetID)
		}

		successText := fmt.Sprintf(`<b>✅ %d Grup Target Ditambahkan (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

👑 <b>Akun Target:</b> %s (<code>%d</code>)

<blockquote>Target grup telah berhasil disimpan ke database akun target.</blockquote>`, addedCount, html.EscapeString(targetName), targetID)

		rows := [][]botgotd.InlineKeyboardButton{
			{{Text: "🔙 Kembali ke Grup & List 🔙", CallbackData: fmt.Sprintf("ctrl:gruplist:%d", targetID)}},
		}
		if parentMsgID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        successText,
				ParseMode:   "HTML",
				ReplyMarkup: &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows},
			})
		}
		return true
	}

	if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
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
				ReplyMarkup: cachedRemoveKeyboard,
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
		if text == "🗑️ Hapus Watermark 🗑️" || strings.EqualFold(text, "hapus") {
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
	if text == "Kembali" || text == "🗑️ Hapus Semua List 🗑️" || text == "🗑️ Hapus Semua Grup 🗑️" {
		return true
	}
	if n, err := strconv.Atoi(text); err == nil && n > 0 && n <= 100 {
		return true
	}
	return false
}

func isKnownPanelButton(text string) bool {
	switch text {
	case "🌐 Home 🌐", "/menu",
		"💎 Status 💎", "/status",
		"🛒 Shop 🛒",
		"🧩 Grup & List 🧩",
		"⚡ Expert Extra ⚡",
		"🗑️ Reset 🗑️",
		"⌛ Atur Jeda ⌛",
		"⚙️ Settings ⚙️",
		"📰 Feed 📰",
		"🌐 Webapp 🌐",
		"📌 Custom Watermark 📌",
		"✉️ Atur Email ✉️",
		"⚠️ Safemode ⚠️",
		"⏱️ Timer ⏱️",
		"👑 Ubah Mode 👑",
		"👤 Admin 👤",
		"📊 Ubah Status 📊",
		"🔔 Notifikasi 🔔",
		"📁 Cek List 📁",
		"📚 Cek Grup 📚",
		"📝 Tambah List 📝",
		"📝 Tambah Grup 📝",
		"🔥 Tambahkan Semua Grup Dalam Akun 🔥",
		"📝 Tambahkan Grup Manual 📝",
		"🧑‍💻 Cek Topic 🧑‍💻", "📌 List Topic",
		"🧑‍💻 Topic 🧑‍💻",
		"📦 Sebar Ke RC 📦",
		"🗑️ Hapus Semua Grup 🗑️",
		"🗑️ Hapus Semua List 🗑️",
		"🗑️ Reset Konfigurasi 🗑️",
		"⬅️ Batal ⬅️":
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

	// Intersep tombol Control (dapat diakses siapa saja, termasuk yang belum punya userbot untuk remote akun lain)
	if text == "🎯 Control 🎯" || text == "🎮 Control 🎮" || strings.EqualFold(text, "control") || text == "/control" {
		username := ""
		if update.Message.From != nil {
			username = update.Message.From.Username
		}
		h.ShowControlMenu(ctx, b, chatID, userID, username)
		return true
	}

	// Intersep tombol Bantuan (dapat diakses siapa saja tanpa mengubah keyboard yang tidak sesuai)
	if text == "⚠️ Bantuan ⚠️" || strings.EqualFold(text, "bantuan") || text == "/help" {
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
	// Jika pesan teks biasa/ngawur, jangan diintersep (teruskan ke DefaultHandler tanpa pesan error)
	if !isKnownPanelButton(text) {
		return false
	}

	// Periksa apakah user memiliki sesi userbot yang aktif (milik sendiri atau akun yang sedang di-remote)
	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, effectiveUserID)
	}

	// Jika belum memasang userbot dan tidak sedang me-remote akun aktif, tolak akses tombol kontrol panel
	if !hasActive {
		h.setPanelState(chatID, "")
		if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "↩️ Tindakan dibatalkan.",
				ReplyMarkup: cachedCreateChoiceMarkup,
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
			ReplyMarkup: cachedCreateChoiceMarkup,
		})
		return true
	}

	// Jika server baru restart (state kosong di RAM) dan user mengklik tombol dari Cek List / Cek Grup
	// (Angka, Kembali, Hapus Semua List, Hapus Semua Grup), otomatis kembalikan ke Home
	if isRestartBrowseButton(text) {
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true
	}

	switch text {
	// Main Menu Navigation
	case "🌐 Home 🌐", "/menu":
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	case "💎 Status 💎", "/status":
		h.ShowStatus(ctx, b, chatID, effectiveUserID)
		return true

	case "🛒 Shop 🛒":
		h.ShowShop(ctx, b, chatID)
		return true

	case "🧩 Grup & List 🧩":
		h.ShowGrupList(ctx, b, chatID, effectiveUserID)
		return true

	case "⚡ Expert Extra ⚡":
		h.ShowExpertExtra(ctx, b, chatID)
		return true

	case "🗑️ Reset 🗑️":
		h.PromptReset(ctx, b, chatID)
		return true

	case "⌛ Atur Jeda ⌛":
		h.PromptAturJeda(ctx, b, chatID, effectiveUserID)
		return true

	case "⚠️ Bantuan ⚠️":
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true

	case "⚙️ Settings ⚙️":
		h.ShowSettings(ctx, b, chatID, effectiveUserID)
		return true

	case "📰 Feed 📰":
		h.ShowFeed(ctx, b, chatID)
		return true

	case "🌐 Webapp 🌐":
		h.ShowWebapp(ctx, b, chatID)
		return true

	// Status Menu Actions
	case "📌 Custom Watermark 📌":
		h.PromptWatermark(ctx, b, chatID, effectiveUserID)
		return true

	case "✉️ Atur Email ✉️":
		h.PromptEmail(ctx, b, chatID, effectiveUserID)
		return true

	case "⚠️ Safemode ⚠️":
		h.ToggleSafemode(ctx, b, chatID, effectiveUserID)
		return true

	case "⏱️ Timer ⏱️":
		h.PromptTimer(ctx, b, chatID, effectiveUserID)
		return true

	case "👑 Ubah Mode 👑":
		h.PromptUbahMode(ctx, b, chatID, effectiveUserID)
		return true

	case "👤 Admin 👤":
		h.PromptAdmin(ctx, b, chatID, effectiveUserID)
		return true

	case "📊 Ubah Status 📊":
		h.ToggleStatus(ctx, b, chatID, effectiveUserID)
		return true

	case "🔔 Notifikasi 🔔":
		h.ToggleNotifikasi(ctx, b, chatID, effectiveUserID)
		return true

	// Grup & List Actions
	case "📁 Cek List 📁":
		h.DisplayList(ctx, b, chatID, effectiveUserID)
		return true

	case "📚 Cek Grup 📚":
		h.DisplayGroup(ctx, b, chatID, effectiveUserID)
		return true

	case "📝 Tambah List 📝":
		h.PromptTambahList(ctx, b, chatID)
		return true

	case "📝 Tambah Grup 📝":
		h.PromptTambahGrup(ctx, b, chatID)
		return true

	case "🔥 Tambahkan Semua Grup Dalam Akun 🔥":
		h.FetchAllGroupsFromAccount(ctx, b, chatID, effectiveUserID)
		return true

	case "📝 Tambahkan Grup Manual 📝":
		h.PromptTambahGrupManual(ctx, b, chatID)
		return true

	case "🧑‍💻 Cek Topic 🧑‍💻", "📌 List Topic":
		h.CekTopic(ctx, b, chatID, effectiveUserID)
		return true

	case "🧑‍💻 Topic 🧑‍💻":
		h.ToggleTopic(ctx, b, chatID, effectiveUserID)
		return true

	case "📦 Sebar Ke RC 📦":
		h.SebarKeRC(ctx, b, chatID, effectiveUserID)
		return true

	case "⬅️ Batal ⬅️":
		h.setPanelState(chatID, "")
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	// Reset Menu Sub-Actions
	case "🗑️ Hapus Semua Grup 🗑️":
		_ = h.db.ClearGroups(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_groups"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>🗑️ Seluruh Grup Berhasil Dihapus</h1>
<blockquote expandable>Semua daftar grup target broadcast telah dibersihkan secara permanen dari database.</blockquote>
<hr/>
<p>GotdBot • Reset Data Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Hapus Semua List 🗑️":
		_ = h.db.ClearLists(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>🗑️ Seluruh List Berhasil Dihapus</h1>
<blockquote expandable>Semua materi list promosi telah dibersihkan secara permanen dari database.</blockquote>
<hr/>
<p>GotdBot • Reset Data Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Reset Konfigurasi 🗑️":
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
			ChatID: chatID,
			Text: `<h1>🔄 Konfigurasi Berhasil Direset</h1>
<blockquote expandable>Seluruh parameter pengaturan bot telah dipulihkan ke setelan default pabrik.</blockquote>
<hr/>
<p>GotdBot • Reset Data Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	return false
}
