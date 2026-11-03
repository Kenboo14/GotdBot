package userbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
)

const maxLogMediaSize = 20 * 1024 * 1024 // 20 MB

// ─── Command Handlers ────────────────────────────────────────────────

// handleLogCommand menangani !log on / !log off
func (s *Service) handleLogCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, args string) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal mendapatkan ID pemilik userbot.")
	}

	val := strings.ToLower(strings.TrimSpace(args))
	if val != "on" && val != "off" {
		return sendUserText(ctx, client, target, "Gunakan: <code>!log on</code> atau <code>!log off</code>")
	}

	enabled := val == "on"
	if err := s.sqlite.SetLogStatus(ctx, ownerID, enabled); err != nil {
		return sendUserText(ctx, client, target, "Gagal memperbarui status log: "+err.Error())
	}
	s.logStatusCache.Store(ownerID, enabled)

	if enabled {
		// Pastikan logbot di-unblock dan di-start secara otomatis (hanya sekali per lifecycle)
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = s.ensureLogBotStarted(bgCtx, client, ownerID)
		}()
		return sendUserText(ctx, client, target, "<b>Log Bot:</b> <code>Aktif</code>\nPesan masuk pribadi dan mention/reply di grup akan diteruskan ke Log Bot.")
	}
	return sendUserText(ctx, client, target, "<b>Log Bot:</b> <code>Nonaktif</code>")
}

// handleStatusLogCommand menangani !statuslog
func (s *Service) handleStatusLogCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal mendapatkan ID pemilik userbot.")
	}

	enabled := s.isLogEnabled(ctx, ownerID)
	statusText := "Nonaktif"
	if enabled {
		statusText = "Aktif"
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("<b>Status Log Bot:</b> <code>%s</code>", statusText))
}

// isLogEnabled mengecek apakah log aktif untuk ownerID (cache via sync.Map)
func (s *Service) isLogEnabled(ctx context.Context, ownerID int64) bool {
	if val, ok := s.logStatusCache.Load(ownerID); ok {
		return val.(bool)
	}
	enabled, err := s.sqlite.GetLogStatus(ctx, ownerID)
	if err != nil {
		return false
	}
	s.logStatusCache.Store(ownerID, enabled)
	return enabled
}

// ─── LogBot Info ─────────────────────────────────────────────────────

// resolveLogBotInfo mengambil UserID & Username dari LOGBOT_TOKEN via getMe (cached)
func (s *Service) resolveLogBotInfo(ctx context.Context) (int64, string, error) {
	s.mu.Lock()
	if s.logBotID != 0 && s.logBotUsername != "" {
		id, username := s.logBotID, s.logBotUsername
		s.mu.Unlock()
		return id, username, nil
	}
	s.mu.Unlock()

	token := s.logBotToken
	if strings.TrimSpace(token) == "" {
		token = s.botToken // fallback
	}
	if strings.TrimSpace(token) == "" {
		return 0, "", fmt.Errorf("LOGBOT_TOKEN belum diset")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+token+"/getMe", nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	var answer struct {
		OK     bool `json:"ok"`
		Result struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return 0, "", err
	}
	if !answer.OK || answer.Result.ID == 0 {
		return 0, "", fmt.Errorf("gagal resolve getMe logbot")
	}

	s.mu.Lock()
	s.logBotID = answer.Result.ID
	s.logBotUsername = strings.TrimPrefix(answer.Result.Username, "@")
	id, username := s.logBotID, s.logBotUsername
	s.mu.Unlock()

	return id, username, nil
}

// ensureLogBotStarted memastikan userbot unblock dan /start logbot (sekali per lifecycle)
func (s *Service) ensureLogBotStarted(ctx context.Context, client *telegram.Client, userbotID int64) error {
	logBotID, logBotUsername, err := s.resolveLogBotInfo(ctx)
	if err != nil {
		return err
	}

	mapKey := [2]int64{userbotID, logBotID}
	if _, started := s.startedLogBots.Load(mapKey); started {
		return nil // Sudah pernah di-unblock & di-start pada lifecycle runtime ini
	}

	// 1. Resolve bot peer
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: logBotUsername})
	if err != nil {
		return err
	}
	botPeer, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
	if err != nil {
		return err
	}
	inputUser, ok := botPeer.(*tg.InputPeerUser)
	if !ok {
		return fmt.Errorf("log bot peer bukan User")
	}

	// 2. Unblock bot (jika terblokir)
	_, _ = client.API().ContactsUnblock(ctx, &tg.ContactsUnblockRequest{
		ID: &tg.InputPeerUser{UserID: inputUser.UserID, AccessHash: inputUser.AccessHash},
	})

	// 3. Start bot via MessagesStartBot
	rndID := inlineRandomID()
	_, err = client.API().MessagesStartBot(ctx, &tg.MessagesStartBotRequest{
		Bot:        &tg.InputUser{UserID: inputUser.UserID, AccessHash: inputUser.AccessHash},
		Peer:       &tg.InputPeerUser{UserID: inputUser.UserID, AccessHash: inputUser.AccessHash},
		RandomID:   rndID,
		StartParam: "start",
	})
	if err != nil {
		log.Printf("[LogBot] StartBot warning: %v", err)
	}

	s.startedLogBots.Store(mapKey, true)
	return nil
}

