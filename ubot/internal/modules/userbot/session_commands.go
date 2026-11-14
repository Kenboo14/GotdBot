package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HandleCommand dispatches incoming command requests from Gateway via Redis Stream
func (s *Service) HandleCommand(ctx context.Context, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return
	}
	var job models.PToService
	if json.Unmarshal([]byte(payload), &job) != nil || (job.ChatID == 0 && job.UserID == 0 && job.OwnerID == 0) {
		return
	}
	if job.Type == "broadcast_cancel" {
		ownerID := job.OwnerID
		if ownerID == 0 {
			ownerID = job.UserID
		}
		s.mu.Lock()
		bJob := s.broadcasts[ownerID]
		s.mu.Unlock()
		if bJob != nil {
			bJob.cancel()
		}
		return
	}
	if job.Type == "stop_panel_broadcast" {
		ownerID := job.UserID
		if ownerID == 0 {
			ownerID = job.OwnerID
		}
		s.mu.Lock()
		pJob := s.panelBroadcasts[ownerID]
		if pJob != nil {
			delete(s.panelBroadcasts, ownerID)
		}
		s.mu.Unlock()
		if pJob != nil {
			pJob.cancel()
		}
		return
	}
	targetOwner := job.OwnerID
	if targetOwner == 0 {
		targetOwner = job.UserID
	}
	if targetOwner == 0 {
		targetOwner = job.ChatID
	}

	s.mu.Lock()
	client := s.active[targetOwner]
	sessionChatID := targetOwner
	if client == nil {
		client = s.active[job.UserID]
		sessionChatID = job.UserID
	}
	if client == nil {
		client = s.active[job.ChatID]
		sessionChatID = job.ChatID
	}
	if client == nil {
		for cid, oid := range s.owners {
			if oid == targetOwner || oid == job.UserID || oid == job.ChatID {
				if c := s.active[cid]; c != nil {
					client = c
					sessionChatID = cid
					break
				}
			}
		}
	}
	if client == nil {
		// On-demand wake up: jika sesi ada di database tapi belum aktif
		s.mu.Unlock()
		s.activate(targetOwner, targetOwner)
		// Tunggu sejenak hingga client siap
		for i := 0; i < 15; i++ {
			time.Sleep(200 * time.Millisecond)
			s.mu.Lock()
			client = s.active[targetOwner]
			if client == nil {
				client = s.active[job.UserID]
			}
			if client == nil {
				client = s.active[job.ChatID]
			}
			if client == nil {
				for cid, oid := range s.owners {
					if oid == targetOwner || oid == job.UserID || oid == job.ChatID {
						if c := s.active[cid]; c != nil {
							client = c
							sessionChatID = cid
							break
						}
					}
				}
			}
			if client != nil {
				sessionChatID = targetOwner
				break
			}
			s.mu.Unlock()
		}
	} else {
		s.lastActive[sessionChatID] = time.Now()
		s.mu.Unlock()
	}
	if client == nil {
		s.reply(ctx, job.ChatID, "Ping gagal. Tidak ada sesi userbot aktif.", "failed")
		return
	}

	ownerID := job.OwnerID
	if ownerID == 0 {
		ownerID = job.UserID
	}
	if ownerID == 0 {
		ownerID = job.ChatID
	}

	dispatch := func(fn func(context.Context)) {
		taskCtx, taskCancel := context.WithTimeout(s.root, 3*time.Minute)
		select {
		case s.cmdSem <- struct{}{}:
			go func() {
				defer func() {
					<-s.cmdSem
					taskCancel()
				}()
				fn(taskCtx)
			}()
		default:
			// Semaphore penuh: tunggu slot kosong maksimal 5 detik sebelum menyerah
			go func() {
				defer taskCancel()
				waitCtx, waitCancel := context.WithTimeout(s.root, 5*time.Second)
				defer waitCancel()
				select {
				case s.cmdSem <- struct{}{}:
					defer func() { <-s.cmdSem }()
					fn(taskCtx)
				case <-waitCtx.Done():
					slog.Warn("cmd semaphore saturated, job dropped", slog.Int64("chat_id", job.ChatID))
					taskCancel()
				case <-s.root.Done():
					taskCancel()
				}
			}()
		}
	}

	if job.Type == "fetch_groups" {
		dispatch(func(c context.Context) { s.fetchAccountGroups(c, client, job.ChatID, ownerID) })
		return
	}
	if job.Type == "cache_list" {
		dispatch(func(c context.Context) {
			s.handleCacheList(c, client, job.ChatID, ownerID, job.Text, job.Entities, job.MediaCacheIDs, job.MediaSourceCount)
		})
		return
	}
	if job.Type == "cache_watermark" {
		dispatch(func(c context.Context) {
			s.handleCacheWatermark(c, client, job.ChatID, ownerID, job.Text, job.Entities, job.MediaCacheIDs, job.MediaSourceCount)
		})
		return
	}
	if job.Type == "get_list_detail" {
		dispatch(func(c context.Context) { s.handleListDetail(c, client, job.ChatID, ownerID, job.TaskID) })
		return
	}
	if job.Type == "get_list_previews" {
		dispatch(func(c context.Context) { s.handleListPreviews(c, client, job.ChatID, ownerID, job.TaskID) })
		return
	}
	if job.Type == "clear_panel_lists" || job.Type == "clear_panel_groups" {
		dispatch(func(c context.Context) { s.clearPanelData(c, ownerID, job.Type == "clear_panel_lists") })
		return
	}
	if job.Type == "delete_panel_list" {
		dispatch(func(c context.Context) { s.deletePanelList(c, ownerID, job.Text) })
		return
	}
	if job.Type == "delete_panel_group" {
		dispatch(func(c context.Context) { s.deletePanelGroup(c, ownerID, job.Text) })
		return
	}
	if job.Type == "sync_panel_config" {
		dispatch(func(c context.Context) { s.syncJasebConfigFromMongo(c, ownerID) })
		return
	}
	if job.Type == "start_panel_broadcast" {
		dispatch(func(c context.Context) { s.handleStartPanelBroadcast(c, client, job.ChatID, ownerID) })
		return
	}
	if job.Type == "test_rc_broadcast" {
		dispatch(func(c context.Context) { s.handleTestRCBroadcast(c, client, job.ChatID, ownerID) })
		return
	}
	started := time.Now()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err != nil {
		s.reply(ctx, job.ChatID, "Ping gagal. Periksa koneksi dan status sesi userbot.", "failed")
		return
	}
	s.reply(ctx, job.ChatID, fmt.Sprintf("<code>%.3f ms</code>", float64(time.Since(started).Microseconds())/1000), "ok")
}

