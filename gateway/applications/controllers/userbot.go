package controllers

import (
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/models"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

var gatewayStartedAt = time.Now()
var inlineRedis *broker.Redis

func SetInlineRedis(redis *broker.Redis) { inlineRedis = redis }

type UserSession struct {
	ChatID int64
	State  string
}
type SessionManager struct {
	sessions map[int64]*UserSession
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[int64]*UserSession)}
}

func (h *Handler) Create(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	h.Sessions.mu.Lock()
	if current := h.Sessions.sessions[chatID]; current != nil {
		h.Sessions.mu.Unlock()
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Pemasangan sedang diproses. Tunggu atau ketik /cancel."})
		return
	}
	h.Sessions.sessions[chatID] = &UserSession{ChatID: chatID, State: "waiting_phone"}
	h.Sessions.mu.Unlock()
	h.Sessions.mu.Lock()
	h.Sessions.sessions[chatID].State = "waiting_check"
	h.Sessions.mu.Unlock()
	if err := h.broker.To(ctx, "to:ubot:auth", models.PToService{ChatID: chatID, UserID: update.Message.From.ID, Type: "auth_check"}); err != nil {
		h.Sessions.mu.Lock()
		delete(h.Sessions.sessions, chatID)
		h.Sessions.mu.Unlock()
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Layanan pemasangan tidak tersedia. Coba lagi."})
	}
}

func (h *Handler) HandleMessage(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID, text := update.Message.Chat.ID, strings.TrimSpace(update.Message.Text)
	h.Sessions.mu.RLock()
	session := h.Sessions.sessions[chatID]
	h.Sessions.mu.RUnlock()
	if session == nil {
		h.DefaultHandler(ctx, b, update)
		return
	}
	if text == "/cancel" || strings.EqualFold(text, "cancel") {
		h.cancelInstall(ctx, b, update)
		return
	}
	if session.State == "waiting_check" {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Sedang memeriksa sesi sebelumnya. Tunggu sebentar."})
		return
	}

	var event string
	switch session.State {
	case "waiting_phone":
		if update.Message.Contact != nil {
			contact := update.Message.Contact
			if contact.UserID != 0 && contact.UserID != update.Message.From.ID {
				b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Kirim kontak milik akun Telegram Anda sendiri."})
				return
			}
			text = normalizePhone(contact.PhoneNumber)
		} else if !validPhone(text) {
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Nomor tidak valid. Gunakan contoh: +6281234567890"})
			return
		}
		event = "auth_phone"
	case "waiting_otp":
		text = strings.ReplaceAll(text, " ", "")
		if len(text) < 4 || len(text) > 10 {
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "OTP harus berisi 4-10 digit."})
			return
		}
		event = "auth_otp"
	case "waiting_password":
		if text == "" {
			return
		}
		event = "auth_password"
	default:
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Pemasangan sedang diproses. Tunggu atau ketik /cancel."})
		return
	}

	h.Sessions.mu.Lock()
	session.State = "waiting_worker"
	h.Sessions.mu.Unlock()
	payload := models.PToService{ChatID: chatID, UserID: update.Message.From.ID, Username: update.Message.From.Username, Text: text, Type: event}
	if err := h.broker.To(ctx, "to:ubot:auth", payload); err != nil {
		h.Sessions.mu.Lock()
		session.State = strings.TrimPrefix(event, "auth_")
		if session.State == "phone" {
			session.State = "waiting_phone"
		} else {
			session.State = "waiting_" + session.State
		}
		h.Sessions.mu.Unlock()
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Layanan pemasangan tidak tersedia. Coba lagi."})
	}
}

func (h *Handler) cancelInstall(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	chatID := update.Message.Chat.ID
	h.Sessions.mu.Lock()
	delete(h.Sessions.sessions, chatID)
	h.Sessions.mu.Unlock()
	_ = h.broker.To(ctx, "to:ubot:auth", models.PToService{ChatID: chatID, Type: "auth_cancel"})
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Pemasangan dibatalkan.", ReplyMarkup: &t_models.ReplyKeyboardRemove{RemoveKeyboard: true}})
}