// ─── Core Log Flow ───────────────────────────────────────────────────

// interceptLogMessage mengecek pesan masuk dan mem-forward ke logbot jika perlu
func (s *Service) interceptLogMessage(ctx context.Context, chatID int64, client *telegram.Client, entities tg.Entities, msg *tg.Message) {
	if msg == nil {
		return
	}

	ownerID := s.owners[chatID]
	var ownerUsername string
	if val, ok := s.ownerUsernames.Load(ownerID); ok {
		ownerUsername = val.(string)
	}

	if ownerID == 0 || ownerUsername == "" {
		self, err := client.Self(ctx)
		if err == nil && self != nil {
			ownerID = self.ID
			s.mu.Lock()
			s.owners[chatID] = ownerID
			s.mu.Unlock()
			if self.Username != "" {
				ownerUsername = strings.ToLower(self.Username)
				s.ownerUsernames.Store(ownerID, ownerUsername)
			}
		}
	}
	if ownerID == 0 {
		return
	}

	logBotID, _, _ := s.resolveLogBotInfo(ctx)
	if logBotID == 0 {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		logBotID, _, _ = s.resolveLogBotInfo(bgCtx)
		cancel()
	}

	// Kasus: User membalas pesan logbot di chat private -> balas ke chat sumber
	if msg.Out {
		if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && logBotID != 0 && peerUser.UserID == logBotID {
			log.Printf("[LogBot] Terdeteksi pesan keluar ke LogBot (ID %d): msgID=%d text=%q", logBotID, msg.ID, msg.Message)
			if rh, hasReply := msg.GetReplyTo(); hasReply {
				if rHeader, ok := rh.(*tg.MessageReplyHeader); ok && rHeader.ReplyToMsgID > 0 {
					go func(replyToID int, replyText string) {
						bgCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
						defer cancel()
						s.replyToLoggedMessage(bgCtx, client, ownerID, logBotID, replyToID, replyText)
					}(rHeader.ReplyToMsgID, msg.Message)
				}
			}
		}
		return // Semua pesan keluar (outgoing) tidak di-log
	}

	// Cek status aktif fitur log
	if !s.isLogEnabled(ctx, ownerID) {
		return
	}

	// Identifikasi tipe chat
	chatType := "Private"
	chatTitle := ""
	var sourceChatID int64
	isPrivate := false

	switch p := msg.PeerID.(type) {
	case *tg.PeerUser:
		isPrivate = true
		sourceChatID = p.UserID
		// Jangan log pesan dari log bot itu sendiri
		if logBotID != 0 && p.UserID == logBotID {
			return
		}
	case *tg.PeerChat:
		chatType = "Group"
		sourceChatID = p.ChatID
		for _, c := range entities.Chats {
			if c.ID == p.ChatID {
				chatTitle = c.Title
				break
			}
		}
	case *tg.PeerChannel:
		chatType = "Group"
		sourceChatID = p.ChannelID
		for _, ch := range entities.Channels {
			if ch.ID == p.ChannelID {
				chatTitle = ch.Title
				if ch.Broadcast {
					chatType = "Channel"
				}
				break
			}
		}
	default:
		return
	}

	// Evaluasi apakah pesan harus di-log:
	// 1. Private Chat (PM) -> Selalu di-log
	// 2. Grup / Channel -> HANYA jika pesan me-reply userbot ATAU me-mention userbot
	shouldLog := false

	if isPrivate {
		shouldLog = true
	} else {
		// A. Cek apakah me-reply userbot
		if rh, ok := msg.GetReplyTo(); ok {
			if rHeader, ok := rh.(*tg.MessageReplyHeader); ok {
				if rHeader.ReplyToPeerID != nil {
					if rUser, ok := rHeader.ReplyToPeerID.(*tg.PeerUser); ok && rUser.UserID == ownerID {
						shouldLog = true
					}
				}
				if !shouldLog {
					if fwd, fwdOk := rHeader.GetReplyFrom(); fwdOk && fwd.FromID != nil {
						if rUser, ok := fwd.FromID.(*tg.PeerUser); ok && rUser.UserID == ownerID {
							shouldLog = true
						}
					}
				}
				if !shouldLog && rHeader.ReplyToMsgID > 0 {
					targetPeer, peerErr := peer.EntitiesFromUpdate(entities).ExtractPeer(msg.PeerID)
					if peerErr == nil {
						origMsg, origErr := s.replyMessage(ctx, client, msg, targetPeer)
						if origErr == nil && origMsg != nil {
							if origFrom, ok := origMsg.FromID.(*tg.PeerUser); ok && origFrom.UserID == ownerID {
								shouldLog = true
							}
						}
					}
				}
			}
		}

		// B. Cek apakah me-mention userbot
		if !shouldLog {
			for _, ent := range msg.Entities {
				switch e := ent.(type) {
				case *tg.MessageEntityMentionName:
					if e.UserID == ownerID {
						shouldLog = true
					}
				case *tg.MessageEntityMention:
					if ownerUsername != "" {
						msgRunes := []rune(msg.Message)
						if e.Offset >= 0 && e.Offset+e.Length <= len(msgRunes) {
							mentionStr := strings.ToLower(string(msgRunes[e.Offset : e.Offset+e.Length]))
							if strings.TrimPrefix(mentionStr, "@") == ownerUsername {
								shouldLog = true
							}
						}
					}
				}
				if shouldLog {
					break
				}
			}
		}
	}

	if !shouldLog {
		return
	}

	// Salin data dan jalankan forwarding secara async
	msgCopy := *msg
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.forwardLogs(bgCtx, client, ownerID, entities, &msgCopy, chatType, chatTitle, sourceChatID)
	}()
}

