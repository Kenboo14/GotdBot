package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/models"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"html"
	"log"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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

func (h *Handler) Create(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	h.Sessions.mu.Lock()
	if current := h.Sessions.sessions[chatID]; current != nil {
		h.Sessions.mu.Unlock()
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Pemasangan sedang diproses. Tunggu atau ketik /cancel."})
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Layanan pemasangan tidak tersedia. Coba lagi."})
	}
}

func (h *Handler) HandleMessage(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	// Intersep tombol ReplyKeyboard dan state input panel kontrol
	if h.RoutePanelButton(ctx, b, update) {
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Sedang memeriksa sesi sebelumnya. Tunggu sebentar."})
		return
	}

	var event string
	switch session.State {
	case "waiting_phone":
		if update.Message.Contact != nil {
			contact := update.Message.Contact
			if contact.UserID != 0 && contact.UserID != update.Message.From.ID {
				b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Kirim kontak milik akun Telegram Anda sendiri."})
				return
			}
			text = normalizePhone(contact.PhoneNumber)
		} else if !validPhone(text) {
			b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Nomor tidak valid. Gunakan contoh: +6281234567890"})
			return
		}
		event = "auth_phone"
	case "waiting_otp":
		text = strings.ReplaceAll(text, " ", "")
		if len(text) < 4 || len(text) > 10 {
			b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "OTP harus berisi 4-10 digit."})
			return
		}
		event = "auth_otp"
	case "waiting_password":
		if text == "" {
			return
		}
		event = "auth_password"
	default:
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Pemasangan sedang diproses. Tunggu atau ketik /cancel."})
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
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Layanan pemasangan tidak tersedia. Coba lagi."})
	}
}

func (h *Handler) cancelInstall(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	chatID := update.Message.Chat.ID
	h.Sessions.mu.Lock()
	delete(h.Sessions.sessions, chatID)
	h.Sessions.mu.Unlock()
	_ = h.broker.To(ctx, "to:ubot:auth", models.PToService{ChatID: chatID, Type: "auth_cancel"})
	b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: chatID, Text: "Pemasangan dibatalkan.", ReplyMarkup: cachedRemoveKeyboard})
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

var (
	cachedInstallPhoneMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{{
			{Text: "Kirim Kontak", RequestContact: true},
			{Text: "Cancel"},
		}},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
	cachedCancelMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard:       [][]botgotd.KeyboardButton{{{Text: "Cancel"}}},
		ResizeKeyboard: true,
	}
	cachedServerMarkup = &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "Refresh", CallbackData: "assist_server:refresh"}}},
	}
	cachedRemoveKeyboard = &botgotd.ReplyKeyboardRemove{RemoveKeyboard: true}
)

func installPhoneKeyboard() *botgotd.ReplyKeyboardMarkup {
	return cachedInstallPhoneMarkup
}

func cancelKeyboard() *botgotd.ReplyKeyboardMarkup {
	return cachedCancelMarkup
}

func (h *Handler) PingCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	if err := h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: update.Message.Chat.ID, UserID: update.Message.From.ID, Text: "!ping", Type: "userbot_command"}); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Ping gagal. Periksa koneksi dan status sesi userbot."})
	}
}

func (h *Handler) HelpCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	text := "<b>📖 Panduan Cepat Perintah</b>\n━━━━━━━━━━━━━━━━━━━━\n" +
		"• <code>!ping</code> / <code>!p</code> — Ukur latency koneksi\n" +
		"• <code>!help</code> — Buka menu bantuan interaktif\n" +
		"• <code>!code [bahasa]</code> — Format kode pesan reply\n" +
		"• <code>!eval</code> / <code>!sh</code> — Eksekusi command shell\n" +
		"• <code>!setp none</code> — Nonaktifkan prefix command\n" +
		"• <code>/create</code> — Setup userbot baru\n" +
		"• <code>/cancel</code> — Batalkan instalasi"
	b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: text, ParseMode: "HTML"})
}

