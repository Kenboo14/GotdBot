package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"context"
	"fmt"
	"html"
	"strings"
)

func (h *Handler) User(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	cmd := strings.TrimSpace(update.Message.Text)

	// Periksa apakah sesi userbot telah aktif (di SQLite lokal atau MongoDB)
	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, userID)
	}

	if hasActive {
		if strings.HasPrefix(cmd, "/status") {
			h.ShowStatus(ctx, b, chatID, userID)
			return
		}
		h.ShowMainMenu(ctx, b, chatID, userID, userName)
		return
	}

	// Cek jika user sudah dalam proses pemasangan (session aktif)
	h.Sessions.mu.RLock()
	existingSession := h.Sessions.sessions[chatID]
	h.Sessions.mu.RUnlock()

	if existingSession != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>⏳ Pemasangan Sedang Berjalan</h1>
<blockquote expandable>Proses pemasangan sesi userbot Anda saat ini sedang berlangsung.</blockquote>
<hr/>
<p>Silakan lanjutkan proses atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: cachedCancelMarkup,
		})
		return
	}

	// Buat sesi baru dan tampilkan pilihan pemasangan
	h.Sessions.mu.Lock()
	h.Sessions.sessions[chatID] = &UserSession{ChatID: chatID, State: "waiting_create_choice"}
	h.Sessions.mu.Unlock()

	greeting := fmt.Sprintf(`<h1>👋 Selamat Datang, %s!</h1>
<blockquote expandable><b>GotdBot • Userbot Manager</b>
Sistem otomatisasi siaran promosi dan manajemen akun userbot Telegram MTProto berkecepatan tinggi.</blockquote>
<table border="1">
  <tr><th>Opsi Pemasangan</th><th>Keterangan</th></tr>
  <tr><td>🔑 Input Token</td><td>Klaim Voucher Master atau Restore Token SHA</td></tr>
  <tr><td>🚀 Buat Userbot</td><td>Sambungkan ulang lisensi aktif Anda</td></tr>
</table>
<hr/>
<p>Silakan pilih opsi pemasangan pada tombol keyboard di bawah:</p>`, html.EscapeString(userName))

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        greeting,
		ParseMode:   "HTML",
		ReplyMarkup: cachedCreateChoiceMarkup,
	})
}
