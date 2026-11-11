package userbot

import (
	"assterisk/ubot/applications/models"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type panelBroadcastJob struct {
	cancel context.CancelFunc
}

func (s *Service) clearPanelData(ctx context.Context, ownerID int64, lists bool) {
	if s.db != nil {
		field := "groups"
		if lists {
			field = "lists"
		}
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{"$set": bson.M{field: []int{}}}, options.Update().SetUpsert(true))
	}
	if s.sqlite != nil {
		field := "groups"
		if lists {
			field = "lists"
		}
		_, _ = s.sqlite.DB.ExecContext(ctx, fmt.Sprintf("UPDATE jaseb_configurasi SET %s = ?, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?", field), "[]", ownerID)
	}
}

func (s *Service) deletePanelList(ctx context.Context, ownerID int64, cacheIDText string) {
	cacheID, err := strconv.Atoi(cacheIDText)
	if err != nil {
		return
	}
	cfg := s.getJasebConfig(ctx, ownerID)
	filtered := make([]int, 0, len(cfg.Lists))
	removed := false
	for _, id := range cfg.Lists {
		if !removed && id == cacheID {
			removed = true
			continue
		}
		filtered = append(filtered, id)
	}
	if !removed {
		return
	}
	if s.db != nil {
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{"$set": bson.M{"lists": filtered, "updated_at": time.Now()}}, options.Update().SetUpsert(true))
	}
	if s.sqlite != nil {
		listsJSON, _ := json.Marshal(filtered)
		_, _ = s.sqlite.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET lists = ?, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?`, string(listsJSON), ownerID)
	}
}

func (s *Service) handleListDetail(ctx context.Context, client *telegram.Client, chatID, ownerID int64, taskID string) {
	index, err := strconv.Atoi(taskID)
	if err != nil || index < 0 {
		s.reply(ctx, chatID, "❌ Nomor list tidak valid.", "panel_notice")
		return
	}
	cfg := s.getJasebConfig(ctx, ownerID)
	if index >= len(cfg.Lists) {
		s.reply(ctx, chatID, "❌ List sudah tidak tersedia. Silakan buka ulang Cek List.", "panel_notice")
		return
	}
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		s.reply(ctx, chatID, "❌ Userbot tidak dapat mengakses cache List.", "panel_notice")
		return
	}
	msgID := cfg.Lists[index]
	history, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: cachePeer, OffsetID: msgID + 1, Limit: 8})
	if err != nil {
		log.Printf("[list-detail] history cache=%d failed: %v", msgID, err)
		s.reply(ctx, chatID, "❌ Detail List tidak dapat diambil dari AiotdCaches.", "panel_notice")
		return
	}
	messages := cacheMessages(history)
	message := findCacheMessage(messages, msgID)
	if message == nil {
		// History pada forum/channel kadang tidak mengembalikan item tepat di
		// boundary offset. Ambil langsung dari peer yang sudah di-resolve.
		var direct tg.MessagesMessagesClass
		if channel, ok := cachePeer.(*tg.InputPeerChannel); ok {
			direct, err = client.API().ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}})
		} else {
			direct, err = client.API().MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}})
		}
		if err == nil {
			message = findCacheMessage(cacheMessages(direct), msgID)
		}
	}
	content := "[Media / Non-teks]"
	if message != nil {
		content = message.Message
		if strings.TrimSpace(content) == "" {
			content = "[Media / Non-teks]"
		}
	} else {
		log.Printf("[list-detail] cache message %d not found in AiotdCaches", msgID)
		s.reply(ctx, chatID, "❌ Detail List tidak ditemukan di AiotdCaches. Silakan buka ulang Cek List.", "panel_notice")
		return
	}
	displayContent := html.EscapeString(content)
	if message != nil && strings.TrimSpace(message.Message) != "" {
		displayContent = convertEntitiesToHTML(message.Message, message.Entities)
	}
	text := fmt.Sprintf("<b>Detail List #%d</b>\n\n%s", index+1, displayContent)
	mediaCacheID := 0
	if message.Media != nil {
		// The Bot API can copy the original cached message, preserving the
		// media, caption, and caption entities without converting them.
		mediaCacheID = msgID
	}
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: chatID, Type: "list_detail", TaskID: taskID, Text: text, MediaCacheID: mediaCacheID})
}

func (s *Service) handleListPreviews(ctx context.Context, client *telegram.Client, chatID, ownerID int64, taskID string) {
	cfg := s.getJasebConfig(ctx, ownerID)
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		s.reply(ctx, chatID, "❌ Userbot tidak dapat mengakses cache List.", "panel_notice")
		return
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "<b>Display List</b>\n\nTotal List tersimpan: <code>%d</code>\n\n", len(cfg.Lists))
	for index, msgID := range cfg.Lists {
		content := "[Media / Non-teks]"
		message, fetchErr := s.fetchCacheMessage(ctx, client, cachePeer, msgID)
		if fetchErr == nil && message != nil && strings.TrimSpace(message.Message) != "" {
			content = message.Message
		}
		words := strings.Fields(content)
		if len(words) > 10 {
			content = strings.Join(words[:10], " ") + "…"
		}
		fmt.Fprintf(&builder, "%d. %s\n", index+1, html.EscapeString(content))
	}
	builder.WriteString("\nPilih angka untuk membuka detail List.")
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: chatID, Type: "list_previews", TaskID: taskID, Text: builder.String()})
}

func (s *Service) fetchCacheMessage(ctx context.Context, client *telegram.Client, cachePeer tg.InputPeerClass, msgID int) (*tg.Message, error) {
	history, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: cachePeer, OffsetID: msgID + 1, Limit: 8})
	if err == nil {
		if message := findCacheMessage(cacheMessages(history), msgID); message != nil {
			return message, nil
		}
	}
	var direct tg.MessagesMessagesClass
	if channel, ok := cachePeer.(*tg.InputPeerChannel); ok {
		direct, err = client.API().ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel.ChannelID, AccessHash: channel.AccessHash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}})
	} else {
		direct, err = client.API().MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}})
	}
	if err != nil {
		return nil, err
	}
	if message := findCacheMessage(cacheMessages(direct), msgID); message != nil {
		return message, nil
	}
	return nil, fmt.Errorf("message %d tidak ditemukan", msgID)
}

func cacheMessages(result tg.MessagesMessagesClass) []tg.MessageClass {
	switch value := result.(type) {
	case *tg.MessagesMessages:
		return value.Messages
	case *tg.MessagesMessagesSlice:
		return value.Messages
	case *tg.MessagesChannelMessages:
		return value.Messages
	default:
		return nil
	}
}

func findCacheMessage(messages []tg.MessageClass, id int) *tg.Message {
	for _, item := range messages {
		if message, ok := item.(*tg.Message); ok && message.ID == id {
			return message
		}
	}
	return nil
}

func toMTProtoEntities(source []models.MessageEntity) []tg.MessageEntityClass {
	result := make([]tg.MessageEntityClass, 0, len(source))
	for _, e := range source {
		rangeEntity := func() (int, int) { return e.Offset, e.Length }
		offset, length := rangeEntity()
		switch e.Type {
		case "mention":
			result = append(result, &tg.MessageEntityMention{Offset: offset, Length: length})
		case "hashtag":
			result = append(result, &tg.MessageEntityHashtag{Offset: offset, Length: length})
		case "cashtag":
			result = append(result, &tg.MessageEntityCashtag{Offset: offset, Length: length})
		case "bot_command":
			result = append(result, &tg.MessageEntityBotCommand{Offset: offset, Length: length})
		case "url":
			result = append(result, &tg.MessageEntityURL{Offset: offset, Length: length})
		case "email":
			result = append(result, &tg.MessageEntityEmail{Offset: offset, Length: length})
		case "phone_number":
			result = append(result, &tg.MessageEntityPhone{Offset: offset, Length: length})
		case "bold":
			result = append(result, &tg.MessageEntityBold{Offset: offset, Length: length})
		case "italic":
			result = append(result, &tg.MessageEntityItalic{Offset: offset, Length: length})
		case "underline":
			result = append(result, &tg.MessageEntityUnderline{Offset: offset, Length: length})
		case "strikethrough":
			result = append(result, &tg.MessageEntityStrike{Offset: offset, Length: length})
		case "spoiler":
			result = append(result, &tg.MessageEntitySpoiler{Offset: offset, Length: length})
		case "code":
			result = append(result, &tg.MessageEntityCode{Offset: offset, Length: length})
		case "pre":
			result = append(result, &tg.MessageEntityPre{Offset: offset, Length: length, Language: e.Language})
		case "text_link":
			result = append(result, &tg.MessageEntityTextURL{Offset: offset, Length: length, URL: e.URL})
		case "text_mention":
			result = append(result, &tg.MessageEntityMentionName{Offset: offset, Length: length, UserID: e.UserID})
		case "blockquote", "expandable_blockquote":
			result = append(result, &tg.MessageEntityBlockquote{Offset: offset, Length: length, Collapsed: e.Type == "expandable_blockquote"})
		case "custom_emoji":
			if id, err := strconv.ParseInt(e.CustomEmojiID, 10, 64); err == nil {
				result = append(result, &tg.MessageEntityCustomEmoji{Offset: offset, Length: length, DocumentID: id})
			}
		}
	}
	return result
}

type jasebGroupDoc struct {
	Target     string `bson:"target" json:"target"`
	Title      string `bson:"title" json:"title"`
	ChatID     int64  `bson:"chat_id" json:"chat_id"`
	AccessHash int64  `bson:"access_hash" json:"access_hash"`
	PeerType   string `bson:"peer_type" json:"peer_type"`
}

type jasebTopicDoc struct {
	ChatID  int64  `bson:"chat_id" json:"chat_id"`
	TopicID int    `bson:"topic_id" json:"topic_id"`
	Title   string `bson:"title" json:"title"`
}

type jasebConfigDoc struct {
	OwnerID          int64           `bson:"_id" json:"owner_id"`
	Type             string          `bson:"type" json:"type"`
	Delay            int             `bson:"delay" json:"delay"`
	SkipFloodWait    bool            `bson:"skip_floodwait" json:"skip_floodwait"`
	Timer            string          `bson:"timer" json:"timer"`
	Extra            int             `bson:"extra" json:"extra"`
	Mode             string          `bson:"mode" json:"mode"`
	Safemode         bool            `bson:"safemode" json:"safemode"`
	Status           string          `bson:"status" json:"status"`
	MasaAktif        string          `bson:"masa_aktif" json:"masa_aktif"`
	Notifikasi       string          `bson:"notifikasi" json:"notifikasi"`
	Email            string          `bson:"email" json:"email"`
	Admin            string          `bson:"admin" json:"admin"`
	Watermark        string          `bson:"watermark" json:"watermark"`
	WatermarkCacheID int             `bson:"watermark_cache_id" json:"watermark_cache_id"`
	TopicEnabled     bool            `bson:"topic_enabled" json:"topic_enabled"`
	TindakanTerkini  string          `bson:"tindakan_terkini" json:"tindakan_terkini"`
	TindakanTerakhir string          `bson:"tindakan_terakhir" json:"tindakan_terakhir"`
	Lists            []int           `bson:"lists" json:"lists"`
	Groups           []jasebGroupDoc `bson:"groups" json:"groups"`
	Topics           []jasebTopicDoc `bson:"topics" json:"topics"`
	BroadcastIndex   int             `bson:"broadcast_index" json:"broadcast_index"`
	BroadcastNextAt  int64           `bson:"broadcast_next_at" json:"broadcast_next_at"`
	UpdatedAt        time.Time       `bson:"updated_at" json:"updated_at"`
}

func (s *Service) syncAddListToSQLite(ctx context.Context, ownerID int64, cacheMsgID int) {
	if s.sqlite == nil {
		return
	}
	cfg := s.getJasebConfig(ctx, ownerID)
	found := false
	for _, id := range cfg.Lists {
		if id == cacheMsgID {
			found = true
			break
		}
	}
	if !found {
		cfg.Lists = append(cfg.Lists, cacheMsgID)
	}
	listsBytes, _ := json.Marshal(cfg.Lists)
	query := `INSERT INTO jaseb_configurasi (owner_id, lists, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(owner_id) DO UPDATE SET lists = excluded.lists, updated_at = CURRENT_TIMESTAMP;`
	_, _ = s.sqlite.DB.ExecContext(ctx, query, ownerID, string(listsBytes))
}

func (s *Service) countLists(ctx context.Context, ownerID int64) int {
	cfg := s.getJasebConfig(ctx, ownerID)
	return len(cfg.Lists)
}

func (s *Service) getJasebConfig(ctx context.Context, ownerID int64) jasebConfigDoc {
	// 1. Coba dari SQLite
	if s.sqlite != nil {
		var listsJSON, groupsJSON, topicsJSON string
		var delay, sfw, sfm, tpe, extra, wmCacheID int
		var typ, timer, mode, status, masaAktif, notif, email, admin, watermark, tTerkini, tTerakhir string
		var broadcastIndex int
		var broadcastNextAt int64
		var updatedAt time.Time

		query := `SELECT type, delay, skip_floodwait, timer, extra, mode, safemode, status,
			masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
			tindakan_terkini, tindakan_terakhir, lists, groups, topics,
			broadcast_index, broadcast_next_at, updated_at
			FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`

		err := s.sqlite.DB.QueryRowContext(ctx, query, ownerID).Scan(
			&typ, &delay, &sfw, &timer, &extra, &mode, &sfm, &status,
			&masaAktif, &notif, &email, &admin, &watermark, &wmCacheID, &tpe,
			&tTerkini, &tTerakhir, &listsJSON, &groupsJSON, &topicsJSON,
			&broadcastIndex, &broadcastNextAt, &updatedAt,
		)
		if err == nil {
			var doc jasebConfigDoc
			doc.OwnerID = ownerID
			doc.Type = typ
			doc.Delay = delay
			doc.SkipFloodWait = sfw == 1
			doc.Timer = timer
			doc.Extra = extra
			doc.Mode = mode
			doc.Safemode = sfm == 1
			doc.Status = status
			doc.MasaAktif = masaAktif
			doc.Notifikasi = notif
			doc.Email = email
			doc.Admin = admin
			doc.Watermark = watermark
			doc.WatermarkCacheID = wmCacheID
			doc.TopicEnabled = tpe == 1
			doc.TindakanTerkini = tTerkini
			doc.TindakanTerakhir = tTerakhir
			doc.BroadcastIndex = broadcastIndex
			doc.BroadcastNextAt = broadcastNextAt
			doc.UpdatedAt = updatedAt
			if listsJSON != "" {
				_ = json.Unmarshal([]byte(listsJSON), &doc.Lists)
			}
			if groupsJSON != "" {
				_ = json.Unmarshal([]byte(groupsJSON), &doc.Groups)
			}
			if topicsJSON != "" {
				_ = json.Unmarshal([]byte(topicsJSON), &doc.Topics)
			}
			return doc
		}
	}

	// 2. Coba dari MongoDB
	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		var doc jasebConfigDoc
		err := col.FindOne(ctx, bson.M{"_id": ownerID}).Decode(&doc)
		if err == nil {
			return doc
		}
	}

	return jasebConfigDoc{
		OwnerID:          ownerID,
		Type:             "Broadcast",
		Delay:            5,
		Timer:            "Belum diatur",
		Mode:             "Normal",
		Status:           "Berhenti",
		MasaAktif:        "November 29, 2026 at 14:18",
		Notifikasi:       "Aktif · Kirim ke Saya",
		Email:            "Belum diatur",
		Admin:            "Belum diatur",
		TindakanTerkini:  "Siaran otomatis dimatikan",
		TindakanTerakhir: "-",
		Lists:            []int{},
		Groups:           []jasebGroupDoc{},
		Topics:           []jasebTopicDoc{},
	}
}

func (s *Service) setPanelBroadcastStatus(ctx context.Context, ownerID int64, status, tindakan string) {
	now := time.Now()
	tLast := now.Format("02 Jan 2006 15:04")

	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		opts := options.Update().SetUpsert(true)
		_, _ = col.UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"status":            status,
				"tindakan_terkini":  tindakan,
				"tindakan_terakhir": tLast,
				"updated_at":        now,
			},
		}, opts)
	}

	if s.sqlite != nil {
		query := `INSERT INTO jaseb_configurasi (owner_id, status, tindakan_terkini, tindakan_terakhir, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(owner_id) DO UPDATE SET status = excluded.status, tindakan_terkini = excluded.tindakan_terkini, tindakan_terakhir = excluded.tindakan_terakhir, updated_at = excluded.updated_at;`
		_, _ = s.sqlite.DB.ExecContext(ctx, query, ownerID, status, tindakan, tLast, now)
	}
}

func (s *Service) savePanelBroadcastCheckpoint(ctx context.Context, ownerID int64, nextIndex int, nextAt time.Time) {
	if s.db != nil {
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"broadcast_index":   nextIndex,
				"broadcast_next_at": nextAt.Unix(),
				"updated_at":        time.Now(),
			},
		}, options.Update().SetUpsert(true))
	}
	if s.sqlite != nil {
		_, _ = s.sqlite.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET broadcast_index = ?, broadcast_next_at = ?, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?`, nextIndex, nextAt.Unix(), ownerID)
	}
}

