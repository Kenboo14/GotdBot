package userbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

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
		// Pastikan logbot di-unblock dan di-start secara otomatis (hanya sekali jika belum)
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = s.ensureLogBotStarted(bgCtx, client, ownerID)
		}()
		return sendUserText(ctx, client, target, "<b>Log Bot:</b> <code>Aktif</code>\nPesan pribadi masuk akan diteruskan ke Log Bot.")
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

// isLogEnabled mengecek apakah log aktif untuk ownerID (memanfaatkan sync.Map cache)
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
		token = s.botToken // fallback jika logbot tidak diset khusus
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

// ensureLogBotStarted memastikan userbot unblock dan /start logbot (dicegah berjalan berulang via sync.Map)
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

	// 2. Unblock bot (jika sebelumnya terblokir)
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

// interceptLogMessage mengecek pesan masuk dan memicu forward log jika PM atau reply ke LogBot
func (s *Service) interceptLogMessage(ctx context.Context, chatID int64, client *telegram.Client, entities tg.Entities, msg *tg.Message) {
	if msg == nil {
		return
	}

	ownerID := s.owners[chatID]
	if ownerID == 0 {
		self, err := client.Self(ctx)
		if err == nil && self != nil {
			ownerID = self.ID
			s.mu.Lock()
			s.owners[chatID] = ownerID
			s.mu.Unlock()
		}
	}
	if ownerID == 0 {
		return
	}

	logBotID, _, _ := s.resolveLogBotInfo(ctx)

	// Kasus 1: User membalas pesan logbot di chat private bot log -> Balas ke chat sumber
	if msg.Out {
		if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && logBotID != 0 && peerUser.UserID == logBotID {
			if rh, hasReply := msg.GetReplyTo(); hasReply {
				if rHeader, ok := rh.(*tg.MessageReplyHeader); ok && rHeader.ReplyToMsgID > 0 {
					go func(replyToID int, replyText string) {
						bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						s.replyToLoggedMessage(bgCtx, client, ownerID, logBotID, replyToID, replyText)
					}(rHeader.ReplyToMsgID, msg.Message)
				}
			}
		}
		return
	}

	// Kasus 2: Pesan masuk dari orang lain di Private Chat (PM) -> Forward ke LogBot
	peerUser, isPrivate := msg.PeerID.(*tg.PeerUser)
	if !isPrivate || peerUser == nil {
		return // Hanya tangani pesan Private Message (PM)
	}

	// Jangan log pesan dari log bot itu sendiri
	if logBotID != 0 && peerUser.UserID == logBotID {
		return
	}

	// Cek status aktif fitur log
	if !s.isLogEnabled(ctx, ownerID) {
		return
	}

	// Salin data dan jalankan forwarding secara async tanpa memblokir dispatcher
	msgCopy := *msg
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.forwardLogs(bgCtx, client, ownerID, entities, &msgCopy)
	}()
}

// forwardLogs memformat dan mengirim log ke private chat pemilik via bot log Telegram
func (s *Service) forwardLogs(ctx context.Context, client *telegram.Client, ownerID int64, entities tg.Entities, msg *tg.Message) {
	logBotID, _, err := s.resolveLogBotInfo(ctx)
	if err != nil {
		return
	}

	// Pastikan bot sudah di-unblock & di-start (di-cache per lifecycle)
	_ = s.ensureLogBotStarted(ctx, client, ownerID)

	senderName := "Pengguna tidak dikenal"
	senderUsername := ""
	var senderID int64

	if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok {
		senderID = peerUser.UserID
		for _, u := range entities.Users {
			if u.ID == peerUser.UserID {
				senderName = strings.TrimSpace(u.FirstName + " " + u.LastName)
				if senderName == "" {
					senderName = u.Username
				}
				senderUsername = u.Username
				break
			}
		}
	}
	if senderName == "" {
		senderName = fmt.Sprintf("User #%d", senderID)
	}

	content := msg.Message
	if strings.TrimSpace(content) == "" {
		content = "[Media / Non-teks]"
	}

	// Format text persis referensi python:
	// <blockquote><code>preview</code></blockquote>
	// <b>Dari :</b> <code>sender_name</code> (@username)
	// <b>Chat :</b> <code>Private</code>
	// <b>Tipe :</b> <code>Private</code>
	// <pre language="Note:">Balas log untuk membalas pesan tanpa membuka pesan.</pre>
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<blockquote expandable><code>%s</code></blockquote>\n\n", html.EscapeString(content)))
	sb.WriteString(fmt.Sprintf("<b>Dari :</b> <code>%s</code>", html.EscapeString(senderName)))
	if senderUsername != "" {
		safeUser := html.EscapeString(senderUsername)
		sb.WriteString(fmt.Sprintf(" (<a href=\"https://t.me/%s\">@%s</a>)", safeUser, safeUser))
	}
	sb.WriteString("\n<b>Chat :</b> <code>Private</code>\n")
	sb.WriteString("<b>Tipe :</b> <code>Private</code>\n\n")
	sb.WriteString("<pre language=\"Note:\">Balas log untuk membalas pesan tanpa membuka pesan.</pre>")

	chatURL := fmt.Sprintf("tg://user?id=%d", senderID)
	if senderUsername != "" {
		chatURL = "https://t.me/" + senderUsername
	}

	token := s.logBotToken
	if strings.TrimSpace(token) == "" {
		token = s.botToken
	}

	payload := map[string]interface{}{
		"chat_id":                  ownerID,
		"text":                     sb.String(),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"reply_markup": map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "Buka Chat", "url": chatURL},
				},
			},
		},
	}

	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(pBytes))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		log.Printf("[LogBot] Gagal kirim pesan log ke %d: %v", ownerID, err)
		return
	}
	defer resp.Body.Close()

	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.OK && res.Result.MessageID > 0 {
		// Simpan routing ke sqlite agar bisa dibalas (reply-to-log)
		_ = s.sqlite.SaveLogRoute(ctx, logBotID, res.Result.MessageID, ownerID, senderID, msg.ID)
	}
}

// replyToLoggedMessage membalas pesan sumber saat pemilik membalas log di chat bot
func (s *Service) replyToLoggedMessage(ctx context.Context, client *telegram.Client, ownerID, logBotID int64, replyToID int, text string) {
	route, err := s.sqlite.GetLogRoute(ctx, logBotID, replyToID)
	if err != nil || route == nil || route.UserID != ownerID {
		return
	}

	targetPeer := &tg.InputPeerUser{UserID: route.SourceChatID}
	rndID := inlineRandomID()

	// Kirim pesan balasan dari akun userbot ke target chat dengan reply_to_msg_id asli
	_, err = client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     targetPeer,
		Message:  text,
		RandomID: rndID,
		ReplyTo:  &tg.InputReplyToMessage{ReplyToMsgID: route.SourceMessageID},
	})
	if err != nil {
		log.Printf("[LogBot] Gagal mengirim balasan ke sumber (%d): %v", route.SourceChatID, err)
		return
	}
	log.Printf("[LogBot] Balasan sukses dikirim ke chat %d (reply to %d)", route.SourceChatID, route.SourceMessageID)
}
