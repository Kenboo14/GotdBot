package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) limitCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	text, err := s.queryBotReplyText(ctx, client, "SpamBot", "/start", 1200*time.Millisecond)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal memeriksa limit akun ke @SpamBot: "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Status Limit (@SpamBot):\n"+text, threadID)
}

func (s *Service) sgCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	if text, qErr := s.queryBotReplyText(ctx, client, "SangMata_beta_bot", strconv.FormatInt(uid, 10), 1400*time.Millisecond); qErr == nil && text != "" {
		return sendUserText(ctx, client, target, text, threadID)
	}
	username := "-"
	if full, fErr := client.API().UsersGetFullUser(ctx, inputUser); fErr == nil {
		for _, rawU := range full.Users {
			if u, ok := rawU.(*tg.User); ok && u.ID == uid {
				combined := strings.TrimSpace(u.FirstName + " " + u.LastName)
				if combined != "" {
					label = combined
				}
				if u.Username != "" {
					username = "@" + u.Username
				}
				break
			}
		}
	}
	fallback := fmt.Sprintf("Riwayat Identitas Saat Ini:\nUser ID: %d\nNama: %s\nUsername: %s\n(SangMata bot tidak merespons)", uid, label, username)
	return sendUserText(ctx, client, target, fallback, threadID)
}

func (s *Service) setBioCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	if msg == nil || !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat mengubah profil.", threadID)
	}
	bio := strings.TrimSpace(args)
	if utf8.RuneCountInString(bio) > 70 {
		return sendUserText(ctx, client, target, "Bio maksimal 70 karakter.", threadID)
	}
	req := &tg.AccountUpdateProfileRequest{}
	req.SetAbout(bio)
	if _, err := client.API().AccountUpdateProfile(ctx, req); err != nil {
		return sendUserText(ctx, client, target, "Gagal mengubah bio: "+err.Error(), threadID)
	}
	if bio == "" {
		return sendUserText(ctx, client, target, "Bio profil berhasil dikosongkan.", threadID)
	}
	return sendUserText(ctx, client, target, "Bio profil diperbarui: "+bio, threadID)
}

func (s *Service) setNameCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	if msg == nil || !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat mengubah profil.", threadID)
	}
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return sendUserText(ctx, client, target, "Gunakan: !setname <nama lengkap>", threadID)
	}
	parts := strings.SplitN(trimmed, " ", 2)
	req := &tg.AccountUpdateProfileRequest{}
	req.SetFirstName(strings.TrimSpace(parts[0]))
	if len(parts) > 1 {
		req.SetLastName(strings.TrimSpace(parts[1]))
	} else {
		req.SetLastName("")
	}
	if _, err := client.API().AccountUpdateProfile(ctx, req); err != nil {
		return sendUserText(ctx, client, target, "Gagal mengubah nama profil: "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Nama profil diperbarui: "+trimmed, threadID)
}

func (s *Service) setUsernameCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	if msg == nil || !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat mengubah username.", threadID)
	}
	raw := strings.TrimSpace(args)
	if raw == "" {
		return sendUserText(ctx, client, target, "Gunakan: !setusername <username|none>", threadID)
	}
	uname := strings.TrimPrefix(raw, "@")
	if strings.EqualFold(uname, "none") || strings.EqualFold(uname, "off") {
		uname = ""
	}
	if _, err := client.API().AccountUpdateUsername(ctx, uname); err != nil {
		return sendUserText(ctx, client, target, "Gagal mengubah username: "+err.Error(), threadID)
	}
	if uname == "" {
		return sendUserText(ctx, client, target, "Username publik berhasil dihapus.", threadID)
	}
	return sendUserText(ctx, client, target, "Username publik diperbarui ke @"+uname, threadID)
}