func (h *Handler) setInstallState(chatID int64, state string) {
	h.Sessions.mu.Lock()
	defer h.Sessions.mu.Unlock()
	if state == "installed" || state == "failed" {
		delete(h.Sessions.sessions, chatID)
		return
	}
	if current := h.Sessions.sessions[chatID]; current != nil {
		current.State = state
	}
}

func validPhone(value string) bool {
	if !strings.HasPrefix(value, "+") || len(value) < 9 || len(value) > 20 {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizePhone(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), " ", "")
	if value != "" && !strings.HasPrefix(value, "+") {
		value = "+" + value
	}
	return value
}

func installPhoneKeyboard() *t_models.ReplyKeyboardMarkup {
	return &t_models.ReplyKeyboardMarkup{
		Keyboard: [][]t_models.KeyboardButton{{
			{Text: "Kirim Kontak", RequestContact: true},
			{Text: "Cancel"},
		}},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func cancelKeyboard() *t_models.ReplyKeyboardMarkup {
	return &t_models.ReplyKeyboardMarkup{
		Keyboard:       [][]t_models.KeyboardButton{{{Text: "Cancel"}}},
		ResizeKeyboard: true,
	}
}

func (h *Handler) PingCommand(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	if err := h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: update.Message.Chat.ID, UserID: update.Message.From.ID, Text: "!ping", Type: "userbot_command"}); err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Ping gagal. Periksa koneksi dan status sesi userbot."})
	}
}

func (h *Handler) HelpCommand(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "<b>Bantuan userbot</b>\n\n!ping / !p — ukur latency koneksi Telegram\n!help — tampilkan bantuan ini\n!code [bahasa] — format pesan reply\n!eval / !sh — jalankan perintah shell\n!setp none — hapus prefix command\n/create — pasang userbot baru\n/cancel — batalkan pemasangan", ParseMode: "HTML"})
}

func helpHomeKeyboard() *t_models.InlineKeyboardMarkup {
	labels := []string{"Admin", "Broadcast", "Code", "Extra", "Global", "Info", "Logs", "Notes", "Ping", "PM Permit", "Prefixes", "Profile", "Server", "Sticker", "Sudo", "VC Tools"}
	keys := []string{"admin", "broadcast", "code", "extra", "globalz", "info", "logs", "notes", "ping", "pmpermit", "prefixes", "profile", "server", "sticker", "sudo", "vctools"}
	rows := make([][]t_models.InlineKeyboardButton, 0, (len(labels)+2)/3)
	for i := range labels {
		if i%3 == 0 {
			rows = append(rows, []t_models.InlineKeyboardButton{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], t_models.InlineKeyboardButton{Text: labels[i], CallbackData: "assist_help:" + keys[i]})
	}
	return &t_models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *Handler) HelpCallback(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	data := update.CallbackQuery.Data
	if strings.HasPrefix(data, "assist_server:") {
		h.serverCallback(ctx, b, update)
		return
	}
	text := "<b>" + strings.TrimPrefix(data, "assist_help:") + "</b>\n\nKategori ini tersedia pada menu bantuan."
	if data == "assist_help:home" {
		text = "<b>Saya adalah asisten pribadi userbot Anda.</b>\n\nPilih kategori untuk melihat bantuan userbot."
	} else if data == "assist_help:ping" {
		text = "<b>Ping</b>\n\n<code>!ping</code> mengukur latency koneksi MTProto userbot.\n<code>!p</code> adalah alias."
	} else if data == "assist_help:notes" {
		text = "<b>Notes</b>\n\nModul Notes belum diaktifkan pada workspace GotdBot."
	} else if data == "assist_help:code" {
		text = "<b>Code</b>\n\nReply pesan dengan <code>!code</code> atau <code>!code go</code> untuk memformat kode melalui inline bot."
	} else if data == "assist_help:server" {
		text = "<b>Server</b>\n\n<code>!server</code> menampilkan status resource gateway dan tombol refresh."
	}
	markup := helpHomeKeyboard()
	if data != "assist_help:home" {
		markup = &t_models.InlineKeyboardMarkup{InlineKeyboard: [][]t_models.InlineKeyboardButton{{{Text: "‹ Kembali", CallbackData: "assist_help:home"}}}}
	}
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID})
	edit := &bot.EditMessageTextParams{Text: text, ParseMode: "HTML", ReplyMarkup: markup}
	if update.CallbackQuery.InlineMessageID != "" {
		// Messages inserted through messages.sendInlineBotResult do not carry a
		// chat/message object in callback_query; Telegram supplies this ID.
		edit.InlineMessageID = update.CallbackQuery.InlineMessageID
	} else if update.CallbackQuery.Message.Message != nil {
		edit.ChatID = update.CallbackQuery.Message.Message.Chat.ID
		edit.MessageID = update.CallbackQuery.Message.Message.ID
	} else {
		return
	}
	_, _ = b.EditMessageText(ctx, edit)
}