// forwardLogs memformat dan mengirim log (teks + media) ke pemilik via Bot API
func (s *Service) forwardLogs(ctx context.Context, client *telegram.Client, ownerID int64, entities tg.Entities, msg *tg.Message, chatType, chatTitle string, sourceChatID int64) {
	logBotID, _, err := s.resolveLogBotInfo(ctx)
	if err != nil {
		log.Printf("[LogBot] resolveLogBotInfo gagal: %v", err)
		return
	}

	// Pastikan bot sudah di-unblock & di-start
	_ = s.ensureLogBotStarted(ctx, client, ownerID)

	// Ekstrak info pengirim
	senderName, senderUsername, senderID := logExtractSenderInfo(msg, entities)

	// Tentukan peer type & access hash untuk log_routes (dipakai saat reply-to-log)
	peerType := "user"
	var peerAccessHash int64
	switch p := msg.PeerID.(type) {
	case *tg.PeerUser:
		peerType = "user"
		if val, ok := s.userAccessHashes.Load(p.UserID); ok {
			peerAccessHash = val.(int64)
		}
		if peerAccessHash == 0 {
			for _, u := range entities.Users {
				if u.ID == p.UserID {
					peerAccessHash = u.AccessHash
					break
				}
			}
		}
	case *tg.PeerChat:
		peerType = "chat"
	case *tg.PeerChannel:
		peerType = "channel"
		if val, ok := s.channelAccessHashes.Load(p.ChannelID); ok {
			peerAccessHash = val.(int64)
		}
		if peerAccessHash == 0 {
			for _, ch := range entities.Channels {
				if ch.ID == p.ChannelID {
					peerAccessHash = ch.AccessHash
					break
				}
			}
		}
	}

	// Deteksi media
	mInfo := logExtractMediaInfo(msg)

	// Konten teks (pesan asli atau label media)
	content := msg.Message
	if strings.TrimSpace(content) == "" {
		if mInfo != nil {
			content = "[" + mInfo.label + "]"
		} else {
			content = "[Media / Non-teks]"
		}
	}

	logText := logFormatText(content, senderName, senderUsername, chatType, chatTitle)
	chatURL := logBuildChatURL(msg, senderID, senderUsername, chatType)

	token := s.logBotToken
	if strings.TrimSpace(token) == "" {
		token = s.botToken
	}

	var logMsgID int
	var fileID string
	var mediaSent bool

	// ─── Flow 1: Forward-to-LogBot (Zero Download & Zero Upload) ─────────
	// Userbot memforward pesan ke LogBot via MTProto -> LogBot mengambil file_id
	// -> Disimpan di MongoDB -> Dikirimkan ke userbot owner sebagai media
	if mInfo != nil && mInfo.downloadable {
		var sourcePeer tg.InputPeerClass
		switch p := msg.PeerID.(type) {
		case *tg.PeerUser:
			sourcePeer = &tg.InputPeerUser{UserID: p.UserID, AccessHash: peerAccessHash}
		case *tg.PeerChat:
			sourcePeer = &tg.InputPeerChat{ChatID: p.ChatID}
		case *tg.PeerChannel:
			sourcePeer = &tg.InputPeerChannel{ChannelID: p.ChannelID, AccessHash: peerAccessHash}
		}

		logBotUser, botResolveErr := s.resolveLogBotInputUser(ctx, client)
		if botResolveErr == nil && logBotUser != nil && sourcePeer != nil {
			forwardedID, fwdErr := s.forwardMediaToLogBot(ctx, client, sourcePeer, msg.ID, logBotUser)
			if fwdErr == nil && forwardedID > 0 {
				// Ambil file_id dari getUpdates LogBot
				time.Sleep(150 * time.Millisecond)
				fid, _ := s.logFetchFileIDFromBotUpdates(ctx, token, ownerID, forwardedID)
				if fid != "" {
					fileID = fid
				}

				// Kirim media ke owner menggunakan file_id jika tersedia
				if fileID != "" && mInfo.fieldName != "sticker" && mInfo.fieldName != "video_note" {
					mid, sendErr := s.logSendMediaViaFileID(ctx, token, ownerID, fileID, mInfo.apiMethod, mInfo.fieldName, logText, chatURL)
					if sendErr == nil {
						logMsgID = mid
						mediaSent = true
						go s.logDeleteMessageViaBot(context.Background(), token, ownerID, forwardedID)
					}
				}

				// Jika pengiriman via file_id belum berhasil, gunakan copyMessage (zero download juga)
				if !mediaSent {
					mid, copyErr := s.logCopyMessageViaBot(ctx, token, ownerID, ownerID, forwardedID, logText, chatURL)
					if copyErr == nil {
						logMsgID = mid
						mediaSent = true
						go s.logDeleteMessageViaBot(context.Background(), token, ownerID, forwardedID)
					}
				}
			} else {
				log.Printf("[LogBot] Forwarding media ke LogBot gagal: %v, mencoba download fallback", fwdErr)
			}
		}
	}

	// ─── Flow 2: Fallback Download/Upload (hanya jika Forwarding gagal) ────
	if !mediaSent && mInfo != nil && mInfo.downloadable && mInfo.fileSize > 0 && mInfo.fileSize <= maxLogMediaSize {
		data, dlErr := logDownloadMediaFile(ctx, client, msg, maxLogMediaSize)
		if dlErr == nil && len(data) > 0 {
			supportsCaption := mInfo.fieldName != "sticker" && mInfo.fieldName != "video_note"
			if supportsCaption {
				mid, fid, sendErr := s.logSendMediaViaBot(ctx, token, ownerID, data, mInfo.fileName, mInfo.apiMethod, mInfo.fieldName, logText, chatURL)
				if sendErr == nil {
					logMsgID = mid
					if fid != "" {
						fileID = fid
					}
					mediaSent = true
				}
			} else {
				_, fid, _ := s.logSendMediaViaBot(ctx, token, ownerID, data, mInfo.fileName, mInfo.apiMethod, mInfo.fieldName, "", "")
				if fid != "" {
					fileID = fid
				}
				mid, textErr := s.logSendTextViaBot(ctx, token, ownerID, logText, chatURL)
				if textErr == nil {
					logMsgID = mid
					mediaSent = true
				}
			}
		}
	}

	// ─── Flow 3: Fallback ke Teks Biasa ──────────────────────────────────
	if !mediaSent {
		mid, textErr := s.logSendTextViaBot(ctx, token, ownerID, logText, chatURL)
		if textErr != nil {
			log.Printf("[LogBot] Gagal kirim log ke %d: %v", ownerID, textErr)
			return
		}
		logMsgID = mid
	}

	// Simpan routing ke sqlite agar bisa dibalas (reply-to-log)
	if logMsgID > 0 {
		_ = s.sqlite.SaveLogRoute(ctx, logBotID, logMsgID, ownerID, sourceChatID, msg.ID, peerType, peerAccessHash)

		// Backup media ke MongoDB (async)
		if mInfo != nil && (fileID != "" || mInfo.mediaRawID != 0) {
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				s.logSaveMediaMongo(bgCtx, ownerID, logBotID, logMsgID, sourceChatID, msg.ID, mInfo, fileID, msg.Message)
			}()
		}
	}
}