func (s *Service) fetchAccountGroups(ctx context.Context, client *telegram.Client, chatID, ownerID int64) {
	if client == nil {
		s.reply(ctx, chatID, `<h1>❌ Gagal Mengambil Grup</h1>
<blockquote expandable>Sesi userbot belum aktif atau sedang terputus.</blockquote>
<hr/>
<p>Silakan hubungkan userbot melalui <b>/start</b>.</p>`, "panel_notice")
		return
	}

	var allGroups []jasebGroupDoc
	seen := make(map[int64]bool)

	err := dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		entities := tg.Entities{
			Users:    item.Entities.Users(),
			Chats:    item.Entities.Chats(),
			Channels: item.Entities.Channels(),
		}
		s.cacheEntities(entities)

		kind, id, skip := dialogKind(item.Peer, entities, ownerID)
		if skip || kind != "group" || seen[id] {
			return nil
		}
		seen[id] = true
		title := dialogName(item.Peer, entities, id)

		var accessHash int64
		peerType := "channel"

		switch p := item.Peer.(type) {
		case *tg.InputPeerChannel:
			accessHash = p.AccessHash
			peerType = "channel"
			s.channelAccessHashes.Store(p.ChannelID, p.AccessHash)
		case *tg.InputPeerChat:
			peerType = "chat"
		}

		if ch, ok := entities.Channels[id]; ok && ch != nil {
			if ch.AccessHash != 0 {
				accessHash = ch.AccessHash
			}
			peerType = "channel"
			s.channelAccessHashes.Store(id, accessHash)
		}

		allGroups = append(allGroups, jasebGroupDoc{
			Target:     fmt.Sprintf("%d", id),
			Title:      title,
			ChatID:     id,
			AccessHash: accessHash,
			PeerType:   peerType,
		})
		return nil
	})

	if err != nil {
		slog.Error("dialogs error during fetchAccountGroups", slog.String("error", err.Error()))
	}

	// 1. Simpan ke MongoDB jaseb_configurasi (koleksi tunggal terpadu)
	if s.db != nil {
		col := s.db.Collection("jaseb_configurasi")
		opts := options.Update().SetUpsert(true)
		_, _ = col.UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"groups":     allGroups,
				"updated_at": time.Now(),
			},
		}, opts)
	}

	// 2. Sinkronkan ke SQLite jaseb_configurasi
	if s.sqlite != nil {
		groupsBytes, _ := json.Marshal(allGroups)
		query := `INSERT INTO jaseb_configurasi (owner_id, groups, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(owner_id) DO UPDATE SET groups = excluded.groups, updated_at = CURRENT_TIMESTAMP;`
		_, _ = s.sqlite.DB.ExecContext(ctx, query, ownerID, string(groupsBytes))
	}

	// 3. Beritahu Gateway via Redis Pub/Sub bahwa config ownerID telah diperbarui di MongoDB
	s.publishConfigUpdated(ctx, ownerID)

	report := fmt.Sprintf(`<h1>✅ Sinkronisasi Grup Berhasil</h1>
<blockquote expandable>Sebanyak <code>%d</code> grup dari akun Telegram Anda telah berhasil diimpor ke daftar target broadcast.</blockquote>
<table border="1">
  <tr><th>📌 Sumber</th><th>Total Grup Terimpor</th></tr>
  <tr><td>Akun Userbot</td><td><code>%d grup</code></td></tr>
</table>
<hr/>
<p>GotdBot • Group Target Manager</p>`, len(allGroups), len(allGroups))
	s.reply(ctx, chatID, report, "panel_notice")
}

func (s *Service) syncAllNotesToRedis(ctx context.Context) {
	if s.redis == nil || s.sqlite == nil {
		return
	}
	notes, err := s.sqlite.ListAllNotes(ctx)
	if err != nil || len(notes) == 0 {
		return
	}
	synced := 0
	for _, n := range notes {
		var buttons []NoteButton
		if n.Buttons != "" {
			_ = json.Unmarshal([]byte(n.Buttons), &buttons)
		}
		payload := NoteInlinePayload{
			OwnerID: n.OwnerID,
			Name:    n.Name,
			Text:    n.Text,
			Buttons: buttons,
			Mode:    n.Mode,
		}
		if pBytes, err := json.Marshal(payload); err == nil {
			_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", n.OwnerID, n.Name), pBytes, 7*24*time.Hour).Err()
		}
		_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", n.OwnerID, n.Name), n.Text, 7*24*time.Hour).Err()
		synced++
	}
	if synced > 0 {
		slog.Info("redis notes cache synchronized", slog.Int("synced_count", synced))
	}
}