func (h *Handler) serverCallback(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	edit := &bot.EditMessageTextParams{Text: serverStatusText(), ParseMode: "HTML", ReplyMarkup: serverKeyboard()}
	if update.CallbackQuery.InlineMessageID != "" {
		edit.InlineMessageID = update.CallbackQuery.InlineMessageID
	} else if update.CallbackQuery.Message.Message != nil {
		edit.ChatID = update.CallbackQuery.Message.Message.Chat.ID
		edit.MessageID = update.CallbackQuery.Message.Message.ID
	} else {
		return
	}
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: "Status diperbarui"})
	_, _ = b.EditMessageText(ctx, edit)
}

func (h *Handler) HelpCommandMenu(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "<b>Saya adalah asisten pribadi userbot Anda.</b>\n\nPilih kategori untuk melihat bantuan userbot.", ParseMode: "HTML", ReplyMarkup: helpHomeKeyboard()})
}

func (h *Handler) ServerCommand(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: serverStatusText(), ParseMode: "HTML", ReplyMarkup: serverKeyboard()})
}

func (h *Handler) CodeCommand(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	if update.Message.ReplyToMessage == nil || strings.TrimSpace(update.Message.ReplyToMessage.Text) == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Reply pesan yang mau di-code terlebih dahulu."})
		return
	}
	language := "go"
	fields := strings.Fields(update.Message.Text)
	if len(fields) > 1 {
		language = fields[1]
	}
	text := fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", html.EscapeString(language), html.EscapeString(update.Message.ReplyToMessage.Text))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: text, ParseMode: "HTML", ReplyParameters: &t_models.ReplyParameters{MessageID: update.Message.ID}})
}

func (h *Handler) HelpInline(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	HandleInline(ctx, b, update)
}

