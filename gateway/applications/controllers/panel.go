package controllers

import (
	"assterisk/gateway/applications/databases"
	"assterisk/gateway/applications/models"
	"context"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

var (
	// Main Menu Keyboard (From Image 1)
	mainMenuMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "💎 Status 💎"}, {Text: "🛒 Shop 🛒"}},
			{{Text: "🧩 Grup & List 🧩"}, {Text: "⚡ Expert Extra ⚡"}},
			{{Text: "🗑️ Reset 🗑️"}, {Text: "⌛ Atur Jeda ⌛"}, {Text: "⚠️ Bantuan ⚠️"}},
			{{Text: "⚙️ Settings ⚙️"}, {Text: "📰 Feed 📰"}, {Text: "🌐 Webapp 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Status Menu Keyboard (From Image 2)
	statusMenuMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "📌 Custom Watermark 📌"}},
			{{Text: "✉️ Atur Email ✉️"}, {Text: "⚠️ Safemode ⚠️"}},
			{{Text: "⏱️ Timer ⏱️"}, {Text: "👑 Ubah Mode 👑"}, {Text: "👤 Admin 👤"}},
			{{Text: "📊 Ubah Status 📊"}, {Text: "🔔 Notifikasi 🔔"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Grup & List Menu Keyboard (From Image 3)
	grupListMenuMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "🧑‍💻 Cek Topic 🧑‍💻"}, {Text: "🧑‍💻 Topic 🧑‍💻"}, {Text: "📌 List Topic"}},
			{{Text: "📁 Cek List 📁"}, {Text: "📚 Cek Grup 📚"}},
			{{Text: "📝 Tambah List 📝"}, {Text: "📝 Tambah Grup 📝"}},
			{{Text: "♨️ Hapus List ♨️"}, {Text: "♨️ Hapus Grup ♨️"}},
			{{Text: "📦 Sebar Ke RC 📦"}, {Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Tambah Grup Options Keyboard (From Image 5)
	tambahGrupOptionsMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "🔥 Tambahkan Semua Grup Dalam Akun 🔥"}},
			{{Text: "📝 Tambahkan Grup Manual 📝"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Cancel Only Keyboard (From Image 4: "⬅️ Batal ⬅️")
	cancelOnlyMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Atur Jeda Keyboard
	aturJedaMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "⏱️ 3 Detik"}, {Text: "⏱️ 5 Detik"}, {Text: "⏱️ 10 Detik"}},
			{{Text: "⏱️ 15 Detik"}, {Text: "⏱️ 30 Detik"}, {Text: "⏱️ 60 Detik"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Ubah Mode Keyboard
	ubahModeMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "👑 Normal"}, {Text: "⚡ Fast"}, {Text: "🔕 Silent"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Timer Keyboard
	timerMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "⏱️ Nonaktifkan Timer"}, {Text: "⏱️ Tiap 1 Jam"}},
			{{Text: "⏱️ Tiap 2 Jam"}, {Text: "⏱️ Tiap 3 Jam"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Reset Menu Keyboard
	resetMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "🗑️ Hapus Semua Grup 🗑️"}, {Text: "🗑️ Hapus Semua List 🗑️"}},
			{{Text: "🗑️ Reset Konfigurasi 🗑️"}},
			{{Text: "🌐 Home 🌐"}},
		},
		ResizeKeyboard: true,
	}

	// Hapus List Options Keyboard
	hapusListMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "🗑️ Hapus Semua List 🗑️"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}

	// Hapus Grup Options Keyboard
	hapusGrupMarkup = &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{
			{{Text: "🗑️ Hapus Semua Grup 🗑️"}},
			{{Text: "⬅️ Batal ⬅️"}},
		},
		ResizeKeyboard: true,
	}
)