var (
	cachedHelpHomeMarkup = buildHelpHomeKeyboard()
	cachedHelpBackMarkup = &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "‹ Kembali", CallbackData: "assist_help:home"}}},
	}
)

type helpPageSnapshot struct {
	text   string
	markup *botgotd.InlineKeyboardMarkup
}

var cachedHelpPages = map[string]helpPageSnapshot{
	"assist_help:home": {
		text:   "<b>🤖 Asisten Pribadi Userbot</b>\n━━━━━━━━━━━━━━━━━━━━\nPilih kategori di bawah untuk melihat daftar bantuan perintah userbot.",
		markup: cachedHelpHomeMarkup,
	},
	"assist_help:ping": {
		text:   "<b>⚡ Ping &amp; Latency</b>\n━━━━━━━━━━━━━━━━━━━━\n<code>!ping</code> — Mengukur latency koneksi MTProto userbot ke Telegram.\n<code>!p</code> — Alias cepat.",
		markup: cachedHelpBackMarkup,
	},
	"assist_help:notes": {
		text:   "<b>📝 Manajemen Catatan</b>\n━━━━━━━━━━━━━━━━━━━━\n• <code>!save &lt;nama&gt; [-cb|-pop]</code> — Simpan catatan\n• <code>!get &lt;nama&gt;</code> / <code>#&lt;nama&gt;</code> — Ambil catatan\n• <code>!lsnote</code> — Daftar semua catatan\n• <code>!delnote &lt;nama&gt;</code> — Hapus catatan\n• <code>!clearnote</code> — Hapus semua catatan\n• <code>!exportnote</code> — Ekspor catatan\n• <code>!importnote</code> — Impor catatan",
		markup: cachedHelpBackMarkup,
	},
	"assist_help:code": {
		text:   "<b>💻 Format Kode</b>\n━━━━━━━━━━━━━━━━━━━━\nReply pesan apapun dengan <code>!code</code> atau <code>!code go</code> untuk memformat syntax highlighting via inline query.",
		markup: cachedHelpBackMarkup,
	},
	"assist_help:server": {
		text:   "<b>🖥 Status Server</b>\n━━━━━━━━━━━━━━━━━━━━\n<code>!server</code> — Menampilkan status resource sistem &amp; gateway botgotd secara real-time.",
		markup: cachedHelpBackMarkup,
	},
}

func isMessageNotModifiedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "message is not modified") ||
		strings.Contains(msg, "message_not_modified") ||
		strings.Contains(msg, "cannot unmarshal bool into go value of type models.message")
}

