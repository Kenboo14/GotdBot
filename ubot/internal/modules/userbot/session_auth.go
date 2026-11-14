package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
)

type login struct {
	cancel    context.CancelFunc
	otp       chan string
	password  chan string
	createdAt time.Time
}

func (s *Service) Handle(ctx context.Context, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return
	}
	var job models.PToService
	if json.Unmarshal([]byte(payload), &job) != nil || job.ChatID == 0 {
		return
	}
	s.mu.Lock()
	current := s.logins[job.ChatID]
	s.mu.Unlock()
	switch job.Type {
	case "auth_check":
		installed, err := s.hasInstalledSession(ctx, job.ChatID, job.UserID)
		if err != nil {
			s.reply(ctx, job.ChatID, `<h1>❌ Gagal Memeriksa Sesi</h1>
<blockquote expandable>Tidak dapat memverifikasi sesi userbot saat ini. Silakan coba sesaat lagi.</blockquote>
<hr/>
<p>Ketik /start untuk mencoba kembali.</p>`, "failed")
		} else if installed {
			s.reply(ctx, job.ChatID, `<h1>⚠️ Sesi Userbot Sudah Aktif</h1>
<blockquote expandable>Akun Telegram ini sudah terpasang dan memiliki sesi aktif di sistem GotdBot.
Satu akun hanya diperbolehkan memiliki satu sesi userbot aktif.</blockquote>
<hr/>
<p>Gunakan menu kontrol panel atau ketik <b>/status</b> untuk melihat status akun Anda.</p>`, "failed")
		} else {
			// Kirim state waiting_phone tanpa text → gateway hanya update state,
			// pesan permintaan nomor sudah dikirim oleh gateway sebelumnya (single response)
			s.reply(ctx, job.ChatID, "", "waiting_phone")
		}
	case "auth_phone":
		if current != nil {
			s.reply(ctx, job.ChatID, `<h1>⏳ Pemasangan Sedang Berjalan</h1>
<blockquote expandable>Proses otorisasi akun Telegram Anda masih berlangsung di latar belakang.</blockquote>
<hr/>
<p>Silakan kirimkan kode OTP Anda, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, "waiting_otp")
			return
		}
		if installed, err := s.hasInstalledSession(ctx, job.ChatID, job.UserID); err != nil {
			s.reply(ctx, job.ChatID, `<h1>❌ Gagal Memeriksa Sesi</h1>
<blockquote expandable>Tidak dapat memverifikasi sesi userbot saat ini. Silakan coba sesaat lagi.</blockquote>
<hr/>
<p>Ketik /start untuk mencoba kembali.</p>`, "failed")
		} else if installed {
			s.reply(ctx, job.ChatID, `<h1>⚠️ Sesi Userbot Sudah Aktif</h1>
<blockquote expandable>Akun Telegram ini sudah terpasang dan memiliki sesi aktif di sistem GotdBot.
Satu akun hanya diperbolehkan memiliki satu sesi userbot aktif.</blockquote>
<hr/>
<p>Gunakan menu kontrol panel atau ketik <b>/status</b> untuk melihat status akun Anda.</p>`, "failed")
		} else {
			s.start(ctx, job.ChatID, job.UserID, strings.TrimSpace(job.Text))
		}
	case "auth_otp":
		if current == nil {
			s.reply(ctx, job.ChatID, `<h1>⏳ Sesi Pemasangan Kedaluwarsa</h1>
<blockquote expandable>Batas waktu tunggu input kode OTP telah habis (timeout).</blockquote>
<hr/>
<p>Ketik atau tekan <b>/start</b> untuk memulai pemasangan kembali.</p>`, "failed")
			return
		}
		sendSecret(current.otp, job.Text)
	case "auth_password":
		if current == nil {
			s.reply(ctx, job.ChatID, `<h1>⏳ Sesi Pemasangan Kedaluwarsa</h1>
<blockquote expandable>Batas waktu tunggu input password 2FA telah habis (timeout).</blockquote>
<hr/>
<p>Ketik atau tekan <b>/start</b> untuk memulai pemasangan kembali.</p>`, "failed")
			return
		}
		sendSecret(current.password, job.Text)
	case "auth_cancel":
		if current != nil {
			current.cancel()
		}
	}
}

func (s *Service) start(parent context.Context, chatID, ownerID int64, phone string) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	l := &login{cancel: cancel, otp: make(chan string, 1), password: make(chan string, 1), createdAt: time.Now()}
	s.mu.Lock()
	s.logins[chatID] = l
	s.mu.Unlock()
	go func() {
		defer cancel()
		defer s.remove(chatID)
		if err := s.authorize(ctx, chatID, ownerID, phone, l); err != nil && !errors.Is(err, context.Canceled) {
			errMsg := fmt.Sprintf(`<h1>❌ Pemasangan Gagal</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>Ketik atau tekan <b>/start</b> untuk memulai pemasangan kembali.</p>`, html.EscapeString(safeError(err)))
			s.reply(parent, chatID, errMsg, "failed")
		}
	}()
}

func (s *Service) authorize(ctx context.Context, chatID, ownerID int64, phone string, l *login) error {
	store := &sqliteStorage{sqlite: s.sqlite, db: s.db, chatID: chatID, ownerID: ownerID, key: s.key}
	client := telegram.NewClient(s.apiID, s.apiHash, telegram.Options{SessionStorage: store, NoUpdates: true})
	return client.Run(ctx, func(ctx context.Context) error {
		sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
		if err != nil {
			slog.Error("send_code failed during auth", slog.String("phone", phone), slog.Int64("chat_id", chatID), slog.String("error", err.Error()))
			return err
		}
		code, ok := sent.(*tg.AuthSentCode)
		if !ok {
			return fmt.Errorf("Telegram mengembalikan jenis OTP yang tidak didukung")
		}

		otpTarget := "aplikasi Telegram Anda (buka chat resmi dari Telegram)"
		switch code.Type.(type) {
		case *tg.AuthSentCodeTypeApp:
			otpTarget = "aplikasi Telegram Anda (cek notifikasi / chat resmi dari Telegram, bukan SMS)"
		case *tg.AuthSentCodeTypeSMS:
			otpTarget = "SMS ke nomor ponsel Anda"
		case *tg.AuthSentCodeTypeCall, *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
			otpTarget = "panggilan suara dari Telegram"
		case *tg.AuthSentCodeTypeEmailCode:
			otpTarget = "alamat email yang terhubung ke Telegram"
		}

		otpMsg := fmt.Sprintf(`<h1>📩 Verifikasi Kode OTP</h1>
<blockquote expandable>Kode OTP telah dikirimkan ke <b>%s</b>.
Silakan periksa pesan notifikasi resmi dari Telegram (bukan SMS biasa).</blockquote>
<table border="1">
  <tr><th>Format Kode</th><th>Ketentuan</th></tr>
  <tr><td>Panjang Digit</td><td>4–10 digit angka</td></tr>
  <tr><td>Pemisah Spasi</td><td>Boleh digunakan (contoh: <code>1 2 3 4 5</code>)</td></tr>
</table>
<hr/>
<p>Kirimkan kode OTP sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, otpTarget)
		s.reply(ctx, chatID, otpMsg, "waiting_otp")

		var signInErr error
		for attempt := 1; attempt <= 3; attempt++ {
			otp, err := receiveSecret(ctx, l.otp)
			if err != nil {
				return err
			}
			cleanOTP := strings.ReplaceAll(otp, " ", "")
			_, signInErr = client.Auth().SignIn(ctx, phone, cleanOTP, code.PhoneCodeHash)
			if signInErr == nil || errors.Is(signInErr, auth.ErrPasswordAuthNeeded) {
				break
			}
			errStr := strings.ToLower(signInErr.Error())
			if strings.Contains(errStr, "phone_code_invalid") {
				if attempt < 3 {
					retryMsg := fmt.Sprintf(`<h1>⚠️ Kode OTP Tidak Valid</h1>
<blockquote expandable>Kode OTP yang Anda masukkan salah atau sudah kedaluwarsa (Percobaan <b>%d/3</b>).
Pastikan memasukkan kode OTP terbaru yang dikirimkan oleh Telegram.</blockquote>
<hr/>
<p>Silakan kirimkan kode OTP yang benar, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, attempt)
					s.reply(ctx, chatID, retryMsg, "waiting_otp")
					continue
				}
			}
			return signInErr
		}

		if errors.Is(signInErr, auth.ErrPasswordAuthNeeded) {
			twoFaMsg := `<h1>🔐 Verifikasi 2 Langkah (2FA)</h1>
<blockquote expandable>Akun Telegram Anda diproteksi dengan kata sandi Two-Step Verification (2FA).</blockquote>
<table border="1">
  <tr><th>Tipe Autentikasi</th><th>Keamanan</th></tr>
  <tr><td>Password Cloud 2FA</td><td>Terenkripsi End-to-End MTProto</td></tr>
</table>
<hr/>
<p>Silakan kirimkan password 2FA akun Anda, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`
			s.reply(ctx, chatID, twoFaMsg, "waiting_password")
			for passAttempt := 1; passAttempt <= 3; passAttempt++ {
				password, err := receiveSecret(ctx, l.password)
				if err != nil {
					return err
				}
				if _, passErr := client.Auth().Password(ctx, password); passErr == nil {
					break
				} else {
					errStr := strings.ToLower(passErr.Error())
					if strings.Contains(errStr, "password") {
						if passAttempt < 3 {
							passRetryMsg := fmt.Sprintf(`<h1>⚠️ Password 2FA Salah</h1>
<blockquote expandable>Password verifikasi 2 langkah yang Anda masukkan tidak sesuai (Percobaan <b>%d/3</b>).
Perhatikan huruf besar/kecil pada password akun Anda.</blockquote>
<hr/>
<p>Silakan kirimkan password 2FA yang benar, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`, passAttempt)
							s.reply(ctx, chatID, passRetryMsg, "waiting_password")
							continue
						}
					}
					return passErr
				}
			}
		} else if signInErr != nil {
			return signInErr
		}

		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		if err := store.setAccount(ctx, self.ID); err != nil {
			return err
		}
		s.mu.Lock()
		s.prefixes[chatID] = "!"
		s.owners[chatID] = self.ID
		s.mu.Unlock()
		if err := store.setPrefix(ctx, "!", true); err != nil {
			return err
		}
		s.replyWithOwner(ctx, chatID, self.ID, fmt.Sprintf("Userbot berhasil dipasang untuk akun %d. Sesi disimpan terenkripsi.", self.ID), "installed")
		s.activate(chatID, self.ID)
		return nil
	})
}

func (s *Service) reply(ctx context.Context, chatID int64, text, state string) {
	if chatID == 0 {
		return
	}
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: chatID, Text: text, State: state})
}

func (s *Service) replyWithOwner(ctx context.Context, chatID, ownerID int64, text, state string) {
	if chatID == 0 {
		return
	}
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: chatID, OwnerID: ownerID, Text: text, State: state})
}

func (s *Service) hasInstalledSession(ctx context.Context, chatID, ownerID int64) (bool, error) {
	if s.sqlite != nil {
		has, err := s.sqlite.HasSession(ctx, chatID, ownerID)
		if err == nil && has {
			return true, nil
		}
	}
	if s.db != nil {
		count, err := s.db.Collection("userbot_sessions").CountDocuments(ctx, bson.M{
			"$or": []bson.M{
				{"_id": chatID},
				{"owner_id": ownerID},
				{"account_id": ownerID},
			},
			"session": bson.M{"$ne": ""},
		})
		if err == nil && count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) remove(chatID int64) {
	s.mu.Lock()
	delete(s.logins, chatID)
	s.mu.Unlock()
}

func sendSecret(ch chan string, value string) {
	select {
	case ch <- value:
	default:
	}
}

func receiveSecret(ctx context.Context, ch <-chan string) (string, error) {
	select {
	case value := <-ch:
		return value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func safeError(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "phone_code_invalid"):
		return "Kode OTP tidak valid atau salah ketik."
	case strings.Contains(text, "phone_code_expired"):
		return "Kode OTP sudah kedaluwarsa. Silakan mulai ulang untuk meminta kode baru."
	case strings.Contains(text, "password_hash_invalid"), strings.Contains(text, "password"):
		return "Password 2FA yang dimasukkan salah setelah batas percobaan tercapai."
	case strings.Contains(text, "phone_number_invalid"):
		return "Nomor telepon tidak valid. Pastikan format nomor internasional benar (+62...)."
	case strings.Contains(text, "phone_number_banned"):
		return "Nomor telepon ini diblokir / banned oleh Telegram."
	case strings.Contains(text, "flood"):
		return "Terlalu banyak percobaan dalam waktu singkat (FloodWait). Silakan tunggu beberapa menit."
	default:
		return "Telegram menolak permintaan otorisasi (" + err.Error() + ")."
	}
}