func (s *Service) handleCacheList(ctx context.Context, client *telegram.Client, chatID, ownerID int64, text string, entities []models.MessageEntity, mediaIDs []int) {
	topicID := getTeksCacheTopicID()
	cachedMsgID := 0
	var err error
	if len(mediaIDs) > 0 {
		cachedMsgID = mediaIDs[0]
	} else {
		cachedMsgID, err = s.sendTextToAiotdCachesTopic(ctx, client, topicID, text, toMTProtoEntities(entities))
		mediaIDs = []int{cachedMsgID}
	}
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Menyimpan ke Cache:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
		return
	}
	if cachedMsgID <= 0 {
		s.reply(ctx, chatID, "❌ <b>Gagal Menyimpan ke Cache:</b> ID pesan cache tidak valid.", "panel_notice")
		return
	}

	// 1. Simpan ke MongoDB (jaseb_configurasi)
	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		opts := options.Update().SetUpsert(true)
		_, _ = col.UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$push": bson.M{"lists": bson.M{"$each": mediaIDs}},
			"$set":  bson.M{"updated_at": time.Now()},
		}, opts)
	}

	// 2. Sinkronkan ke SQLite
	for _, cachedMsgID := range mediaIDs {
		s.syncAddListToSQLite(ctx, ownerID, cachedMsgID)
	}

	totalList := s.countLists(ctx, ownerID)
	report := fmt.Sprintf("✅ <b>List Berhasil Disimpan!</b>\n\n<blockquote><i>Pesan materi promosi telah diamankan ke @%s (Topic: <code>%d</code>) dengan Cache Message ID: <code>#%d</code></i></blockquote>\n\nTotal list tersimpan: <code>%d</code>", getGroupCache(), topicID, cachedMsgID, totalList)
	s.reply(ctx, chatID, report, "panel_notice")
}

