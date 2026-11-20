package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// findActiveClientLocked mencari client aktif secara berurutan pada targetOwner, userID, chatID,
// dan s.owners. s.mu HARUS sedang dikunci oleh pemanggil.
func (s *Service) findActiveClientLocked(targetOwner, userID, chatID int64) (*gobot.Client, int64) {
	if targetOwner != 0 {
		if c := s.active[targetOwner]; c != nil {
			return c, targetOwner
		}
	}
	if userID != 0 {
		if c := s.active[userID]; c != nil {
			return c, userID
		}
	}
	if chatID != 0 {
		if c := s.active[chatID]; c != nil {
			return c, chatID
		}
	}
	for cid, oid := range s.owners {
		if (targetOwner != 0 && oid == targetOwner) || (userID != 0 && oid == userID) || (chatID != 0 && oid == chatID) {
			if c := s.active[cid]; c != nil {
				return c, cid
			}
		}
	}
	return nil, 0
}

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
	if job.Type == "relink_user_data" {
		s.relinkUserData(ctx, job.OwnerID, job.ChatID, job.OldUserbotID, job.UserID)
		return
	}
	if job.Type == "revoke_expired_session" {
		s.revokeExpiredSession(ctx, job.ChatID, job.UserID, job.Text)
		return
	}
	if job.Type == "revoke_transferred_session" {
		s.revokeSessionWithReason(ctx, job.ChatID, job.UserID, job.Text)
		return
	}
	if job.Type == "alive_ping" {
		s.handleAlivePingJob(job)
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
	client, sessionChatID := s.findActiveClientLocked(targetOwner, job.UserID, job.ChatID)
	if client != nil {
		s.lastActive[sessionChatID] = time.Now()
		s.mu.Unlock()
	} else {
		// On-demand wake up: jika sesi ada di database tapi belum aktif
		s.mu.Unlock()
		s.activate(targetOwner, targetOwner)
		// Tunggu sejenak hingga client siap
		for i := 0; i < 15; i++ {
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}

			s.mu.Lock()
			client, sessionChatID = s.findActiveClientLocked(targetOwner, job.UserID, job.ChatID)
			if client != nil {
				s.lastActive[sessionChatID] = time.Now()
				s.mu.Unlock()
				break
			}
			s.mu.Unlock()
		}
	}
	if client == nil {
		s.reply(ctx, job.ChatID, job.ThreadID, "Ping gagal. Tidak ada sesi userbot aktif.", "failed")
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
	if job.Type == "clear_watermark" {
		dispatch(func(c context.Context) { s.clearPanelWatermark(c, ownerID) })
		return
	}
	if job.Type == "fetch_forum_groups" {
		dispatch(func(c context.Context) { s.handleFetchForumGroups(c, client, job.ChatID, ownerID, job.TaskID) })
		return
	}
	if job.Type == "fetch_group_topics" {
		dispatch(func(c context.Context) { s.handleFetchGroupTopics(c, client, job.ChatID, ownerID, job.TaskID, job.Text) })
		return
	}
	if job.Type == "cache_topic_list_post" {
		dispatch(func(c context.Context) {
			s.handleCacheTopicListPost(c, client, job.ChatID, ownerID, job.ThreadID, job.TaskID, job.Text, job.Entities, job.MediaCacheIDs, job.MediaSourceCount)
		})
		return
	}
	if job.Type == "get_topic_list_posts" {
		dispatch(func(c context.Context) { s.handleTopicListPosts(c, client, job.ChatID, ownerID, job.ThreadID, job.TaskID) })
		return
	}
	if job.Type == "get_topic_list_post_detail" {
		dispatch(func(c context.Context) { s.handleTopicListPostDetail(c, client, job.ChatID, ownerID, job.ThreadID, job.TaskID, job.Text) })
		return
	}
	started := time.Now()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err != nil {
		s.reply(ctx, job.ChatID, job.ThreadID, "Ping gagal. Periksa koneksi dan status sesi userbot.", "failed")
		return
	}
	s.reply(ctx, job.ChatID, job.ThreadID, fmt.Sprintf("<code>%.3f ms</code>", float64(time.Since(started).Microseconds())/1000), "ok")
}