// replyToLoggedMessage membalas pesan sumber saat pemilik membalas log di chat bot
func (s *Service) replyToLoggedMessage(ctx context.Context, client *telegram.Client, ownerID, logBotID int64, replyToID int, text string) {
	log.Printf("[LogBot] Memproses balasan log: logBotID=%d replyToID=%d text=%q", logBotID, replyToID, text)
	route, err := s.sqlite.GetLogRoute(ctx, logBotID, replyToID)
	if err != nil || route == nil || route.UserID != ownerID {
		log.Printf("[LogBot] Route tidak ditemukan di sqlite: botID=%d replyToID=%d ownerID=%d err=%v route=%+v", logBotID, replyToID, ownerID, err, route)
		return
	}
	log.Printf("[LogBot] Route ditemukan: Target=%s ChatID=%d MsgID=%d", route.PeerType, route.SourceChatID, route.SourceMessageID)

	// Resolve target peer berdasarkan peer_type dan access_hash tersimpan
	var targetPeer tg.InputPeerClass
	switch route.PeerType {
	case "channel":
		accessHash := route.AccessHash
		if accessHash == 0 {
			if val, ok := s.channelAccessHashes.Load(route.SourceChatID); ok {
				accessHash = val.(int64)
			}
		}
		if accessHash == 0 {
			// Resolve channel via Dialogs jika belum ada di cache
			dialogs, dErr := client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{Limit: 100})
			if dErr == nil {
				if mod, ok := dialogs.AsModified(); ok {
					for _, c := range mod.GetChats() {
						if ch, ok := c.(*tg.Channel); ok {
							s.channelAccessHashes.Store(ch.ID, ch.AccessHash)
							if ch.ID == route.SourceChatID {
								accessHash = ch.AccessHash
							}
						}
					}
				}
			}
		}
		targetPeer = &tg.InputPeerChannel{ChannelID: route.SourceChatID, AccessHash: accessHash}
	case "chat":
		targetPeer = &tg.InputPeerChat{ChatID: route.SourceChatID}
	default: // "user"
		accessHash := route.AccessHash
		if accessHash == 0 {
			if val, ok := s.userAccessHashes.Load(route.SourceChatID); ok {
				accessHash = val.(int64)
			}
		}
		if accessHash == 0 {
			// Coba resolve via API jika cache kosong
			users, resolveErr := client.API().UsersGetUsers(ctx, []tg.InputUserClass{
				&tg.InputUser{UserID: route.SourceChatID},
			})
			if resolveErr == nil {
				for _, u := range users {
					if user, ok := u.(*tg.User); ok && user.ID == route.SourceChatID {
						accessHash = user.AccessHash
						s.userAccessHashes.Store(user.ID, user.AccessHash)
						break
					}
				}
			}
		}
		targetPeer = &tg.InputPeerUser{UserID: route.SourceChatID, AccessHash: accessHash}
	}

	rndID := inlineRandomID()

	var replyTo tg.InputReplyToClass
	if route.SourceMessageID > 0 {
		replyTo = &tg.InputReplyToMessage{ReplyToMsgID: route.SourceMessageID}
	}

	_, err = client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     targetPeer,
		Message:  text,
		RandomID: rndID,
		ReplyTo:  replyTo,
	})
	if err != nil {
		log.Printf("[LogBot] Gagal mengirim balasan ke %s %d: %v", route.PeerType, route.SourceChatID, err)
		return
	}
	log.Printf("[LogBot] Balasan sukses dikirim ke %s %d (msg %d)", route.PeerType, route.SourceChatID, route.SourceMessageID)
}

