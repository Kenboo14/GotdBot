package userbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	gobotsession "github.com/Kenboo14/GoBot/session"
	"github.com/Kenboo14/GoBot/types"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
)

func isSessionRevokedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToUpper(err.Error())
	if strings.Contains(errStr, "INPUT_USER_DEACTIVATED") ||
		strings.Contains(errStr, "USER_IS_BLOCKED") ||
		strings.Contains(errStr, "USER_IS_BOT") ||
		strings.Contains(errStr, "USER_PRIVACY_RESTRICTED") ||
		strings.Contains(errStr, "USER_ID_INVALID") ||
		strings.Contains(errStr, "PEER_FLOOD") {
		return false
	}
	return gobotsession.IsSessionRevoked(err)
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
	delete(s.lastPublishedStates, chatID)
	delete(s.lastPublishedAt, chatID)
	delete(s.reconnectAttempts, chatID)
	if client != nil {
		delete(s.clientOwners, client)
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

func (s *Service) resolvePeer(ctx context.Context, client *gobot.Client, chatID int64, entities tg.Entities, p tg.PeerClass, msg *tg.Message) (tg.InputPeerClass, error) {
	if p == nil {
		return nil, fmt.Errorf("peer is nil")
	}

	s.mu.RLock()
	ownerID := s.owners[chatID]
	if ownerID == 0 && client != nil {
		ownerID = s.clientOwners[client]
	}
	s.mu.RUnlock()

	if ownerID == 0 && client != nil {
		if self, err := client.Self(ctx); err == nil && self != nil {
			ownerID = self.ID
			s.mu.Lock()
			if chatID != 0 {
				s.owners[chatID] = self.ID
			}
			s.clientOwners[client] = self.ID
			s.mu.Unlock()
		}
	}

	cache := s.peerCache()
	if peerUser, ok := p.(*tg.PeerUser); ok {
		if peerUser.UserID == 0 || (ownerID > 0 && peerUser.UserID == ownerID) {
			return &tg.InputPeerSelf{}, nil
		}
	}

	var api *tg.Client
	if client != nil {
		api = client.API()
	}
	var msgID int
	if msg != nil {
		msgID = msg.ID
	}

	resolved, err := cache.ResolvePeer(ctx, api, ownerID, entities, p, msgID)
	if err == nil {
		s.saveResolvedPeerToSQLite(resolved)
		return resolved, nil
	}

	// Fallback tambahan untuk private chat / channel ketika entitas UpdateShortMessage kosong
	// dan 100 dialog pertama belum memuat chat target (mis. chat lama atau diarsipkan).
	switch peerObj := p.(type) {
	case *tg.PeerUser:
		if s.sqlite != nil {
			if hash, pType, dbErr := s.sqlite.GetPeerAccessHash(ctx, peerObj.UserID); dbErr == nil && hash != 0 && pType != "channel" {
				cache.SaveUser(peerObj.UserID, hash)
				return &tg.InputPeerUser{UserID: peerObj.UserID, AccessHash: hash}, nil
			}
		}
		if api != nil {
			if users, uErr := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: peerObj.UserID}}); uErr == nil {
				cache.ApplyUsersChats(users, nil)
				if hash, ok := cache.UserHash(peerObj.UserID); ok && hash != 0 {
					ip := &tg.InputPeerUser{UserID: peerObj.UserID, AccessHash: hash}
					s.saveResolvedPeerToSQLite(ip)
					return ip, nil
				}
			}
			// Cari bertahap di seluruh dialog (maks 500 dialog) dan folder arsip
			if hash := s.findUserHashInDialogs(ctx, api, cache, peerObj.UserID); hash != 0 {
				ip := &tg.InputPeerUser{UserID: peerObj.UserID, AccessHash: hash}
				s.saveResolvedPeerToSQLite(ip)
				return ip, nil
			}
		}
	case *tg.PeerChannel:
		if s.sqlite != nil {
			if hash, pType, dbErr := s.sqlite.GetPeerAccessHash(ctx, peerObj.ChannelID); dbErr == nil && hash != 0 && pType == "channel" {
				cache.SaveChannel(peerObj.ChannelID, hash)
				return &tg.InputPeerChannel{ChannelID: peerObj.ChannelID, AccessHash: hash}, nil
			}
		}
	}

	return nil, err
}

func (s *Service) saveResolvedPeerToSQLite(p tg.InputPeerClass) {
	if s.sqlite == nil || p == nil {
		return
	}
	switch v := p.(type) {
	case *tg.InputPeerUser:
		if v.UserID > 0 && v.AccessHash != 0 {
			go func(id, hash int64) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = s.sqlite.SavePeer(bgCtx, id, "user", hash)
			}(v.UserID, v.AccessHash)
		}
	case *tg.InputPeerChannel:
		if v.ChannelID > 0 && v.AccessHash != 0 {
			go func(id, hash int64) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = s.sqlite.SavePeer(bgCtx, id, "channel", hash)
			}(v.ChannelID, v.AccessHash)
		}
	}
}