func buildHelpHomeKeyboard() *botgotd.InlineKeyboardMarkup {
	labels := []string{"Admin", "Broadcast", "Code", "Extra", "Global", "Info", "Logs", "Notes", "Ping", "PM Permit", "Prefixes", "Profile", "Server", "Sticker", "Sudo", "VC Tools"}
	keys := []string{"admin", "broadcast", "code", "extra", "globalz", "info", "logs", "notes", "ping", "pmpermit", "prefixes", "profile", "server", "sticker", "sudo", "vctools"}
	rows := make([][]botgotd.InlineKeyboardButton, 0, (len(labels)+2)/3)
	for i := range labels {
		if i%3 == 0 {
			rows = append(rows, []botgotd.InlineKeyboardButton{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], botgotd.InlineKeyboardButton{Text: labels[i], CallbackData: "assist_help:" + keys[i]})
	}
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func helpHomeKeyboard() *botgotd.InlineKeyboardMarkup {
	return cachedHelpHomeMarkup
}

func (h *Handler) HelpCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.CallbackQuery == nil {
		return
	}
	start := time.Now()
	data := update.CallbackQuery.Data

	if strings.HasPrefix(data, "assist_server:") {
		h.serverCallback(ctx, b, update)
		return
	}

	var (
		ackDispatch time.Duration
		cacheDur    time.Duration
		renderDur   time.Duration
		editDur     time.Duration
		rpcCount    = 0
		cbErr       error
	)

	// Step 1: Dispatch ACK segera di background (menghilangkan loading spinner tombol di client Telegram)
	ackStart := time.Now()
	go func(cbID string) {
		actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ackNetStart := time.Now()
		_, err := b.AnswerCallbackQuery(actx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID})
		ackNetDur := time.Since(ackNetStart)
		if err != nil {
			log.Printf("[callback-ack] type=help cbID=%s net_dur=%v err=%v", cbID, ackNetDur, err)
		} else {
			log.Printf("[callback-ack] type=help cbID=%s net_dur=%v status=ok", cbID, ackNetDur)
		}
	}(update.CallbackQuery.ID)
	ackDispatch = time.Since(ackStart)
	rpcCount++

	// Step 2: Ambil template halaman bantuan dari in-memory cache
	cacheStart := time.Now()
	page, ok := cachedHelpPages[data]
	cacheDur = time.Since(cacheStart)

	text := page.text
	markup := page.markup
	if !ok {
		renderStart := time.Now()
		category := strings.TrimPrefix(data, "assist_help:")
		text = fmt.Sprintf("<b>📖 Bantuan: %s</b>\n━━━━━━━━━━━━━━━━━━━━\nKategori ini tersedia pada menu bantuan userbot.", html.EscapeString(strings.ToUpper(category)))
		markup = cachedHelpBackMarkup
		renderDur = time.Since(renderStart)
	}

	// Step 3: Edit pesan
	edit := &botgotd.EditMessageTextParams{
		Text:               text,
		ParseMode:          "HTML",
		ReplyMarkup:        markup,
		RawInlineMessageID: update.CallbackQuery.RawInlineMessageID,
	}
	if update.CallbackQuery.RawInlineMessageID != nil || update.CallbackQuery.InlineMessageID != "" {
		edit.InlineMessageID = update.CallbackQuery.InlineMessageID
	} else if update.CallbackQuery.Message.Message != nil {
		edit.ChatID = update.CallbackQuery.Message.Message.Chat.ID
		edit.MessageID = update.CallbackQuery.Message.Message.ID
	} else {
		return
	}

	editStart := time.Now()
	_, editErr := b.EditMessageText(ctx, edit)
	editDur = time.Since(editStart)
	rpcCount++

	if editErr != nil && !isMessageNotModifiedErr(editErr) {
		errStr := strings.ToLower(editErr.Error())
		if strings.Contains(errStr, "can't be edited") {
			if edit.ChatID != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      edit.ChatID,
					Text:        text,
					ParseMode:   "HTML",
					ReplyMarkup: markup,
				})
			}
		} else {
			cbErr = editErr
		}
	}

	totalDur := time.Since(start)
	log.Printf("[callback] data=%q ack_dispatch=%v cache=%v db=0s render=%v edit=%v total=%v rpc_count=%d err=%v",
		data, ackDispatch, cacheDur, renderDur, editDur, totalDur, rpcCount, cbErr)
}