// ─── Media Helpers ───────────────────────────────────────────────────

type logMediaInfo struct {
	label         string // display: "Foto", "Video", "GIF", "Sticker", dll
	apiMethod     string // Bot API method: "sendPhoto", "sendVideo", dll
	fieldName     string // multipart field: "photo", "video", dll
	fileName      string // filename untuk upload
	fileSize      int64
	mimeType      string
	mediaRawID    int64
	accessHash    int64
	fileReference []byte
	downloadable  bool
}

func logExtractMediaInfo(msg *tg.Message) *logMediaInfo {
	if msg.Media == nil {
		return nil
	}

	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := media.Photo.(*tg.Photo)
		if !ok || photo == nil {
			return nil
		}
		_, size := logBestPhotoSize(photo)
		return &logMediaInfo{
			label:         "Foto",
			apiMethod:     "sendPhoto",
			fieldName:     "photo",
			fileName:      "photo.jpg",
			fileSize:      size,
			mimeType:      "image/jpeg",
			mediaRawID:    photo.ID,
			accessHash:    photo.AccessHash,
			fileReference: photo.FileReference,
			downloadable:  true,
		}

	case *tg.MessageMediaDocument:
		doc, ok := media.Document.(*tg.Document)
		if !ok || doc == nil {
			return nil
		}
		info := &logMediaInfo{
			label:         "Dokumen",
			apiMethod:     "sendDocument",
			fieldName:     "document",
			fileName:      "file",
			fileSize:      doc.Size,
			mimeType:      doc.MimeType,
			mediaRawID:    doc.ID,
			accessHash:    doc.AccessHash,
			fileReference: doc.FileReference,
			downloadable:  true,
		}
		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeFilename:
				info.fileName = a.FileName
			case *tg.DocumentAttributeVideo:
				if a.RoundMessage {
					info.label = "Video note"
					info.apiMethod = "sendVideoNote"
					info.fieldName = "video_note"
				} else {
					info.label = "Video"
					info.apiMethod = "sendVideo"
					info.fieldName = "video"
				}
			case *tg.DocumentAttributeAnimated:
				info.label = "GIF"
				info.apiMethod = "sendAnimation"
				info.fieldName = "animation"
			case *tg.DocumentAttributeAudio:
				if a.Voice {
					info.label = "Voice"
					info.apiMethod = "sendVoice"
					info.fieldName = "voice"
				} else {
					info.label = "Audio"
					info.apiMethod = "sendAudio"
					info.fieldName = "audio"
				}
			case *tg.DocumentAttributeSticker:
				info.label = "Sticker"
				info.apiMethod = "sendSticker"
				info.fieldName = "sticker"
			}
		}
		return info

	case *tg.MessageMediaContact:
		return &logMediaInfo{label: "Kontak"}
	case *tg.MessageMediaGeo:
		return &logMediaInfo{label: "Lokasi"}
	case *tg.MessageMediaGeoLive:
		return &logMediaInfo{label: "Lokasi"}
	case *tg.MessageMediaVenue:
		return &logMediaInfo{label: "Lokasi"}
	case *tg.MessageMediaPoll:
		return &logMediaInfo{label: "Poll"}
	default:
		return &logMediaInfo{label: "Media"}
	}
}

// logBestPhotoSize memilih ukuran foto terbesar yang tersedia
func logBestPhotoSize(photo *tg.Photo) (thumbSize string, fileSize int64) {
	order := map[string]int{"s": 1, "m": 2, "x": 3, "y": 4, "w": 5}
	bestOrder := 0
	for _, sz := range photo.Sizes {
		switch s := sz.(type) {
		case *tg.PhotoSize:
			if order[s.Type] > bestOrder {
				thumbSize = s.Type
				fileSize = int64(s.Size)
				bestOrder = order[s.Type]
			}
		case *tg.PhotoSizeProgressive:
			totalSize := int64(0)
			if len(s.Sizes) > 0 {
				totalSize = int64(s.Sizes[len(s.Sizes)-1])
			}
			if order[s.Type] > bestOrder {
				thumbSize = s.Type
				fileSize = totalSize
				bestOrder = order[s.Type]
			}
		}
	}
	if thumbSize == "" {
		thumbSize = "x" // fallback
	}
	return
}

