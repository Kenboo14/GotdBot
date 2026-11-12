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
	// Main Menu Keyboard (From Image 1)
	mainMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "💎 Status 💎"}, {Text: "🛒 Shop 🛒"}},
			{{Text: "🧩 Grup & List 🧩"}, {Text: "⚡ Expert Extra ⚡"}},
			{{Text: "🗑️ Reset 🗑️"}, {Text: "⌛ Atur Jeda ⌛"}, {Text: "⚠️ Bantuan ⚠️"}},
			{{Text: "⚙️ Settings ⚙️"}, {Text: "📰 Feed 📰"}, {Text: "🌐 Webapp 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Status Menu Keyboard (From Image 2)
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

	// Grup & List Menu Keyboard (From Image 3)
	grupListMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🧑‍💻 Cek Topic 🧑‍💻"}, {Text: "🧑‍💻 Topic 🧑‍💻"}, {Text: "📌 List Topic"}},
			{{Text: "📁 Cek List 📁"}, {Text: "📚 Cek Grup 📚"}},
			{{Text: "📝 Tambah List 📝"}, {Text: "📝 Tambah Grup 📝"}},
			{{Text: "📦 Sebar Ke RC 📦"}, {Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Tambah Grup Options Keyboard (From Image 5)
	tambahGrupOptionsMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: "🔥 Tambahkan Semua Grup Dalam Akun 🔥"}},
			{{Text: "📝 Tambahkan Grup Manual 📝"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Cancel Only Keyboard (From Image 4: "⬅️ Batal ⬅️")
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

// ShowMainMenu renders the active bot control panel using RichText
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

// ShowStatus renders the exact status layout from Image 2 in RichText
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
	sb.WriteString("<h1>💎 Status Userbot</h1>\n<blockquote expandable>")
	sb.WriteString(fmt.Sprintf("🏷️ <b>UserID :</b> <code>%d</code>\n", cfg.OwnerID))
	sb.WriteString(fmt.Sprintf("🧩 <b>Tipe :</b> <code>%s</code>\n", html.EscapeString(cfg.Type)))
	sb.WriteString(fmt.Sprintf("🕒 <b>Jeda :</b> <code>%d detik · antar-grup</code>\n", cfg.Delay))
	sb.WriteString(fmt.Sprintf("⏭️ <b>Skip FloodWait :</b> <code>%s</code>\n", skipFw))
	sb.WriteString(fmt.Sprintf("⏱️ <b>Timer :</b> <code>%s</code>\n", html.EscapeString(cfg.Timer)))
	sb.WriteString(fmt.Sprintf("⚡ <b>Extra :</b> <code>%d</code>\n", cfg.Extra))
	sb.WriteString(fmt.Sprintf("🌐 <b>Grup :</b> <code>%d</code>\n", gCount))
	sb.WriteString(fmt.Sprintf("📚 <b>List :</b> <code>%d</code>\n", lCount))
	sb.WriteString(fmt.Sprintf("👑 <b>Mode :</b> <code>%s</code>\n", html.EscapeString(cfg.Mode)))
	sb.WriteString(fmt.Sprintf("⚠️ <b>Safemode :</b> <code>%s</code>\n", safeMode))
	sb.WriteString(fmt.Sprintf("🚀 <b>Status :</b> <code>%s</code>\n", html.EscapeString(cfg.Status)))
	sb.WriteString(fmt.Sprintf("🔥 <b>Masa aktif :</b> <code>%s</code>\n", html.EscapeString(cfg.MasaAktif)))
	sb.WriteString(fmt.Sprintf("🔔 <b>Notifikasi :</b> <code>%s</code>\n", html.EscapeString(cfg.Notifikasi)))
	sb.WriteString(fmt.Sprintf("✉️ <b>Email :</b> <code>%s</code>\n", html.EscapeString(cfg.Email)))
	sb.WriteString(fmt.Sprintf("👤 <b>Admin :</b> <code>%s</code>\n", html.EscapeString(cfg.Admin)))
	sb.WriteString(fmt.Sprintf("📋 <b>Tindakan Terkini :</b> <i>%s</i>\n", html.EscapeString(cfg.TindakanTerkini)))
	sb.WriteString(fmt.Sprintf("📄 <b>Tindakan Terakhir :</b> <i>%s</i>\n", html.EscapeString(cfg.TindakanTerakhir)))
	sb.WriteString("</blockquote>\n<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:status\" style=\"primary\">🔄 Refresh Status</tg-button>\n</tg-button-row>\n<hr/>\n<p>GotdBot • Userbot Status Dashboard</p>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// ShowGrupList renders the Grup & List menu from Image 3
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

// PromptTambahList prompts user to enter broadcast list text (Image 4)
func (h *Handler) PromptTambahList(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_add_list")

	var sb strings.Builder
	sb.WriteString("📝 <b>Masukkan list yang ingin ditambahkan</b>\n\n")
	sb.WriteString("<blockquote>ℹ️ <b>List adalah teks yang Anda promosikan.</b> <i>Format teks bebas atau caption promosi multi-baris juga didukung.</i></blockquote>\n\n")
	sb.WriteString("<i>Kirimkan teks pesan sekarang atau klik tombol <b>Batal</b> di bawah.</i>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTambahGrup prompts user for group adding method (Image 5)
func (h *Handler) PromptTambahGrup(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.WriteString("<b>Pilih cara menambahkan grup ke list sebar</b>\n\n")
	sb.WriteString("<blockquote>💡 <i>Pilih otomatis untuk mengimpor seluruh grup dari akun Anda, atau manual untuk memasukkan target tertentu.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: tambahGrupOptionsMarkup,
	})
}

// PromptTambahGrupManual prompts user to input group usernames/IDs
func (h *Handler) PromptTambahGrupManual(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_add_group_manual")

	var sb strings.Builder
	sb.WriteString("📝 <b>Masukkan daftar grup yang ingin ditambahkan</b>\n\n")
	sb.WriteString("<blockquote>ℹ️ <i>Kirim username grup (contoh: <code>@grupchat</code>), ID grup (contoh: <code>-1001234567890</code>), atau tautan undangan t.me/xxx. Jika lebih dari satu, pisahkan dengan baris baru.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// CekList displays all saved promotional lists
func (h *Handler) CekList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)

	if len(lists) == 0 {
		var sb strings.Builder
		sb.WriteString("📁 <b>Daftar List Pesan Promosi</b>\n\n")
		sb.WriteString("<blockquote>ℹ️ <i>Belum ada list yang tersimpan. Gunakan tombol <b>📝 Tambah List 📝</b> untuk menambahkan teks materi promosi Anda. Pesan akan disimpan aman di cache cloud (@AiotdCaches).</i></blockquote>")

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📁 <b>Daftar List Pesan Promosi</b> (Total: <code>%d</code> list tersimpan)\n\n", len(lists)))
	for i, msgID := range lists {
		sb.WriteString(fmt.Sprintf("<blockquote><b>List #%d:</b>\n<i>Tersimpan di Cache AiotdCaches:</i> <code>Message ID #%d</code> (Topic: <code>3325</code>)</blockquote>\n\n", i+1, msgID))
		if i >= 19 {
			sb.WriteString(fmt.Sprintf("<i>...dan <code>%d</code> list lainnya.</i>\n", len(lists)-20))
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

// CekGrup displays all registered broadcast groups
func (h *Handler) CekGrup(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	groups := h.db.GetGroups(ctx, userID)

	if len(groups) == 0 {
		var sb strings.Builder
		sb.WriteString("📚 <b>Daftar Grup Sebar</b>\n\n")
		sb.WriteString("<blockquote>ℹ️ <i>Belum ada grup yang tersimpan di list sebar. Gunakan tombol <b>📝 Tambah Grup 📝</b> untuk menambahkan target grup.</i></blockquote>")

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 <b>Daftar Grup Sebar</b> (Total: <code>%d</code> grup)\n\n", len(groups)))
	for i, g := range groups {
		title := g.Title
		if title == "" {
			title = g.Target
		}
		sb.WriteString(fmt.Sprintf("<b>%d.</b> %s (<code>%s</code>)\n", i+1, html.EscapeString(title), html.EscapeString(g.Target)))
		if i >= 24 {
			sb.WriteString(fmt.Sprintf("\n<i>...dan <code>%d</code> grup lainnya.</i>\n", len(groups)-25))
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
	return browseKeyboard(count, "Hapus Semua List")
}
func browseGroupMarkup(count int) *botgotd.ReplyKeyboardMarkup {
	return browseKeyboard(count, "Hapus Semua Grup")
}

func (h *Handler) DisplayList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	if len(lists) == 0 {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "📁 <b>List kosong.</b>\n\nBelum ada list yang tersimpan.", ParseMode: "HTML", ReplyMarkup: grupListMenuMarkup})
		return
	}
	h.setPanelState(chatID, "browse_list")
	payload := models.PToService{ChatID: chatID, UserID: userID, Type: "get_list_previews", TaskID: strconv.Itoa(len(lists))}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Preview List tidak dapat diminta dari userbot.", ReplyMarkup: browseListMarkup(len(lists))})
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "❌ Detail list tidak dapat diminta dari userbot.", ReplyMarkup: browseListMarkup(len(lists))})
	}
}

func (h *Handler) showGroupDetail(ctx context.Context, b *botgotd.Bot, chatID, userID int64, index int) {
	groups := h.db.GetGroups(ctx, userID)
	if index < 0 || index >= len(groups) {
		h.DisplayGroup(ctx, b, chatID, userID)
		return
	}
	g := groups[index]
	title := g.Title
	if title == "" {
		title = g.Target
	}
	text := fmt.Sprintf("📚 <b>Detail Grup #%d</b>\n\n<b>Nama:</b> %s\n<b>Target:</b> <code>%s</code>\n<b>Chat ID:</b> <code>%d</code>", index+1, html.EscapeString(title), html.EscapeString(g.Target), g.ChatID)
	markup := &botgotd.InlineKeyboardMarkup{InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "🗑️ Hapus Grup", CallbackData: fmt.Sprintf("panel:group:delete:%d", index)}}}}
	b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: text, ParseMode: "HTML", ReplyMarkup: markup})
}

// PromptHapusList prompts for list deletion
func (h *Handler) PromptHapusList(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_del_list")

	lists := h.db.GetLists(ctx, userID)
	var sb strings.Builder
	sb.WriteString("♨️ <b>Hapus List Promosi</b>\n\n")
	if len(lists) == 0 {
		sb.WriteString("<blockquote>ℹ️ <i>Tidak ada list yang tersimpan saat ini.</i></blockquote>")
	} else {
		sb.WriteString(fmt.Sprintf("Tersimpan <code>%d</code> list materi promosi.\n\n", len(lists)))
		sb.WriteString("<blockquote><i>Ketik nomor urut list (misal: <code>1</code> atau <code>2</code>) yang ingin dihapus, atau tekan <b>Hapus Semua List</b> di bawah.</i></blockquote>")
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
	sb.WriteString("♨️ <b>Hapus Target Grup</b>\n\n")
	if len(groups) == 0 {
		sb.WriteString("<blockquote>ℹ️ <i>Tidak ada grup yang tersimpan saat ini.</i></blockquote>")
	} else {
		sb.WriteString(fmt.Sprintf("Tersimpan <code>%d</code> target grup sebar.\n\n", len(groups)))
		sb.WriteString("<blockquote><i>Ketik nomor urut grup (misal: <code>1</code>) yang ingin dihapus, atau tekan <b>Hapus Semua Grup</b> di bawah.</i></blockquote>")
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
	sb.WriteString("📌 <b>Daftar Topic Forum Terdaftar</b>\n\n")
	sb.WriteString(fmt.Sprintf("Status Topic Global : <code>%s</code>\n\n", topicStatus))

	if len(topics) == 0 {
		sb.WriteString("<blockquote>ℹ️ <i>Belum ada topic forum yang terdaftar secara khusus.</i></blockquote>")
	} else {
		for i, t := range topics {
			sb.WriteString(fmt.Sprintf("<b>%d.</b> %s (Chat: <code>%d</code> | Thread: <code>%d</code>)\n", i+1, html.EscapeString(t.Title), t.ChatID, t.TopicID))
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

	var sb strings.Builder
	sb.WriteString("🧑‍💻 <b>Status Topic Forum Diperbarui!</b>\n\n")
	sb.WriteString(fmt.Sprintf("Status Topic Saat Ini : <code>%s</code>\n\n", statusStr))
	sb.WriteString("<blockquote><i>Ketika aktif, pesan broadcast akan diarahkan khusus ke forum topic thread yang terdaftar.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// SebarKeRC sends test broadcast to Saved Messages (RC)
func (h *Handler) SebarKeRC(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("📦 <b>Uji Coba Sebar Ke Root Chat (RC)</b>\n\n")

	if len(lists) == 0 {
		sb.WriteString("<blockquote>⚠️ <i>Belum ada list materi promosi yang tersimpan! Tambahkan list terlebih dahulu sebelum melakukan uji coba.</i></blockquote>")
	} else {
		payload := models.PToService{
			ChatID: chatID,
			UserID: userID,
			Type:   "test_rc_broadcast",
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("test_rc_broadcast broker error: %v", err)
		}

		sb.WriteString("<blockquote>🚀 <b>Status Uji Coba :</b> <code>Sedang Diproses oleh Userbot</code>\n")
		sb.WriteString(fmt.Sprintf("<b>Target Cache ID :</b> <code>#%d</code> (Topic: <code>3325</code>)\n", lists[0]))
		if cfg.Watermark != "" {
			sb.WriteString(fmt.Sprintf("<b>Watermark :</b> <i>%s</i>\n", html.EscapeString(cfg.Watermark)))
		}
		sb.WriteString("</blockquote>\n<i>Materi sedang dikirimkan ke Saved Messages akun Telegram Anda.</i>")
	}

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

	var sb strings.Builder
	sb.WriteString("⌛ <b>Pengaturan Jeda Broadcast Antar-Grup</b>\n\n")
	sb.WriteString(fmt.Sprintf("Jeda saat ini: <code>%d detik · antar-grup</code>\n\n", cfg.Delay))
	sb.WriteString("<blockquote><i>Pilih tombol interval penundaan di bawah atau ketik langsung angka detik yang Anda inginkan (contoh: <code>8</code>).</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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

	var sb strings.Builder
	sb.WriteString("⚠️ <b>Status Safemode Diperbarui!</b>\n\n")
	sb.WriteString(fmt.Sprintf("Status Safemode : <code>%s</code>\n\n", statusStr))
	sb.WriteString("<blockquote>🛡️ <i>Safemode menyaring pengiriman berkecepatan tinggi dan otomatis melompat saat terdeteksi risiko pembatasan spam Telegram.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// PromptUbahMode displays mode options
func (h *Handler) PromptUbahMode(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_mode")
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("👑 <b>Ubah Mode Siaran Userbot</b>\n\n")
	sb.WriteString(fmt.Sprintf("Mode saat ini: <code>%s</code>\n\n", html.EscapeString(cfg.Mode)))
	sb.WriteString("<blockquote>• <b>Normal :</b> <i>Kecepatan seimbang dengan proteksi interval standar.</i>\n")
	sb.WriteString("• <b>Fast :</b> <i>Kecepatan tinggi untuk kebutuhan promosi cepat.</i>\n")
	sb.WriteString("• <b>Silent :</b> <i>Pengiriman tanpa memicu notifikasi suara di grup penerima.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: ubahModeMarkup,
	})
}

// ToggleStatus toggles userbot broadcast status
func (h *Handler) ToggleStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	cfg := h.db.GetUserConfig(ctx, userID)
	if strings.HasPrefix(cfg.Status, "Berhenti") {
		cfg.Status = "Berjalan"
		cfg.TindakanTerkini = "Siaran otomatis diaktifkan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		// Picu broadcast di ubot via Redis
		payload := models.PToService{
			ChatID: chatID,
			UserID: userID,
			Type:   "start_panel_broadcast",
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("start_panel_broadcast broker error: %v", err)
		}
	} else {
		cfg.Status = "Berhenti"
		cfg.TindakanTerkini = "Siaran otomatis dimatikan"
		cfg.TindakanTerakhir = time.Now().Format("02 Jan 2006 15:04")
		_ = h.db.SaveUserConfig(ctx, cfg)

		// Hentikan broadcast di ubot via Redis
		payload := models.PToService{
			ChatID: chatID,
			UserID: userID,
			Type:   "stop_panel_broadcast",
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("stop_panel_broadcast broker error: %v", err)
		}
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>Status Siaran Otomatis Diperbarui!</b>\n\n")
	sb.WriteString(fmt.Sprintf("Status Saat Ini : <code>%s</code>\n", html.EscapeString(cfg.Status)))
	sb.WriteString(fmt.Sprintf("Tindakan Terkini : <i>%s</i>\n\n", html.EscapeString(cfg.TindakanTerkini)))
	if cfg.Status == "Berjalan" {
		sb.WriteString("<blockquote>🚀 <i>Siaran otomatis telah dimulai! Userbot aktif mengirim materi promosi dari cache ke seluruh grup target sesuai jeda yang dikonfigurasi.</i></blockquote>")
	} else {
		sb.WriteString("<blockquote>🛑 <i>Siaran otomatis telah dihentikan. Seluruh antrean pengiriman dinonaktifkan.</i></blockquote>")
	}

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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

	var sb strings.Builder
	sb.WriteString("🔔 <b>Pengaturan Notifikasi Diperbarui!</b>\n\n")
	sb.WriteString(fmt.Sprintf("Notifikasi : <code>%s</code>\n\n", html.EscapeString(cfg.Notifikasi)))
	sb.WriteString("<blockquote>📬 <i>Laporan pengiriman siaran akan dikirimkan langsung ke chat bot ini secara real-time.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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
		wm = "(Belum diatur)"
	}

	var sb strings.Builder
	sb.WriteString("📌 <b>Pengaturan Custom Watermark</b>\n\n")
	sb.WriteString(fmt.Sprintf("Watermark saat ini: <code>%s</code>\n\n", html.EscapeString(wm)))
	sb.WriteString("<blockquote><i>Kirimkan teks watermark baru yang akan otomatis disematkan di baris akhir setiap pesan siaran, atau klik <b>Batal</b>.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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

	var sb strings.Builder
	sb.WriteString("✉️ <b>Pengaturan Email Notifikasi</b>\n\n")
	sb.WriteString(fmt.Sprintf("Email saat ini: <code>%s</code>\n\n", html.EscapeString(em)))
	sb.WriteString("<blockquote><i>Kirimkan alamat email yang valid untuk menerima laporan siaran darurat dan backup akun, atau klik <b>Batal</b>.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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

	var sb strings.Builder
	sb.WriteString("👤 <b>Pengaturan Admin / Sudo</b>\n\n")
	sb.WriteString(fmt.Sprintf("Admin saat ini: <code>%s</code>\n\n", html.EscapeString(adm)))
	sb.WriteString("<blockquote><i>Kirimkan User ID atau Username pengguna yang diberikan wewenang mengelola siaran bot ini, atau klik <b>Batal</b>.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTimer prompts user for timer
func (h *Handler) PromptTimer(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_timer")
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("⏱️ <b>Pengaturan Timer Siaran Otomatis</b>\n\n")
	sb.WriteString(fmt.Sprintf("Timer saat ini: <code>%s</code>\n\n", html.EscapeString(cfg.Timer)))
	sb.WriteString("<blockquote><i>Pilih interval pengulangan siaran berkala di bawah atau ketik format interval Anda sendiri.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: timerMarkup,
	})
}

// PromptReset prompts user for reset options
func (h *Handler) PromptReset(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.WriteString("🗑️ <b>Pusat Reset Data</b>\n\n")
	sb.WriteString("<blockquote>⚠️ <b>Perhatian:</b> <i>Tindakan reset akan menghapus data yang dipilih dari MongoDB dan SQLite. Pastikan Anda telah memeriksa kembali sebelum melanjutkan.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: resetMarkup,
	})
}

// ShowShop displays Shop info
func (h *Handler) ShowShop(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("🛒 <b>GotdBot Store &amp; Layanan Premium</b>\n\n")
	sb.WriteString("<blockquote>💎 <b>Daftar Layanan Tersedia:</b>\n")
	sb.WriteString("• <b>VIP Userbot :</b> <i>Masa aktif tanpa batas waktu &amp; prioritas koneksi MTProto.</i>\n")
	sb.WriteString("• <b>Multi-Account Hub :</b> <i>Koneksikan hingga puluhan nomor Telegram secara paralel.</i>\n")
	sb.WriteString("• <b>Dedicated Instance :</b> <i>Private bot gateway eksklusif untuk bisnis Anda.</i></blockquote>\n\n")
	sb.WriteString("<i>Untuk konsultasi atau upgrade paket, silakan hubungi admin botgotd.</i>")

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
<blockquote expandable>🛠️ <b>Fitur Tingkat Lanjut:</b>
• <b>Smart FloodWait Bypass :</b> <i>Otomatis mengatur antrean jeda adaptif saat Telegram mengirim sinyal FloodWait.</i>
• <b>Anti-Duplicate Filter :</b> <i>Mencegah siaran ganda ke dialog yang sama dalam satu putaran.</i>
• <b>Pure-Go MTProto Architecture :</b> <i>Performa tinggi full gotd/td dengan SQLite WAL lokal &amp; MongoDB Disaster Recovery.</i></blockquote>
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
<blockquote expandable>📘 <b>Navigasi Tombol Panel:</b>
• <b>💎 Status :</b> <i>Melihat ringkasan lengkap data dan konfigurasi aktif akun.</i>
• <b>🧩 Grup &amp; List :</b> <i>Mengatur materi teks promosi, target grup, dan topic forum.</i>
• <b>⌛ Atur Jeda :</b> <i>Mengubah jeda pengiriman antar grup.</i>
• <b>⚠️ Safemode :</b> <i>Melindungi akun dari limitasi akun Telegram.</i></blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="assist_help:home" style="primary">📖 Menu Bantuan Lengkap</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Multi-configuration VPS control panel</p>`

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowSettings displays Settings info
func (h *Handler) ShowSettings(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.ShowStatus(ctx, b, chatID, userID)
}

// ShowFeed displays updates feed
func (h *Handler) ShowFeed(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("📰 <b>Informasi &amp; Berita GotdBot</b>\n\n")
	sb.WriteString("<blockquote>🚀 <b>Versi Terpasang :</b> <code>v2.5.0-HighPerformance</code>\n")
	sb.WriteString("• <i>Sinkronisasi real-time MongoDB Cloud Atlas &amp; SQLite lokal aktif.</i>\n")
	sb.WriteString("• <i>Dual-layer messaging MTProto + Bot API stabil.</i>\n")
	sb.WriteString("• <i>Manajemen grup &amp; list sebar terintegrasi.</i></blockquote>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowWebapp displays Webapp info
func (h *Handler) ShowWebapp(ctx context.Context, b *botgotd.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("🌐 <b>GotdBot Web Dashboard</b>\n\n")
	sb.WriteString("<blockquote><i>Akses panel kendali visual untuk memantau performa siaran secara langsung melalui browser atau Telegram Mini App.</i></blockquote>\n\n")
	sb.WriteString("<i>Integrasi Web Dashboard sedang dalam tahap penyempurnaan cloud.</i>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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
		// Media yang datang di luar sesi input panel diabaikan. Jangan kirim
		// pesan tambahan karena update album dapat tiba setelah state selesai.
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
	// Relay hanya ke private chat pemilik agar userbot dapat mengambilnya
	// melalui MTProto. Pengiriman ke AiotdCaches dilakukan oleh userbot.
	h.setPanelState(chatID, "")
	if err := h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: jobType, MediaSourceCount: len(sourceIDs)}); err != nil {
		log.Printf("media cache broker error: %v", err)
		return
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{ChatID: chatID, Text: "⏳ <b>Sedang mengamankan media ke cache...</b>", ParseMode: "HTML", ReplyMarkup: grupListMenuMarkup})
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
			Text:        "<blockquote>↩️ <b>Tindakan dibatalkan.</b></blockquote>",
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
				Text:        "❌ <b>Gagal Menyimpan List:</b> <i>Layanan broker userbot tidak merespon.</i>",
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⏳ <b>Sedang mengamankan materi list ke cache...</b>\n\n<blockquote><i>Userbot sedang me-reply teks promosi ke @AiotdCaches (Topic 3325).</i></blockquote>",
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
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("✅ <b>Berhasil Menambahkan <code>%d</code> Target Grup!</b>\n\n", addedCount))
		sb.WriteString(fmt.Sprintf("Total target grup sebar saat ini: <code>%d</code>", gCount))

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "browse_list", "list_detail":
		if text == "Hapus Semua List" {
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
		if text == "Hapus Semua Grup" {
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
				richGroupText := fmt.Sprintf(`<h1>📚 Detail Grup #%d</h1>
<p><b>Nama:</b> %s<br/><b>Target:</b> <code>%s</code><br/><b>Chat ID:</b> <code>%d</code></p>
<tg-button-row>
  <tg-button type="callback_data" data="panel:group:delete:%d" style="danger">🗑️ Hapus Grup</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Group Target Manager</p>`, idx, html.EscapeString(title), html.EscapeString(grp.Target), grp.ChatID, idx-1)

				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:    chatID,
					Text:      richGroupText,
					ParseMode: "HTML",
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
				Text:        "✅ <b>Seluruh list materi promosi telah dibersihkan.</b>",
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
					Text:        fmt.Sprintf("✅ <b>List #%d berhasil dihapus.</b>", idx),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <i>Nomor list tidak valid. Operasi dibatalkan.</i>",
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
				Text:        "✅ <b>Seluruh target grup sebar telah dibersihkan.</b>",
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
					Text:        fmt.Sprintf("✅ <b>Target grup #%d (%s) berhasil dihapus.</b>", idx, html.EscapeString(groups[idx-1].Target)),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <i>Nomor grup tidak valid. Operasi dibatalkan.</i>",
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
				Text:        fmt.Sprintf("✅ <b>Jeda broadcast berhasil diatur ke <code>%d detik</code>.</b>", sec),
				ParseMode:   "HTML",
				ReplyMarkup: mainMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <i>Nilai jeda tidak valid. Masukkan angka detik yang benar.</i>",
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
				Text:        fmt.Sprintf("✅ <b>Mode siaran berhasil diubah ke <code>%s</code>.</b>", mode),
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <i>Pilihan mode tidak dikenali.</i>",
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
				Text:        "✅ <b>Custom watermark berhasil dihapus.</b>",
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
				Text:        "❌ <b>Gagal Menyimpan Watermark:</b> <i>Layanan broker userbot tidak merespon.</i>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⏳ <b>Sedang mengamankan custom watermark ke cache...</b>\n\n<blockquote><i>Userbot sedang me-reply teks watermark ke @AiotdCaches (Topic 6847).</i></blockquote>",
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
				Text:        "✅ <b>Email notifikasi berhasil dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Email = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Email notifikasi berhasil diatur ke <code>%s</code>.</b>", html.EscapeString(text)),
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
				Text:        "✅ <b>Admin berhasil dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Admin = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ <b>Admin berhasil diatur ke <code>%s</code>.</b>", html.EscapeString(text)),
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
			Text:        fmt.Sprintf("✅ <b>Timer siaran otomatis diatur ke <code>%s</code>.</b>", html.EscapeString(cfg.Timer)),
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true
	}

	return false
}

// FetchAllGroupsFromAccount communicates with ubot via Redis to import all groups
func (h *Handler) FetchAllGroupsFromAccount(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	var sb strings.Builder
	sb.WriteString("⏳ <b>Sedang Mengimpor Seluruh Grup Akun...</b>\n\n")
	sb.WriteString("<blockquote><i>Menghubungi engine userbot untuk membaca seluruh grup dan supergroup yang diikuti akun Telegram Anda.</i></blockquote>\n\n")
	sb.WriteString("<i>Proses ini berjalan di latar belakang. Anda akan menerima laporan otomatis begitu sinkronisasi selesai.</i>")

	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
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
			Text:        "✅ <b>Seluruh target grup telah dibersihkan dari database (MongoDB &amp; SQLite).</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Hapus Semua List 🗑️":
		_ = h.db.ClearLists(ctx, userID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: userID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Seluruh materi list promosi telah dibersihkan dari database (MongoDB &amp; SQLite).</b>",
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
			Text:        "✅ <b>Konfigurasi userbot telah direset ke pengaturan default.</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	return false
}
