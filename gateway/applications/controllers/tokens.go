package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/databases"
)

const alphabetChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GenerateMasterToken creates a 32-letter alphabetical token prefixed with GOTD:
func GenerateMasterToken() string {
	bytes := make([]byte, 32)
	max := len(alphabetChars)
	lim := 256 - (256 % max)
	for i := 0; i < 32; {
		b := make([]byte, 1)
		_, _ = rand.Read(b)
		if int(b[0]) < lim {
			bytes[i] = alphabetChars[int(b[0])%max]
			i++
		}
	}
	return "GOTD:" + string(bytes)
}

// ComputeTokenSHA creates the backup token: GOTD_SHA:<sha256(masterToken:userID)>
func ComputeTokenSHA(masterToken string, telegramUserID int64) string {
	payload := fmt.Sprintf("%s:%d", strings.TrimSpace(masterToken), telegramUserID)
	hash := sha256.Sum256([]byte(payload))
	return "GOTD_SHA:" + hex.EncodeToString(hash[:])
}

var reDurationPart = regexp.MustCompile(`(?i)(\d+)\s*([hjm])`)

// ParseDurationStr parses formats like "2h4j", "30h", "1h30m"
// 'h' = Hari (24 hours), 'j' = Jam (1 hour), 'm' = Menit (1 minute)
func ParseDurationStr(input string) (time.Duration, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, fmt.Errorf("durasi kosong")
	}

	matches := reDurationPart.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("format durasi tidak valid (contoh: 2h4j, 30h, 1h30m)")
	}

	var total time.Duration
	for _, m := range matches {
		val, err := strconv.Atoi(m[1])
		if err != nil || val <= 0 {
			continue
		}
		unit := strings.ToLower(m[2])
		switch unit {
		case "h":
			total += time.Duration(val) * 24 * time.Hour
		case "j":
			total += time.Duration(val) * time.Hour
		case "m":
			total += time.Duration(val) * time.Minute
		}
	}

	if total <= 0 {
		return 0, fmt.Errorf("total durasi harus lebih dari 0")
	}
	return total, nil
}

// FormatDurationID formats duration to Indonesian string
func FormatDurationID(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d Hari", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d Jam", hours))
	}
	if minutes > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf("%d Menit", minutes))
	}
	if len(parts) == 0 {
		return "Kurang dari 1 Menit"
	}
	return strings.Join(parts, " ")
}

func isOwner(userID int64) bool {
	ownerEnv := strings.TrimSpace(os.Getenv("OWNER"))
	if ownerEnv == "" {
		return false
	}
	ownerID, _ := strconv.ParseInt(ownerEnv, 10, 64)
	return ownerID > 0 && ownerID == userID
}