// logDownloadMediaFile mengunduh file media dari MTProto (foto atau dokumen)
func logDownloadMediaFile(ctx context.Context, client *telegram.Client, msg *tg.Message, maxSize int64) ([]byte, error) {
	switch media := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := media.Photo.(*tg.Photo)
		if !ok || photo == nil {
			return nil, fmt.Errorf("foto kosong")
		}
		thumbType, fSize := logBestPhotoSize(photo)
		if fSize > maxSize {
			return nil, fmt.Errorf("foto terlalu besar: %d bytes", fSize)
		}
		location := &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbType,
		}
		return logDownloadChunks(ctx, client, location, fSize)

	case *tg.MessageMediaDocument:
		doc, ok := media.Document.(*tg.Document)
		if !ok || doc == nil {
			return nil, fmt.Errorf("dokumen kosong")
		}
		if doc.Size > maxSize {
			return nil, fmt.Errorf("dokumen terlalu besar: %d bytes", doc.Size)
		}
		location := &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}
		return logDownloadChunks(ctx, client, location, doc.Size)

	default:
		return nil, fmt.Errorf("tipe media tidak didukung untuk download")
	}
}

// logDownloadChunks mengunduh file dari MTProto secara chunked
func logDownloadChunks(ctx context.Context, client *telegram.Client, location tg.InputFileLocationClass, expectedSize int64) ([]byte, error) {
	var buf bytes.Buffer
	if expectedSize > 0 {
		buf.Grow(int(expectedSize))
	}
	offset := int64(0)
	limit := 1024 * 1024 // 1 MB chunks
	for {
		res, err := client.API().UploadGetFile(ctx, &tg.UploadGetFileRequest{
			Location: location,
			Offset:   offset,
			Limit:    limit,
		})
		if err != nil {
			return nil, fmt.Errorf("UploadGetFile offset=%d: %w", offset, err)
		}
		switch f := res.(type) {
		case *tg.UploadFile:
			buf.Write(f.Bytes)
			offset += int64(len(f.Bytes))
			if len(f.Bytes) < limit {
				return buf.Bytes(), nil
			}
		default:
			return buf.Bytes(), nil
		}
	}
}

// ─── Bot API Helpers ─────────────────────────────────────────────────

// resolveLogBotInputUser mengembalikan *tg.InputPeerUser untuk LogBot
func (s *Service) resolveLogBotInputUser(ctx context.Context, client *telegram.Client) (*tg.InputPeerUser, error) {
	_, logBotUsername, err := s.resolveLogBotInfo(ctx)
	if err != nil {
		return nil, err
	}
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: logBotUsername})
	if err != nil {
		return nil, err
	}
	botPeer, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
	if err != nil {
		return nil, err
	}
	inputUser, ok := botPeer.(*tg.InputPeerUser)
	if !ok {
		return nil, fmt.Errorf("peer bukan User")
	}
	return inputUser, nil
}

// forwardMediaToLogBot mem-forward pesan media ke LogBot via MTProto (zero download & zero upload)
func (s *Service) forwardMediaToLogBot(ctx context.Context, client *telegram.Client, sourcePeer tg.InputPeerClass, msgID int, logBotUser *tg.InputPeerUser) (int, error) {
	rndID := inlineRandomID()
	updates, err := client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: sourcePeer,
		ID:       []int{msgID},
		ToPeer:   logBotUser,
		RandomID: []int64{rndID},
	})
	if err != nil {
		return 0, err
	}

	var forwardedID int
	switch u := updates.(type) {
	case *tg.Updates:
		for _, upd := range u.Updates {
			if nm, ok := upd.(*tg.UpdateNewMessage); ok {
				if m, ok := nm.Message.(*tg.Message); ok {
					forwardedID = m.ID
					break
				}
			}
		}
	case *tg.UpdatesCombined:
		for _, upd := range u.Updates {
			if nm, ok := upd.(*tg.UpdateNewMessage); ok {
				if m, ok := nm.Message.(*tg.Message); ok {
					forwardedID = m.ID
					break
				}
			}
		}
	case *tg.UpdateShortSentMessage:
		forwardedID = u.ID
	}
	return forwardedID, nil
}