func (s *Service) handleCacheWatermark(ctx context.Context, client *telegram.Client, chatID, ownerID int64, text string, entities []models.MessageEntity, mediaIDs []int) {
	topicID := getWatermarkCacheTopicID()
	cachedMsgID := 0
	var err error
	if len(mediaIDs) > 0 {
		cachedMsgID = mediaIDs[0]
	} else {
		cachedMsgID, err = s.sendTextToAiotdCachesTopic(ctx, client, topicID, text, toMTProtoEntities(entities))
	}
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Menyimpan ke Cache:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
		return
	}
	if cachedMsgID <= 0 {
		s.reply(ctx, chatID, "❌ <b>Gagal Menyimpan ke Cache:</b> ID pesan cache tidak valid.", "panel_notice")
		return
	}

	// 1. Simpan ke MongoDB (jaseb_configurasi)
	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		opts := options.Update().SetUpsert(true)
		_, _ = col.UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"watermark":          text,
				"watermark_cache_id": cachedMsgID,
				"updated_at":         time.Now(),
			},
		}, opts)
	}

	// 2. Sinkronkan ke SQLite
	if s.sqlite != nil {
		query := `INSERT INTO jaseb_configurasi (owner_id, watermark, watermark_cache_id, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(owner_id) DO UPDATE SET watermark = excluded.watermark, watermark_cache_id = excluded.watermark_cache_id, updated_at = CURRENT_TIMESTAMP;`
		_, _ = s.sqlite.DB.ExecContext(ctx, query, ownerID, text, cachedMsgID)
	}

	report := fmt.Sprintf("✅ <b>Custom Watermark Berhasil Disimpan!</b>\n\n<blockquote><i>Watermark telah diamankan ke @%s (Topic: <code>%d</code>) dengan Cache Message ID: <code>#%d</code></i></blockquote>\n\nWatermark: <code>%s</code>", getGroupCache(), topicID, cachedMsgID, html.EscapeString(text))
	s.reply(ctx, chatID, report, "panel_notice")
}

