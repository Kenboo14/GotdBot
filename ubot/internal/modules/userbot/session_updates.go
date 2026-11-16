package userbot

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
)

func isSessionRevokedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToUpper(err.Error())

	if strings.Contains(errStr, "TIMEOUT") ||
		strings.Contains(errStr, "DEADLINE EXCEEDED") ||
		strings.Contains(errStr, "RESET BY PEER") ||
		strings.Contains(errStr, "BROKEN PIPE") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "NETWORK IS UNREACHABLE") ||
		strings.Contains(errStr, "CONNECTION REFUSED") ||
		strings.Contains(errStr, "NO SUCH HOST") {
		return false
	}

	if strings.Contains(errStr, "FLOOD_WAIT") ||
		strings.Contains(errStr, "CHAT_WRITE_FORBIDDEN") ||
		strings.Contains(errStr, "PEER_ID_INVALID") ||
		strings.Contains(errStr, "CHANNEL_PRIVATE") ||
		strings.Contains(errStr, "CHAT_ID_INVALID") {
		return false
	}

	return strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") ||
		strings.Contains(errStr, "SESSION_REVOKED") ||
		strings.Contains(errStr, "SESSION_EXPIRED") ||
		strings.Contains(errStr, "USER_DEACTIVATED") ||
		strings.Contains(errStr, "AUTH_KEY_PERM_EMPTY") ||
		strings.Contains(errStr, "AUTH_KEY_DUPLICATED")
}

func (s *Service) handleSessionRevoked(chatID int64, genID uint64, reason error) {
	s.mu.Lock()
	currentGen := s.accountGens[chatID]
	if currentGen == 0 || (genID != 0 && currentGen != genID) {
		s.mu.Unlock()
		return
	}
	delete(s.accountGens, chatID)
	s.accountStates[chatID] = StateSessionInvalid

	client := s.active[chatID]
	ownerID := s.owners[chatID]
	stateReason := "Sesi Telegram dicabut"
	if reason != nil {
		stateReason = reason.Error()
	}
	delete(s.active, chatID)
	delete(s.owners, chatID)
	delete(s.lastUpdate, chatID)
	delete(s.lastVerified, chatID)
	delete(s.verifying, chatID)
	if client != nil {
		delete(s.clientOwners, client)
		delete(s.senderCache, client)
	}
	cancel := s.activeCancels[chatID]
	delete(s.activeCancels, chatID)
	delete(s.accountCtxs, chatID)
	s.mu.Unlock()

	slog.Error("session revoked from telegram",
		slog.Int64("chat_id", chatID),
		slog.Int64("owner_id", ownerID),
		slog.Uint64("gen_id", genID),
		slog.String("reason", stateReason),
	)

	s.publishSessionState(chatID, ownerID, StateSessionInvalid, stateReason)

	if cancel != nil {
		cancel()
	}

	if s.sqlite != nil {
		_ = s.sqlite.DeleteSession(context.Background(), chatID)
	}
	if s.db != nil {
		_, _ = s.db.Collection("userbot_sessions").DeleteOne(context.Background(), bson.M{"_id": chatID})
	}

	s.mu.Lock()
	s.accountStates[chatID] = StateStopped
	s.mu.Unlock()

	s.reply(context.Background(), chatID, 0, `<h1>⚠️ Sesi Userbot Terlepas</h1>
<blockquote expandable>Koneksi MTProto userbot Anda terputus atau sesi telah dicabut dari aplikasi Telegram.</blockquote>
<hr/>
<p>Ketik atau tekan <b>/start</b> untuk menyambungkan kembali akun Anda.</p>`, "session_revoked")
}

