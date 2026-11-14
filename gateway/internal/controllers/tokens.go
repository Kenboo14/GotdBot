package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
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
		ownerEnv = strings.TrimSpace(os.Getenv("OWNER_ID"))
	}
	if ownerEnv == "" {
		return false
	}
	ownerEnv = strings.Trim(ownerEnv, `"'`)
	for _, part := range strings.FieldsFunc(ownerEnv, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		if id, err := strconv.ParseInt(part, 10, 64); err == nil && id > 0 && id == userID {
			return true
		}
	}
	return false
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
			ChatID: chatID,
			Text: `<h1>⛔ Akses Ditolak</h1>
<blockquote expandable>Hanya Owner sistem yang memiliki wewenang untuk membuat dan mengelola token lisensi GotdBot.</blockquote>
<hr/>
<p>GotdBot • Access Control</p>`,
			ParseMode: "HTML",
		})
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	fields := strings.Fields(text)

	if len(fields) < 2 {
		helpText := `<h1>🔑 Panduan Perintah /token</h1>
<blockquote expandable>Menu khusus Owner untuk membuat voucher lisensi userbot GotdBot.</blockquote>
<table border="1">
  <tr><th>Format Perintah</th><th>Keterangan</th></tr>
  <tr><td><code>/token -free</code></td><td>Shared voucher Free massal</td></tr>
  <tr><td><code>/token pro &lt;durasi&gt;</code></td><td>Voucher tier Pro (Personal)</td></tr>
  <tr><td><code>/token pro++ &lt;durasi&gt;</code></td><td>Voucher tier Pro++ (Custom WM)</td></tr>
  <tr><td><code>/token vip &lt;durasi&gt;</code></td><td>Voucher tier VIP Unlimited</td></tr>
</table>
<hr/>
<p>Format durasi: <code>h</code> (Hari), <code>j</code> (Jam), <code>m</code> (Menit). Contoh: <code>/token pro++ 30h</code></p>`
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
		resp := fmt.Sprintf(`<h1>🎫 Token Master Free GotdBot</h1>
<blockquote expandable>Shared voucher massal aktif untuk seluruh pengguna umum.</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token Voucher</td><td><code>%s</code></td></tr>
  <tr><td>⏱️ Durasi Aktif</td><td><code>%s</code></td></tr>
  <tr><td>👥 Total Diklaim</td><td><code>%d pengguna</code></td></tr>
  <tr><td>🟢 Status</td><td><code>Active (Shared)</code></td></tr>
</table>
<hr/>
<p>Kirimkan token di atas kepada pengguna untuk diklaim via <b>/start</b>.</p>`,
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
			ChatID: chatID,
			Text: `<h1>⚠️ Tier Tidak Valid</h1>
<blockquote expandable>Pilihan tier lisensi yang tersedia adalah <code>pro</code>, <code>pro++</code>, <code>vip</code>, atau <code>-free</code>.</blockquote>
<hr/>
<p>Contoh: <code>/token pro 30h</code></p>`,
			ParseMode: "HTML",
		})
		return
	}

	if len(fields) < 3 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>⚠️ Format Durasi Diperlukan</h1>
<blockquote expandable>Silakan masukkan format durasi yang valid untuk tier <b>%s</b>.</blockquote>
<hr/>
<p>Contoh: <code>/token %s 30h</code> atau <code>/token %s 2h4j</code></p>`, normalizedTier, subCmd, subCmd),
			ParseMode: "HTML",
		})
		return
	}

	durationStr := fields[2]
	dur, durErr := ParseDurationStr(durationStr)
	if durErr != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>⚠️ Format Durasi Tidak Valid</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>Format satuan: <code>h</code> (Hari), <code>j</code> (Jam), <code>m</code> (Menit). Contoh: <code>30h</code> atau <code>1h30m</code>.</p>`, html.EscapeString(durErr.Error())),
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
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>❌ Gagal Menyimpan Token</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>Silakan coba beberapa saat lagi.</p>`, html.EscapeString(err.Error())),
			ParseMode: "HTML",
		})
		return
	}

	durDesc := FormatDurationID(dur)
	featureDesc := ""
	switch normalizedTier {
	case "Pro":
		featureDesc = "• Jeda Cepat &amp; Kuota Besar<br/>• Media Group/Album Aktif<br/>• ❌ Custom Watermark Terkunci"
	case "Pro++":
		featureDesc = "• Semua Fitur Siaran Aktif<br/>• ✅ Custom Watermark Aktif<br/>• ✅ Topic Forum Telegram Aktif"
	case "Vip":
		featureDesc = "• Khusus Admin/VIP (Unlimited)<br/>• Prioritas Jalur MTProto Tertinggi"
	}

	resp := fmt.Sprintf(`<h1>🎫 Token Lisensi GotdBot (%s)</h1>
<blockquote expandable>Voucher lisensi personal berhasil digenerate dan tersimpan di database.</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Keterangan</th></tr>
  <tr><td>🔑 Token Voucher</td><td><code>%s</code></td></tr>
  <tr><td>🎖️ Tier Lisensi</td><td><code>%s</code></td></tr>
  <tr><td>⏳ Durasi Aktif</td><td><code>%s</code></td></tr>
  <tr><td>👥 Status Klaim</td><td><code>0 / 1 (Personal)</code></td></tr>
</table>
<blockquote expandable><b>Ketentuan Fitur:</b><br/>%s</blockquote>
<hr/>
<p>Kirimkan token di atas kepada pembeli untuk diklaim via <b>/start</b>.</p>`,
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
			menuText := fmt.Sprintf(`<h1>⏱️ Atur Durasi Token Free</h1>
<blockquote expandable>Silakan tentukan masa aktif baru untuk voucher token massal free.</blockquote>
<table border="1">
  <tr><th>Token Voucher</th><td><code>%s</code></td></tr>
</table>
<hr/>
<p>Pilih durasi aktif yang diinginkan pada tombol di bawah:</p>`, token)
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      cb.Message.Message.Chat.ID,
				MessageID:   cb.Message.Message.ID,
				Text:        menuText,
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

		resp := fmt.Sprintf(`<h1>🎫 Token Master Free GotdBot</h1>
<blockquote expandable>Durasi token free berhasil diperbarui. Token ini dapat digunakan oleh semua pengguna secara massal.</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token Voucher</td><td><code>%s</code></td></tr>
  <tr><td>⏱️ Durasi Baru</td><td><code>%s</code></td></tr>
  <tr><td>👥 Total Diklaim</td><td><code>%d pengguna</code></td></tr>
  <tr><td>🟢 Status</td><td><code>Active (Shared)</code></td></tr>
</table>
<hr/>
<p>Kirimkan token di atas kepada pengguna untuk diklaim via <b>/start</b>.</p>`,
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
		resp := fmt.Sprintf(`<h1>🎫 Token Master Free GotdBot</h1>
<blockquote expandable>Shared voucher massal aktif untuk seluruh pengguna umum.</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Token Voucher</td><td><code>%s</code></td></tr>
  <tr><td>⏱️ Durasi Aktif</td><td><code>%s</code></td></tr>
  <tr><td>👥 Total Diklaim</td><td><code>%d pengguna</code></td></tr>
  <tr><td>🟢 Status</td><td><code>Active (Shared)</code></td></tr>
</table>
<hr/>
<p>Kirimkan token di atas kepada pengguna untuk diklaim via <b>/start</b>.</p>`,
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