func (s *Service) handleStartPanelBroadcast(ctx context.Context, client *telegram.Client, chatID, ownerID int64) {
	s.launchPanelBroadcast(client, chatID, ownerID, false)
}

func (s *Service) resumePanelBroadcast(client *telegram.Client, chatID, ownerID int64) {
	cfg := s.getJasebConfig(context.Background(), ownerID)
	if strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan") {
		s.launchPanelBroadcast(client, chatID, ownerID, true)
	}
}

func (s *Service) launchPanelBroadcast(client *telegram.Client, chatID, ownerID int64, resume bool) {
	s.mu.Lock()
	if existing, ok := s.panelBroadcasts[ownerID]; ok && existing != nil {
		existing.cancel()
	}
	bCtx, bCancel := context.WithCancel(context.Background())
	s.panelBroadcasts[ownerID] = &panelBroadcastJob{cancel: bCancel}
	s.mu.Unlock()

	go s.executePanelBroadcast(bCtx, client, chatID, ownerID, resume)
}

func (s *Service) executePanelBroadcast(ctx context.Context, client *telegram.Client, chatID, ownerID int64, resume bool) {
	defer func() {
		s.mu.Lock()
		delete(s.panelBroadcasts, ownerID)
		s.mu.Unlock()
	}()

	cfg := s.getJasebConfig(ctx, ownerID)
	if len(cfg.Groups) == 0 {
		s.reply(ctx, chatID, "⚠️ <b>Siaran Tidak Dapat Berjalan:</b>\n\n<blockquote><i>Daftar grup target masih kosong! Silakan tambahkan grup terlebih dahulu melalui menu <b>🧩 Grup & List 🧩</b>.</i></blockquote>", "panel_notice")
		s.setPanelBroadcastStatus(ctx, ownerID, "Berhenti", "Grup target kosong")
		return
	}
	if len(cfg.Lists) == 0 {
		s.reply(ctx, chatID, "⚠️ <b>Siaran Tidak Dapat Berjalan:</b>\n\n<blockquote><i>Daftar materi list promosi masih kosong! Silakan tambahkan list terlebih dahulu melalui menu <b>🧩 Grup & List 🧩</b>.</i></blockquote>", "panel_notice")
		s.setPanelBroadcastStatus(ctx, ownerID, "Berhenti", "Materi list kosong")
		return
	}

	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Menghubungi @%s:</b> <code>%s</code>", getGroupCache(), html.EscapeString(err.Error())), "panel_notice")
		s.setPanelBroadcastStatus(ctx, ownerID, "Berhenti", "Gagal menghubungi cache")
		return
	}

	delaySec := cfg.Delay
	if delaySec <= 0 {
		delaySec = 5
	}

	if !resume {
		s.reply(ctx, chatID, fmt.Sprintf("🚀 <b>Siaran Otomatis Dimulai!</b>\n\n<blockquote><i>Mengirim materi promosi ke <code>%d</code> grup target dengan jeda <code>%d detik</code> per grup.</i></blockquote>", len(cfg.Groups), delaySec), "panel_notice")
		s.setPanelBroadcastStatus(ctx, ownerID, "Berjalan", fmt.Sprintf("Sedang mengirim ke %d grup...", len(cfg.Groups)))
	}

	// Pre-warm seluruh dialog channel/supergroup access hash agar kirim pesan tidak CHAT_ID_INVALID
	s.preWarmDialogAccessHashes(ctx, client, ownerID)

	for {
		cfg = s.getJasebConfig(ctx, ownerID)
		if !strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan") {
			return
		}
		if len(cfg.Groups) == 0 || len(cfg.Lists) == 0 {
			s.setPanelBroadcastStatus(context.Background(), ownerID, "Berhenti", "Target atau materi kosong")
			return
		}
		delaySec = cfg.Delay
		if delaySec <= 0 {
			delaySec = 5
		}
		// Broadcast berjalan per putaran list: List A ke semua grup,
		// kemudian List B ke semua grup, dan seterusnya.
		totalWork := len(cfg.Lists) * len(cfg.Groups)
		workIndex := cfg.BroadcastIndex
		if workIndex < 0 || workIndex >= totalWork {
			workIndex = 0
		}
		listIndex := workIndex / len(cfg.Groups)
		groupIndex := workIndex % len(cfg.Groups)
		if cfg.BroadcastNextAt > time.Now().Unix() && !s.waitPanelBroadcast(ctx, ownerID, time.Unix(cfg.BroadcastNextAt, 0)) {
			return
		}
		grp := cfg.Groups[groupIndex]
		select {
		case <-ctx.Done():
			return
		default:
		}

		targetPeer, err := s.resolveTargetPeer(ctx, client, grp.ChatID, grp.Target, grp.AccessHash, grp.PeerType)
		if err != nil {
			log.Printf("[panelBroadcast] resolve target %s error: %v", grp.Target, err)
			nextAt := time.Now().Add(time.Duration(delaySec) * time.Second)
			s.savePanelBroadcastCheckpoint(context.Background(), ownerID, (workIndex+1)%totalWork, nextAt)
			continue
		}

		msgID := cfg.Lists[listIndex]

		forwardIDs := s.cacheForwardIDs(ctx, client, cachePeer, msgID)
		randomIDs := make([]int64, len(forwardIDs))
		for i := range randomIDs {
			randomIDs[i] = time.Now().UnixNano() + int64(i)
		}
		req := &tg.MessagesForwardMessagesRequest{
			FromPeer:   cachePeer,
			ID:         forwardIDs,
			RandomID:   randomIDs,
			ToPeer:     targetPeer,
			DropAuthor: true,
		}
		if cfg.Mode == "Silent" {
			req.Silent = true
		}

		_, err = client.API().MessagesForwardMessages(ctx, req)
		if err != nil {
			log.Printf("[panelBroadcast] forward to %s (%d) error: %v", grp.Title, grp.ChatID, err)
			if flood, ok := tgerr.AsFloodWait(err); ok {
				if cfg.SkipFloodWait {
					log.Printf("[panelBroadcast] FloodWait %v skipped", flood)
					nextAt := time.Now().Add(time.Duration(delaySec) * time.Second)
					s.savePanelBroadcastCheckpoint(context.Background(), ownerID, (workIndex+1)%totalWork, nextAt)
					continue
				}
				sleepDur := flood + time.Second
				select {
				case <-ctx.Done():
					return
				case <-time.After(sleepDur):
				}
				_, _ = client.API().MessagesForwardMessages(ctx, req)
			}
		}

		nextAt := time.Now().Add(time.Duration(delaySec) * time.Second)
		if cfg.Safemode {
			nextAt = nextAt.Add(500 * time.Millisecond)
		}
		s.savePanelBroadcastCheckpoint(context.Background(), ownerID, (workIndex+1)%totalWork, nextAt)
	}
}

