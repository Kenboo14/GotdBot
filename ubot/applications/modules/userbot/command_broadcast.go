package userbot

import (
	"assterisk/ubot/applications/models"
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
)

type broadcastTarget struct {
	Peer tg.InputPeerClass
	Kind string
	ID   int64
	Name string
}

func (s *Service) broadcastCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, args string) error {
	parts := strings.Fields(args)
	if len(parts) > 0 && (strings.EqualFold(parts[0], "cancel") || strings.EqualFold(parts[0], "stop")) {
		ownerID, err := s.userbotOwner(ctx, client)
		if err != nil {
			return err
		}
		s.mu.Lock()
		cancel := s.broadcasts[ownerID]
		s.mu.Unlock()
		if cancel == nil {
			return sendUserText(ctx, client, target, "Tidak ada broadcast yang berjalan.")
		}
		cancel()
		return sendUserText(ctx, client, target, "Permintaan pembatalan broadcast diterima.")
	}
	if len(parts) == 0 {
		return sendUserText(ctx, client, target, "Gunakan: !gcast -gc|-pc|-ch [-bl] <pesan>")
	}
	types := map[string]bool{}
	excludeBlacklist := false
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "-gc":
			types["group"] = true
		case "-pc":
			types["private"] = true
		case "-ch":
			types["channel"] = true
		case "-bl":
			excludeBlacklist = true
		default:
			textParts = append(textParts, part)
		}
	}
	if len(types) == 0 {
		return sendUserText(ctx, client, target, "Flag target wajib diisi. Contoh: !gcast -gc -bl Halo grup.")
	}
	text := strings.Join(textParts, " ")
	var textEntities []tg.MessageEntityClass
	replyMessageID := 0
	if text == "" {
		var replyErr error
		text, textEntities, replyErr = repliedText(ctx, client, target, msg)
		if replyErr != nil {
			return sendUserText(ctx, client, target, "Reply pesan teks atau tulis pesan setelah flag.")
		}
		replyMessageID = broadcastReplyMessageID(msg)
	} else if manualText, manualEntities, ok := manualBroadcastText(msg); ok {
		// Keep the exact substring sent by the user. Rebuilding it with
		// strings.Join would discard Telegram's entities and formatting.
		text, textEntities = manualText, manualEntities
	}
	if len([]rune(text)) > 4096 {
		return sendUserText(ctx, client, target, "Pesan broadcast maksimal 4096 karakter.")
	}
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	targets, err := s.broadcastTargets(ctx, client, types, excludeBlacklist, ownerID)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada target yang sesuai.")
	}
	taskID := fmt.Sprintf("%d-%d", ownerID, time.Now().UnixNano())
	// The inline acknowledgement stays in the originating chat. Detailed
	// progress and the final report are always delivered by BOT_TOKEN to the
	// owner's private chat.
	monitorChatID := ownerID
	jobCtx, cancel := context.WithCancel(s.root)
	s.mu.Lock()
	if s.broadcasts[ownerID] != nil {
		s.mu.Unlock()
		cancel()
		return sendUserText(ctx, client, target, "Broadcast akun ini sedang berjalan. Gunakan !gcast cancel.")
	}
	s.broadcasts[ownerID] = cancel
	s.mu.Unlock()
	// Put an inline message into the originating chat so Telegram displays
	// the `via @bot` attribution. The Bot API monitor below remains active as
	// a second, editable status message.
	if err := s.sendInlineQuery(ctx, client, target, fmt.Sprintf("broadcast %d %s %d", ownerID, taskID, len(targets))); err != nil {
		log.Printf("[userbot %d] broadcast inline monitor unavailable: %v", ownerID, err)
		// Some groups disallow inline bots. Keep the acknowledgement visible
		// as a normal userbot message, without a button or via attribution.
		if sendErr := sendUserText(ctx, client, target, fmt.Sprintf("Proses Pengiriman\nCek bot untuk detail broadcast anda\n#AIO:%s", taskID)); sendErr != nil {
			log.Printf("[userbot %d] broadcast fallback acknowledgement failed: %v", ownerID, sendErr)
		}
	}
	if monitorChatID != 0 {
		if err = s.notifyBroadcast(ctx, monitorChatID, ownerID, taskID, "broadcast_start", len(targets), 0, 0, 0, nil); err != nil {
			cancel()
			return err
		}
	} else if _, err = message.NewSender(tg.NewClient(client)).To(target).Text(ctx, fmt.Sprintf("Broadcast dimulai ke %d target.", len(targets))); err != nil {
		cancel()
		return err
	}
	go func() {
		// Batasi concurrent broadcast untuk menjaga CPU dan memory
		s.broadcastSem <- struct{}{}
		defer func() { <-s.broadcastSem }()
		s.runBroadcast(jobCtx, client, ownerID, targets, text, textEntities, replyMessageID, target, monitorChatID, taskID)
	}()
	return nil
}

