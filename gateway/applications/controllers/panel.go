package controllers

import (
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
	// Main Menu Keyboard
	mainMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "💎 Status 💎"}, {Text: "🛒 Shop 🛒"}},
			{{Text: "🧩 Grup & List 🧩"}, {Text: "⚡ Expert Extra ⚡"}},
			{{Text: "🗑️ Reset 🗑️"}, {Text: "⌛ Atur Jeda ⌛"}, {Text: "⚠️ Bantuan ⚠️"}},
			{{Text: "⚙️ Settings ⚙️"}, {Text: "📰 Feed 📰"}, {Text: "🌐 Webapp 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Status Menu Keyboard
	statusMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "📌 Custom Watermark 📌"}},
			{{Text: "✉️ Atur Email ✉️"}, {Text: "⚠️ Safemode ⚠️"}},
			{{Text: "⏱️ Timer ⏱️"}, {Text: "👑 Ubah Mode 👑"}, {Text: "👤 Admin 👤"}},
			{{Text: "📊 Ubah Status 📊"}, {Text: "🔔 Notifikasi 🔔"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Grup & List Menu Keyboard
	grupListMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🧑‍💻 Cek Topic 🧑‍💻"}, {Text: "🧑‍💻 Topic 🧑‍💻"}, {Text: "📌 List Topic"}},
			{{Text: "📁 Cek List 📁"}, {Text: "📚 Cek Grup 📚"}},
			{{Text: "📝 Tambah List 📝"}, {Text: "📝 Tambah Grup 📝"}},
			{{Text: "📦 Sebar Ke RC 📦"}, {Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Tambah Grup Options Keyboard
	tambahGrupOptionsMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🔥 Tambahkan Semua Grup Dalam Akun 🔥"}},
			{{Text: "📝 Tambahkan Grup Manual 📝"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Cancel Only Keyboard
	cancelOnlyMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Atur Jeda Keyboard
	aturJedaMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "⏱️ 3 Detik"}, {Text: "⏱️ 5 Detik"}, {Text: "⏱️ 10 Detik"}},
			{{Text: "⏱️ 15 Detik"}, {Text: "⏱️ 30 Detik"}, {Text: "⏱️ 60 Detik"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Ubah Mode Keyboard
	ubahModeMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "👑 Normal"}, {Text: "⚡ Fast"}, {Text: "🔕 Silent"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Timer Keyboard
	timerMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "⏱️ Nonaktifkan Timer"}, {Text: "⏱️ Tiap 1 Jam"}},
			{{Text: "⏱️ Tiap 2 Jam"}, {Text: "⏱️ Tiap 3 Jam"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Reset Menu Keyboard
	resetMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🗑️ Hapus Semua Grup 🗑️"}, {Text: "🗑️ Hapus Semua List 🗑️"}},
			{{Text: "🗑️ Reset Konfigurasi 🗑️"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Hapus List Options Keyboard
	hapusListMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🗑️ Hapus Semua List 🗑️"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Hapus Grup Options Keyboard
	hapusGrupMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🗑️ Hapus Semua Grup 🗑️"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}
)

// ShowMainMenu renders the main control panel
func (h *Handler) ShowMainMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64, userName string) {
	h.setPanelState(chatID, "")

	if userName == "" {
		userName = "User"
	}

	text := fmt.Sprintf(`<h1>🤖 GotdBot Control Panel</h1>
<blockquote expandable>👋 <b>Halo, <a href="tg://user?id=%d">%s</a>!</b>
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
	sb.WriteString("<blockquote expandable>")
	sb.WriteString(fmt.Sprintf("📋 <b>Tindakan Terkini :</b> <i>%s</i>\n", html.EscapeString(cfg.TindakanTerkini)))
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
• <b>List :</b> <i>Materi promosi / pesan teks / media yang akan disiarkan.</i>
• <b>Grup :</b> <i>Daftar grup atau supergroup tujuan siaran.</i>
• <b>Topic :</b> <i>Thread forum grup Telegram untuk penargetan topik khusus.</i></blockquote>
<hr/>
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
	h.setPanelState(chatID, "waiting_add_list")

	text := "📝 <b>Masukkan list yang ingin ditambahkan</b>\n\n" +
		"<blockquote>ℹ️ <b>List adalah teks yang Anda promosikan.</b> <i>Format teks bebas atau caption promosi multi-baris juga didukung.</i></blockquote>\n\n" +
		"<i>Kirimkan teks pesan sekarang atau klik tombol <b>⬅️ Batal ⬅️</b> di bawah.</i>"

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTambahGrup prompts user for group adding method
func (h *Handler) PromptTambahGrup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	text := "<b>Pilih cara menambahkan grup ke list sebar</b>\n\n" +
		"<blockquote>💡 <i>Pilih otomatis untuk mengimpor seluruh grup dari akun Anda, atau manual untuk memasukkan target tertentu.</i></blockquote>"

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: tambahGrupOptionsMarkup,
	})
}

// PromptTambahGrupManual prompts user to input group usernames/IDs
func (h *Handler) PromptTambahGrupManual(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_add_group_manual")

	text := "📝 <b>Masukkan daftar grup yang ingin ditambahkan</b>\n\n" +
		"<blockquote>ℹ️ <i>Kirim username grup (contoh: <code>@grupchat</code>), ID grup (contoh: <code>-1001234567890</code>), atau tautan undangan t.me/xxx. Jika lebih dari satu, pisahkan dengan baris baru.</i></blockquote>"

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// CekList displays all saved promotional lists (summary view)
func (h *Handler) CekList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)

	if len(lists) == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<b>📋 Daftar List</b>\n━━━━━━━━━━━━━━━━━━━━\n\nBelum ada list tersimpan.\n\n<i>Gunakan ➕ Tambah List untuk menambahkan materi siaran.</i>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>📋 Daftar List</b>\n━━━━━━━━━━━━━━━━━━━━\nTotal: <code>%d</code> list tersimpan\n\n", len(lists)))
	for i, msgID := range lists {
		sb.WriteString(fmt.Sprintf("• List #%d — Cache ID <code>%d</code>\n", i+1, msgID))
		if i >= 19 {
			sb.WriteString(fmt.Sprintf("\n<i>... dan <code>%d</code> list lainnya.</i>", len(lists)-20))
			break
		}
	}

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
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "<b>📚 Daftar Grup</b>\n━━━━━━━━━━━━━━━━━━━━\n\nBelum ada grup tersimpan.\n\n<i>Gunakan ➕ Tambah Grup untuk menambahkan target siaran.</i>",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>📚 Daftar Grup Target</b>\n━━━━━━━━━━━━━━━━━━━━\nTotal: <code>%d</code> grup\n\n", len(groups)))
	for i, g := range groups {
		title := g.Title
		if title == "" {
			title = g.Target
		}
		sb.WriteString(fmt.Sprintf("%d. %s — <code>%s</code>\n", i+1, html.EscapeString(title), html.EscapeString(g.Target)))
		if i >= 24 {
			sb.WriteString(fmt.Sprintf("\n<i>... dan <code>%d</code> grup lainnya.</i>", len(groups)-25))
			break
		}
	}

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
	fmt.Fprintf(&builder, "<h1>📚 Display Grup</h1>\n<blockquote expandable>Total tersimpan: <b>%d grup</b>\n\n", len(groups))
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
	builder.WriteString("</blockquote>\n<hr/>\n<p>Pilih nomor di bawah untuk melihat detail atau menghapus grup.</p>")
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
  <tr><th>Field</th><th>Nilai</th></tr>
  <tr><td>🏷️ Nama</td><td>%s</td></tr>
  <tr><td>🎯 Target</td><td><code>%s</code></td></tr>
  <tr><td>🆔 Chat ID</td><td><code>%d</code></td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="panel:group:delete:%d" style="danger">🗑️ Hapus Grup</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Group Target Manager</p>`, index+1, html.EscapeString(title), html.EscapeString(grp.Target), grp.ChatID, index)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      richGroupText,
		ParseMode: "HTML",
	})
}

// PromptHapusList prompts for list deletion
func (h *Handler) PromptHapusList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_del_list")

	lists := h.db.GetLists(ctx, userID)
	var sb strings.Builder
	sb.WriteString("<b>🗑 Hapus List</b>\n━━━━━━━━━━━━━━━━━━━━\n")
	if len(lists) == 0 {
		sb.WriteString("\nTidak ada list yang tersimpan.")
	} else {
		sb.WriteString(fmt.Sprintf("\nTersimpan <code>%d</code> list.\n\n", len(lists)))
		sb.WriteString("Ketik nomor urut list yang ingin dihapus, atau tekan <b>Hapus Semua List</b>.")
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: hapusListMarkup,
	})
}

// PromptHapusGrup prompts for group deletion
func (h *Handler) PromptHapusGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_del_group")

	groups := h.db.GetGroups(ctx, userID)
	var sb strings.Builder
	sb.WriteString("<b>🗑 Hapus Grup</b>\n━━━━━━━━━━━━━━━━━━━━\n")
	if len(groups) == 0 {
		sb.WriteString("\nTidak ada grup yang tersimpan.")
	} else {
		sb.WriteString(fmt.Sprintf("\nTersimpan <code>%d</code> grup.\n\n", len(groups)))
		sb.WriteString("Ketik nomor urut grup yang ingin dihapus, atau tekan <b>Hapus Semua Grup</b>.")
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: hapusGrupMarkup,
	})
}