// cacheForwardIDs returns all messages belonging to the same Telegram album.
// A normal text/media message simply returns its own ID.
func (s *Service) cacheForwardIDs(ctx context.Context, client *telegram.Client, cachePeer tg.InputPeerClass, msgID int) []int {
	result, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer: cachePeer, OffsetID: msgID + 20, Limit: 40,
	})
	if err != nil {
		return []int{msgID}
	}
	var messages []tg.MessageClass
	switch value := result.(type) {
	case *tg.MessagesMessages:
		messages = value.Messages
	case *tg.MessagesMessagesSlice:
		messages = value.Messages
	case *tg.MessagesChannelMessages:
		messages = value.Messages
	}
	var groupID int64
	for _, item := range messages {
		if m, ok := item.(*tg.Message); ok && m.ID == msgID {
			groupID, _ = m.GetGroupedID()
			break
		}
	}
	if groupID == 0 {
		return []int{msgID}
	}
	ids := make([]int, 0, 10)
	for _, item := range messages {
		if m, ok := item.(*tg.Message); ok {
			if grouped, ok := m.GetGroupedID(); ok && grouped == groupID {
				ids = append(ids, m.ID)
			}
		}
	}
	if len(ids) == 0 {
		return []int{msgID}
	}
	sort.Ints(ids)
	return ids
}

