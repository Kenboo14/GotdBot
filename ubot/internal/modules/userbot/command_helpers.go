package userbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

type commandReplyCtxKey struct{}

var errNoTargetUser = errors.New("target user tidak diberikan dan tidak membalas pesan")

// withCommandReplyContext menyimpan ID pesan command di context agar seluruh
// handler userbot otomatis membalas (reply) ke pesan command tersebut.
func withCommandReplyContext(ctx context.Context, msg *tg.Message) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if msg == nil || msg.ID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, commandReplyCtxKey{}, msg.ID)
}

func commandReplyMsgID(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	id, _ := ctx.Value(commandReplyCtxKey{}).(int)
	return id
}

// buildReplyTo menyusun InputReplyToMessage yang membalas command msgID sekaligus
// menjaga routing topic forum (TopMsgID) jika command dikirim di dalam topic.
func buildReplyTo(threadID, replyToMsgID int) *tg.InputReplyToMessage {
	if replyToMsgID <= 0 && threadID <= 0 {
		return nil
	}
	if replyToMsgID <= 0 {
		replyToMsgID = threadID
	}
	rt := &tg.InputReplyToMessage{
		ReplyToMsgID: replyToMsgID,
	}
	if threadID > 0 {
		rt.TopMsgID = threadID
	}
	return rt
}

func isReplyTargetInvalidError(err error) bool {
	if err == nil {
		return false
	}
	up := strings.ToUpper(err.Error())
	return strings.Contains(up, "MESSAGE_ID_INVALID") || strings.Contains(up, "REPLY_MESSAGE_ID_INVALID")
}

func isMessageNotModifiedError(err error) bool {
	if err == nil {
		return false
	}
	up := strings.ToUpper(err.Error())
	return strings.Contains(up, "MESSAGE_NOT_MODIFIED")
}

func extractSentMessageID(updates tg.UpdatesClass) int {
	if updates == nil {
		return 0
	}
	scanList := func(items []tg.UpdateClass) int {
		for _, item := range items {
			switch v := item.(type) {
			case *tg.UpdateMessageID:
				return v.ID
			case *tg.UpdateNewMessage:
				if m, ok := v.Message.(*tg.Message); ok && m.ID > 0 {
					return m.ID
				}
			case *tg.UpdateNewChannelMessage:
				if m, ok := v.Message.(*tg.Message); ok && m.ID > 0 {
					return m.ID
				}
			}
		}
		return 0
	}
	switch u := updates.(type) {
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.Updates:
		return scanList(u.Updates)
	case *tg.UpdatesCombined:
		return scanList(u.Updates)
	}
	return 0
}

func sendUserText(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, text string, threadID int) error {
	_, err := sendUserStatusMessage(ctx, client, target, text, threadID)
	if err != nil {
		log.Printf("[sendUserText] MTProto send failed (thread=%d reply=%d): %v", threadID, commandReplyMsgID(ctx), err)
	}
	return err
}

func sendUserStatusMessage(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, text string, threadID int) (int, error) {
	if client == nil || target == nil {
		return 0, fmt.Errorf("parameter kirim pesan tidak valid")
	}
	cleanText, entities := formatUserbotResponse(text)
	replyToMsgID := commandReplyMsgID(ctx)
	req := &tg.MessagesSendMessageRequest{
		Peer:     target,
		Message:  cleanText,
		Entities: entities,
		RandomID: randInt64(),
	}
	if replyTo := buildReplyTo(threadID, replyToMsgID); replyTo != nil {
		req.ReplyTo = replyTo
	}
	upd, err := client.API().MessagesSendMessage(ctx, req)
	if err != nil && replyToMsgID > 0 && isReplyTargetInvalidError(err) {
		req.ReplyTo = buildReplyTo(threadID, 0)
		upd, err = client.API().MessagesSendMessage(ctx, req)
	}
	if err != nil {
		return 0, err
	}
	return extractSentMessageID(upd), nil
}

