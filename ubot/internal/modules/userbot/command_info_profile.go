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

func (s *Service) idCommand(ctx context.Context, client *gobot.Client, chatID int64, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	ownerID, _ := s.userbotOwner(ctx, client)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Chat ID: %d\n", chatID)
	if msg != nil && msg.ID > 0 {
		fmt.Fprintf(&sb, "Message ID: %d\n", msg.ID)
	}
	if threadID > 0 {
		fmt.Fprintf(&sb, "Topic ID: %d\n", threadID)
	}
	if ownerID > 0 {
		fmt.Fprintf(&sb, "Your User ID: %d\n", ownerID)
	}
	if reply, err := s.replyMessage(ctx, client, msg, target); err == nil && reply != nil {
		if reply.ID > 0 {
			fmt.Fprintf(&sb, "Replied Message ID: %d\n", reply.ID)
		}
		if uid, ok := extractUserIDFromMessage(reply, ownerID); ok {
			fmt.Fprintf(&sb, "Replied User ID: %d\n", uid)
		}
		switch m := reply.Media.(type) {
		case *tg.MessageMediaDocument:
			if doc, ok := m.Document.(*tg.Document); ok {
				fmt.Fprintf(&sb, "Document ID: %d\n", doc.ID)
			}
		case *tg.MessageMediaPhoto:
			if p, ok := m.Photo.(*tg.Photo); ok {
				fmt.Fprintf(&sb, "Photo ID: %d\n", p.ID)
			}
		}
	}
	return sendUserText(ctx, client, target, strings.TrimSpace(sb.String()), threadID)
}

func (s *Service) infoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	var uid int64
	var name string
	var inputUser tg.InputUserClass
	if strings.TrimSpace(args) != "" || broadcastReplyMessageID(msg) > 0 {
		var err error
		uid, name, inputUser, err = s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
		if err != nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
	} else {
		ownerID, err := s.userbotOwner(ctx, client)
		if err != nil {
			return err
		}
		uid = ownerID
		inputUser = &tg.InputUserSelf{}
	}

	full, err := client.API().UsersGetFullUser(ctx, inputUser)
	if err != nil {
		if name == "" {
			name = displayNameFromEntities(entities, uid)
		}
		if name == "" {
			name = "-"
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("Info Pengguna:\nUser ID: %d\nNama: %s", uid, name), threadID)
	}

	var uObj *tg.User
	for _, rawU := range full.Users {
		if u, ok := rawU.(*tg.User); ok && u.ID == uid {
			uObj = u
			break
		}
	}
	fullName := name
	username := "-"
	userType := "User"
	flags := make([]string, 0, 2)
	if uObj != nil {
		combined := strings.TrimSpace(uObj.FirstName + " " + uObj.LastName)
		if combined != "" {
			fullName = combined
		}
		if uObj.Username != "" {
			username = "@" + uObj.Username
		}
		if uObj.Bot {
			userType = "Bot"
		}
		if uObj.Verified {
			flags = append(flags, "Verified")
		}
		if uObj.Premium {
			flags = append(flags, "Premium")
		}
	}
	if fullName == "" {
		fullName = "-"
	}
	about := strings.TrimSpace(full.FullUser.About)
	if about == "" {
		about = "-"
	}
	flagStr := "-"
	if len(flags) > 0 {
		flagStr = strings.Join(flags, ", ")
	}
	report := fmt.Sprintf("Info Pengguna:\nUser ID: %d\nNama: %s\nUsername: %s\nTipe: %s\nStatus: %s\nCommon Chats: %d\nBio: %s",
		uid, fullName, username, userType, flagStr, full.FullUser.CommonChatsCount, about)
	return sendUserText(ctx, client, target, report, threadID)
}

func (s *Service) resolveBotPeerByUsername(ctx context.Context, client *gobot.Client, username string) (tg.InputPeerClass, error) {
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: strings.TrimPrefix(username, "@"),
	})
	if err != nil || resolved == nil {
		return nil, fmt.Errorf("gagal resolve @%s: %v", strings.TrimPrefix(username, "@"), err)
	}
	s.peerCache().ApplyUsersChats(resolved.Users, resolved.Chats)
	peerUser, ok := resolved.Peer.(*tg.PeerUser)
	if !ok || peerUser.UserID <= 0 {
		return nil, fmt.Errorf("peer @%s bukan pengguna/bot", strings.TrimPrefix(username, "@"))
	}
	hash, _ := s.peerCache().UserHash(peerUser.UserID)
	return &tg.InputPeerUser{UserID: peerUser.UserID, AccessHash: hash}, nil
}

func (s *Service) queryBotReplyText(ctx context.Context, client *gobot.Client, botUsername, prompt string, waitDur time.Duration) (string, error) {
	botPeer, err := s.resolveBotPeerByUsername(ctx, client, botUsername)
	if err != nil {
		return "", err
	}
	if _, err := client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     botPeer,
		Message:  prompt,
		RandomID: randInt64(),
	}); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(waitDur):
	}
	hist, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  botPeer,
		Limit: 2,
	})
	if err != nil {
		return "", err
	}
	for _, item := range extractMessagesSlice(hist) {
		if m, ok := item.(*tg.Message); ok && !m.Out && strings.TrimSpace(m.Message) != "" {
			return strings.TrimSpace(m.Message), nil
		}
	}
	return "", fmt.Errorf("belum ada balasan dari @%s", strings.TrimPrefix(botUsername, "@"))
}

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