// manualBroadcastText extracts the message body after leading gcast flags and
// shifts Telegram entities from the complete command to that body. Entity
// offsets are UTF-16 offsets, as required by Telegram's MTProto API.
func manualBroadcastText(msg *tg.Message) (string, []tg.MessageEntityClass, bool) {
	if msg == nil || strings.TrimSpace(msg.Message) == "" {
		return "", nil, false
	}
	raw := msg.Message
	commandEnd := strings.IndexAny(raw, " \t\r\n")
	if commandEnd < 0 {
		return "", nil, false
	}
	start := commandEnd
	for start < len(raw) && isBroadcastSpace(raw[start]) {
		start++
	}
	for start < len(raw) {
		end := start
		for end < len(raw) && !isBroadcastSpace(raw[end]) {
			end++
		}
		token := strings.ToLower(raw[start:end])
		if token != "-gc" && token != "-pc" && token != "-ch" && token != "-bl" {
			break
		}
		start = end
		for start < len(raw) && isBroadcastSpace(raw[start]) {
			start++
		}
	}
	end := len(raw)
	for end > start && isBroadcastSpace(raw[end-1]) {
		end--
	}
	if start >= end {
		return "", nil, false
	}
	text := raw[start:end]
	entities := shiftBroadcastEntities(msg.Entities, utf16Length(raw[:start]), utf16Length(text))
	return text, entities, true
}

func isBroadcastSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

type broadcastEntityRange interface {
	GetOffset() int
	GetLength() int
}

func shiftBroadcastEntities(entities []tg.MessageEntityClass, prefix, bodyLength int) []tg.MessageEntityClass {
	result := make([]tg.MessageEntityClass, 0, len(entities))
	for _, entity := range entities {
		rangeEntity, ok := entity.(broadcastEntityRange)
		if !ok {
			continue
		}
		begin := rangeEntity.GetOffset()
		end := begin + rangeEntity.GetLength()
		bodyEnd := prefix + bodyLength
		if end <= prefix || begin >= bodyEnd {
			continue
		}
		if begin < prefix {
			begin = prefix
		}
		if end > bodyEnd {
			end = bodyEnd
		}
		copyEntity := cloneBroadcastEntity(entity)
		if copyEntity == nil {
			continue
		}
		value := reflect.ValueOf(copyEntity)
		if value.Kind() != reflect.Ptr || value.Elem().Kind() != reflect.Struct {
			continue
		}
		offsetField := value.Elem().FieldByName("Offset")
		lengthField := value.Elem().FieldByName("Length")
		if !offsetField.IsValid() || !lengthField.IsValid() || !offsetField.CanSet() || !lengthField.CanSet() {
			continue
		}
		offsetField.SetInt(int64(begin - prefix))
		lengthField.SetInt(int64(end - begin))
		result = append(result, copyEntity)
	}
	return result
}

func cloneBroadcastEntity(entity tg.MessageEntityClass) tg.MessageEntityClass {
	value := reflect.ValueOf(entity)
	if value.Kind() != reflect.Ptr || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return nil
	}
	copyValue := reflect.New(value.Elem().Type())
	copyValue.Elem().Set(value.Elem())
	copyEntity, ok := copyValue.Interface().(tg.MessageEntityClass)
	if !ok {
		return nil
	}
	return copyEntity
}

func broadcastReplyMessageID(msg *tg.Message) int {
	if msg == nil {
		return 0
	}
	headerValue, ok := msg.GetReplyTo()
	if !ok {
		return 0
	}
	header, ok := headerValue.(*tg.MessageReplyHeader)
	if !ok {
		return 0
	}
	return header.ReplyToMsgID
}