// HandleInline is called by the custom polling loop because the bot library
// version used by this project does not expose an inline-query handler type.
func HandleInline(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.InlineQuery == nil {
		return
	}
	query := strings.TrimSpace(update.InlineQuery.Query)
	if strings.HasPrefix(query, "broadcast ") {
		fields := strings.Fields(query)
		if len(fields) >= 4 {
			ownerID, ownerErr := strconv.ParseInt(fields[1], 10, 64)
			if ownerErr == nil {
				result := &t_models.InlineQueryResultArticle{
					ID: "broadcast_monitor_" + fields[2], Title: "Proses Pengiriman",
					Description: "Monitor broadcast userbot",
					InputMessageContent: &t_models.InputTextMessageContent{
						MessageText: fmt.Sprintf("<b>Proses Pengiriman</b>\nCek bot untuk detail broadcast anda\n<code>#AIO:%s</code>", html.EscapeString(fields[2])),
						ParseMode:   "HTML",
					},
					ReplyMarkup: broadcastStopKeyboard(ownerID, fields[2]),
				}
				_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []t_models.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
				return
			}
		}
	}
	if strings.HasPrefix(query, "gcancel ") {
		fields := strings.Fields(query)
		if len(fields) >= 3 && inlineRedis != nil {
			ownerID, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil && ownerID != 0 {
				_ = inlineRedis.To(ctx, "to:ubot:command", models.PToService{
					ChatID: ownerID, UserID: update.InlineQuery.From.ID, OwnerID: ownerID,
					TaskID: fields[2], Type: "broadcast_cancel",
				})
			}
		}
		result := &t_models.InlineQueryResultArticle{
			ID: "broadcast_cancel", Title: "Hentikan Broadcast",
			Description:         "Permintaan penghentian broadcast dikirim.",
			InputMessageContent: &t_models.InputTextMessageContent{MessageText: "Permintaan penghentian broadcast dikirim."},
		}
		_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []t_models.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
		return
	}
	if query == "server" {
		result := &t_models.InlineQueryResultArticle{ID: "assist_server", Title: "Server status", Description: "Status gateway dan resource proses", InputMessageContent: &t_models.InputTextMessageContent{MessageText: serverStatusText(), ParseMode: "HTML"}, ReplyMarkup: serverKeyboard()}
		_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []t_models.InlineQueryResult{result}, CacheTime: 0, IsPersonal: false})
		return
	}
	if strings.HasPrefix(query, "code ") {
		parts := strings.SplitN(query, " ", 3)
		if len(parts) == 3 {
			source := []byte(nil)
			var err error
			if strings.HasPrefix(parts[2], "ref:") && inlineRedis != nil {
				source, err = inlineRedis.Client.Get(ctx, "inline:code:"+strings.TrimPrefix(parts[2], "ref:")).Bytes()
			} else {
				source, err = base64.RawURLEncoding.DecodeString(parts[2])
			}
			if err == nil {
				lang := html.EscapeString(parts[1])
				body := html.EscapeString(string(source))
				result := &t_models.InlineQueryResultArticle{ID: "assist_code", Title: "Code", Description: "Format kode", InputMessageContent: &t_models.InputTextMessageContent{MessageText: fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", lang, body), ParseMode: "HTML"}}
				_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []t_models.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
				return
			}
		}
	}
	result := &t_models.InlineQueryResultArticle{
		ID:                  "assist_help",
		Title:               "Bantuan userbot",
		Description:         "Pilih kategori untuk melihat bantuan userbot.",
		InputMessageContent: &t_models.InputTextMessageContent{MessageText: "<b>Saya adalah asisten pribadi userbot Anda.</b>\n\nPilih kategori untuk melihat bantuan userbot.", ParseMode: "HTML"},
		ReplyMarkup:         helpHomeKeyboard(),
	}
	_, _ = b.AnswerInlineQuery(ctx, &bot.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []t_models.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
}

func mustInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func serverKeyboard() *t_models.InlineKeyboardMarkup {
	return &t_models.InlineKeyboardMarkup{InlineKeyboard: [][]t_models.InlineKeyboardButton{{{Text: "Refresh", CallbackData: "assist_server:refresh"}}}}
}

func serverStatusText() string {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return fmt.Sprintf("<b>Status server</b>\nGoroutine: <b>%d</b>\nRAM proses: <b>%.1f MB</b>\nHeap aktif: <b>%.1f MB</b>\nUptime gateway: <b>%s</b>", runtime.NumGoroutine(), float64(mem.Sys)/1024/1024, float64(mem.HeapAlloc)/1024/1024, formatUptime(time.Since(gatewayStartedAt)))
}

func formatUptime(d time.Duration) string {
	minutes := int(d.Minutes())
	days, minutes := minutes/1440, minutes%1440
	hours, minutes := minutes/60, minutes%60
	return fmt.Sprintf("%d hari %d jam %d menit", days, hours, minutes)
}