func (s *Service) fetchAccountGroups(ctx context.Context, client *gobot.Client, chatID, ownerID int64) {
	if client == nil {
		s.reply(ctx, chatID, 0, `<h1>❌ Gagal Mengambil Grup</h1>
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
		isForum := false

		switch p := item.Peer.(type) {
		case *tg.InputPeerChannel:
			accessHash = p.AccessHash
			peerType = "channel"
			s.peerCache().SaveChannel(p.ChannelID, p.AccessHash)
		case *tg.InputPeerChat:
			peerType = "chat"
		}

		if ch, ok := entities.Channels[id]; ok && ch != nil {
			if ch.AccessHash != 0 {
				accessHash = ch.AccessHash
			}
			isForum = ch.Forum
			peerType = "channel"
			s.peerCache().SaveChannel(id, accessHash)
		}

		allGroups = append(allGroups, jasebGroupDoc{
			Target:     fmt.Sprintf("%d", id),
			Title:      title,
			ChatID:     id,
			AccessHash: accessHash,
			PeerType:   peerType,
			IsForum:    isForum,
		})
		return nil
	})

	if err != nil {
		slog.Error("dialogs error during fetchAccountGroups", slog.String("error", err.Error()))
	}

	cappedFree := false
	if strings.EqualFold(s.getActiveTier(ctx, ownerID), "Free") && len(allGroups) > 30 {
		allGroups = allGroups[:30]
		cappedFree = true
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

	capNote := ""
	if cappedFree {
		capNote = "<br/>⚠️ <i>Batas paket Free maksimal <b>30 grup</b> diterapkan.</i>"
	}
	report := fmt.Sprintf(`<h1>✅ Sinkronisasi Grup Berhasil</h1>
<blockquote expandable>Sebanyak <code>%d</code> grup dari akun Telegram Anda telah berhasil diimpor ke daftar target broadcast.%s</blockquote>
<table border="1">
  <tr><th>📌 Sumber</th><th>Total Grup Terimpor</th></tr>
  <tr><td>Akun Userbot</td><td><code>%d grup</code></td></tr>
</table>
<hr/>
<p>GotdBot • Group Target Manager</p>`, len(allGroups), capNote, len(allGroups))
	s.reply(ctx, chatID, 0, report, "panel_notice")
}

func (s *Service) revokeExpiredSession(ctx context.Context, chatID, userbotID int64, expiredAt string) {
	reason := "Masa aktif token berakhir"
	if expiredAt != "" {
		reason = "Masa aktif token berakhir pada " + expiredAt
	}
	s.revokeSessionWithReason(ctx, chatID, userbotID, reason)
}

func (s *Service) revokeSessionWithReason(ctx context.Context, chatID, userbotID int64, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "Sesi dicabut oleh sistem"
	}
	var targetChatIDs []int64
	seen := make(map[int64]bool)
	addTarget := func(cid int64) {
		if cid != 0 && !seen[cid] {
			seen[cid] = true
			targetChatIDs = append(targetChatIDs, cid)
		}
	}
	addTarget(chatID)

	s.mu.RLock()
	for cid, oid := range s.owners {
		if (chatID != 0 && cid == chatID) || (userbotID != 0 && oid == userbotID) {
			addTarget(cid)
		}
	}
	s.mu.RUnlock()

	if s.sqlite != nil && userbotID != 0 {
		if row, err := s.sqlite.GetSessionByAccount(ctx, userbotID); err == nil && row != nil {
			addTarget(row.ChatID)
		}
	}

	for _, uid := range []int64{chatID, userbotID} {
		if uid == 0 {
			continue
		}
		s.mu.Lock()
		pJob := s.panelBroadcasts[uid]
		if pJob != nil {
			delete(s.panelBroadcasts, uid)
		}
		bJob := s.broadcasts[uid]
		if bJob != nil {
			delete(s.broadcasts, uid)
		}
		s.mu.Unlock()
		if pJob != nil {
			pJob.cancel()
		}
		if bJob != nil {
			bJob.cancel()
		}
	}

	for _, cid := range targetChatIDs {
		s.mu.Lock()
		client := s.active[cid]
		oid := s.owners[cid]
		if oid == 0 {
			oid = userbotID
		}
		delete(s.accountGens, cid)
		delete(s.connecting, cid)
		delete(s.active, cid)
		delete(s.owners, cid)
		delete(s.lastActive, cid)
		delete(s.lastUpdate, cid)
		delete(s.lastVerified, cid)
		delete(s.verifying, cid)
		delete(s.lastPublishedStates, cid)
		delete(s.lastPublishedAt, cid)
		delete(s.reconnectAttempts, cid)
		if client != nil {
			delete(s.clientOwners, client)
		}
		cancel := s.activeCancels[cid]
		delete(s.activeCancels, cid)
		delete(s.accountCtxs, cid)
		s.accountStates[cid] = StateSessionInvalid
		s.mu.Unlock()

		if client != nil {
			s.evictInlineBot(client)
		}
		if cancel != nil {
			cancel()
		}
		s.publishSessionState(cid, oid, StateSessionInvalid, reason)

		if s.sqlite != nil {
			_ = s.sqlite.DeleteSession(context.Background(), cid)
		}
		if s.db != nil {
			_, _ = s.db.Collection("userbot_sessions").DeleteOne(context.Background(), bson.M{"_id": cid})
		}

		s.mu.Lock()
		s.accountStates[cid] = StateStopped
		s.mu.Unlock()
	}
}

func (s *Service) relinkUserData(ctx context.Context, oldChatID, newChatID, oldAccountID, newAccountID int64) {
	if s.sqlite != nil {
		if err := s.sqlite.RelinkUserbotData(ctx, oldChatID, newChatID, oldAccountID, newAccountID); err != nil {
			slog.Warn("relink userbot sqlite failed", slog.Int64("old_chat_id", oldChatID), slog.Int64("new_chat_id", newChatID), slog.String("error", err.Error()))
		}
		if newChatID != 0 {
			if st, err := s.sqlite.GetUserbotSettings(ctx, newChatID); err == nil && st != nil && st.PrefixSet {
				s.mu.Lock()
				s.prefixes[newChatID] = st.Prefix
				s.mu.Unlock()
			}
		}
	}

	if s.db != nil {
		if newChatID != 0 && oldChatID != 0 && oldChatID != newChatID {
			var src settingsRecord
			if err := s.db.Collection("userbot_settings").FindOne(ctx, bson.M{"_id": oldChatID, "prefix_configured": true}).Decode(&src); err == nil && src.PrefixSet {
				accID := newAccountID
				if accID == 0 {
					accID = src.AccountID
				}
				_, _ = s.db.Collection("userbot_settings").UpdateByID(ctx, newChatID, bson.M{
					"$set": bson.M{
						"owner_id":          newChatID,
						"account_id":        accID,
						"prefix":            src.Prefix,
						"prefix_configured": true,
						"updated_at":        time.Now().UTC(),
					},
				}, options.Update().SetUpsert(true))
				_, _ = s.db.Collection("userbot_sessions").UpdateByID(ctx, newChatID, bson.M{
					"$set": bson.M{"prefix": src.Prefix, "prefix_configured": true, "updated_at": time.Now().UTC()},
				})
				_, _ = s.db.Collection("userbot_settings").DeleteOne(ctx, bson.M{"_id": oldChatID})
				if s.sqlite != nil {
					_ = s.sqlite.SaveUserbotPrefix(ctx, newChatID, newChatID, accID, src.Prefix, true)
					_ = s.sqlite.UpdatePrefix(ctx, newChatID, src.Prefix)
				}
				s.mu.Lock()
				s.prefixes[newChatID] = src.Prefix
				s.mu.Unlock()
			}
		}

		fromAccount := oldAccountID
		if fromAccount == 0 {
			fromAccount = oldChatID
		}
		if fromAccount != 0 && newAccountID != 0 && fromAccount != newAccountID {
			_, _ = s.db.Collection("userbot_sudo").UpdateMany(ctx, bson.M{"owner_id": fromAccount}, bson.M{"$set": bson.M{"owner_id": newAccountID}})
			_, _ = s.db.Collection("userbot_broadcast_blacklist").UpdateMany(ctx, bson.M{"owner_id": fromAccount}, bson.M{"$set": bson.M{"owner_id": newAccountID}})
			_, _ = s.db.Collection("userbot_notes").UpdateMany(ctx, bson.M{"owner_id": fromAccount}, bson.M{"$set": bson.M{"owner_id": newAccountID}})
			_, _ = s.db.Collection("userbot_log_status").UpdateMany(ctx, bson.M{"owner_id": fromAccount}, bson.M{"$set": bson.M{"owner_id": newAccountID}})
			_, _ = s.db.Collection("userbot_log_routes").UpdateMany(ctx, bson.M{"user_id": fromAccount}, bson.M{"$set": bson.M{"user_id": newAccountID}})
		}
	}

	if oldAccountID != 0 {
		s.logStatusCache.Delete(oldAccountID)
	}
	if newAccountID != 0 {
		s.logStatusCache.Delete(newAccountID)
	}
	s.sudoMu.Lock()
	s.sudoCache = make(map[string]sudoCacheEntry)
	s.sudoMu.Unlock()
	s.syncAllNotesToRedis(ctx)
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