// TokenCommand handles /token commands by Owner
func (h *Handler) TokenCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	senderID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	if !isOwner(senderID) {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⛔ <b>Akses Ditolak.</b> Hanya Owner sistem yang berwenang mengelola token lisensi.",
			ParseMode: "HTML",
		})
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	fields := strings.Fields(text)

	if len(fields) < 2 {
		helpText := `<b>🔑 Panduan Perintah /token (Khusus Owner)</b>
━━━━━━━━━━━━━━━━━━━━
1. <b>Token Massal Free:</b>
   <code>/token -free</code> — Generate/lihat shared token free massal.

2. <b>Token Lisensi Personal:</b>
   <code>/token pro &lt;durasi&gt;</code> (Contoh: <code>/token pro 2h4j</code>)
   <code>/token pro++ &lt;durasi&gt;</code> (Contoh: <code>/token pro++ 30h</code>)
   <code>/token vip &lt;durasi&gt;</code> (Contoh: <code>/token vip 365h</code>)

<i>Format durasi: <b>h</b> (Hari), <b>j</b> (Jam), <b>m</b> (Menit).</i>`
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      helpText,
			ParseMode: "HTML",
		})
		return
	}

	subCmd := strings.ToLower(fields[1])

	// Case 1: /token -free
	if subCmd == "-free" || subCmd == "free" {
		existing, err := h.db.GetActiveSharedFreeToken(ctx)
		if err != nil {
			log.Printf("[token] GetActiveSharedFreeToken err: %v", err)
		}

		var tm *databases.TokenMaster
		if existing != nil {
			tm = existing
		} else {
			tm = &databases.TokenMaster{
				Token:           GenerateMasterToken(),
				Tier:            "Free",
				DurationSeconds: int64((7 * 24 * time.Hour).Seconds()), // default 7 hari
				IsShared:        true,
				ClaimCount:      0,
				CreatedBy:       senderID,
				CreatedAt:       time.Now(),
				Status:          "active",
			}
			if saveErr := h.db.SaveTokenMaster(ctx, tm); saveErr != nil {
				log.Printf("[token] SaveTokenMaster free err: %v", saveErr)
			}
		}

		durStr := FormatDurationID(time.Duration(tm.DurationSeconds) * time.Second)
		resp := fmt.Sprintf(`🎫 <b>Token Master Free GotdBot</b>
━━━━━━━━━━━━━━━━━━━━
🔑 <b>Token:</b> <code>%s</code>
⏱️ <b>Durasi:</b> <code>%s</code>
👥 <b>Jumlah Claimed:</b> <code>%d user</code>
🟢 <b>Status:</b> <code>Active</code> (Shared Voucher)

<i>Token ini dapat digunakan oleh semua pengguna secara massal.</i>`,
			tm.Token, durStr, tm.ClaimCount,
		)

		inlineMarkup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "⏱️ Set Durasi Token Free", CallbackData: "token_set_duration:menu:" + tm.Token}},
			},
		}

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        resp,
			ParseMode:   "HTML",
			ReplyMarkup: inlineMarkup,
		})
		return
	}

	// Case 2: /token <pro|pro++|vip> <durasi>
	tierInput := subCmd
	var normalizedTier string
	switch tierInput {
	case "pro":
		normalizedTier = "Pro"
	case "pro++", "proplus", "pro+":
		normalizedTier = "Pro++"
	case "vip":
		normalizedTier = "Vip"
	default:
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Tier tidak valid. Pilihan tier: <code>pro</code>, <code>pro++</code>, <code>vip</code>, atau <code>-free</code>.",
			ParseMode: "HTML",
		})
		return
	}

	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("⚠️ Masukkan format durasi untuk tier <b>%s</b>.\nContoh: <code>/token %s 2h4j</code> atau <code>/token %s 30h</code>.", normalizedTier, subCmd, subCmd),
			ParseMode: "HTML",
		})
		return
	}

	durationStr := fields[2]
	dur, durErr := ParseDurationStr(durationStr)
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "⚠️ Format durasi salah: " + durErr.Error() + "\nContoh: <code>2h4j</code> (2 hari 4 jam), <code>30h</code> (30 hari), <code>1h30m</code>.",
			ParseMode: "HTML",
		})
		return
	}

	newToken := GenerateMasterToken()
	tm := &databases.TokenMaster{
		Token:           newToken,
		Tier:            normalizedTier,
		DurationSeconds: int64(dur.Seconds()),
		IsShared:        false,
		ClaimCount:      0,
		CreatedBy:       senderID,
		CreatedAt:       time.Now(),
		Status:          "active",
	}

	if err := h.db.SaveTokenMaster(ctx, tm); err != nil {
		log.Printf("[token] SaveTokenMaster err: %v", err)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      "❌ Gagal menyimpan token ke database: " + err.Error(),
			ParseMode: "HTML",
		})
		return
	}

	durDesc := FormatDurationID(dur)
	featureDesc := ""
	switch normalizedTier {
	case "Pro":
		featureDesc = "• Jeda Cepat & Kuota Besar\n• Media Group/Album Aktif\n• ❌ Custom Watermark Terkunci"
	case "Pro++":
		featureDesc = "• Semua Fitur Siaran Aktif\n• ✅ Custom Watermark Aktif\n• ✅ Topic Forum Telegram Aktif"
	case "Vip":
		featureDesc = "• Khusus Admin/VIP (Unlimited)\n• Prioritas Jalur MTProto Tertinggi"
	}

	resp := fmt.Sprintf(`🎫 <b>Token Lisensi GotdBot (%s)</b>
━━━━━━━━━━━━━━━━━━━━
🔑 <b>Token:</b> <code>%s</code>
🎖️ <b>Tier:</b> <code>%s</code>
⏳ <b>Durasi:</b> <code>%s</code>
👥 <b>Status Claim:</b> <code>0 / 1 (Personal)</code>
🟢 <b>Status:</b> <code>Active</code>

<b>Ketentuan Fitur:</b>
%s

<i>Kirimkan token di atas kepada pengguna untuk diklaim via /start.</i>`,
		normalizedTier, newToken, normalizedTier, durDesc, featureDesc,
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      resp,
		ParseMode: "HTML",
	})
}