func (s *Service) resolvePeer(ctx context.Context, client *telegram.Client, chatID int64, entities tg.Entities, p tg.PeerClass) (tg.InputPeerClass, error) {
	if p == nil {
		return nil, fmt.Errorf("peer is nil")
	}

	s.mu.Lock()
	ownerID := s.owners[chatID]
	if ownerID == 0 && client != nil {
		ownerID = s.clientOwners[client]
	}
	s.mu.Unlock()

	switch peerObj := p.(type) {
	case *tg.PeerUser:
		if (ownerID > 0 && peerObj.UserID == ownerID) || peerObj.UserID == 0 {
			return &tg.InputPeerSelf{}, nil
		}
		if u, ok := entities.Users[peerObj.UserID]; ok && u != nil {
			if u.AccessHash != 0 {
				s.userAccessHashes.Store(u.ID, u.AccessHash)
			}
			return &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
		}
		if val, ok := s.userAccessHashes.Load(peerObj.UserID); ok {
			if hash, ok := val.(int64); ok {
				return &tg.InputPeerUser{UserID: peerObj.UserID, AccessHash: hash}, nil
			}
		}
		if ownerID == 0 {
			return &tg.InputPeerSelf{}, nil
		}
		accessHash := s.resolveUserAccessHash(ctx, client, peerObj.UserID)
		return &tg.InputPeerUser{UserID: peerObj.UserID, AccessHash: accessHash}, nil

	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: peerObj.ChatID}, nil

	case *tg.PeerChannel:
		if ch, ok := entities.Channels[peerObj.ChannelID]; ok && ch != nil {
			if ch.AccessHash != 0 {
				s.channelAccessHashes.Store(ch.ID, ch.AccessHash)
			}
			return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
		}
		if val, ok := s.channelAccessHashes.Load(peerObj.ChannelID); ok {
			if hash, ok := val.(int64); ok {
				return &tg.InputPeerChannel{ChannelID: peerObj.ChannelID, AccessHash: hash}, nil
			}
		}
		return &tg.InputPeerChannel{ChannelID: peerObj.ChannelID, AccessHash: 0}, nil

	default:
		return peer.EntitiesFromUpdate(entities).ExtractPeer(p)
	}
}

func extractThreadID(msg *tg.Message) int {
	if replyTo, ok := msg.GetReplyTo(); ok {
		if header, ok := replyTo.(*tg.MessageReplyHeader); ok {
			if header.ReplyToTopID != 0 {
				return header.ReplyToTopID
			}
			if header.ForumTopic && header.ReplyToMsgID != 0 {
				return header.ReplyToMsgID
			}
		}
	}
	return 0
}