// logFetchFileIDFromBotUpdates mengambil file_id dari update terbaru LogBot
func (s *Service) logFetchFileIDFromBotUpdates(ctx context.Context, token string, ownerID int64, targetMsgID int) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?limit=20", token), nil)
	if err != nil {
		return "", ""
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	var res struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int                    `json:"update_id"`
			Message  map[string]interface{} `json:"message"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || !res.OK {
		return "", ""
	}

	for i := len(res.Result) - 1; i >= 0; i-- {
		upd := res.Result[i]
		if upd.Message == nil {
			continue
		}
		from, _ := upd.Message["from"].(map[string]interface{})
		if from != nil {
			fromID, _ := from["id"].(float64)
			if int64(fromID) == ownerID {
				for _, field := range []string{"photo", "video", "document", "voice", "audio", "animation", "sticker", "video_note"} {
					if fid := logExtractFileID(upd.Message, field); fid != "" {
						return fid, field
					}
				}
			}
		}
	}
	return "", ""
}

// logCopyMessageViaBot menyalin pesan media di chat bot dengan caption dan inline keyboard baru
func (s *Service) logCopyMessageViaBot(ctx context.Context, token string, toChatID, fromChatID int64, messageID int, caption, chatURL string) (int, error) {
	payload := map[string]interface{}{
		"chat_id":      toChatID,
		"from_chat_id": fromChatID,
		"message_id":   messageID,
		"caption":      caption,
		"parse_mode":   "HTML",
	}
	if strings.HasPrefix(chatURL, "https://") || strings.HasPrefix(chatURL, "http://") {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "Buka Chat", "url": chatURL},
				},
			},
		}
	}

	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/copyMessage", bytes.NewReader(pBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("copyMessage HTTP error: %w", err)
	}
	defer resp.Body.Close()

	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("decode copyMessage response: %w", err)
	}
	if !res.OK {
		return 0, fmt.Errorf("copyMessage API error: %s", res.Description)
	}
	return res.Result.MessageID, nil
}

// logDeleteMessageViaBot menghapus pesan sementara di chat bot
func (s *Service) logDeleteMessageViaBot(ctx context.Context, token string, chatID int64, messageID int) {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/deleteMessage", bytes.NewReader(pBytes))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if resp, err := sharedHTTPClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}

// logSendMediaViaFileID mengirim media menggunakan file_id langsung tanpa download/upload
func (s *Service) logSendMediaViaFileID(ctx context.Context, token string, ownerID int64, fileID, apiMethod, fieldName, caption, chatURL string) (int, error) {
	payload := map[string]interface{}{
		"chat_id":    ownerID,
		fieldName:    fileID,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	if strings.HasPrefix(chatURL, "https://") || strings.HasPrefix(chatURL, "http://") {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "Buka Chat", "url": chatURL},
				},
			},
		}
	}
	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/"+apiMethod, bytes.NewReader(pBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s HTTP error: %w", apiMethod, err)
	}
	defer resp.Body.Close()

	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("decode %s response: %w", apiMethod, err)
	}
	if !res.OK {
		return 0, fmt.Errorf("%s API error: %s", apiMethod, res.Description)
	}
	return res.Result.MessageID, nil
}

// logSendTextViaBot mengirim pesan teks log ke pemilik via Bot API sendMessage
func (s *Service) logSendTextViaBot(ctx context.Context, token string, ownerID int64, text, chatURL string) (int, error) {
	payload := map[string]interface{}{
		"chat_id":                  ownerID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if chatURL != "" {
		payload["reply_markup"] = map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "Buka Chat", "url": chatURL},
				},
			},
		}
	}

	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(pBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("sendMessage HTTP error: %w", err)
	}
	defer resp.Body.Close()

	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("decode sendMessage response: %w", err)
	}
	if !res.OK {
		return 0, fmt.Errorf("sendMessage API error: %s", res.Description)
	}
	return res.Result.MessageID, nil
}

// logSendMediaViaBot mengirim media log ke pemilik via Bot API multipart upload
func (s *Service) logSendMediaViaBot(ctx context.Context, token string, ownerID int64, data []byte, fileName, apiMethod, fieldName, caption, chatURL string) (msgID int, fileID string, err error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	_ = w.WriteField("chat_id", fmt.Sprintf("%d", ownerID))
	if caption != "" {
		_ = w.WriteField("caption", caption)
		_ = w.WriteField("parse_mode", "HTML")
	}

	if chatURL != "" {
		markup, _ := json.Marshal(map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "Buka Chat", "url": chatURL},
				},
			},
		})
		_ = w.WriteField("reply_markup", string(markup))
	}

	part, partErr := w.CreateFormFile(fieldName, fileName)
	if partErr != nil {
		return 0, "", partErr
	}
	if _, writeErr := part.Write(data); writeErr != nil {
		return 0, "", writeErr
	}
	if closeErr := w.Close(); closeErr != nil {
		return 0, "", closeErr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/"+apiMethod, &body)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%s HTTP error: %w", apiMethod, err)
	}
	defer resp.Body.Close()

	var raw struct {
		OK          bool                   `json:"ok"`
		Description string                 `json:"description"`
		Result      map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return 0, "", fmt.Errorf("decode %s response: %w", apiMethod, err)
	}
	if !raw.OK {
		return 0, "", fmt.Errorf("%s API error: %s", apiMethod, raw.Description)
	}

	if mid, ok := raw.Result["message_id"].(float64); ok {
		msgID = int(mid)
	}

	// Ekstrak file_id dari response
	fileID = logExtractFileID(raw.Result, fieldName)

	return msgID, fileID, nil
}

// logExtractFileID mengekstrak file_id dari response Bot API
func logExtractFileID(result map[string]interface{}, fieldName string) string {
	mediaField := result[fieldName]
	if mediaField == nil {
		return ""
	}

	// Photo: array of PhotoSize, ambil yang terakhir (terbesar)
	if arr, ok := mediaField.([]interface{}); ok && len(arr) > 0 {
		last := arr[len(arr)-1]
		if m, ok := last.(map[string]interface{}); ok {
			if fid, ok := m["file_id"].(string); ok {
				return fid
			}
		}
		return ""
	}

	// Media lain: object langsung
	if m, ok := mediaField.(map[string]interface{}); ok {
		if fid, ok := m["file_id"].(string); ok {
			return fid
		}
	}

	return ""
}

// ─── Formatting Helpers ──────────────────────────────────────────────

// logExtractSenderInfo mengekstrak nama, username, dan ID pengirim dari entities
func logExtractSenderInfo(msg *tg.Message, entities tg.Entities) (name, username string, id int64) {
	name = "Pengguna tidak dikenal"

	// Cek FromID (pengirim asli)
	if fromUser, ok := msg.FromID.(*tg.PeerUser); ok {
		id = fromUser.UserID
	} else if fromChannel, ok := msg.FromID.(*tg.PeerChannel); ok {
		// Pesan dikirim atas nama channel
		id = fromChannel.ChannelID
		for _, ch := range entities.Channels {
			if ch.ID == id {
				name = ch.Title
				username = ch.Username
				return
			}
		}
		name = fmt.Sprintf("Channel #%d", id)
		return
	} else if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok {
		// Fallback untuk PM tanpa FromID
		id = peerUser.UserID
	}

	if id == 0 {
		return
	}

	for _, u := range entities.Users {
		if u.ID == id {
			first := strings.TrimSpace(u.FirstName)
			last := strings.TrimSpace(u.LastName)
			name = strings.TrimSpace(first + " " + last)
			if name == "" {
				name = u.Username
			}
			if name == "" {
				name = fmt.Sprintf("User #%d", id)
			}
			username = u.Username
			return
		}
	}

	name = fmt.Sprintf("User #%d", id)
	return
}

// logFormatText memformat teks log HTML (blockquote + sender + chat info + note)
func logFormatText(content, senderName, senderUsername, chatType, chatTitle string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<blockquote expandable><code>%s</code></blockquote>\n\n", html.EscapeString(content)))
	sb.WriteString(fmt.Sprintf("<b>Dari :</b> <code>%s</code>", html.EscapeString(senderName)))
	if senderUsername != "" {
		safe := html.EscapeString(senderUsername)
		sb.WriteString(fmt.Sprintf(` (<a href="https://t.me/%s">@%s</a>)`, safe, safe))
	}
	if chatTitle != "" {
		sb.WriteString(fmt.Sprintf("\n<b>Chat :</b> <code>%s</code>", html.EscapeString(chatTitle)))
	} else {
		sb.WriteString("\n<b>Chat :</b> <code>Private</code>")
	}
	sb.WriteString(fmt.Sprintf("\n<b>Tipe :</b> <code>%s</code>\n\n", html.EscapeString(chatType)))
	sb.WriteString(`<pre language="Note:">Balas log untuk membalas pesan tanpa membuka pesan.</pre>`)
	return sb.String()
}

// logBuildChatURL membuat URL deep link ke chat sumber
func logBuildChatURL(msg *tg.Message, senderID int64, senderUsername, chatType string) string {
	if chatType == "Group" || chatType == "Channel" {
		if ch, ok := msg.PeerID.(*tg.PeerChannel); ok {
			return fmt.Sprintf("https://t.me/c/%d/%d", ch.ChannelID, msg.ID)
		}
	}
	if senderUsername != "" {
		return "https://t.me/" + senderUsername
	}
	return fmt.Sprintf("tg://user?id=%d", senderID)
}

// ─── MongoDB Backup ──────────────────────────────────────────────────

// logSaveMediaMongo menyimpan metadata media MTProto + file_id ke MongoDB untuk backup
func (s *Service) logSaveMediaMongo(ctx context.Context, ownerID, logBotID int64, logMsgID int, sourceChatID int64, sourceMsgID int, mInfo *logMediaInfo, fileID, caption string) {
	if s.db == nil || mInfo == nil {
		return
	}
	col := s.db.Collection("userbot_log_media")
	doc := bson.M{
		"owner_id":          ownerID,
		"log_bot_id":        logBotID,
		"log_message_id":    logMsgID,
		"source_chat_id":    sourceChatID,
		"source_message_id": sourceMsgID,
		"media_type":        mInfo.label,
		"file_id":           fileID, // Telegram Bot API file_id jika ada
		"raw_id":            mInfo.mediaRawID,
		"access_hash":       mInfo.accessHash,
		"file_reference":    base64.StdEncoding.EncodeToString(mInfo.fileReference),
		"file_name":         mInfo.fileName,
		"mime_type":         mInfo.mimeType,
		"file_size":         mInfo.fileSize,
		"caption":           caption,
		"created_at":        time.Now(),
	}
	_, err := col.InsertOne(ctx, doc)
	if err != nil {
		log.Printf("[LogBot] Gagal simpan media ke MongoDB: %v", err)
	}
}
