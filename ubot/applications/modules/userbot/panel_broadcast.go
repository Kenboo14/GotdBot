package userbot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"log"
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
		var updatedAt time.Time

		query := `SELECT type, delay, skip_floodwait, timer, extra, mode, safemode, status,
			masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
			tindakan_terkini, tindakan_terakhir, lists, groups, topics, updated_at
			FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`

		err := s.sqlite.DB.QueryRowContext(ctx, query, ownerID).Scan(
			&typ, &delay, &sfw, &timer, &extra, &mode, &sfm, &status,
			&masaAktif, &notif, &email, &admin, &watermark, &wmCacheID, &tpe,
			&tTerkini, &tTerakhir, &listsJSON, &groupsJSON, &topicsJSON, &updatedAt,
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

func (s *Service) handleCacheList(ctx context.Context, client *telegram.Client, chatID, ownerID int64, text string) {
	topicID := getTeksCacheTopicID()
	cachedMsgID, err := s.sendTextToAiotdCachesTopic(ctx, client, topicID, text)
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Menyimpan ke Cache:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
		return
	}

	// 1. Simpan ke MongoDB (jaseb_configurasi)
	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		opts := options.Update().SetUpsert(true)
		_, _ = col.UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$push": bson.M{"lists": cachedMsgID},
			"$set":  bson.M{"updated_at": time.Now()},
		}, opts)
	}

	// 2. Sinkronkan ke SQLite
	s.syncAddListToSQLite(ctx, ownerID, cachedMsgID)

	totalList := s.countLists(ctx, ownerID)
	report := fmt.Sprintf("✅ <b>List Berhasil Disimpan!</b>\n\n<blockquote><i>Pesan materi promosi telah diamankan ke @%s (Topic: <code>%d</code>) dengan Cache Message ID: <code>#%d</code></i></blockquote>\n\nTotal list tersimpan: <code>%d</code>", getGroupCache(), topicID, cachedMsgID, totalList)
	s.reply(ctx, chatID, report, "panel_notice")
}

func (s *Service) handleCacheWatermark(ctx context.Context, client *telegram.Client, chatID, ownerID int64, text string) {
	topicID := getWatermarkCacheTopicID()
	cachedMsgID, err := s.sendTextToAiotdCachesTopic(ctx, client, topicID, text)
	if err != nil {
		s.reply(ctx, chatID, fmt.Sprintf("❌ <b>Gagal Menyimpan ke Cache:</b> <code>%s</code>", html.EscapeString(err.Error())), "panel_notice")
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
	s.mu.Lock()
	if existing, ok := s.panelBroadcasts[ownerID]; ok && existing != nil {
		existing.cancel()
	}
	bCtx, bCancel := context.WithCancel(context.Background())
	s.panelBroadcasts[ownerID] = &panelBroadcastJob{cancel: bCancel}
	s.mu.Unlock()

	go s.executePanelBroadcast(bCtx, client, chatID, ownerID)
}

func (s *Service) executePanelBroadcast(ctx context.Context, client *telegram.Client, chatID, ownerID int64) {
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

	s.reply(ctx, chatID, fmt.Sprintf("🚀 <b>Siaran Otomatis Dimulai!</b>\n\n<blockquote><i>Mengirim materi promosi ke <code>%d</code> grup target dengan jeda <code>%d detik</code> per grup.</i></blockquote>", len(cfg.Groups), delaySec), "panel_notice")
	s.setPanelBroadcastStatus(ctx, ownerID, "Berjalan", fmt.Sprintf("Sedang mengirim ke %d grup...", len(cfg.Groups)))

	// Pre-warm seluruh dialog channel/supergroup access hash agar kirim pesan tidak CHAT_ID_INVALID
	s.preWarmDialogAccessHashes(ctx, client, ownerID)

	sent := 0
	failed := 0

	for i, grp := range cfg.Groups {
		select {
		case <-ctx.Done():
			s.reply(context.Background(), chatID, "🛑 <b>Siaran Otomatis Dihentikan oleh Pengguna.</b>", "panel_notice")
			s.setPanelBroadcastStatus(context.Background(), ownerID, "Berhenti", "Dihentikan oleh pengguna")
			return
		default:
		}

		targetPeer, err := s.resolveTargetPeer(ctx, client, grp.ChatID, grp.Target, grp.AccessHash, grp.PeerType)
		if err != nil {
			log.Printf("[panelBroadcast] resolve target %s error: %v", grp.Target, err)
			failed++
			continue
		}

		msgID := cfg.Lists[i%len(cfg.Lists)]
		randomID := time.Now().UnixNano()

		req := &tg.MessagesForwardMessagesRequest{
			FromPeer:   cachePeer,
			ID:         []int{msgID},
			RandomID:   []int64{randomID},
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
					failed++
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
			failed++
		} else {
			sent++
		}

		sleepDur := time.Duration(delaySec) * time.Second
		if cfg.Safemode {
			sleepDur += 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDur):
		}
	}

	report := fmt.Sprintf("✅ <b>Putaran Siaran Selesai!</b>\n\n<blockquote><b>Hasil Pengiriman:</b>\n• Berhasil : <code>%d</code> grup\n• Gagal : <code>%d</code> grup\n• Total Target : <code>%d</code> grup</blockquote>\n\n<i>Putaran ini telah selesai dikirim.</i>", sent, failed, len(cfg.Groups))
	s.reply(ctx, chatID, report, "panel_notice")
	s.setPanelBroadcastStatus(ctx, ownerID, "Berhenti", fmt.Sprintf("Selesai (%d terkirim, %d gagal)", sent, failed))
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