func (s *Service) handleUserbotMessage(ctx context.Context, chatID int64, client *telegram.Client, entities tg.Entities, update *tg.UpdateNewMessage) error {
	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}
	threadID := extractThreadID(msg)
	prefix := "!"
	s.mu.Lock()
	if p, ok := s.prefixes[chatID]; ok {
		prefix = p
	}
	s.mu.Unlock()

	if strings.HasPrefix(msg.Message, "#") && len(msg.Message) > 1 && !strings.ContainsAny(msg.Message, " \t\r\n") {
		hashName := strings.TrimPrefix(msg.Message, "#")
		if isValidNoteName(hashName) && s.commandAllowed(ctx, client, msg) {
			peerID, err := s.resolvePeer(ctx, client, chatID, entities, msg.PeerID)
			if err == nil {
				return s.showNote(ctx, client, peerID, hashName, false, threadID)
			}
		}
	}

	if prefix != "" {
		if !strings.HasPrefix(msg.Message, prefix) {
			return nil
		}
	} else {
		if !msg.Out {
			s.mu.Lock()
			ownerID := s.owners[chatID]
			s.mu.Unlock()
			senderID := int64(0)
			if sender, ok := msg.FromID.(*tg.PeerUser); ok {
				senderID = sender.UserID
			}
			if senderID == 0 || !s.isSudo(ctx, ownerID, senderID) {
				return nil
			}
		}
	}

	name, args, ok := parseUserCommand(msg.Message, prefix)
	if !ok {
		return nil
	}
	if !s.commandAllowed(ctx, client, msg) {
		return nil
	}
	peerID, err := s.resolvePeer(ctx, client, chatID, entities, msg.PeerID)
	if err != nil {
		slog.Error("failed to resolve peer for userbot command",
			slog.String("command", name),
			slog.Int64("chat_id", chatID),
			slog.String("error", err.Error()),
		)
		return err
	}

	slog.Info("executing userbot command",
		slog.String("command", name),
		slog.Int64("chat_id", chatID),
		slog.Int("thread_id", threadID),
		slog.Bool("out", msg.Out),
	)

	if name == "setp" {
		return s.handleSetPrefix(ctx, client, chatID, peerID, args, threadID)
	}
	if name == "help" {
		if err := s.sendInlineHelp(ctx, client, peerID, threadID); err != nil {
			log.Printf("[userbot %d] !help inline gagal: %v", msg.ID, err)
			text := `<h1>📖 Panduan Perintah GotdBot</h1>
<blockquote expandable>Daftar shortcut dan perintah esensial untuk kontrol userbot MTProto.</blockquote>
<table border="1">
  <tr><th>Perintah</th><th>Fungsi</th></tr>
  <tr><td><code>!ping</code> / <code>!p</code></td><td>Ukur latency koneksi MTProto</td></tr>
  <tr><td><code>!help</code></td><td>Buka menu bantuan interaktif</td></tr>
  <tr><td><code>!server</code></td><td>Status server userbot</td></tr>
  <tr><td><code>!ping</code></td><td>Alias cepat</td></tr>
</table>
<hr/>
<p>GotdBot • Interactive Assistance</p>`
			sendErr := sendUserText(ctx, client, peerID, text, threadID)
			return sendErr
		}
		return nil
	}
	if name == "server" {
		if err := s.sendInlineQuery(ctx, client, peerID, "server", threadID); err != nil {
			log.Printf("[userbot %d] !server inline gagal: %v", msg.ID, err)
			text := `<h1>🖥 Status Server</h1>
<blockquote expandable>Menampilkan beban resource CPU, RAM, Uptime, dan performa Gateway MTProto.</blockquote>
<hr/>
<p>Ketik <code>!server</code> pada chat untuk memeriksa statistik sistem secara real-time.</p>
<p>GotdBot • Server Status</p>`
			sendErr := sendUserText(ctx, client, peerID, text, threadID)
			return sendErr
		}
		return nil
	}
	if name == "code" {
		return s.handleCodeCommand(ctx, client, peerID, msg, args, threadID)
	}
	if name == "gcast" || name == "broadcast" || name == "cancel" {
		if name == "cancel" {
			args = "cancel"
		}
		if err := s.broadcastCommand(ctx, client, peerID, msg, args, threadID); err != nil {
			log.Printf("[userbot %d] broadcast gagal: %v", msg.ID, err)
			return sendUserText(ctx, client, peerID, "Broadcast gagal: "+err.Error(), threadID)
		}
		return nil
	}
	if name == "addbl" || name == "rmbl" {
		return s.blacklistCommand(ctx, client, peerID, msg, entities, args, map[string]string{"addbl": "add", "rmbl": "remove"}[name], threadID)
	}
	if name == "rmallbl" {
		return s.clearBlacklist(ctx, client, peerID, threadID)
	}
	if name == "lsbl" {
		return s.listBlacklist(ctx, client, peerID, threadID)
	}
	if name == "addsudo" || name == "rmsudo" {
		return s.sudoCommand(ctx, client, peerID, msg, args, name == "addsudo", threadID)
	}
	if name == "save" {
		return s.saveNoteCommand(ctx, client, peerID, msg, args, threadID)
	}
	if name == "log" {
		return s.handleLogCommand(ctx, client, peerID, args, threadID)
	}
	if name == "statuslog" {
		return s.handleStatusLogCommand(ctx, client, peerID, threadID)
	}
	if name == "get" {
		fields := strings.Fields(args)
		if len(fields) == 0 {
			return sendUserText(ctx, client, peerID, "Gunakan: !get <nama>", threadID)
		}
		return s.showNote(ctx, client, peerID, fields[0], false, threadID)
	}
	if name == "cat" {
		noFm := false
		catName := ""
		fields := strings.Fields(args)
		if len(fields) > 0 {
			catName = fields[0]
			for _, f := range fields[1:] {
				if strings.ToLower(f) == "-nofm" {
					noFm = true
				}
			}
		}
		if catName == "" {
			return sendUserText(ctx, client, peerID, "Gunakan: !cat <nama> [-nofm]", threadID)
		}
		return s.showNote(ctx, client, peerID, catName, noFm, threadID)
	}
	if name == "lsnote" || name == "notes" {
		return s.listNotesCommand(ctx, client, peerID, threadID)
	}
	if name == "delnote" {
		return s.deleteNoteCommand(ctx, client, peerID, args, threadID)
	}
	if name == "clearnote" {
		return s.clearNotesCommand(ctx, client, peerID, threadID)
	}
	if name == "nofm" {
		return s.nofmCommand(ctx, client, peerID, msg, args, threadID)
	}
	if name == "exportnote" {
		return s.exportNotesCommand(ctx, client, peerID, threadID)
	}
	if name == "importnote" {
		return s.importNotesCommand(ctx, client, peerID, msg, threadID)
	}
	if name == "eval" || name == "sh" {
		if name == "eval" {
			return s.evaluatorCommand(ctx, client, peerID, msg, entities, prefix, args, threadID)
		}
		return s.shellCommand(ctx, client, peerID, prefix, args, threadID)
	}
	if name != "ping" && name != "p" {
		return nil
	}
	started := time.Now()
	text := "Ping gagal. Periksa koneksi dan status sesi userbot."
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err == nil {
		latency := float64(time.Since(started).Microseconds()) / 1000
		if name == "p" {
			text = fmt.Sprintf("Pong! %.3f ms", latency)
		} else {
			text = fmt.Sprintf("<code>%.3f ms</code>", latency)
		}
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	defer sendCancel()
	err = sendUserText(sendCtx, client, peerID, text, threadID)
	return err
}