func (h *Handler) serverCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	start := time.Now()
	var (
		ackDispatch time.Duration
		renderDur   time.Duration
		editDur     time.Duration
		rpcCount    = 0
		cbErr       error
	)

	// Step 1: Dispatch ACK segera di awal sebelum render/profiling CPU OS
	ackStart := time.Now()
	go func(cbID string) {
		actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ackNetStart := time.Now()
		_, err := b.AnswerCallbackQuery(actx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            "Status diperbarui",
		})
		ackNetDur := time.Since(ackNetStart)
		if err != nil {
			log.Printf("[callback-ack] type=server cbID=%s net_dur=%v err=%v", cbID, ackNetDur, err)
		} else {
			log.Printf("[callback-ack] type=server cbID=%s net_dur=%v status=ok", cbID, ackNetDur)
		}
	}(update.CallbackQuery.ID)
	ackDispatch = time.Since(ackStart)
	rpcCount++

	// Step 2: Render status server
	renderStart := time.Now()
	var invokerID int64
	if update.CallbackQuery != nil && update.CallbackQuery.From.ID > 0 {
		invokerID = update.CallbackQuery.From.ID
	}
	text := serverStatusText(invokerID, time.Since(start))
	edit := &botgotd.EditMessageTextParams{
		Text:               text,
		ParseMode:          "HTML",
		ReplyMarkup:        serverKeyboard(),
		RawInlineMessageID: update.CallbackQuery.RawInlineMessageID,
	}
	if update.CallbackQuery.RawInlineMessageID != nil || update.CallbackQuery.InlineMessageID != "" {
		edit.InlineMessageID = update.CallbackQuery.InlineMessageID
	} else if update.CallbackQuery.Message.Message != nil {
		edit.ChatID = update.CallbackQuery.Message.Message.Chat.ID
		edit.MessageID = update.CallbackQuery.Message.Message.ID
	} else {
		return
	}
	renderDur = time.Since(renderStart)

	// Step 3: Edit message
	editStart := time.Now()
	_, editErr := b.EditMessageText(ctx, edit)
	editDur = time.Since(editStart)
	rpcCount++

	if editErr != nil && !isMessageNotModifiedErr(editErr) {
		errStr := strings.ToLower(editErr.Error())
		if strings.Contains(errStr, "can't be edited") {
			if edit.ChatID != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      edit.ChatID,
					Text:        text,
					ParseMode:   "HTML",
					ReplyMarkup: serverKeyboard(),
				})
			}
		} else {
			cbErr = editErr
		}
	}

	totalDur := time.Since(start)
	log.Printf("[callback] data=\"assist_server:refresh\" ack_dispatch=%v cache=0s db=0s render=%v edit=%v total=%v rpc_count=%d err=%v",
		ackDispatch, renderDur, editDur, totalDur, rpcCount, cbErr)
}

func (h *Handler) HelpCommandMenu(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "<b>Saya adalah asisten pribadi userbot Anda.</b>\n\nPilih kategori untuk melihat bantuan userbot.", ParseMode: "HTML", ReplyMarkup: helpHomeKeyboard()})
}

func (h *Handler) ServerCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	invokeStart := time.Now()
	var invokerID int64
	if update.Message.From != nil {
		invokerID = update.Message.From.ID
	}
	text := serverStatusText(invokerID, time.Since(invokeStart))
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: text, ParseMode: "HTML", ReplyMarkup: serverKeyboard()})
}

func (h *Handler) CodeCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}
	if update.Message.ReplyToMessage == nil || strings.TrimSpace(update.Message.ReplyToMessage.Text) == "" {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Reply pesan yang mau di-code terlebih dahulu."})
		return
	}
	language := "go"
	fields := strings.Fields(update.Message.Text)
	if len(fields) > 1 {
		language = fields[1]
	}
	text := fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", html.EscapeString(language), html.EscapeString(update.Message.ReplyToMessage.Text))
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, Text: text, ParseMode: "HTML", ReplyParameters: &botgotd.ReplyParameters{MessageID: update.Message.ID}})
}

func (h *Handler) HelpInline(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	HandleInline(ctx, b, update)
}