func (s *Service) waitPanelBroadcast(ctx context.Context, ownerID int64, due time.Time) bool {
	for {
		remaining := time.Until(due)
		if remaining <= 0 {
			return true
		}
		wait := remaining
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		cfg := s.getJasebConfig(ctx, ownerID)
		if !strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan") {
			return false
		}
	}
}

func (s *Service) handleTestRCBroadcast(ctx context.Context, client *telegram.Client, chatID, ownerID int64) {
	cfg := s.getJasebConfig(ctx, ownerID)
	if len(cfg.Lists) == 0 {
		s.reply(ctx, chatID, "⚠️ <b>Uji Coba Gagal:</b> List materi promosi masih kosong.", "panel_notice")
		return
	}
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Cache:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
		return
	}

	msgID := cfg.Lists[0]
	selfPeer := &tg.InputPeerSelf{}
	randomID := time.Now().UnixNano()
	req := &tg.MessagesForwardMessagesRequest{
		FromPeer:   cachePeer,
		ID:         []int{msgID},
		RandomID:   []int64{randomID},
		ToPeer:     selfPeer,
		DropAuthor: true,
	}
	_, err = client.API().MessagesForwardMessages(ctx, req)
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Uji Coba Gagal:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
		return
	}
	s.reply(ctx, chatID, fmt.Sprintf("✅ <b>Uji Coba Berhasil!</b>\n\n<blockquote><i>Materi promosi (Cache ID: <code>#%d</code>) telah berhasil dikirim ke Saved Messages akun Telegram Anda.</i></blockquote>", msgID), "panel_notice")
}