// editUserText mengedit pesan milik sendiri berdasarkan msgID.
func editUserText(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msgID int, text string) error {
	if client == nil || target == nil || msgID <= 0 {
		return fmt.Errorf("parameter edit pesan tidak valid")
	}
	cleanText, entities := formatUserbotResponse(text)
	_, err := client.API().MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:     target,
		ID:       msgID,
		Message:  cleanText,
		Entities: entities,
	})
	if isMessageNotModifiedError(err) {
		return nil
	}
	return err
}

// replyUserText membalas pesan command msg secara eksplisit (menjaga topic threadID).
func replyUserText(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, text string, threadID int) error {
	return sendUserText(withCommandReplyContext(ctx, msg), client, target, text, threadID)
}

// eor (Edit Or Reply) adalah helper untuk fitur userbot yang ingin mengedit pesan sendiri
// jika memungkinkan (msg.Out) dan otomatis fallback ke reply jika gagal atau berasal dari sudo/orang lain.
func eor(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, text string, threadID int) error {
	if msg != nil && msg.Out && msg.ID > 0 {
		if err := editUserText(ctx, client, target, msg.ID, text); err == nil {
			return nil
		}
	}
	return replyUserText(ctx, client, target, msg, text, threadID)
}

func (s *Service) eor(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, text string, threadID int) error {
	return eor(ctx, client, target, msg, text, threadID)
}

// extractUserIDFromMessage mengambil ID pengirim dari *tg.Message (mendukung grup maupun private chat).
func extractUserIDFromMessage(msg *tg.Message, ownerID int64) (int64, bool) {
	if msg == nil {
		return 0, false
	}
	if sender, ok := msg.FromID.(*tg.PeerUser); ok && sender.UserID > 0 {
		return sender.UserID, true
	}
	if msg.Out && ownerID > 0 {
		return ownerID, true
	}
	if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && peerUser.UserID > 0 && !msg.Out {
		return peerUser.UserID, true
	}
	return 0, false
}

func displayNameFromEntities(entities tg.Entities, userID int64) string {
	if userID <= 0 || entities.Users == nil {
		return ""
	}
	u := entities.Users[userID]
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return ""
}

// resolveTargetUserID mengekstrak target User ID secara fleksibel untuk fitur userbot:
// 1) ID numerik pada argumen,
// 2) text mention entity pada pesan command,
// 3) @username pada entities atau ContactsResolveUsername,
// 4) pesan yang di-reply (otomatis mengambil User ID pengirim pesan yang dibalas).
func (s *Service) resolveTargetUserID(
	ctx context.Context,
	client *gobot.Client,
	target tg.InputPeerClass,
	msg *tg.Message,
	entities tg.Entities,
	args string,
) (int64, string, error) {
	fields := strings.Fields(args)
	if len(fields) > 0 {
		raw := fields[0]
		cleanNumeric := strings.TrimPrefix(raw, "@")
		if uid, err := strconv.ParseInt(cleanNumeric, 10, 64); err == nil && uid > 0 {
			return uid, displayNameFromEntities(entities, uid), nil
		}
		if msg != nil {
			for _, ent := range msg.Entities {
				if mention, ok := ent.(*tg.MessageEntityMentionName); ok && mention.UserID > 0 {
					return mention.UserID, displayNameFromEntities(entities, mention.UserID), nil
				}
			}
		}
		if strings.HasPrefix(raw, "@") && len(raw) > 1 {
			uname := strings.TrimPrefix(raw, "@")
			for _, u := range entities.Users {
				if u != nil && u.Username != "" && strings.EqualFold(u.Username, uname) {
					return u.ID, displayNameFromEntities(entities, u.ID), nil
				}
			}
			if client != nil {
				resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: uname})
				if err == nil && resolved != nil {
					s.peerCache().ApplyUsersChats(resolved.Users, resolved.Chats)
					if peerUser, ok := resolved.Peer.(*tg.PeerUser); ok && peerUser.UserID > 0 {
						name := ""
						for _, u := range resolved.Users {
							if userObj, ok := u.(*tg.User); ok && userObj.ID == peerUser.UserID {
								name = strings.TrimSpace(userObj.FirstName + " " + userObj.LastName)
								if name == "" && userObj.Username != "" {
									name = "@" + userObj.Username
								}
								break
							}
						}
						return peerUser.UserID, name, nil
					}
				}
			}
			return 0, "", fmt.Errorf("username %s tidak ditemukan", raw)
		}
		return 0, "", fmt.Errorf("User ID tidak valid. Gunakan ID numerik, @username, atau balas pesan pengguna")
	}

	if msg == nil {
		return 0, "", errNoTargetUser
	}
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil {
		return 0, "", err
	}
	if reply == nil {
		return 0, "", errNoTargetUser
	}

	var ownerID int64
	if client != nil {
		ownerID, _ = s.userbotOwner(ctx, client)
	}
	uid, ok := extractUserIDFromMessage(reply, ownerID)
	if !ok {
		if _, isChannel := reply.FromID.(*tg.PeerChannel); isChannel {
			return 0, "", fmt.Errorf("pesan yang dibalas dikirim oleh channel/anonim dan tidak memiliki User ID")
		}
		return 0, "", fmt.Errorf("gagal mengambil User ID dari pesan yang dibalas")
	}

	name := displayNameFromEntities(entities, uid)
	return uid, name, nil
}