// HandleInline is called by the custom polling loop because the bot library
// version used by this project does not expose an inline-query handler type.
func HandleInline(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.InlineQuery == nil {
		return
	}
	query := strings.TrimSpace(update.InlineQuery.Query)
	if strings.HasPrefix(query, "broadcast ") {
		fields := strings.Fields(query)
		if len(fields) >= 4 {
			ownerID, ownerErr := strconv.ParseInt(fields[1], 10, 64)
			if ownerErr == nil {
				result := &botgotd.InlineQueryResultArticle{
					ID: "broadcast_monitor_" + fields[2], Title: "Proses Pengiriman",
					Description: "Monitor broadcast userbot",
					InputMessageContent: &botgotd.InputTextMessageContent{
						MessageText: fmt.Sprintf("<b>Proses Pengiriman</b>\nCek bot untuk detail broadcast anda\n<code>#AIO:%s</code>", html.EscapeString(fields[2])),
						ParseMode:   "HTML",
					},
					ReplyMarkup: broadcastStopKeyboard(ownerID, fields[2]),
				}
				_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
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
		result := &botgotd.InlineQueryResultArticle{
			ID: "broadcast_cancel", Title: "Hentikan Broadcast",
			Description:         "Permintaan penghentian broadcast dikirim.",
			InputMessageContent: &botgotd.InputTextMessageContent{MessageText: "Permintaan penghentian broadcast dikirim."},
		}
		_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
		return
	}
	if query == "server" {
		invokeStart := time.Now()
		var invokerID int64
		if update.InlineQuery != nil && update.InlineQuery.From.ID > 0 {
			invokerID = update.InlineQuery.From.ID
		}
		text := serverStatusText(invokerID, time.Since(invokeStart))
		result := &botgotd.InlineQueryResultArticle{ID: "assist_server", Title: "Server status", Description: "Status gateway dan resource sistem lengkap", InputMessageContent: &botgotd.InputTextMessageContent{MessageText: text, ParseMode: "HTML"}, ReplyMarkup: serverKeyboard()}
		_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: false})
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
				result := &botgotd.InlineQueryResultArticle{ID: "assist_code", Title: "Code", Description: "Format kode", InputMessageContent: &botgotd.InputTextMessageContent{MessageText: fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>", lang, body), ParseMode: "HTML"}}
				_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
				return
			}
		}
	}
	if strings.HasPrefix(query, "note_show") {
		parts := strings.Fields(query)
		var ref string
		for _, p := range parts {
			if strings.HasPrefix(p, "ref:") {
				ref = strings.TrimPrefix(p, "ref:")
				break
			}
		}
		if ref != "" && inlineRedis != nil {
			data, err := inlineRedis.Client.Get(ctx, "inline:note:"+ref).Bytes()
			if err == nil {
				var p NoteInlinePayload
				if err := json.Unmarshal(data, &p); err == nil {
					markup := buildNoteInlineKeyboard(p)
					msgText := p.Text
					if strings.TrimSpace(msgText) == "" {
						msgText = fmt.Sprintf("Catatan: <b>%s</b>", html.EscapeString(p.Name))
					}
					desc := msgText
					if len(desc) > 80 {
						desc = desc[:77] + "..."
					}
					result := &botgotd.InlineQueryResultArticle{
						ID:          fmt.Sprintf("note_%d_%s", p.OwnerID, p.Name),
						Title:       "Catatan: " + p.Name,
						Description: desc,
						InputMessageContent: &botgotd.InputTextMessageContent{
							MessageText: msgText,
							ParseMode:   "HTML",
						},
						ReplyMarkup: markup,
					}
					_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{
						InlineQueryID: update.InlineQuery.ID,
						Results:       []botgotd.InlineQueryResult{result},
						CacheTime:     0,
						IsPersonal:    true,
					})
					return
				}
			}
		}
		_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{}, CacheTime: 0, IsPersonal: true})
		return
	}
	result := &botgotd.InlineQueryResultArticle{
		ID:                  "assist_help",
		Title:               "Bantuan userbot",
		Description:         "Pilih kategori untuk melihat bantuan userbot.",
		InputMessageContent: &botgotd.InputTextMessageContent{MessageText: "<b>Saya adalah asisten pribadi userbot Anda.</b>\n\nPilih kategori untuk melihat bantuan userbot.", ParseMode: "HTML"},
		ReplyMarkup:         helpHomeKeyboard(),
	}
	_, _ = b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: true})
}

func mustInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func serverKeyboard() *botgotd.InlineKeyboardMarkup {
	return cachedServerMarkup
}