func (s *Service) cacheEntities(entities tg.Entities) {
	for _, u := range entities.Users {
		if u != nil && u.ID != 0 && u.AccessHash != 0 {
			s.userAccessHashes.Store(u.ID, u.AccessHash)
		}
	}
	for _, ch := range entities.Channels {
		if ch != nil && ch.ID != 0 && ch.AccessHash != 0 {
			s.channelAccessHashes.Store(ch.ID, ch.AccessHash)
		}
	}
}

func (s *Service) resolveUserAccessHash(ctx context.Context, client *telegram.Client, userID int64) int64 {
	var accessHash int64

	// Try to get from cache first
	if val, ok := s.userAccessHashes.Load(userID); ok {
		if hash, ok := val.(int64); ok {
			accessHash = hash
		}
	}

	// If not in cache, try to resolve via API
	if accessHash == 0 {
		// Try UsersGetUsers first (fast, targeted)
		users, err := client.API().UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: userID}})
		if err == nil {
			for _, u := range users {
				if user, ok := u.(*tg.User); ok && user.ID == userID {
					accessHash = user.AccessHash
					s.userAccessHashes.Store(user.ID, user.AccessHash)
					return accessHash
				}
			}
		}

		// Fall back to MessagesGetDialogs to find user's access hash
		dialogs, dErr := client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{Limit: 100})
		if dErr == nil {
			if mod, ok := dialogs.AsModified(); ok {
				for _, u := range mod.GetUsers() {
					if user, ok := u.(*tg.User); ok && user.ID == userID {
						accessHash = user.AccessHash
						s.userAccessHashes.Store(user.ID, user.AccessHash)
						return accessHash
					}
				}
			}
		}
	}

	return accessHash
}