type seenCmdKey struct {
	chatID int64
	peer   string
	msgID  int
}

type seenCmdEntry struct {
	text     string
	editDate int
	seenAt   time.Time
}

func peerKeyString(p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("u:%d", v.UserID)
	case *tg.PeerChat:
		return fmt.Sprintf("g:%d", v.ChatID)
	case *tg.PeerChannel:
		return fmt.Sprintf("c:%d", v.ChannelID)
	default:
		return "unknown"
	}
}

func (s *Service) markCommandExecuted(chatID int64, msg *tg.Message) {
	if s == nil || msg == nil || msg.ID <= 0 {
		return
	}
	key := seenCmdKey{chatID: chatID, peer: peerKeyString(msg.PeerID), msgID: msg.ID}
	now := time.Now()
	s.seenCmdMu.Lock()
	defer s.seenCmdMu.Unlock()
	if s.seenCmds == nil {
		s.seenCmds = make(map[seenCmdKey]seenCmdEntry)
	}
	if len(s.seenCmds) > 1024 {
		for k, entry := range s.seenCmds {
			if now.Sub(entry.seenAt) > 10*time.Minute {
				delete(s.seenCmds, k)
			}
		}
	}
	s.seenCmds[key] = seenCmdEntry{
		text:     msg.Message,
		editDate: msg.EditDate,
		seenAt:   now,
	}
}

// shouldProcessEditedCommand mencegah reaction, update view count, atau link preview
// pada pesan command yang sudah diproses sebelumnya memicu eksekusi ulang.
func (s *Service) shouldProcessEditedCommand(chatID int64, msg *tg.Message, nowUnix int64) bool {
	if msg == nil || msg.ID <= 0 || msg.EditDate <= 0 {
		return false
	}
	if nowUnix > 0 && nowUnix-int64(msg.EditDate) > 300 {
		return false
	}
	if s == nil {
		return true
	}
	key := seenCmdKey{chatID: chatID, peer: peerKeyString(msg.PeerID), msgID: msg.ID}
	s.seenCmdMu.Lock()
	defer s.seenCmdMu.Unlock()
	if entry, ok := s.seenCmds[key]; ok {
		if entry.text == msg.Message || (entry.editDate != 0 && entry.editDate == msg.EditDate) {
			return false
		}
	}
	return true
}