func repliedText(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message) (string, []tg.MessageEntityClass, error) {
	if msg == nil {
		return "", nil, fmt.Errorf("pesan command tidak tersedia")
	}
	headerValue, ok := msg.GetReplyTo()
	header, ok := headerValue.(*tg.MessageReplyHeader)
	if !ok || header.ReplyToMsgID == 0 {
		if ok && strings.TrimSpace(header.QuoteText) != "" {
			return header.QuoteText, header.QuoteEntities, nil
		}
		return "", nil, fmt.Errorf("reply tidak tersedia")
	}
	// Supergroups and channels have a dedicated API method. It returns the
	// canonical channel message, including its original entities.
	if channelPeer, ok := target.(*tg.InputPeerChannel); ok {
		channelResult, channelErr := client.API().ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: channelPeer.ChannelID, AccessHash: channelPeer.AccessHash},
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: header.ReplyToMsgID}},
		})
		if channelErr == nil {
			if messageValue := textMessageFromResult(channelResult, header.ReplyToMsgID, msg.ID); messageValue != nil {
				log.Printf("[broadcast] reply channel message=%d entities=%d", messageValue.ID, len(messageValue.Entities))
				return messageValue.Message, messageValue.Entities, nil
			}
		}
	}
	// Resolve through the concrete peer first. This preserves the complete
	// entity list (including blockquote) for channel/supergroup messages.
	history, historyErr := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer: target, OffsetID: header.ReplyToMsgID + 1, AddOffset: -1, Limit: 1,
	})
	if historyErr == nil {
		if messageValue := textMessageFromResult(history, header.ReplyToMsgID, msg.ID); messageValue != nil {
			log.Printf("[broadcast] reply history message=%d entities=%d", messageValue.ID, len(messageValue.Entities))
			return messageValue.Message, messageValue.Entities, nil
		}
	}
	// inputMessageReplyTo is the fallback for peers where history is limited.
	result, err := client.API().MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageReplyTo{ID: header.ReplyToMsgID}})
	if err == nil {
		if messageValue := textMessageFromResult(result, header.ReplyToMsgID, msg.ID); messageValue != nil {
			log.Printf("[broadcast] reply getMessages message=%d entities=%d", messageValue.ID, len(messageValue.Entities))
			return messageValue.Message, messageValue.Entities, nil
		}
	}
	if strings.TrimSpace(header.QuoteText) != "" {
		return header.QuoteText, header.QuoteEntities, nil
	}
	if err != nil {
		return "", nil, err
	}
	if historyErr != nil {
		return "", nil, historyErr
	}
	return "", nil, fmt.Errorf("reply bukan pesan teks")
}

func textMessageFromResult(result tg.MessagesMessagesClass, expectedID, excludedID int) *tg.Message {
	var items []tg.MessageClass
	switch value := result.(type) {
	case *tg.MessagesMessages:
		items = value.Messages
	case *tg.MessagesMessagesSlice:
		items = value.Messages
	case *tg.MessagesChannelMessages:
		items = value.Messages
	}
	var fallback *tg.Message
	for _, item := range items {
		if messageValue, ok := item.(*tg.Message); ok && strings.TrimSpace(messageValue.Message) != "" {
			if messageValue.ID == expectedID {
				return messageValue
			}
			// Some channel history responses normalize the local ID. Never use
			// the command itself, but allow the other returned text message as
			// the reply fallback.
			if messageValue.ID != excludedID && fallback == nil {
				fallback = messageValue
			}
		}
	}
	return fallback
}