func (s *Service) findUserHashInDialogs(ctx context.Context, api *tg.Client, cache *gobot.PeerStorage, targetUserID int64) int64 {
	if api == nil || targetUserID <= 0 {
		return 0
	}
	errFound := errors.New("peer found")
	errLimit := errors.New("dialog scan limit")
	scanned := 0

	_ = dialogs.NewQueryBuilder(api).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		scanned++
		cache.CacheEntities(tg.Entities{
			Users:    item.Entities.Users(),
			Chats:    item.Entities.Chats(),
			Channels: item.Entities.Channels(),
		})
		if u, ok := item.Peer.(*tg.InputPeerUser); ok && u.UserID == targetUserID && u.AccessHash != 0 {
			cache.SaveUser(u.UserID, u.AccessHash)
			return errFound
		}
		if hash, ok := cache.UserHash(targetUserID); ok && hash != 0 {
			return errFound
		}
		if scanned >= 500 {
			return errLimit
		}
		return nil
	})
	if hash, ok := cache.UserHash(targetUserID); ok && hash != 0 {
		return hash
	}

	if archived, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		FolderID:   1,
		Limit:      100,
		OffsetPeer: &tg.InputPeerEmpty{},
	}); err == nil {
		if mod, ok := archived.AsModified(); ok {
			cache.ApplyUsersChats(mod.GetUsers(), mod.GetChats())
			if hash, ok := cache.UserHash(targetUserID); ok && hash != 0 {
				return hash
			}
		}
	}
	return 0
}

func extractThreadID(msg *tg.Message) int {
	return types.ExtractThreadID(msg)
}

func (s *Service) isRelevantMessage(chatID int64, msg *tg.Message) bool {
	if msg == nil {
		return false
	}
	if _, isUser := msg.PeerID.(*tg.PeerUser); isUser {
		return true
	}
	if msg.Out || msg.Mentioned || msg.ReplyTo != nil {
		return true
	}
	text := msg.Message
	if len(text) > 1 && text[0] == '#' {
		return true
	}

	prefix := "!"
	s.mu.RLock()
	if p, ok := s.prefixes[chatID]; ok {
		prefix = p
	}
	ownerID := s.owners[chatID]
	s.mu.RUnlock()
	if ownerID == 0 && chatID > 0 {
		ownerID = chatID
	}

	if prefix == "" || strings.HasPrefix(text, prefix) {
		return true
	}
	if ownerID != 0 && len(msg.Entities) > 0 && s.IsPluginEnabledForOwner(ownerID, PluginLogger) {
		if val, ok := s.logStatusCache.Load(ownerID); ok {
			if enabled, ok := val.(bool); ok && enabled {
				for _, ent := range msg.Entities {
					switch ent.(type) {
					case *tg.MessageEntityMention, *tg.MessageEntityMentionName:
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Service) handleUserbotMessage(ctx context.Context, chatID int64, client *gobot.Client, entities tg.Entities, update *tg.UpdateNewMessage) error {
	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}
	threadID := extractThreadID(msg)
	prefix := "!"
	s.mu.RLock()
	if p, ok := s.prefixes[chatID]; ok {
		prefix = p
	}
	ownerID := s.owners[chatID]
	s.mu.RUnlock()
	if ownerID == 0 && client != nil {
		s.mu.RLock()
		ownerID = s.clientOwners[client]
		s.mu.RUnlock()
	}
	if ownerID == 0 && chatID > 0 {
		ownerID = chatID
	}

	if strings.HasPrefix(msg.Message, "#") && len(msg.Message) > 1 && !strings.ContainsAny(msg.Message, " \t\r\n") {
		if !s.IsCommandEnabledForOwner(ownerID, "get") {
			return nil
		}
		hashName := strings.TrimPrefix(msg.Message, "#")
		if isValidNoteName(hashName) && s.commandAllowed(ctx, client, msg) {
			peerID, err := s.resolvePeer(ctx, client, chatID, entities, msg.PeerID, msg)
			if err == nil {
				s.markCommandExecuted(chatID, msg)
				return s.showNote(withCommandReplyContext(ctx, msg), client, peerID, hashName, false, threadID)
			}
		}
	}

	if prefix != "" {
		if !strings.HasPrefix(msg.Message, prefix) {
			return nil
		}
	} else {
		if !msg.Out {
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
	if !s.IsCommandEnabledForOwner(ownerID, name) {
		return nil
	}
	if !s.commandAllowed(ctx, client, msg) {
		return nil
	}
	peerID, err := s.resolvePeer(ctx, client, chatID, entities, msg.PeerID, msg)
	if err != nil {
		slog.Error("failed to resolve peer for userbot command",
			slog.String("command", name),
			slog.Int64("chat_id", chatID),
			slog.String("error", err.Error()),
		)
		return err
	}

	ctx = withCommandReplyContext(ctx, msg)
	s.markCommandExecuted(chatID, msg)

	slog.Info("executing userbot command",
		slog.String("command", name),
		slog.Int64("chat_id", chatID),
		slog.Int("thread_id", threadID),
		slog.Bool("out", msg.Out),
	)

	if name == "setp" {
		return s.handleSetPrefix(ctx, client, chatID, peerID, args, threadID)
	}
	if name == "alive" {
		return s.handleAliveCommand(ctx, client, chatID, peerID, threadID)
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
		return s.sudoCommand(ctx, client, peerID, msg, entities, args, name == "addsudo", threadID)
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
	if name == "addblock" || name == "rmblock" {
		return s.handleLogBlockCommand(ctx, client, peerID, msg, entities, args, name == "addblock", threadID)
	}
	if name == "lsblock" {
		return s.handleListLogBlockCommand(ctx, client, peerID, threadID)
	}
	if name == "rmallblock" {
		return s.handleClearLogBlockCommand(ctx, client, peerID, threadID)
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
	if handled, err := s.dispatchExtendedCommand(ctx, client, chatID, peerID, msg, entities, name, args, threadID); handled {
		return err
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
	s.peerCache().CacheEntities(entities)
}