func serverStatusText(invokerID int64, invokeDuration time.Duration) string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Latency / Ping Server (TCP connect ke DNS 1.1.1.1:53 dengan timeout ketat 350ms agar responsif)
	pingServerStart := time.Now()
	conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 350*time.Millisecond)
	pingServerMs := float64(time.Since(pingServerStart).Microseconds()) / 1000.0
	if err == nil {
		_ = conn.Close()
	} else {
		pingServerMs = -1
	}

	// CPU Usage
	cpuPercents, _ := cpu.Percent(0, false)
	cpuUsage := 0.0
	if len(cpuPercents) > 0 {
		cpuUsage = cpuPercents[0]
	}

	// System RAM & Swap / Swapon
	vMem, _ := mem.VirtualMemory()
	sMem, _ := mem.SwapMemory()

	totalRAMMB := 0.0
	usedRAMMB := 0.0
	ramPercent := 0.0
	if vMem != nil {
		totalRAMMB = float64(vMem.Total) / 1024 / 1024
		usedRAMMB = float64(vMem.Used) / 1024 / 1024
		ramPercent = vMem.UsedPercent
	}

	totalSwapMB := 0.0
	usedSwapMB := 0.0
	swapPercent := 0.0
	if sMem != nil {
		totalSwapMB = float64(sMem.Total) / 1024 / 1024
		usedSwapMB = float64(sMem.Used) / 1024 / 1024
		swapPercent = sMem.UsedPercent
	}

	// Disk I/O
	diskStats, _ := disk.IOCounters()
	readMB := 0.0
	writeMB := 0.0
	for _, stat := range diskStats {
		readMB += float64(stat.ReadBytes) / 1024 / 1024
		writeMB += float64(stat.WriteBytes) / 1024 / 1024
	}

	// Format invoke & ping
	invokeStr := "-"
	if invokeDuration > 0 {
		invokeStr = fmt.Sprintf("%.2f ms", float64(invokeDuration.Microseconds())/1000.0)
	}
	serverPingStr := "Timeout"
	if pingServerMs >= 0 {
		serverPingStr = fmt.Sprintf("%.2f ms", pingServerMs)
	}

	userInvokeStr := "-"
	if invokerID > 0 {
		userInvokeStr = fmt.Sprintf("<code>%d</code>", invokerID)
	}

	return fmt.Sprintf(`<b>🖥 Dashboard Server</b>
━━━━━━━━━━━━━━━━━━━━
<b>User:</b> %s  <b>Invoke:</b> <code>%s</code>
<b>Ping:</b> <code>%s</code>  <b>Uptime:</b> <code>%s</code>

<b>Sistem:</b>
• CPU: <code>%.1f%%</code> (%d Cores)
• RAM: <code>%.1f / %.1f MB (%.1f%%)</code>
• Swap: <code>%.1f / %.1f MB (%.1f%%)</code>
• Disk: <code>R: %.1f MB | W: %.1f MB</code>

<b>Runtime Go:</b>
• Mem: <code>%.1f MB</code> (Heap: <code>%.1f MB</code>)
• Goroutine: <code>%d</code>
━━━━━━━━━━━━━━━━━━━━
<i>GotdBot • Infrastructure Monitor</i>`,
		userInvokeStr,
		invokeStr,
		serverPingStr,
		formatUptime(time.Since(gatewayStartedAt)),
		cpuUsage,
		runtime.NumCPU(),
		usedRAMMB,
		totalRAMMB,
		ramPercent,
		usedSwapMB,
		totalSwapMB,
		swapPercent,
		readMB,
		writeMB,
		float64(m.Sys)/1024/1024,
		float64(m.HeapAlloc)/1024/1024,
		runtime.NumGoroutine(),
	)
}

func formatUptime(d time.Duration) string {
	minutes := int(d.Minutes())
	days, minutes := minutes/1440, minutes%1440
	hours, minutes := minutes/60, minutes%60
	return fmt.Sprintf("%d hari %d jam %d menit", days, hours, minutes)
}

type NoteInlineButton struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	URL    string `json:"url,omitempty"`
	Target string `json:"target,omitempty"`
}

