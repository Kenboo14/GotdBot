package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"html"
	"log"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

func parseAdminChatIDs(adminField string, excludeID int64) []int64 {
	trimmed := strings.TrimSpace(adminField)
	if trimmed == "" || strings.EqualFold(trimmed, "Belum diatur") {
		return nil
	}
	tokens := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	seen := make(map[int64]bool, len(tokens))
	out := make([]int64, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if id, err := strconv.ParseInt(tok, 10, 64); err == nil && id > 0 && id != excludeID && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func sendEmergencyEmail(toEmail, subject, body string) error {
	to := strings.TrimSpace(toEmail)
	if to == "" || !strings.Contains(to, "@") || strings.EqualFold(to, "Belum diatur") {
		return nil
	}
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	pass := strings.TrimSpace(os.Getenv("SMTP_PASS"))
	if host == "" || user == "" || pass == "" {
		return nil
	}
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user
	}
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from, to, subject, body))
	auth := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(host+":"+port, auth, from, []string{to}, msg)
}

func (h *Handler) dispatchSessionRevokedBackup(b *botgotd.Bot, chatID int64, reason string) {
	if h == nil || h.db == nil || chatID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := h.db.GetUserConfig(ctx, chatID)
	lic, _ := h.db.GetActiveUserLicense(ctx, chatID)

	var tokenLineHTML, tokenLinePlain string
	if lic != nil && databases.GetTierPolicy(lic.Tier).AllowLicenseToken {
		tokenLineHTML = fmt.Sprintf("• <b>Token Lisensi (GOTD_SHA):</b>\n<code>%s</code>\n<i>Tier: %s · Exp: %s WIB</i>\n",
			html.EscapeString(lic.TokenSHA), html.EscapeString(lic.Tier), lic.ExpiresAt.Local().Format("02-01-2006 15:04"))
		tokenLinePlain = fmt.Sprintf("Token Lisensi (GOTD_SHA): %s (Tier: %s, Exp: %s WIB)\n",
			lic.TokenSHA, lic.Tier, lic.ExpiresAt.Local().Format("02-01-2006 15:04"))
	}

	var backupLineHTML, backupLinePlain string
	if len(cfg.Lists) > 0 || len(cfg.Groups) > 0 || len(cfg.Topics) > 0 || len(cfg.TopicLists) > 0 {
		code := GenerateBackupCode()
		backup := &databases.DataBackupCode{
			Code:       code,
			OwnerID:    chatID,
			Lists:      append([]int(nil), cfg.Lists...),
			Groups:     append([]databases.GroupEntry(nil), cfg.Groups...),
			Topics:     append([]databases.TopicEntry(nil), cfg.Topics...),
			TopicLists: append([]databases.TopicListEntry(nil), cfg.TopicLists...),
			CreatedAt:  time.Now(),
		}
		if err := h.db.SaveDataBackup(ctx, backup); err == nil {
			backupLineHTML = fmt.Sprintf("• <b>Kode Backup Data (List &amp; Grup):</b>\n<code>%s</code> (%d list · %d grup)\n",
				html.EscapeString(code), len(backup.Lists), len(backup.Groups))
			backupLinePlain = fmt.Sprintf("Kode Backup Data: %s (%d list, %d grup)\n",
				code, len(backup.Lists), len(backup.Groups))
		}
	}

	if tokenLineHTML == "" && backupLineHTML == "" {
		return
	}

	cleanReason := strings.TrimSpace(reason)
	if cleanReason == "" {
		cleanReason = "Sesi Telegram terputus atau tercabut"
	}

	if b != nil {
		adminIDs := parseAdminChatIDs(cfg.Admin, chatID)
		if len(adminIDs) > 0 {
			alertHTML := fmt.Sprintf(`<h1>🚨 Alert Pemulihan Sesi Userbot</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Keterangan</th></tr>
  <tr><td>👤 Akun ID</td><td><code>%d</code></td></tr>
  <tr><td>⚠️ Alasan</td><td><code>%s</code></td></tr>
</table>
<blockquote expandable>%s%s
<i>Simpan kode di atas untuk memulihkan paket/data ke akun baru bila diperlukan.</i></blockquote>
<hr/>
<p>GotdBot • Emergency Session Monitor</p>`,
				chatID, html.EscapeString(cleanReason), tokenLineHTML, backupLineHTML,
			)
			for _, adminID := range adminIDs {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:    adminID,
					Text:      alertHTML,
					ParseMode: "HTML",
				})
			}
		}
	}

	if cfg.Email != "" && strings.Contains(cfg.Email, "@") {
		plainBody := fmt.Sprintf("Pemberitahuan Pemulihan Sesi GotdBot\nAkun ID: %d\nAlasan: %s\n\n%s%s",
			chatID, cleanReason, tokenLinePlain, backupLinePlain)
		if err := sendEmergencyEmail(cfg.Email, fmt.Sprintf("[GotdBot] Pemulihan Sesi Akun %d", chatID), plainBody); err != nil {
			log.Printf("⚠️ emergency email send error (owner=%d): %v", chatID, err)
		}
	}
}