func (s *Service) preWarmDialogAccessHashes(ctx context.Context, client *telegram.Client, ownerID int64) {
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		s.cacheEntities(tg.Entities{
			Users:    item.Entities.Users(),
			Chats:    item.Entities.Chats(),
			Channels: item.Entities.Channels(),
		})
		switch p := item.Peer.(type) {
		case *tg.InputPeerChannel:
			if p.ChannelID != 0 && p.AccessHash != 0 {
				s.channelAccessHashes.Store(p.ChannelID, p.AccessHash)
			}
		}
		return nil
	})
}

func (s *Service) resolveTargetPeer(ctx context.Context, client *telegram.Client, chatID int64, target string, accessHash int64, peerType string) (tg.InputPeerClass, error) {
	cleanID := chatID
	if cleanID < 0 {
		cleanID = -cleanID
	}
	strID := fmt.Sprintf("%d", chatID)
	if strings.HasPrefix(strID, "-100") {
		if parsed, err := strconv.ParseInt(strings.TrimPrefix(strID, "-100"), 10, 64); err == nil {
			cleanID = parsed
		}
	} else if strings.HasPrefix(target, "-100") {
		if parsed, err := strconv.ParseInt(strings.TrimPrefix(target, "-100"), 10, 64); err == nil {
			cleanID = parsed
		}
	} else if strings.HasPrefix(target, "-") {
		if parsed, err := strconv.ParseInt(strings.TrimPrefix(target, "-"), 10, 64); err == nil {
			cleanID = parsed
		}
	}

	// 1. Cek dari channelAccessHashes yang telah dicache
	if hashVal, ok := s.channelAccessHashes.Load(cleanID); ok {
		if hash, ok := hashVal.(int64); ok && hash != 0 {
			return &tg.InputPeerChannel{ChannelID: cleanID, AccessHash: hash}, nil
		}
	}

	// 2. Jika ada accessHash dari database record
	if accessHash != 0 {
		s.channelAccessHashes.Store(cleanID, accessHash)
		return &tg.InputPeerChannel{ChannelID: cleanID, AccessHash: accessHash}, nil
	}

	// 3. Jika target adalah username @username
	if strings.HasPrefix(target, "@") {
		resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: strings.TrimPrefix(target, "@")})
		if err == nil {
			p, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
			if err == nil {
				if ch, ok := p.(*tg.InputPeerChannel); ok {
					s.channelAccessHashes.Store(ch.ChannelID, ch.AccessHash)
				}
				return p, nil
			}
		}
	}

	// 4. Jika peerType adalah channel/supergroup atau cleanID besar (supergroup ID biasanya > 1.000.000)
	if peerType == "channel" || cleanID > 1000000 {
		_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
			if ch, ok := item.Peer.(*tg.InputPeerChannel); ok {
				s.channelAccessHashes.Store(ch.ChannelID, ch.AccessHash)
			}
			return nil
		})
		if hashVal, ok := s.channelAccessHashes.Load(cleanID); ok {
			if hash, ok := hashVal.(int64); ok && hash != 0 {
				return &tg.InputPeerChannel{ChannelID: cleanID, AccessHash: hash}, nil
			}
		}
		return nil, fmt.Errorf("supergroup %d tidak memiliki access_hash (pastikan userbot sudah bergabung ke grup)", cleanID)
	}

	// 5. Basic group (chat)
	if cleanID != 0 {
		return &tg.InputPeerChat{ChatID: cleanID}, nil
	}
	return nil, fmt.Errorf("peer tidak dapat ditemukan")
}

var _ = sql.ErrNoRows