type NoteInlinePayload struct {
	OwnerID int64              `json:"owner_id"`
	Name    string             `json:"name"`
	Text    string             `json:"text"`
	Buttons []NoteInlineButton `json:"buttons"`
	Mode    string             `json:"mode"`
}

func buildNoteInlineKeyboard(p NoteInlinePayload) *botgotd.InlineKeyboardMarkup {
	buttons := p.Buttons
	if len(buttons) == 0 {
		if p.Mode == "callback" {
			buttons = []NoteInlineButton{{Kind: "callback", Label: p.Name, Target: p.Name}}
		} else if p.Mode == "popup" {
			buttons = []NoteInlineButton{{Kind: "popup", Label: p.Name, Target: p.Name}}
		}
	}

	var rows [][]botgotd.InlineKeyboardButton
	var currentRow []botgotd.InlineKeyboardButton

	for _, b := range buttons {
		var btn botgotd.InlineKeyboardButton
		switch b.Kind {
		case "url":
			if b.URL != "" {
				btn = botgotd.InlineKeyboardButton{Text: b.Label, URL: b.URL}
			}
		case "popup":
			target := b.Target
			if target == "" {
				target = p.Name
			}
			btn = botgotd.InlineKeyboardButton{
				Text:         b.Label,
				CallbackData: fmt.Sprintf("note:pop:%d:%s", p.OwnerID, target),
			}
		case "callback":
			target := b.Target
			if target == "" {
				target = p.Name
			}
			btn = botgotd.InlineKeyboardButton{
				Text:         b.Label,
				CallbackData: fmt.Sprintf("note:show:%d:%s", p.OwnerID, target),
			}
		}
		if btn.Text != "" {
			currentRow = append(currentRow, btn)
			if len(currentRow) >= 3 {
				rows = append(rows, currentRow)
				currentRow = nil
			}
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}
	if len(rows) == 0 {
		return nil
	}
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (h *Handler) NoteCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.CallbackQuery == nil {
		return
	}
	start := time.Now()
	data := update.CallbackQuery.Data

	var (
		ackDispatch time.Duration
		ackNetDur   time.Duration
		redisDur    time.Duration
		renderDur   time.Duration
		editDur     time.Duration
		rpcCount    = 0
		cbErr       error
	)

	parts := strings.Split(data, ":")
	if len(parts) < 4 {
		// Single ACK untuk tombol tidak valid
		ackStart := time.Now()
		_, ackErr := b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Tombol catatan tidak valid.",
			ShowAlert:       true,
		})
		ackNetDur = time.Since(ackStart)
		rpcCount = 1
		log.Printf("[callback] data=%q ack_net=%v cache=0s db=0s render=0s edit=0s total=%v rpc_count=%d err=%v",
			data, ackNetDur, time.Since(start), rpcCount, ackErr)
		return
	}

	action := parts[1] // "pop" or "show"
	ownerID, _ := strconv.ParseInt(parts[2], 10, 64)
	name := strings.ToLower(parts[3])

	// Case 1: action == "pop" (Modal Popup Alert)
	// Single synchronous ACK modal alert (menghilangkan double-ACK bug)
	if action == "pop" {
		redisStart := time.Now()
		var popText string
		if inlineRedis != nil {
			if recData, err := inlineRedis.Client.Get(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, name)).Bytes(); err == nil {
				var p NoteInlinePayload
				if err := json.Unmarshal(recData, &p); err == nil && p.Text != "" {
					popText = p.Text
				}
			}
			if popText == "" {
				popText, _ = inlineRedis.Client.Get(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, name)).Result()
			}
		}
		redisDur = time.Since(redisStart)

		if popText == "" {
			popText = fmt.Sprintf("Catatan: %s", name)
		}
		if len(popText) > 200 {
			popText = popText[:197] + "..."
		}

		ackStart := time.Now()
		_, ackErr := b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            popText,
			ShowAlert:       true,
		})
		ackNetDur = time.Since(ackStart)
		rpcCount = 1
		cbErr = ackErr

		totalDur := time.Since(start)
		log.Printf("[callback] data=%q ack_net=%v cache=0s db=%v render=0s edit=0s total=%v rpc_count=%d err=%v",
			data, ackNetDur, redisDur, totalDur, rpcCount, cbErr)
		return
	}

	// Case 2: action == "show" (Navigasi / Ganti Halaman Catatan)
	// Step 1: Dispatch ACK segera di awal (menghilangkan spinner pada tombol)
	ackStart := time.Now()
	go func(cbID string) {
		actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ackNetStart := time.Now()
		_, err := b.AnswerCallbackQuery(actx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
		})
		netDur := time.Since(ackNetStart)
		if err != nil {
			log.Printf("[callback-ack] type=note_show cbID=%s net_dur=%v err=%v", cbID, netDur, err)
		} else {
			log.Printf("[callback-ack] type=note_show cbID=%s net_dur=%v status=ok", cbID, netDur)
		}
	}(update.CallbackQuery.ID)
	ackDispatch = time.Since(ackStart)
	rpcCount++

	// Step 2: Ambil data catatan terbaru dari Redis (menjamin data tidak usang)
	redisStart := time.Now()
	var p NoteInlinePayload
	found := false
	if inlineRedis != nil {
		if recData, err := inlineRedis.Client.Get(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, name)).Bytes(); err == nil {
			if err := json.Unmarshal(recData, &p); err == nil {
				found = true
			}
		}
		if !found {
			if text, err := inlineRedis.Client.Get(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, name)).Result(); err == nil && text != "" {
				p = NoteInlinePayload{
					OwnerID: ownerID,
					Name:    name,
					Text:    text,
					Mode:    "callback",
				}
				found = true
			}
		}
	}
	redisDur = time.Since(redisStart)

	// Step 3: Render keyboard & text
	renderStart := time.Now()
	var (
		markup  *botgotd.InlineKeyboardMarkup
		msgText string
	)
	if !found {
		msgText = fmt.Sprintf("Catatan <b>%s</b> tidak ditemukan.", html.EscapeString(name))
		markup = nil
	} else {
		markup = buildNoteInlineKeyboard(p)
		msgText = p.Text
		if strings.TrimSpace(msgText) == "" {
			msgText = fmt.Sprintf("Catatan: <b>%s</b>", html.EscapeString(p.Name))
		}
	}
	renderDur = time.Since(renderStart)

	// Step 4: Edit message
	edit := &botgotd.EditMessageTextParams{
		Text:               msgText,
		ParseMode:          "HTML",
		ReplyMarkup:        markup,
		RawInlineMessageID: update.CallbackQuery.RawInlineMessageID,
	}

	if update.CallbackQuery.RawInlineMessageID != nil || update.CallbackQuery.InlineMessageID != "" {
		edit.InlineMessageID = update.CallbackQuery.InlineMessageID
	} else if update.CallbackQuery.Message.Message != nil {
		edit.ChatID = update.CallbackQuery.Message.Message.Chat.ID
		edit.MessageID = update.CallbackQuery.Message.Message.ID
	} else {
		return
	}

	editStart := time.Now()
	_, editErr := b.EditMessageText(ctx, edit)
	editDur = time.Since(editStart)
	rpcCount++

	if editErr != nil && !isMessageNotModifiedErr(editErr) {
		errStr := strings.ToLower(editErr.Error())
		if strings.Contains(errStr, "can't be edited") {
			if edit.ChatID != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      edit.ChatID,
					Text:        msgText,
					ParseMode:   "HTML",
					ReplyMarkup: markup,
				})
			}
		} else {
			cbErr = editErr
		}
	}

	totalDur := time.Since(start)
	log.Printf("[callback] data=%q ack_dispatch=%v cache=0s db=%v render=%v edit=%v total=%v rpc_count=%d err=%v",
		data, ackDispatch, redisDur, renderDur, editDur, totalDur, rpcCount, cbErr)
}
