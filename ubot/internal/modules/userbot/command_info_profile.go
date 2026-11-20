package userbot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

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
	fields := strings.Fields(args)
	if len(fields) > 0 {
		rawTarget := strings.TrimSpace(fields[0])
		if targetUID, _, err := s.resolveTargetUserID(ctx, client, target, msg, entities, args); err == nil && targetUID > 0 {
			return sendUserCode(ctx, client, target, strconv.FormatInt(targetUID, 10), threadID)
		}
		if strings.HasPrefix(rawTarget, "@") && len(rawTarget) > 1 {
			if resolved, rErr := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: strings.TrimPrefix(rawTarget, "@")}); rErr == nil && resolved != nil {
				switch p := resolved.Peer.(type) {
				case *tg.PeerUser:
					return sendUserCode(ctx, client, target, strconv.FormatInt(p.UserID, 10), threadID)
				case *tg.PeerChannel:
					return sendUserCode(ctx, client, target, strconv.FormatInt(-1000000000000-p.ChannelID, 10), threadID)
				case *tg.PeerChat:
					return sendUserCode(ctx, client, target, strconv.FormatInt(-p.ChatID, 10), threadID)
				}
			}
		}
		return sendUserText(ctx, client, target, "Target tidak ditemukan.", threadID)
	}

	if broadcastReplyMessageID(msg) > 0 {
		if reply, err := s.replyMessage(ctx, client, msg, target); err == nil && reply != nil {
			if uid, ok := extractUserIDFromMessage(reply, ownerID); ok && uid > 0 {
				return sendUserCode(ctx, client, target, strconv.FormatInt(uid, 10), threadID)
			}
			if pc, ok := reply.FromID.(*tg.PeerChannel); ok && pc.ChannelID > 0 {
				return sendUserCode(ctx, client, target, strconv.FormatInt(-1000000000000-pc.ChannelID, 10), threadID)
			}
		}
	}

	actualChatID := formatPeerChatID(target, msg, ownerID)
	return sendUserCode(ctx, client, target, strconv.FormatInt(actualChatID, 10), threadID)
}

func extractMissRoseField(roseText string, prefixes ...string) string {
	for _, rawLine := range strings.Split(strings.ReplaceAll(roseText, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		low := strings.ToLower(line)
		for _, p := range prefixes {
			if strings.HasPrefix(low, p) {
				return strings.TrimSpace(line[len(p):])
			}
		}
	}
	return ""
}

func formatMissRoseInfoHTML(roseText string) string {
	lines := strings.Split(strings.ReplaceAll(roseText, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if colon := strings.IndexByte(trimmed, ':'); colon > 0 && colon < len(trimmed)-1 && !strings.HasPrefix(trimmed[colon:], "://") {
			lbl := trimmed[:colon+1]
			val := strings.TrimSpace(trimmed[colon+1:])
			out = append(out, fmt.Sprintf("<b>%s</b> <i>%s</i>", html.EscapeString(lbl), html.EscapeString(val)))
		} else {
			out = append(out, fmt.Sprintf("<b>%s</b>", html.EscapeString(trimmed)))
		}
	}
	return strings.Join(out, "\n")
}

func (s *Service) sendMissRoseInfoCard(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, roseText string, uid int64, rawArg string, threadID int) error {
	if uid == 0 {
		if idStr := extractMissRoseField(roseText, "id:", "user id:"); idStr != "" {
			if parsed, err := strconv.ParseInt(strings.Fields(idStr)[0], 10, 64); err == nil && parsed > 0 {
				uid = parsed
			}
		}
	}
	about := extractMissRoseField(roseText, "bio:", "about:")
	if about == "" {
		about = "Bio tidak tercantum."
	}
	directURL := ""
	if uid > 0 {
		directURL = fmt.Sprintf("tg://user?id=%d", uid)
	} else if uname := strings.TrimPrefix(strings.TrimSpace(rawArg), "@"); uname != "" {
		directURL = "https://t.me/" + uname
	}
	buttons := []ubotInlineCardButton{{Text: "Bio", Alert: about}}
	if directURL != "" {
		buttons = append(buttons, ubotInlineCardButton{Text: "Direct link", URL: directURL})
	}
	card := ubotInlineCardPayload{
		Title:   "Info Pengguna",
		HTML:    formatMissRoseInfoHTML(roseText),
		Buttons: [][]ubotInlineCardButton{buttons},
	}
	return s.sendInlineCardWithFallback(ctx, client, target, card, roseText, threadID)
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
					return s.sendMissRoseInfoCard(ctx, client, target, roseText, 0, rawArg, threadID)
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
			return s.sendMissRoseInfoCard(ctx, client, target, roseText, uid, rawArg, threadID)
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