// CekTopic displays forum topics
func (h *Handler) CekTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	topics := h.db.GetTopics(ctx, userID)

	topicStatus := "Nonaktif"
	if cfg.TopicEnabled {
		topicStatus = "Aktif"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>🧵 Topic Forum</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Status:</b> <code>%s</code>\n\n", topicStatus))

	if len(topics) == 0 {
		sb.WriteString("<i>Belum ada topic terdaftar.</i>")
	} else {
		for i, t := range topics {
			sb.WriteString(fmt.Sprintf("%d. %s — Chat <code>%d</code> Thread <code>%d</code>\n", i+1, html.EscapeString(t.Title), t.ChatID, t.TopicID))
		}
	}

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

	statusStr := "Nonaktif"
	if cfg.TopicEnabled {
		statusStr = "Aktif"
	}

	text := fmt.Sprintf(
		"✅ <b>Topic diperbarui.</b>\n\nStatus: <code>%s</code>",
		statusStr,
	)

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
	sb.WriteString("<b>📦 Uji Coba Sebar ke RC</b>\n━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("<b>Cache ID:</b> <code>#%d</code>\n", lists[0]))
	if cfg.Watermark != "" {
		sb.WriteString(fmt.Sprintf("<b>Watermark:</b> <i>%s</i>\n", html.EscapeString(cfg.Watermark)))
	}
	sb.WriteString("\n<i>Materi dikirim ke Saved Messages akun Telegram Anda.</i>")

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

	text := fmt.Sprintf(
		"<b>⏱ Atur Jeda Broadcast</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Jeda saat ini:</b> <code>%d detik</code>\n\nPilih interval dari tombol di bawah, atau ketik angka detik secara langsung.",
		cfg.Delay,
	)

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

	statusStr := "Nonaktif"
	if cfg.Safemode {
		statusStr = "Aktif"
	}

	text := fmt.Sprintf(
		"✅ <b>Safemode diperbarui.</b>\n\nStatus: <code>%s</code>\n\n<i>Safemode melindungi akun dari risiko limitasi Telegram.</i>",
		statusStr,
	)

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

	text := fmt.Sprintf(
		"<b>👑 Ubah Mode Siaran</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Mode saat ini:</b> <code>%s</code>\n\n"+
			"• <b>Normal</b> — kecepatan seimbang, interval standar.\n"+
			"• <b>Fast</b> — kecepatan tinggi, minim jeda.\n"+
			"• <b>Silent</b> — tanpa memicu notifikasi suara di grup.",
		html.EscapeString(cfg.Mode),
	)

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

		payload := models.PToService{ChatID: chatID, UserID: userID, Type: "start_panel_broadcast"}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("start_panel_broadcast broker error: %v", err)
		}
		text = "🚀 <b>Siaran dimulai.</b>\n\nStatus: <code>Berjalan</code>\n\n<i>Userbot aktif mengirim materi ke seluruh grup target.</i>"
	} else {
		cfg.Status = "Berhenti"
		cfg.TindakanTerkini = "Siaran otomatis dimatikan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		payload := models.PToService{ChatID: chatID, UserID: userID, Type: "stop_panel_broadcast"}
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
	h.setPanelState(chatID, "waiting_set_watermark")
	cfg := h.db.GetUserConfig(ctx, userID)

	wm := cfg.Watermark
	if wm == "" {
		wm = "Belum diatur"
	}

	text := fmt.Sprintf(
		"<b>📌 Custom Watermark</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Saat ini:</b> <code>%s</code>\n\nKirimkan teks watermark baru. Akan disematkan di baris akhir setiap pesan siaran.\n\n<i>Ketik /cancel untuk membatalkan.</i>",
		html.EscapeString(wm),
	)

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
		em = "Belum diatur"
	}

	text := fmt.Sprintf(
		"<b>✉️ Atur Email Notifikasi</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Saat ini:</b> <code>%s</code>\n\nKirimkan alamat email yang valid.\n\n<i>Ketik /cancel untuk membatalkan.</i>",
		html.EscapeString(em),
	)

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
		adm = "Belum diatur"
	}

	text := fmt.Sprintf(
		"<b>👤 Atur Admin / Sudo</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Saat ini:</b> <code>%s</code>\n\nKirimkan User ID atau Username yang diberikan wewenang.\n\n<i>Ketik /cancel untuk membatalkan.</i>",
		html.EscapeString(adm),
	)

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

	text := fmt.Sprintf(
		"<b>⏱ Timer Siaran Otomatis</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Saat ini:</b> <code>%s</code>\n\nPilih interval pengulangan siaran berkala.",
		html.EscapeString(cfg.Timer),
	)

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

	text := "<b>🗑 Pusat Reset</b>\n━━━━━━━━━━━━━━━━━━━━\n\n" +
		"⚠️ Tindakan reset akan menghapus data dari database. Pastikan Anda yakin sebelum melanjutkan."

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
func (h *Handler) ShowBantuan(ctx context.Context, b *botgotd.Bot, chatID int64) {
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

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
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
	if state != "waiting_add_list" && state != "waiting_add_watermark" {
		return
	}
	if message.MediaGroupID != "" {
		key := fmt.Sprintf("%d:%s:%s", message.Chat.ID, state, message.MediaGroupID)
		h.mediaMu.Lock()
		batch := h.mediaBatches[key]
		if batch == nil {
			batch = &panelMediaBatch{ChatID: message.Chat.ID, UserID: message.From.ID, State: state}
			h.mediaBatches[key] = batch
		}
		batch.SourceIDs = append(batch.SourceIDs, message.ID)
		h.mediaMu.Unlock()
		time.AfterFunc(2*time.Second, func() { h.flushPanelMediaGroup(context.Background(), b, key) })
		return
	}
	h.stagePanelMedia(ctx, b, message.Chat.ID, message.From.ID, state, []int{message.ID})
}

