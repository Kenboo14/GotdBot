package userbot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func formatPeerChatID(target tg.InputPeerClass, msg *tg.Message, ownerID int64) int64 {
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		return -1000000000000 - p.ChannelID
	case *tg.InputPeerChat:
		return -p.ChatID
	case *tg.InputPeerUser:
		return p.UserID
	case *tg.InputPeerSelf:
		return ownerID
	}
	if msg != nil && msg.PeerID != nil {
		switch p := msg.PeerID.(type) {
		case *tg.PeerChannel:
			return -1000000000000 - p.ChannelID
		case *tg.PeerChat:
			return -p.ChatID
		case *tg.PeerUser:
			return p.UserID
		}
	}
	return ownerID
}

func formatUserDCID(u *tg.User) string {
	if u == nil || u.Photo == nil {
		return "-"
	}
	if photo, ok := u.Photo.(*tg.UserProfilePhoto); ok && photo.DCID > 0 {
		return strconv.Itoa(photo.DCID)
	}
	return "-"
}

func formatUserLastSeen(u *tg.User) string {
	if u == nil {
		return "Hidden"
	}
	if u.Bot {
		return "Bot"
	}
	switch st := u.Status.(type) {
	case *tg.UserStatusOnline:
		return "Online"
	case *tg.UserStatusRecently:
		return "Recently"
	case *tg.UserStatusLastWeek:
		return "Within a week"
	case *tg.UserStatusLastMonth:
		return "Within a month"
	case *tg.UserStatusOffline:
		if st.WasOnline > 0 {
			return time.Unix(int64(st.WasOnline), 0).Local().Format("02 Jan 2006 15:04")
		}
		return "Offline"
	default:
		return "Hidden"
	}
}

func (s *Service) idCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	ownerID, _ := s.userbotOwner(ctx, client)
	yourID := ownerID
	if senderID, ok := extractUserIDFromMessage(msg, ownerID); ok && senderID > 0 {
		yourID = senderID
	}
	actualChatID := formatPeerChatID(target, msg, ownerID)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Your ID: %d\nChat ID: %d\n", yourID, actualChatID)

	if strings.TrimSpace(args) != "" || broadcastReplyMessageID(msg) > 0 {
		if targetUID, _, err := s.resolveTargetUserID(ctx, client, target, msg, entities, args); err == nil && targetUID > 0 {
			fmt.Fprintf(&sb, "User ID: %d\n", targetUID)
		}
	}
	return sendUserText(ctx, client, target, strings.TrimSpace(sb.String()), threadID)
}

func (s *Service) fetchMissRoseUserInfo(ctx context.Context, client *gobot.Client, rawQuery string) (string, bool) {
	q := strings.TrimSpace(rawQuery)
	if q == "" {
		return "", false
	}
	text, err := s.queryBotReplyText(ctx, client, "MissRose_bot", "/info "+q, 1500*time.Millisecond)
	if err != nil || strings.TrimSpace(text) == "" {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func (s *Service) infoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	var uid int64
	var name string
	var inputUser tg.InputUserClass
	rawArg := ""
	if fields := strings.Fields(args); len(fields) > 0 {
		rawArg = strings.TrimSpace(fields[0])
	}

	if rawArg != "" || broadcastReplyMessageID(msg) > 0 {
		var err error
		uid, name, inputUser, err = s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
		if err != nil {
			if rawArg != "" {
				if roseText, ok := s.fetchMissRoseUserInfo(ctx, client, rawArg); ok {
					return sendUserText(ctx, client, target, roseText, threadID)
				}
			}
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
		qTarget := rawArg
		if qTarget == "" && uid > 0 {
			qTarget = strconv.FormatInt(uid, 10)
		}
		if roseText, ok := s.fetchMissRoseUserInfo(ctx, client, qTarget); ok {
			if uid > 0 {
				card := ubotInlineCardPayload{
					Title: fmt.Sprintf("Info User %d", uid),
					HTML:  fmt.Sprintf("<b>Informasi Pengguna (@MissRose_bot)</b>\n<i>%s</i>", html.EscapeString(roseText)),
					Buttons: [][]ubotInlineCardButton{
						{{Text: "Direct link", URL: fmt.Sprintf("tg://user?id=%d", uid)}},
					},
				}
				return s.sendInlineCardWithFallback(ctx, client, target, card, roseText, threadID)
			}
			return sendUserText(ctx, client, target, roseText, threadID)
		}
		if name == "" {
			name = displayNameFromEntities(entities, uid)
		}
		if name == "" {
			name = "-"
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("User ID: %d\nFirst Name: %s", uid, name), threadID)
	}

	var uObj *tg.User
	for _, rawU := range full.Users {
		if u, ok := rawU.(*tg.User); ok && u.ID == uid {
			uObj = u
			break
		}
	}
	firstName := name
	username := "-"
	premiumStr := "False"
	if uObj != nil {
		combined := strings.TrimSpace(uObj.FirstName + " " + uObj.LastName)
		if combined != "" {
			firstName = combined
		}
		if uObj.Username != "" {
			username = "@" + uObj.Username
		}
		if uObj.Premium {
			premiumStr = "True"
		}
	}
	if firstName == "" {
		firstName = "-"
	}
	dcID := formatUserDCID(uObj)
	lastSeen := formatUserLastSeen(uObj)
	sameGroup := full.FullUser.CommonChatsCount
	about := strings.TrimSpace(full.FullUser.About)
	if about == "" {
		about = "Tidak ada bio."
	}

	cardHTML := strings.Join([]string{
		fmt.Sprintf("<b>DC ID:</b> <i>%s</i>", html.EscapeString(dcID)),
		fmt.Sprintf("<b>User ID:</b> <i>%d</i>", uid),
		fmt.Sprintf("<b>First Name:</b> <i>%s</i>", html.EscapeString(firstName)),
		fmt.Sprintf("<b>Username:</b> <i>%s</i>", html.EscapeString(username)),
		fmt.Sprintf("<b>Premium:</b> <i>%s</i>", premiumStr),
		fmt.Sprintf("<b>Same Group:</b> <i>%d</i>", sameGroup),
		fmt.Sprintf("<b>Terakhir terlihat:</b> <i>%s</i>", html.EscapeString(lastSeen)),
	}, "\n")

	fallbackText := strings.Join([]string{
		fmt.Sprintf("DC ID: %s", dcID),
		fmt.Sprintf("User ID: %d", uid),
		fmt.Sprintf("First Name: %s", firstName),
		fmt.Sprintf("Username: %s", username),
		fmt.Sprintf("Premium: %s", premiumStr),
		fmt.Sprintf("Same Group: %d", sameGroup),
		fmt.Sprintf("Terakhir terlihat: %s", lastSeen),
		fmt.Sprintf("Bio: %s", about),
	}, "\n")

	card := ubotInlineCardPayload{
		Title: fmt.Sprintf("Info %s", firstName),
		HTML:  cardHTML,
		Buttons: [][]ubotInlineCardButton{
			{
				{Text: "Bio", Alert: about},
				{Text: "Direct link", URL: fmt.Sprintf("tg://user?id=%d", uid)},
			},
		},
	}
	return s.sendInlineCardWithFallback(ctx, client, target, card, fallbackText, threadID)
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