// ShowMainMenu renders the active bot control panel using RichText
func (h *Handler) ShowMainMenu(ctx context.Context, b *bot.Bot, chatID, userID int64, userName string) {
	h.setPanelState(chatID, "")

	if userName == "" {
		userName = "User"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👋 <b>Halo, <a href=\"tg://user?id=%d\">%s</a>!</b>\n\n", userID, html.EscapeString(userName)))
	sb.WriteString("<blockquote>✨ <b>Panel Kontrol Userbot Aktif</b>\n")
	sb.WriteString("<i>Sesi userbot Anda sedang berjalan dan terhubung dengan aman. Seluruh data disinkronkan secara real-time.</i></blockquote>\n\n")
	sb.WriteString("Silakan pilih menu navigasi di bawah untuk mengelola broadcast, grup, status, dan konfigurasi bot Anda:")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowStatus renders the exact status layout from Image 2 in RichText
func (h *Handler) ShowStatus(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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
	sb.WriteString("💎 <b>Status Userbot</b>\n\n")
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
	sb.WriteString(fmt.Sprintf("📄 <b>Tindakan Terakhir :</b> <i>%s</i>\n\n", html.EscapeString(cfg.TindakanTerakhir)))
	sb.WriteString("<blockquote>ℹ️ <b>Bingung dengan status?</b> <i>Gunakan tombol kontrol di bawah untuk mengubah pengaturan atau konfigurasi secara instan.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// ShowGrupList renders the Grup & List menu from Image 3
func (h *Handler) ShowGrupList(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.WriteString("🔥 <b>Grup &amp; List</b>\n\n")
	sb.WriteString("<i>Atur grup dan list Anda lewat tombol di bawah.</i>\n\n")
	sb.WriteString("<blockquote>💡 <b>Petunjuk Singkat:</b>\n")
	sb.WriteString("• <b>List :</b> <i>Materi promosi / pesan teks / media yang akan disiarkan.</i>\n")
	sb.WriteString("• <b>Grup :</b> <i>Daftar grup atau supergroup tujuan siaran.</i>\n")
	sb.WriteString("• <b>Topic :</b> <i>Thread forum grup Telegram untuk penargetan topik khusus.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// PromptTambahList prompts user to enter broadcast list text (Image 4)
func (h *Handler) PromptTambahList(ctx context.Context, b *bot.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_add_list")

	var sb strings.Builder
	sb.WriteString("📝 <b>Masukkan list yang ingin ditambahkan</b>\n\n")
	sb.WriteString("<blockquote>ℹ️ <b>List adalah teks yang Anda promosikan.</b> <i>Format teks bebas atau caption promosi multi-baris juga didukung.</i></blockquote>\n\n")
	sb.WriteString("<i>Kirimkan teks pesan sekarang atau klik tombol <b>Batal</b> di bawah.</i>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTambahGrup prompts user for group adding method (Image 5)
func (h *Handler) PromptTambahGrup(ctx context.Context, b *bot.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.WriteString("<b>Pilih cara menambahkan grup ke list sebar</b>\n\n")
	sb.WriteString("<blockquote>💡 <i>Pilih otomatis untuk mengimpor seluruh grup dari akun Anda, atau manual untuk memasukkan target tertentu.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: tambahGrupOptionsMarkup,
	})
}

// PromptTambahGrupManual prompts user to input group usernames/IDs
func (h *Handler) PromptTambahGrupManual(ctx context.Context, b *bot.Bot, chatID int64) {
	h.setPanelState(chatID, "waiting_add_group_manual")

	var sb strings.Builder
	sb.WriteString("📝 <b>Masukkan daftar grup yang ingin ditambahkan</b>\n\n")
	sb.WriteString("<blockquote>ℹ️ <i>Kirim username grup (contoh: <code>@grupchat</code>), ID grup (contoh: <code>-1001234567890</code>), atau tautan undangan t.me/xxx. Jika lebih dari satu, pisahkan dengan baris baru.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// CekList displays all saved promotional lists
func (h *Handler) CekList(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	lists := h.db.GetLists(ctx, userID)

	if len(lists) == 0 {
		var sb strings.Builder
		sb.WriteString("📁 <b>Daftar List Pesan Promosi</b>\n\n")
		sb.WriteString("<blockquote>ℹ️ <i>Belum ada list yang tersimpan. Gunakan tombol <b>📝 Tambah List 📝</b> untuk menambahkan teks materi promosi Anda. Pesan akan disimpan aman di cache cloud (@AiotdCaches).</i></blockquote>")

		b.SendMessage(ctx, &bot.SendMessageParams{
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// CekGrup displays all registered broadcast groups
func (h *Handler) CekGrup(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	groups := h.db.GetGroups(ctx, userID)

	if len(groups) == 0 {
		var sb strings.Builder
		sb.WriteString("📚 <b>Daftar Grup Sebar</b>\n\n")
		sb.WriteString("<blockquote>ℹ️ <i>Belum ada grup yang tersimpan di list sebar. Gunakan tombol <b>📝 Tambah Grup 📝</b> untuk menambahkan target grup.</i></blockquote>")

		b.SendMessage(ctx, &bot.SendMessageParams{
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// PromptHapusList prompts for list deletion
func (h *Handler) PromptHapusList(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: hapusListMarkup,
	})
}

// PromptHapusGrup prompts for group deletion
func (h *Handler) PromptHapusGrup(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: hapusGrupMarkup,
	})
}

// CekTopic displays forum topics
func (h *Handler) CekTopic(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// ToggleTopic toggles topic mode
func (h *Handler) ToggleTopic(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// SebarKeRC sends test broadcast to Saved Messages (RC)
func (h *Handler) SebarKeRC(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

// PromptAturJeda displays delay settings
func (h *Handler) PromptAturJeda(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_jeda")
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("⌛ <b>Pengaturan Jeda Broadcast Antar-Grup</b>\n\n")
	sb.WriteString(fmt.Sprintf("Jeda saat ini: <code>%d detik · antar-grup</code>\n\n", cfg.Delay))
	sb.WriteString("<blockquote><i>Pilih tombol interval penundaan di bawah atau ketik langsung angka detik yang Anda inginkan (contoh: <code>8</code>).</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: aturJedaMarkup,
	})
}

// ToggleSafemode toggles safemode
func (h *Handler) ToggleSafemode(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// PromptUbahMode displays mode options
func (h *Handler) PromptUbahMode(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_mode")
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("👑 <b>Ubah Mode Siaran Userbot</b>\n\n")
	sb.WriteString(fmt.Sprintf("Mode saat ini: <code>%s</code>\n\n", html.EscapeString(cfg.Mode)))
	sb.WriteString("<blockquote>• <b>Normal :</b> <i>Kecepatan seimbang dengan proteksi interval standar.</i>\n")
	sb.WriteString("• <b>Fast :</b> <i>Kecepatan tinggi untuk kebutuhan promosi cepat.</i>\n")
	sb.WriteString("• <b>Silent :</b> <i>Pengiriman tanpa memicu notifikasi suara di grup penerima.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: ubahModeMarkup,
	})
}

// ToggleStatus toggles userbot broadcast status
func (h *Handler) ToggleStatus(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// ToggleNotifikasi toggles notification setting
func (h *Handler) ToggleNotifikasi(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: statusMenuMarkup,
	})
}

// PromptWatermark prompts user for watermark text
func (h *Handler) PromptWatermark(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptEmail prompts user for email
func (h *Handler) PromptEmail(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptAdmin prompts user for admin ID
func (h *Handler) PromptAdmin(ctx context.Context, b *bot.Bot, chatID, userID int64) {
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: cancelOnlyMarkup,
	})
}

// PromptTimer prompts user for timer
func (h *Handler) PromptTimer(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "waiting_set_timer")
	cfg := h.db.GetUserConfig(ctx, userID)

	var sb strings.Builder
	sb.WriteString("⏱️ <b>Pengaturan Timer Siaran Otomatis</b>\n\n")
	sb.WriteString(fmt.Sprintf("Timer saat ini: <code>%s</code>\n\n", html.EscapeString(cfg.Timer)))
	sb.WriteString("<blockquote><i>Pilih interval pengulangan siaran berkala di bawah atau ketik format interval Anda sendiri.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: timerMarkup,
	})
}

// PromptReset prompts user for reset options
func (h *Handler) PromptReset(ctx context.Context, b *bot.Bot, chatID int64) {
	h.setPanelState(chatID, "")

	var sb strings.Builder
	sb.WriteString("🗑️ <b>Pusat Reset Data</b>\n\n")
	sb.WriteString("<blockquote>⚠️ <b>Perhatian:</b> <i>Tindakan reset akan menghapus data yang dipilih dari MongoDB dan SQLite. Pastikan Anda telah memeriksa kembali sebelum melanjutkan.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: resetMarkup,
	})
}

// ShowShop displays Shop info
func (h *Handler) ShowShop(ctx context.Context, b *bot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("🛒 <b>GotdBot Store &amp; Layanan Premium</b>\n\n")
	sb.WriteString("<blockquote>💎 <b>Daftar Layanan Tersedia:</b>\n")
	sb.WriteString("• <b>VIP Userbot :</b> <i>Masa aktif tanpa batas waktu &amp; prioritas koneksi MTProto.</i>\n")
	sb.WriteString("• <b>Multi-Account Hub :</b> <i>Koneksikan hingga puluhan nomor Telegram secara paralel.</i>\n")
	sb.WriteString("• <b>Dedicated Instance :</b> <i>Private bot gateway eksklusif untuk bisnis Anda.</i></blockquote>\n\n")
	sb.WriteString("<i>Untuk konsultasi atau upgrade paket, silakan hubungi admin bot.</i>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowExpertExtra displays Expert Extra info
func (h *Handler) ShowExpertExtra(ctx context.Context, b *bot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("⚡ <b>Expert Extra Features</b>\n\n")
	sb.WriteString("<blockquote>🛠️ <b>Fitur Tingkat Lanjut:</b>\n")
	sb.WriteString("• <b>Smart FloodWait Bypass :</b> <i>Otomatis mengatur antrean jeda adaptif saat Telegram mengirim sinyal FloodWait.</i>\n")
	sb.WriteString("• <b>Anti-Duplicate Filter :</b> <i>Mencegah siaran ganda ke dialog yang sama dalam satu putaran.</i>\n")
	sb.WriteString("• <b>Pure-Go Architecture :</b> <i>Performa tinggi dengan SQLite WAL lokal &amp; MongoDB Disaster Recovery.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowBantuan displays Bantuan info
func (h *Handler) ShowBantuan(ctx context.Context, b *bot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("⚠️ <b>Bantuan &amp; Panduan Penggunaan</b>\n\n")
	sb.WriteString("<blockquote>📘 <b>Navigasi Tombol Panel:</b>\n")
	sb.WriteString("• <b>💎 Status :</b> <i>Melihat ringkasan lengkap data dan konfigurasi aktif akun.</i>\n")
	sb.WriteString("• <b>🧩 Grup &amp; List :</b> <i>Mengatur materi teks promosi, target grup, dan topic forum.</i>\n")
	sb.WriteString("• <b>⌛ Atur Jeda :</b> <i>Mengubah jeda pengiriman antar grup.</i>\n")
	sb.WriteString("• <b>⚠️ Safemode :</b> <i>Melindungi akun dari limitasi akun Telegram.</i></blockquote>\n\n")
	sb.WriteString("<i>Seluruh fitur diatur langsung melalui tombol bot di atas secara instan.</i>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowSettings displays Settings info
func (h *Handler) ShowSettings(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	h.ShowStatus(ctx, b, chatID, userID)
}

// ShowFeed displays updates feed
func (h *Handler) ShowFeed(ctx context.Context, b *bot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("📰 <b>Informasi &amp; Berita GotdBot</b>\n\n")
	sb.WriteString("<blockquote>🚀 <b>Versi Terpasang :</b> <code>v2.5.0-HighPerformance</code>\n")
	sb.WriteString("• <i>Sinkronisasi real-time MongoDB Cloud Atlas &amp; SQLite lokal aktif.</i>\n")
	sb.WriteString("• <i>Dual-layer messaging MTProto + Bot API stabil.</i>\n")
	sb.WriteString("• <i>Manajemen grup &amp; list sebar terintegrasi.</i></blockquote>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// ShowWebapp displays Webapp info
func (h *Handler) ShowWebapp(ctx context.Context, b *bot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("🌐 <b>GotdBot Web Dashboard</b>\n\n")
	sb.WriteString("<blockquote><i>Akses panel kendali visual untuk memantau performa siaran secara langsung melalui browser atau Telegram Mini App.</i></blockquote>\n\n")
	sb.WriteString("<i>Integrasi Web Dashboard sedang dalam tahap penyempurnaan cloud.</i>")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: mainMenuMarkup,
	})
}

// HandlePanelInput processes text inputs when user is in a state
func (h *Handler) HandlePanelInput(ctx context.Context, b *bot.Bot, chatID, userID int64, text string) bool {
	state := h.getPanelState(chatID)
	if state == "" {
		return false
	}

	if text == "⬅️ Batal ⬅️" || strings.EqualFold(text, "batal") || text == "/cancel" {
		h.setPanelState(chatID, "")
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "<blockquote>↩️ <b>Tindakan dibatalkan.</b></blockquote>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	switch state {
	case "waiting_add_list":
		h.setPanelState(chatID, "")
		payload := models.PToService{
			ChatID: chatID,
			UserID: userID,
			Type:   "cache_list",
			Text:   text,
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("cache_list broker error: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ <b>Gagal Menyimpan List:</b> <i>Layanan broker userbot tidak merespon.</i>",
				ParseMode:   "HTML",
				ReplyMarkup: grupListMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
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

		gCount, _ := h.db.CountGroupsAndLists(ctx, userID)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("✅ <b>Berhasil Menambahkan <code>%d</code> Target Grup!</b>\n\n", addedCount))
		sb.WriteString(fmt.Sprintf("Total target grup sebar saat ini: <code>%d</code>", gCount))

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: grupListMenuMarkup,
		})
		return true

	case "waiting_del_list":
		h.setPanelState(chatID, "")
		if text == "🗑️ Hapus Semua List 🗑️" {
			_ = h.db.ClearLists(ctx, userID)
			b.SendMessage(ctx, &bot.SendMessageParams{
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
				_ = h.db.DeleteListByIndex(ctx, userID, idx-1)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:      chatID,
					Text:        fmt.Sprintf("✅ <b>List #%d berhasil dihapus.</b>", idx),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
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
			b.SendMessage(ctx, &bot.SendMessageParams{
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
				_ = h.db.DeleteGroupByIndex(ctx, userID, idx-1)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:      chatID,
					Text:        fmt.Sprintf("✅ <b>Target grup #%d (%s) berhasil dihapus.</b>", idx, html.EscapeString(groups[idx-1].Target)),
					ParseMode:   "HTML",
					ReplyMarkup: grupListMenuMarkup,
				})
				return true
			}
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
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
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("✅ <b>Jeda broadcast berhasil diatur ke <code>%d detik</code>.</b>", sec),
				ParseMode:   "HTML",
				ReplyMarkup: mainMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
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
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("✅ <b>Mode siaran berhasil diubah ke <code>%s</code>.</b>", mode),
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "⚠️ <i>Pilihan mode tidak dikenali.</i>",
			ParseMode:   "HTML",
			ReplyMarkup: statusMenuMarkup,
		})
		return true

	case "waiting_set_watermark":
		h.setPanelState(chatID, "")
		cfg := h.db.GetUserConfig(ctx, userID)
		if text == "🗑️ Hapus Watermark 🗑️" || strings.EqualFold(text, "hapus") {
			cfg.Watermark = ""
			cfg.WatermarkCacheID = 0
			_ = h.db.SaveUserConfig(ctx, cfg)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Custom watermark berhasil dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		payload := models.PToService{
			ChatID: chatID,
			UserID: userID,
			Type:   "cache_watermark",
			Text:   text,
		}
		if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
			log.Printf("cache_watermark broker error: %v", err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ <b>Gagal Menyimpan Watermark:</b> <i>Layanan broker userbot tidak merespon.</i>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		b.SendMessage(ctx, &bot.SendMessageParams{
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
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Email notifikasi berhasil dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Email = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &bot.SendMessageParams{
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
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "✅ <b>Admin berhasil dihapus.</b>",
				ParseMode:   "HTML",
				ReplyMarkup: statusMenuMarkup,
			})
			return true
		}
		cfg.Admin = text
		_ = h.db.SaveUserConfig(ctx, cfg)
		b.SendMessage(ctx, &bot.SendMessageParams{
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
		b.SendMessage(ctx, &bot.SendMessageParams{
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
func (h *Handler) FetchAllGroupsFromAccount(ctx context.Context, b *bot.Bot, chatID, userID int64) {
	var sb strings.Builder
	sb.WriteString("⏳ <b>Sedang Mengimpor Seluruh Grup Akun...</b>\n\n")
	sb.WriteString("<blockquote><i>Menghubungi engine userbot untuk membaca seluruh grup dan supergroup yang diikuti akun Telegram Anda.</i></blockquote>\n\n")
	sb.WriteString("<i>Proses ini berjalan di latar belakang. Anda akan menerima laporan otomatis begitu sinkronisasi selesai.</i>")

	b.SendMessage(ctx, &bot.SendMessageParams{
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
func (h *Handler) RoutePanelButton(ctx context.Context, b *bot.Bot, update *t_models.Update) bool {
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
	if h.HandlePanelInput(ctx, b, chatID, userID, text) {
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
		h.CekList(ctx, b, chatID, userID)
		return true

	case "📚 Cek Grup 📚":
		h.CekGrup(ctx, b, chatID, userID)
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

	case "♨️ Hapus List ♨️":
		h.PromptHapusList(ctx, b, chatID, userID)
		return true

	case "♨️ Hapus Grup ♨️":
		h.PromptHapusGrup(ctx, b, chatID, userID)
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
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Seluruh target grup telah dibersihkan dari database (MongoDB &amp; SQLite).</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Hapus Semua List 🗑️":
		_ = h.db.ClearLists(ctx, userID)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Seluruh materi list promosi telah dibersihkan dari database (MongoDB &amp; SQLite).</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true

	case "🗑️ Reset Konfigurasi 🗑️":
		defaultCfg := &databases.UserConfig{
			OwnerID:         userID,
			Type:            "Broadcast",
			Delay:           5,
			SkipFloodWait:   false,
			Timer:           "Belum diatur",
			Extra:           0,
			Mode:            "Normal",
			Safemode:        false,
			Status:          "Berhenti",
			MasaAktif:       "November 29, 2026 at 14:18",
			Notifikasi:      "Aktif · Kirim ke Saya",
			Email:           "Belum diatur",
			Admin:           "Belum diatur",
			Watermark:       "",
			TopicEnabled:    false,
			TindakanTerkini: "Siaran otomatis dimatikan",
			TindakanTerakhir: "-",
			UpdatedAt:       time.Now(),
		}
		_ = h.db.SaveUserConfig(ctx, defaultCfg)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ <b>Konfigurasi userbot telah direset ke pengaturan default.</b>",
			ParseMode:   "HTML",
			ReplyMarkup: mainMenuMarkup,
		})
		return true
	}

	return false
}