func (h *Handler) flushPanelMediaGroup(ctx context.Context, b *botgotd.Bot, key string) {
	h.mediaMu.Lock()
	batch := h.mediaBatches[key]
	delete(h.mediaBatches, key)
	h.mediaMu.Unlock()
	if batch == nil {
		return
	}
	h.stagePanelMedia(ctx, b, batch.ChatID, batch.UserID, batch.State, batch.SourceIDs)
}

func (h *Handler) stagePanelMedia(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state string, sourceIDs []int) {
	jobType := "cache_list"
	if state == "waiting_add_watermark" {
		jobType = "cache_watermark"
	}
	h.setPanelState(chatID, "")
	if err := h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: jobType, MediaSourceCount: len(sourceIDs)}); err != nil {
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

	if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "↩️ Tindakan dibatalkan.",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	switch state {
	case "waiting_add_list":
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
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Berhasil.</b> Ditambahkan <code>%d</code> grup. Total: <code>%d</code> grup.", addedCount, gCount),
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
			h.ShowGrupList(ctx, b, chatID, userID)
			return true
		}
		if idx, err := strconv.Atoi(text); err == nil && idx > 0 {
			groups := h.db.GetGroups(ctx, userID)
			if idx <= len(groups) {
				grp := groups[idx-1]
				title := strings.TrimSpace(grp.Title)
				if title == "" {
					title = grp.Target
				}
				detailText := fmt.Sprintf(
					"<b>📌 Detail Grup #%d</b>\n━━━━━━━━━━━━━━━━━━━━\n<b>Nama:</b> %s\n<b>Target:</b> <code>%s</code>\n<b>Chat ID:</b> <code>%d</code>",
					idx, html.EscapeString(title), html.EscapeString(grp.Target), grp.ChatID,
				)
				markup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: [][]botgotd.InlineKeyboardButton{
					{{Text: "🗑 Hapus Grup", CallbackData: fmt.Sprintf("panel:group:delete:%d", idx-1)}},
					{{Text: "← Kembali ke Grup", CallbackData: "panel:group:back"}},
				}}
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:    chatID,
					Text:      detailText,
					ParseMode: "HTML",
					ReplyMarkup: markup,
				})
			}
			return true
		}
		return true

	case "waiting_del_list":
		h.setPanelState(chatID, "")
		if text == "🗑️ Hapus Semua List 🗑️" {
			_ = h.db.ClearLists(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Semua list dihapus.</b>",
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
					ChatID:      chatID,
					Text:        fmt.Sprintf("✅ <b>List #%d dihapus.</b>", idx),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Nomor list tidak valid.",
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "waiting_del_group":
		h.setPanelState(chatID, "")
		if text == "🗑️ Hapus Semua Grup 🗑️" {
			_ = h.db.ClearGroups(ctx, userID)
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Semua grup dihapus.</b>",
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
					ChatID:      chatID,
					Text:        fmt.Sprintf("✅ <b>Grup #%d (%s) dihapus.</b>", idx, html.EscapeString(groups[idx-1].Target)),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ Nomor grup tidak valid.",
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
				ChatID:      chatID,
				Text:        "✅ <b>Email dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Email = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Email diatur ke <code>%s</code>.</b>", html.EscapeString(text)),
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
				ChatID:      chatID,
				Text:        "✅ <b>Admin dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Admin = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Admin diatur ke <code>%s</code>.</b>", html.EscapeString(text)),
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
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Timer diatur ke <code>%s</code>.</b>", html.EscapeString(cfg.Timer)),
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
	text := strings.TrimSpace(update.Message.Text)

	// Cek apakah user sedang berada dalam input state
	if h.HandlePanelInput(ctx, b, chatID, userID, text, update.Message) {
		return true
	}

	switch text {
	// Main Menu Navigation
	case "🌐 Home 🌐", "/menu":
		h.ShowMainMenu(ctx, b, chatID, userID, userName)
		return true

	case "💎 Status 💎", "/status":
		h.ShowStatus(ctx, b, chatID, userID)
		return true

	case "🛒 Shop 🛒":
		h.ShowShop(ctx, b, chatID)
		return true

	case "🧩 Grup & List 🧩":
		h.ShowGrupList(ctx, b, chatID, userID)
		return true

	case "⚡ Expert Extra ⚡":
		h.ShowExpertExtra(ctx, b, chatID)
		return true

	case "🗑️ Reset 🗑️":
		h.PromptReset(ctx, b, chatID)
		return true

	case "⌛ Atur Jeda ⌛":
		h.PromptAturJeda(ctx, b, chatID, userID)
		return true

	case "⚠️ Bantuan ⚠️":
		h.ShowBantuan(ctx, b, chatID)
		return true

	case "⚙️ Settings ⚙️":
		h.ShowSettings(ctx, b, chatID, userID)
		return true

	case "📰 Feed 📰":
		h.ShowFeed(ctx, b, chatID)
		return true

	case "🌐 Webapp 🌐":
		h.ShowWebapp(ctx, b, chatID)
		return true

	// Status Menu Actions
	case "📌 Custom Watermark 📌":
		h.PromptWatermark(ctx, b, chatID, userID)
		return true

	case "✉️ Atur Email ✉️":
		h.PromptEmail(ctx, b, chatID, userID)
		return true

	case "⚠️ Safemode ⚠️":
		h.ToggleSafemode(ctx, b, chatID, userID)
		return true

	case "⏱️ Timer ⏱️":
		h.PromptTimer(ctx, b, chatID, userID)
		return true

	case "👑 Ubah Mode 👑":
		h.PromptUbahMode(ctx, b, chatID, userID)
		return true

	case "👤 Admin 👤":
		h.PromptAdmin(ctx, b, chatID, userID)
		return true

	case "📊 Ubah Status 📊":
		h.ToggleStatus(ctx, b, chatID, userID)
		return true

	case "🔔 Notifikasi 🔔":
		h.ToggleNotifikasi(ctx, b, chatID, userID)
		return true

	// Grup & List Actions
	case "📁 Cek List 📁":
		h.DisplayList(ctx, b, chatID, userID)
		return true

	case "📚 Cek Grup 📚":
		h.DisplayGroup(ctx, b, chatID, userID)
		return true

	case "📝 Tambah List 📝":
		h.PromptTambahList(ctx, b, chatID)
		return true

	case "📝 Tambah Grup 📝":
		h.PromptTambahGrup(ctx, b, chatID)
		return true

	case "🔥 Tambahkan Semua Grup Dalam Akun 🔥":
		h.FetchAllGroupsFromAccount(ctx, b, chatID, userID)
		return true

	case "📝 Tambahkan Grup Manual 📝":
		h.PromptTambahGrupManual(ctx, b, chatID)
		return true

	case "🧑‍💻 Cek Topic 🧑‍💻", "📌 List Topic":
		h.CekTopic(ctx, b, chatID, userID)
		return true

	case "🧑‍💻 Topic 🧑‍💻":
		h.ToggleTopic(ctx, b, chatID, userID)
		return true

	case "📦 Sebar Ke RC 📦":
		h.SebarKeRC(ctx, b, chatID, userID)
		return true

	case "⬅️ Batal ⬅️":
		h.setPanelState(chatID, "")
		h.ShowMainMenu(ctx, b, chatID, userID, userName)
		return true

	// Reset Menu Sub-Actions
	case "🗑️ Hapus Semua Grup 🗑️":
		_ = h.db.ClearGroups(ctx, userID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_groups"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Semua grup dihapus dari database.</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Hapus Semua List 🗑️":
		_ = h.db.ClearLists(ctx, userID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Semua list dihapus dari database.</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Reset Konfigurasi 🗑️":
		defaultCfg := &databases.UserConfig{
			OwnerID:          userID,
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
			Text:        "✅ <b>Konfigurasi direset ke pengaturan default.</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	return false
}