func (s *Service) broadcastTargets(ctx context.Context, client *telegram.Client, types map[string]bool, excludeBlacklist bool, ownerID int64) ([]broadcastTarget, error) {
	blacklisted := map[string]bool{}
	if excludeBlacklist {
		cur, err := s.db.Collection("userbot_broadcast_blacklist").Find(ctx, bson.M{"owner_id": ownerID})
		if err != nil {
			return nil, err
		}
		var rows []blacklistRecord
		if err = cur.All(ctx, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			blacklisted[fmt.Sprintf("%s:%d", row.Type, row.ChatID)] = true
		}
	}
	targets := make([]broadcastTarget, 0)
	seen := map[string]bool{}
	// Iterate every page. A single raw getDialogs request only returns up to
	// 100 dialogs, which previously caused broadcasts to miss older groups.
	err := dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		peer := item.Peer
		entities := tg.Entities{
			Users:    item.Entities.Users(),
			Chats:    item.Entities.Chats(),
			Channels: item.Entities.Channels(),
		}
		kind, id, skip := dialogKind(peer, entities, ownerID)
		legacySupergroupKey := fmt.Sprintf("channel:%d", id)
		if skip || !types[kind] || (blacklisted[fmt.Sprintf("%s:%d", kind, id)] || kind == "group" && blacklisted[legacySupergroupKey]) {
			return nil
		}
		key := fmt.Sprintf("%s:%d", kind, id)
		if !seen[key] {
			seen[key] = true
			targets = append(targets, broadcastTarget{Peer: peer, Kind: kind, ID: id, Name: dialogName(peer, entities, id)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

func dialogKind(peer tg.InputPeerClass, entities tg.Entities, ownerID int64) (string, int64, bool) {
	switch value := peer.(type) {
	case *tg.InputPeerUser:
		user := entities.Users[value.UserID]
		return "private", value.UserID, value.UserID == ownerID || user != nil && user.Bot
	case *tg.InputPeerChat:
		return "group", value.ChatID, false
	case *tg.InputPeerChannel:
		// Telegram represents supergroups as channels with Megagroup=true.
		// They should be selected by -gc; only broadcast channels belong to
		// the separate -ch category.
		if channel := entities.Channels[value.ChannelID]; channel != nil && channel.Megagroup {
			return "group", value.ChannelID, false
		}
		return "channel", value.ChannelID, false
	default:
		return "", 0, true
	}
}

func dialogName(peer tg.InputPeerClass, entities tg.Entities, id int64) string {
	switch value := peer.(type) {
	case *tg.InputPeerUser:
		if user := entities.Users[value.UserID]; user != nil {
			name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
			if name != "" {
				return name
			}
			if user.Username != "" {
				return "@" + user.Username
			}
		}
	case *tg.InputPeerChat:
		if chat := entities.Chats[value.ChatID]; chat != nil && chat.Title != "" {
			return chat.Title
		}
	case *tg.InputPeerChannel:
		if channel := entities.Channels[value.ChannelID]; channel != nil && channel.Title != "" {
			return channel.Title
		}
	}
	return fmt.Sprintf("%d", id)
}

func (s *Service) runBroadcast(ctx context.Context, client *telegram.Client, ownerID int64, targets []broadcastTarget, text string, entities []tg.MessageEntityClass, replyMessageID int, target tg.InputPeerClass, monitorChatID int64, taskID string) {
	success, failed := 0, 0
	lastBucket := 0
	lastProgress := 0
	failures := make([]string, 0)
	for index, item := range targets {
		if ctx.Err() != nil {
			break
		}
		randomID, randomErr := crypto.RandInt64(crypto.DefaultRand())
		if randomErr != nil {
			failed++
			if len(failures) < 100 {
				failures = append(failures, fmt.Sprintf("%s: %s", item.Name, compactBroadcastError(randomErr)))
			}
		} else {
			var sendErr error
			if replyMessageID != 0 && len(entities) == 0 {
				// If Telegram did not return entities, forward the original
				// message as a lossless formatting fallback.
				_, sendErr = client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
					FromPeer: target, ID: []int{replyMessageID}, RandomID: []int64{randomID},
					ToPeer: item.Peer, DropAuthor: true,
				})
			} else {
				_, sendErr = client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{Peer: item.Peer, Message: text, Entities: entities, RandomID: randomID})
			}
			if sendErr != nil {
				failed++
				if len(failures) < 100 {
					failures = append(failures, fmt.Sprintf("%s: %s", item.Name, compactBroadcastError(sendErr)))
				}
			} else {
				success++
			}
		}
		processed := index + 1
		bucket := (processed*5 + len(targets) - 1) / len(targets)
		if bucket > 5 {
			bucket = 5
		}
		if bucket > lastBucket {
			lastBucket = bucket
			lastProgress = (processed*100 + len(targets) - 1) / len(targets)
			if lastProgress > 100 {
				lastProgress = 100
			}
			if monitorChatID != 0 {
				_ = s.notifyBroadcast(context.Background(), monitorChatID, ownerID, taskID, "broadcast_progress", len(targets), lastProgress, success, failed, nil)
			}
		}
		time.Sleep(350 * time.Millisecond)
	}
	if monitorChatID != 0 {
		progress := lastProgress
		if ctx.Err() != nil {
			progress = lastProgress
		} else {
			progress = 100
		}
		_ = s.notifyBroadcast(context.Background(), monitorChatID, ownerID, taskID, "broadcast_done", len(targets), progress, success, failed, failures)
	} else {
		final := fmt.Sprintf("Broadcast selesai: %d berhasil, %d gagal, dari %d target.", success, failed, len(targets))
		if ctx.Err() != nil {
			final = fmt.Sprintf("Broadcast dibatalkan: %d berhasil, %d gagal.", success, failed)
		}
		_, _ = message.NewSender(tg.NewClient(client)).To(target).Text(context.Background(), final)
	}
	s.mu.Lock()
	delete(s.broadcasts, ownerID)
	s.mu.Unlock()
}

func (s *Service) notifyBroadcast(ctx context.Context, chatID, ownerID int64, taskID, kind string, total, progress, success, failed int, failures []string) error {
	return s.redis.To(ctx, "from:ubot", &models.PFromService{
		ChatID: chatID, Type: kind, OwnerID: ownerID, TaskID: taskID,
		Total: total, Progress: progress, Success: success, Failed: failed, Errors: failures,
	})
}

func compactBroadcastError(err error) string {
	text := strings.TrimSpace(err.Error())
	if len([]rune(text)) > 120 {
		return string([]rune(text)[:120]) + "…"
	}
	return text
}