// TokenCallback handles inline button clicks related to token management
func (h *Handler) TokenCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	senderID := cb.From.ID

	if !isOwner(senderID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⛔ Hanya Owner yang dapat mengatur durasi token.",
			ShowAlert:       true,
		})
		return
	}

	data := cb.Data
	// Format: token_set_duration:menu:<token> or token_set_duration:apply:<token>:<seconds>
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return
	}

	action := parts[1]
	token := parts[2]

	if action == "menu" {
		markup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{
					{Text: "1 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 1*86400)},
					{Text: "3 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 3*86400)},
					{Text: "7 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 7*86400)},
				},
				{
					{Text: "14 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 14*86400)},
					{Text: "30 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 30*86400)},
					{Text: "90 Hari", CallbackData: fmt.Sprintf("token_set_duration:apply:%s:%d", token, 90*86400)},
				},
				{
					{Text: "🔙 Kembali", CallbackData: "token_set_duration:back:" + token},
				},
			},
		}

		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		if cb.Message.Message != nil {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      cb.Message.Message.Chat.ID,
				MessageID:   cb.Message.Message.ID,
				Text:        fmt.Sprintf("⏱️ <b>Pilih Durasi Aktif Token Free:</b>\n<code>%s</code>", token),
				ParseMode:   "HTML",
				ReplyMarkup: markup,
			})
		}
		return
	}

	if action == "apply" && len(parts) >= 4 {
		seconds, _ := strconv.ParseInt(parts[3], 10, 64)
		if seconds <= 0 {
			seconds = 7 * 86400
		}

		_ = h.db.UpdateTokenMasterDuration(ctx, token, seconds)
		durStr := FormatDurationID(time.Duration(seconds) * time.Second)

		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "✅ Durasi token Free berhasil diatur ke " + durStr,
			ShowAlert:       true,
		})

		tm, _ := h.db.GetTokenMaster(ctx, token)
		claimCount := 0
		if tm != nil {
			claimCount = tm.ClaimCount
		}

		resp := fmt.Sprintf(`🎫 <b>Token Master Free GotdBot</b>
━━━━━━━━━━━━━━━━━━━━
🔑 <b>Token:</b> <code>%s</code>
⏱️ <b>Durasi:</b> <code>%s</code>
👥 <b>Jumlah Claimed:</b> <code>%d user</code>
🟢 <b>Status:</b> <code>Active</code> (Shared Voucher)

<i>Token ini dapat digunakan oleh semua pengguna secara massal.</i>`,
			token, durStr, claimCount,
		)

		inlineMarkup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "⏱️ Set Durasi Token Free", CallbackData: "token_set_duration:menu:" + token}},
			},
		}

		if cb.Message.Message != nil {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      cb.Message.Message.Chat.ID,
				MessageID:   cb.Message.Message.ID,
				Text:        resp,
				ParseMode:   "HTML",
				ReplyMarkup: inlineMarkup,
			})
		}
		return
	}

	if action == "back" {
		tm, _ := h.db.GetTokenMaster(ctx, token)
		if tm == nil {
			return
		}
		durStr := FormatDurationID(time.Duration(tm.DurationSeconds) * time.Second)
		resp := fmt.Sprintf(`🎫 <b>Token Master Free GotdBot</b>
━━━━━━━━━━━━━━━━━━━━
🔑 <b>Token:</b> <code>%s</code>
⏱️ <b>Durasi:</b> <code>%s</code>
👥 <b>Jumlah Claimed:</b> <code>%d user</code>
🟢 <b>Status:</b> <code>Active</code> (Shared Voucher)

<i>Token ini dapat digunakan oleh semua pengguna secara massal.</i>`,
			tm.Token, durStr, tm.ClaimCount,
		)

		inlineMarkup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "⏱️ Set Durasi Token Free", CallbackData: "token_set_duration:menu:" + tm.Token}},
			},
		}

		if cb.Message.Message != nil {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      cb.Message.Message.Chat.ID,
				MessageID:   cb.Message.Message.ID,
				Text:        resp,
				ParseMode:   "HTML",
				ReplyMarkup: inlineMarkup,
			})
		}
		return
	}
}
