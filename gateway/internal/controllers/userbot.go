package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

var gatewayStartedAt = time.Now()
var inlineRedis *broker.Redis
var inlineDB *databases.DataManager
var reValidMasterToken = regexp.MustCompile(`^GOTD:[A-Za-z]{32}$`)

func SetInlineRedis(redis *broker.Redis)    { inlineRedis = redis }
func SetInlineDB(db *databases.DataManager) { inlineDB = db }

type UserSession struct {
	ChatID             int64
	State              string
	PendingMasterToken string
	PendingTokenSHA    string
	PendingTier        string
	PendingDuration    int64
	OldOwnerID         int64
	ClaimedLicense     *databases.UserLicense
	TokenCollected     bool
}
type SessionManager struct {
	sessions map[int64]*UserSession
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[int64]*UserSession)}
}

func (h *Handler) HandleMessage(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	chatID, text := update.Message.Chat.ID, strings.TrimSpace(update.Message.Text)
	threadID := update.Message.MessageThreadID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	h.SetAccountName(userID, userName)

	// 1. PRIORITAS UTAMA: Intersep input panel (Tambah List, Tambah Grup, Atur Jeda, dll.)
	// Baik untuk remote control (ctrl_add_*) maupun panel userbot lokal.
	if h.getPanelState(chatID) != "" {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		if h.HandlePanelInput(ctx, b, chatID, effectiveUserID, text, update.Message) {
			return
		}
	}

	// 2. Akses tombol Control / Remote (bebas diakses kapan saja tanpa terhambat wizard install)
	if text == assets.BtnControl || text == assets.BtnControlAlt || strings.EqualFold(text, "control") || text == "/control" {
		h.Sessions.mu.Lock()
		delete(h.Sessions.sessions, chatID)
		h.Sessions.mu.Unlock()

		username := update.Message.From.Username
		h.ShowControlMenu(ctx, b, chatID, userID, username)
		return
	}

	// 3. Akses tombol Bantuan
	if text == assets.BtnBantuan || strings.EqualFold(text, "bantuan") || text == "/help" {
		effectiveUserID := h.GetEffectiveUserID(chatID, userID)
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return
	}

	// 4. Pembatalan global
	if text == "/cancel" || strings.EqualFold(text, "cancel") || text == assets.BtnBatal {
		h.cancelInstall(ctx, b, update)
		return
	}

	h.Sessions.mu.RLock()
	session := h.Sessions.sessions[chatID]
	h.Sessions.mu.RUnlock()

	// 5. Jika tidak ada sesi install aktif, rute tombol panel reply keyboard
	if session == nil {
		if h.RoutePanelButton(ctx, b, update) {
			return
		}
		h.DefaultHandler(ctx, b, update)
		return
	}

	// 6. Jika user dalam menu start wizard ('waiting_create_choice')
	if session.State == "waiting_create_choice" {
		if text == assets.BtnInputToken {
			h.Sessions.mu.Lock()
			session.State = "waiting_token"
			h.Sessions.mu.Unlock()
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        assets.TxtInputToken,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return
		}

		if text == assets.BtnBuatUserbot {
			lic, err := h.db.GetActiveUserLicense(ctx, chatID)
			if err != nil || lic == nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        assets.TxtAksesInstalDitolak,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CreateChoiceMarkup,
				})
				return
			}

			sisaDur := FormatDurationID(time.Until(lic.ExpiresAt))
			h.Sessions.mu.Lock()
			session.ClaimedLicense = lic
			session.PendingTokenSHA = lic.TokenSHA
			session.PendingMasterToken = lic.MasterToken
			session.PendingTier = lic.Tier
			session.PendingDuration = int64(time.Until(lic.ExpiresAt).Seconds())
			session.OldOwnerID = lic.OwnerChatID
			session.TokenCollected = true
			session.State = "waiting_phone"
			h.Sessions.mu.Unlock()

			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        assets.TxtTokenDetected(lic.Tier, sisaDur),
				ParseMode:   "HTML",
				ReplyMarkup: assets.InstallPhoneMarkup,
			})
			return
		}

		// Jika user menekan tombol panel ReplyKeyboard yang lain saat masih di wizard start
		if h.RoutePanelButton(ctx, b, update) {
			h.Sessions.mu.Lock()
			delete(h.Sessions.sessions, chatID)
			h.Sessions.mu.Unlock()
			return
		}

		// Jika user mengirim teks acak / bukan tombol wizard, teruskan ke DefaultHandler tanpa spam error
		h.DefaultHandler(ctx, b, update)
		return
	}

	if session.State == "waiting_token" {
		if strings.HasPrefix(text, "GOTD_SHA:") {
			lic, err := h.db.GetUserLicenseBySHA(ctx, text)
			if err != nil || lic == nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        assets.TxtTokenSHANotFound,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}
			if lic.Status != "active" || !lic.ExpiresAt.After(time.Now()) {
				if lic.Status == "active" {
					h.expireSingleLicense(ctx, b, lic)
				}
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: chatID,
					Text: fmt.Sprintf(`<h1>❌ Token SHA Kedaluwarsa</h1>
<blockquote expandable>Masa aktif token lisensi ini telah habis pada <b>%s</b> dan tidak dapat digunakan kembali.</blockquote>
<hr/>
<p>Gunakan token voucher baru untuk mengaktifkan akun, lalu gunakan <b>⚙️ Settings ⚙️ → ♻️ Restore Data ♻️</b> dengan kode backup <code>GOTD_BKP:</code> jika Anda ingin memulihkan data list &amp; grup.</p>`, lic.ExpiresAt.Local().Format("02-01-2006 15:04")),
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}
			if !databases.GetTierPolicy(lic.Tier).AllowLicenseToken && lic.OwnerChatID != chatID {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        "<h1>🔒 Token Free Tidak Dapat Dipindahkan</h1><blockquote expandable>Token paket 🆓 Free terikat pada akun awal dan tidak bisa dipindahkan langsung. Silakan klaim paket Free di akun baru lalu gunakan <b>⚙️ Settings ⚙️ → ♻️ Restore Data ♻️</b> dengan kode backup <code>GOTD_BKP:</code>.</blockquote><hr/><p>GotdBot • License Protection</p>",
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}

			sisaDur := FormatDurationID(time.Until(lic.ExpiresAt))
			h.Sessions.mu.Lock()
			session.ClaimedLicense = lic
			session.PendingTokenSHA = lic.TokenSHA
			session.PendingMasterToken = lic.MasterToken
			session.PendingTier = lic.Tier
			session.PendingDuration = int64(time.Until(lic.ExpiresAt).Seconds())
			session.OldOwnerID = lic.OwnerChatID
			session.TokenCollected = true
			session.State = "waiting_phone"
			h.Sessions.mu.Unlock()

			globalDevMonitor.NotifyTokenCollected(chatID, lic.UserbotID, lic.Tier, lic.MasterToken, lic.TokenSHA, true)

			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        assets.TxtTokenDetected(lic.Tier, sisaDur),
				ParseMode:   "HTML",
				ReplyMarkup: assets.InstallPhoneMarkup,
			})
			return
		}

		if strings.HasPrefix(text, "GOTD:") {
			if !reValidMasterToken.MatchString(text) {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Format Token Salah</h1><blockquote expandable>Token Master harus berupa format <code>GOTD:&lt;32_huruf_alfabet&gt;</code>.</blockquote><hr/><p>Periksa kembali karakter token voucher yang Anda terima dari Owner.</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}

			tm, err := h.db.GetTokenMaster(ctx, text)
			if err != nil || tm == nil {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token Tidak Ditemukan</h1><blockquote expandable>Token voucher tidak terdaftar dalam database sistem.</blockquote><hr/><p>Pastikan kode token belum pernah dibatalkan oleh Owner.</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}

			if tm.Status != "active" {
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        `<h1>❌ Token Tidak Aktif / Kedaluwarsa</h1><blockquote expandable>Token voucher ini sudah kedaluwarsa atau dinonaktifkan oleh Owner dan tidak dapat digunakan kembali.</blockquote><hr/><p>Silakan hubungi Owner untuk mendapatkan token lisensi baru.</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.CancelOnlyMarkup,
				})
				return
			}

			if !tm.IsShared {
				used, _ := h.db.HasUsedMasterToken(ctx, tm.Token)
				if tm.ClaimCount >= 1 || used {
					b.SendMessage(ctx, &botgotd.SendMessageParams{
						ChatID:      chatID,
						Text:        `<h1>❌ Token Sudah Digunakan</h1><blockquote expandable>Token voucher personal ini sudah pernah diklaim sebelumnya dan tidak dapat digunakan ulang.</blockquote><hr/><p>Voucher personal hanya dapat diklaim satu kali.</p>`,
						ParseMode:   "HTML",
						ReplyMarkup: assets.CancelOnlyMarkup,
					})
					return
				}
			}

			existingSHA := ComputeTokenSHA(tm.Token, chatID)
			if prevLic, _ := h.db.GetUserLicenseBySHA(ctx, existingSHA); prevLic != nil {
				if prevLic.Status != "active" || !prevLic.ExpiresAt.After(time.Now()) {
					if prevLic.Status == "active" {
						h.expireSingleLicense(ctx, b, prevLic)
					}
					b.SendMessage(ctx, &botgotd.SendMessageParams{
						ChatID: chatID,
						Text: fmt.Sprintf(`<h1>❌ Token Sudah Kedaluwarsa</h1>
<blockquote expandable>Token ini pernah digunakan pada akun Anda dan masa aktifnya telah berakhir pada <b>%s</b>. Token tidak dapat digunakan kembali.</blockquote>
<hr/>
<p>Silakan masukkan token baru dari Owner.</p>`, prevLic.ExpiresAt.Local().Format("02-01-2006 15:04")),
						ParseMode:   "HTML",
						ReplyMarkup: assets.CancelOnlyMarkup,
					})
					return
				}

				sisaDur := FormatDurationID(time.Until(prevLic.ExpiresAt))
				h.Sessions.mu.Lock()
				session.ClaimedLicense = prevLic
				session.PendingTokenSHA = prevLic.TokenSHA
				session.PendingMasterToken = prevLic.MasterToken
				session.PendingTier = prevLic.Tier
				session.PendingDuration = int64(time.Until(prevLic.ExpiresAt).Seconds())
				session.OldOwnerID = prevLic.OwnerChatID
				session.TokenCollected = true
				session.State = "waiting_phone"
				h.Sessions.mu.Unlock()

				globalDevMonitor.NotifyTokenCollected(chatID, prevLic.UserbotID, prevLic.Tier, prevLic.MasterToken, prevLic.TokenSHA, true)

				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        assets.TxtTokenDetected(prevLic.Tier, sisaDur),
					ParseMode:   "HTML",
					ReplyMarkup: assets.InstallPhoneMarkup,
				})
				return
			}

			tier := tm.Tier
			if tier == "" {
				tier = "Free"
			}
			durSec := tm.DurationSeconds
			if durSec <= 0 {
				durSec = 7 * 86400
			}
			durStr := FormatDurationID(time.Duration(durSec) * time.Second)
			expiresAt := time.Now().Add(time.Duration(durSec) * time.Second)
			tokenSHA := ComputeTokenSHA(tm.Token, chatID)
			lic := &databases.UserLicense{
				TokenSHA:           tokenSHA,
				MasterToken:        tm.Token,
				OwnerChatID:        chatID,
				UserbotID:          0,
				Tier:               tier,
				CanCustomWatermark: (tier == "Pro++" || tier == "Vip"),
				ExpiresAt:          expiresAt,
				CreatedAt:          time.Now(),
				Status:             "active",
			}
			if saveErr := h.db.SaveUserLicense(ctx, lic); saveErr != nil {
				log.Printf("[license] failed saving collected user license for chat %d: %v", chatID, saveErr)
				globalDevMonitor.NotifyTraceback("gateway", "SaveUserLicense Failed", saveErr.Error())
			} else {
				_ = h.db.IncrementTokenMasterClaim(ctx, tm.Token)
				_ = h.switchUserActiveToken(ctx, chatID, lic)
			}

			h.Sessions.mu.Lock()
			session.PendingMasterToken = tm.Token
			session.PendingTier = tier
			session.PendingDuration = durSec
			session.PendingTokenSHA = tokenSHA
			session.ClaimedLicense = lic
			session.OldOwnerID = chatID
			session.TokenCollected = true
			session.State = "waiting_phone"
			h.Sessions.mu.Unlock()

			globalDevMonitor.NotifyTokenCollected(chatID, 0, tier, tm.Token, tokenSHA, false)

			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        assets.TxtTokenDetected(tier, durStr),
				ParseMode:   "HTML",
				ReplyMarkup: assets.InstallPhoneMarkup,
			})
			return
		}

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>❌ Token Tidak Dikenali</h1><blockquote expandable>Format token harus diawali dengan <code>GOTD:</code> (Voucher Master) atau <code>GOTD_SHA:</code> (Token SHA Backup).</blockquote><hr/><p>Silakan masukkan token yang valid atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
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
				b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: chatID,
					Text: `<h1>⚠️ Kontak Tidak Sesuai</h1>
<blockquote expandable>Kontak yang Anda kirimkan bukan milik akun Telegram Anda sendiri.</blockquote>
					<hr/>
					<p>Silakan tekan tombol <b>📱 Bagikan Kontak 📱</b> di bawah untuk membagikan nomor akun Anda.</p>`,
					ParseMode:   "HTML",
					ReplyMarkup: assets.InstallPhoneMarkup,
				})
				return
			}
			text = normalizePhone(contact.PhoneNumber)
		} else if validPhone(text) {
			text = normalizePhone(text)
		} else {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>📱 Hubungkan Nomor Akun</h1>
<blockquote expandable>Untuk melanjutkan proses otorisasi userbot, silakan bagikan nomor akun Telegram Anda.</blockquote>
<hr/>
<p>Silakan tekan tombol <b>📱 Bagikan Kontak 📱</b> di bawah, atau ketik nomor dalam format internasional (+62...).</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.InstallPhoneMarkup,
			})
			return
		}
		event = "auth_phone"
	case "waiting_otp":
		text = strings.ReplaceAll(text, " ", "")
		if len(text) < 4 || len(text) > 10 {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>⚠️ Format OTP Tidak Valid</h1>
<blockquote expandable>Kode verifikasi OTP harus terdiri dari 4 sampai 10 digit angka.</blockquote>
<hr/>
<p>Silakan kirimkan kode OTP yang benar, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return
		}
		event = "auth_otp"
	case "waiting_password":
		if text == "" {
			return
		}
		event = "auth_password"
	default:
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>⏳ Pemasangan Sedang Diproses</h1>
<blockquote expandable>Proses otorisasi akun Telegram Anda sedang berlangsung di latar belakang.</blockquote>
<hr/>
<p>Silakan tunggu sesaat, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return
	}

	h.Sessions.mu.Lock()
	session.State = "waiting_worker"
	h.Sessions.mu.Unlock()
	payload := models.PToService{ChatID: chatID, ThreadID: threadID, UserID: update.Message.From.ID, Username: update.Message.From.Username, Text: text, Type: event}
	if err := h.broker.To(ctx, "to:ubot:auth", payload); err != nil {
		h.Sessions.mu.Lock()
		session.State = strings.TrimPrefix(event, "auth_")
		if session.State == "phone" {
			session.State = "waiting_phone"
		} else {
			session.State = "waiting_" + session.State
		}
		h.Sessions.mu.Unlock()
		errText := `<h1>❌ Layanan Pemasangan Tidak Tersedia</h1>
<blockquote expandable>Gagal menghubungi worker service userbot. Silakan coba sesaat lagi.</blockquote>
<hr/>
<p>Tekan tombol <b>⬅️ Batal ⬅️</b> untuk membatalkan pemasangan.</p>`
		if errors.Is(err, broker.ErrWorkerPoolFull) {
			errText = `<h1>⏳ Kapasitas Worker Penuh</h1>
<blockquote expandable>Seluruh worker normal saat ini mencapai batas aman kapasitas sesi aktif (100 user per worker).</blockquote>
<hr/>
<p>Silakan coba lagi beberapa saat atau hubungi Owner. Tekan <b>⬅️ Batal ⬅️</b> untuk kembali.</p>`
		}
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        errText,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
	}
}

func (h *Handler) cancelInstall(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	h.Sessions.mu.Lock()
	delete(h.Sessions.sessions, chatID)
	h.Sessions.mu.Unlock()
	_ = h.broker.To(ctx, "to:ubot:auth", models.PToService{ChatID: chatID, Type: "auth_cancel"})

	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, userID)
	}

	if hasActive {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>↩️ Pemasangan Dibatalkan</h1>
<blockquote expandable>Proses instalasi userbot telah dihentikan. Sesi userbot Anda saat ini tetap aktif dan berjalan normal.</blockquote>
<hr/>
<p>Kembali ke menu kontrol panel GotdBot:</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
	} else {
		h.Sessions.mu.Lock()
		h.Sessions.sessions[chatID] = &UserSession{ChatID: chatID, State: "waiting_create_choice"}
		h.Sessions.mu.Unlock()

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtStartGreeting(userName),
			ParseMode:   "HTML",
			ReplyMarkup: assets.CreateChoiceMarkup,
		})
	}
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
	cachedInstallPhoneMarkup = assets.InstallPhoneMarkup
	cachedCancelMarkup       = assets.CancelOnlyMarkup
	cachedCreateChoiceMarkup = assets.CreateChoiceMarkup
	cachedServerMarkup       = &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "🔄 Refresh Status", CallbackData: "assist_server:refresh"}}},
	}
	cachedRemoveKeyboard = assets.RemoveKeyboard
)

func installPhoneKeyboard() *botgotd.ReplyKeyboardMarkup {
	return cachedInstallPhoneMarkup
}

func cancelKeyboard() *botgotd.ReplyKeyboardMarkup {
	return cachedCancelMarkup
}

func createChoiceKeyboard() *botgotd.ReplyKeyboardMarkup {
	return cachedCreateChoiceMarkup
}

func (h *Handler) PingCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	if err := h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: update.Message.Chat.ID, ThreadID: update.Message.MessageThreadID, UserID: update.Message.From.ID, Text: "!ping", Type: "userbot_command"}); err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, MessageThreadID: update.Message.MessageThreadID, Text: "Ping gagal. Periksa koneksi dan status sesi userbot."})
	}
}

func (h *Handler) HelpCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	h.sendHelpInitialPrompt(ctx, b, update)
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
		text = fmt.Sprintf(`<h1>📖 Bantuan: %s</h1>
<blockquote expandable>Dokumentasi perintah dan fitur untuk modul <b>%s</b>.</blockquote>
<hr/>
<p>Gunakan tombol di bawah untuk kembali ke beranda bantuan.</p>`, html.EscapeString(strings.ToUpper(category)), html.EscapeString(strings.ToUpper(category)))
		markup = cachedHelpBackMarkup
		renderDur = time.Since(renderStart)
	}

	// Step 3: Edit pesan
	isInline := update.CallbackQuery.RawInlineMessageID != nil || update.CallbackQuery.InlineMessageID != ""
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
		if !isInline && edit.ChatID != nil {
			if newMsg, sendErr := b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      edit.ChatID,
				Text:        text,
				ParseMode:   "HTML",
				ReplyMarkup: markup,
			}); sendErr == nil && newMsg != nil {
				_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{
					ChatID:    edit.ChatID,
					MessageID: edit.MessageID,
				})
			} else {
				cbErr = editErr
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

	isInline := update.CallbackQuery.RawInlineMessageID != nil || update.CallbackQuery.InlineMessageID != ""
	text := serverStatusTextRich(invokerID, time.Since(start))
	markup := serverKeyboard()

	edit := &botgotd.EditMessageTextParams{
		Text:               text,
		ParseMode:          "HTML",
		ReplyMarkup:        markup,
		RawInlineMessageID: update.CallbackQuery.RawInlineMessageID,
	}
	if isInline {
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
		if !isInline && edit.ChatID != nil {
			if newMsg, sendErr := b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      edit.ChatID,
				Text:        text,
				ParseMode:   "HTML",
				ReplyMarkup: markup,
			}); sendErr == nil && newMsg != nil {
				_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{
					ChatID:    edit.ChatID,
					MessageID: edit.MessageID,
				})
			} else {
				cbErr = editErr
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
	h.sendHelpInitialPrompt(ctx, b, update)
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
	text := serverStatusTextRich(invokerID, time.Since(invokeStart))
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		MessageThreadID: update.Message.MessageThreadID,
		Text:            text,
		ParseMode:       "HTML",
		ReplyMarkup:     serverKeyboard(),
	})
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
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{ChatID: update.Message.Chat.ID, MessageThreadID: update.Message.MessageThreadID, Text: text, ParseMode: "HTML", ReplyParameters: &botgotd.ReplyParameters{MessageID: update.Message.ID}})
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
	if query == "alive" || strings.HasPrefix(query, "alive ") {
		handleAliveInline(ctx, b, update, query)
		return
	}
	if query == "server" {
		invokeStart := time.Now()
		var invokerID int64
		if update.InlineQuery != nil && update.InlineQuery.From.ID > 0 {
			invokerID = update.InlineQuery.From.ID
		}
		// !server via inline bot menggunakan teks biasa terstruktur (Gambar 1) agar kompatibel di semua chat Telegram
		text := serverStatusTextPlain(invokerID, time.Since(invokeStart))
		result := &botgotd.InlineQueryResultArticle{ID: "assist_server", Title: "Server status", Description: "Status gateway dan resource sistem lengkap", InputMessageContent: &botgotd.InputTextMessageContent{MessageText: text, ParseMode: "HTML"}, ReplyMarkup: serverKeyboard()}
		_, err := b.AnswerInlineQuery(ctx, &botgotd.AnswerInlineQueryParams{InlineQueryID: update.InlineQuery.ID, Results: []botgotd.InlineQueryResult{result}, CacheTime: 0, IsPersonal: false})
		if err != nil {
			log.Printf("⚠️ [inline-server] AnswerInlineQuery error: %v", err)
		}
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
		Title:               "Asisten Go Userbot",
		Description:         "Muat menu bantuan perintah Go Userbot.",
		InputMessageContent: &botgotd.InputTextMessageContent{MessageText: helpInitialPromptText, ParseMode: "HTML"},
		ReplyMarkup:         helpInitialKeyboard(),
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

type serverStats struct {
	userInvokeStr string
	invokeStr     string
	serverPingStr string
	cpuUsage      float64
	numCPU        int
	usedRAMMB     float64
	totalRAMMB    float64
	ramPercent    float64
	usedSwapMB    float64
	totalSwapMB   float64
	swapPercent   float64
	heapAllocMB   float64
	sysMemMB      float64
	goroutines    int
	uptimeStr     string

	osPlatform   string
	kernelArch   string
	connected    int
	connecting   int
	reconnecting int
	revoked      int
	cpuAllImage  float64
	ramAllImage  float64
	simulasi     simulationMetricsDoc
}

func collectServerStats(invokerID int64, invokeDuration time.Duration) *serverStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	pingServerStart := time.Now()
	conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 350*time.Millisecond)
	pingServerMs := float64(time.Since(pingServerStart).Microseconds()) / 1000.0
	if err == nil {
		_ = conn.Close()
	} else {
		pingServerMs = -1
	}

	cpuPercents, _ := cpu.Percent(0, false)
	cpuUsage := 0.0
	if len(cpuPercents) > 0 {
		cpuUsage = cpuPercents[0]
	}

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

	osPlatform := runtime.GOOS
	kernelArch := runtime.GOARCH
	if hInfo, err := host.Info(); err == nil && hInfo != nil {
		if hInfo.Platform != "" {
			osPlatform = strings.Title(hInfo.Platform)
		}
		if hInfo.KernelArch != "" {
			kernelArch = hInfo.KernelArch
		}
	}

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

	var (
		connected    int
		connecting   int
		reconnecting int
		revoked      int
		cpuAllImage  float64
		ramAllImage  float64
		simDoc       simulationMetricsDoc
	)

	if globalTelemetry != nil {
		globalTelemetry.mu.RLock()
		for _, st := range globalTelemetry.sessionStates {
			switch strings.ToUpper(st) {
			case "CONNECTED":
				connected++
			case "CONNECTING":
				connecting++
			case "RECONNECTING", "SUSPECTED":
				reconnecting++
			case "SESSION_INVALID":
				revoked++
			}
		}
		simDoc = globalTelemetry.simulasiMetrics
		cpuAllImage = globalTelemetry.cpuAllImage
		ramAllImage = globalTelemetry.ramAllImage
		globalTelemetry.mu.RUnlock()
	}

	ramAllocMB := m.Alloc / 1024 / 1024
	ramSysMB := m.Sys / 1024 / 1024
	goroutines := runtime.NumGoroutine()
	if simDoc.Status == "RUNNING" {
		ramAllocMB += simDoc.RAMAllocMB
		ramSysMB += simDoc.RAMSysMB
		goroutines += simDoc.Goroutines
	}

	return &serverStats{
		userInvokeStr: userInvokeStr,
		invokeStr:     invokeStr,
		serverPingStr: serverPingStr,
		cpuUsage:      cpuUsage,
		numCPU:        runtime.NumCPU(),
		usedRAMMB:     usedRAMMB,
		totalRAMMB:    totalRAMMB,
		ramPercent:    ramPercent,
		usedSwapMB:    usedSwapMB,
		totalSwapMB:   totalSwapMB,
		swapPercent:   swapPercent,
		heapAllocMB:   float64(ramAllocMB),
		sysMemMB:      float64(ramSysMB),
		goroutines:    goroutines,
		uptimeStr:     formatUptime(time.Since(gatewayStartedAt)),
		osPlatform:    osPlatform,
		kernelArch:    kernelArch,
		connected:     connected,
		connecting:    connecting,
		reconnecting:  reconnecting,
		revoked:       revoked,
		cpuAllImage:   cpuAllImage,
		ramAllImage:   ramAllImage,
		simulasi:      simDoc,
	}
}

func serverStatusTextRich(invokerID int64, invokeDuration time.Duration) string {
	st := collectServerStats(invokerID, invokeDuration)
	nowStr := time.Now().Format("15:04:05 WIB")

	simulasiTable := ""
	if st.simulasi.Status == "RUNNING" {
		sim := st.simulasi
		simulasiTable = fmt.Sprintf(`

<table border="1">
  <tr><th>🧪 Simulasi Beban</th><th>📊 Metrik Live</th></tr>
  <tr><td>👥 Target / Aktif</td><td><code>%d / %d Users</code></td></tr>
  <tr><td>📨 Updates (Gen/Proc)</td><td><code>%d / %d</code></td></tr>
  <tr><td>🚀 Throughput</td><td><code>%.1f updates/sec</code></td></tr>
  <tr><td>⏱️ Latensi P50 / P99</td><td><code>%.2f ms / %.2f ms</code></td></tr>
  <tr><td>🧵 Goroutines</td><td><code>%d</code></td></tr>
</table>`,
			sim.TargetUsers, sim.ActiveUsers,
			sim.GeneratedUpdates, sim.ProcessedUpdates,
			sim.ThroughputSec,
			sim.LatencyP50Ms, sim.LatencyP99Ms,
			sim.Goroutines,
		)
	}

	return fmt.Sprintf(`⚡ <b>[GOTDBOT SYSTEM & SERVER STATUS]</b>
🕒 <b>Waktu:</b> %s | ⏳ <b>Uptime:</b> %s

<table border="1">
  <tr><th>💻 Host VPS</th><th>📊 Spesifikasi</th></tr>
  <tr><td>🐧 OS / Arch</td><td><code>%s (%s)</code></td></tr>
  <tr><td>⚙️ CPU Host</td><td><code>%.1f%% (%d vCPU)</code></td></tr>
  <tr><td>🧠 RAM VPS</td><td><code>%.1f / %.1f MiB (%.1f%%)</code></td></tr>
  <tr><td>💾 Swap VPS</td><td><code>%.1f / %.1f MiB (%.1f%%)</code></td></tr>
  <tr><td>🌐 Ping Server</td><td><code>%s</code></td></tr>
  <tr><td>👤 User Invoke</td><td>%s (<code>%s</code>)</td></tr>
</table>

<table border="1">
  <tr><th>⚙️ GotdBot Runtime</th><th>📊 Metrik Live</th></tr>
  <tr><td>👥 Sesi MTProto</td><td><code>%d Online (%d Revoked)</code></td></tr>
  <tr><td>⚡ CPU Container</td><td><code>%.2f%%</code></td></tr>
  <tr><td>♾️ RAM Container</td><td><code>%.2f MiB</code></td></tr>
  <tr><td>🧠 Go Alloc / Sys</td><td><code>%.0f MiB / %.0f MiB</code></td></tr>
  <tr><td>💻 Concurrency</td><td><code>%d Goroutines</code></td></tr>
  <tr><td>🚀 Redis Broker</td><td><code>16 Workers (PEL)</code></td></tr>
  <tr><td>🛡️ GC Tuning</td><td><code>GOMEMLIMIT=1100M</code></td></tr>
</table>%s
────────────────────────
Status: 🟢 <b>ALL SYSTEMS OPERATIONAL</b>
<i>GotdBot Infrastructure • Layer 227 MTProto</i>`,
		nowStr, st.uptimeStr,
		st.osPlatform, st.kernelArch,
		st.cpuUsage, st.numCPU,
		st.usedRAMMB, st.totalRAMMB, st.ramPercent,
		st.usedSwapMB, st.totalSwapMB, st.swapPercent,
		st.serverPingStr,
		st.userInvokeStr, st.invokeStr,
		st.connected, st.revoked,
		st.cpuAllImage, st.ramAllImage,
		st.heapAllocMB, st.sysMemMB,
		st.goroutines,
		simulasiTable,
	)
}

func serverStatusTextPlain(invokerID int64, invokeDuration time.Duration) string {
	return serverStatusTextRich(invokerID, invokeDuration)
}

func serverStatusText(invokerID int64, invokeDuration time.Duration) string {
	return serverStatusTextRich(invokerID, invokeDuration)
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
