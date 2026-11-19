package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type forumGroupInfo struct {
	ChatID     int64  `json:"chat_id"`
	Title      string `json:"title"`
	Target     string `json:"target"`
	AccessHash int64  `json:"access_hash,omitempty"`
}

type forumTopicInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Closed bool   `json:"closed,omitempty"`
}

type forumTopicsPayload struct {
	ChatID     int64            `json:"chat_id"`
	GroupTitle string           `json:"group_title"`
	Topics     []forumTopicInfo `json:"topics"`
}

func normalizePeerChannelID(id int64) int64 {
	if id < 0 {
		s := strconv.FormatInt(id, 10)
		if strings.HasPrefix(s, "-100") {
			if v, err := strconv.ParseInt(strings.TrimPrefix(s, "-100"), 10, 64); err == nil {
				return v
			}
		}
		return -id
	}
	return id
}

func sameGroupID(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	return a == b || normalizePeerChannelID(a) == normalizePeerChannelID(b)
}

func (s *Service) clearPanelWatermark(ctx context.Context, ownerID int64) {
	if s.db != nil {
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"watermark":          "",
				"watermark_cache_id": 0,
				"updated_at":         time.Now(),
			},
		}, options.Update().SetUpsert(true))
		s.publishConfigUpdated(ctx, ownerID)
	}
	if s.sqlite != nil {
		_, _ = s.sqlite.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET watermark = '', watermark_cache_id = 0, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?`, ownerID)
	}
}

func (s *Service) saveTopicLists(ctx context.Context, ownerID int64, lists []jasebTopicListDoc) {
	if s.db != nil {
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
			"$set": bson.M{
				"topic_lists": lists,
				"updated_at":  time.Now(),
			},
		}, options.Update().SetUpsert(true))
		s.publishConfigUpdated(ctx, ownerID)
	}
	if s.sqlite != nil {
		raw, _ := json.Marshal(lists)
		_, _ = s.sqlite.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET topic_lists = ?, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?`, string(raw), ownerID)
	}
}

func (s *Service) handleFetchForumGroups(ctx context.Context, client *gobot.Client, chatID, ownerID int64, taskID string) {
	cfg := s.getJasebConfig(ctx, ownerID)
	forumByID := make(map[int64]bool)
	titleByID := make(map[int64]string)
	hashByID := make(map[int64]int64)

	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		for cid, ch := range item.Entities.Channels() {
			if ch == nil {
				continue
			}
			norm := normalizePeerChannelID(cid)
			forumByID[norm] = ch.Forum
			if ch.Title != "" {
				titleByID[norm] = ch.Title
			}
			if ch.AccessHash != 0 {
				hashByID[norm] = ch.AccessHash
				s.peerCache().SaveChannel(norm, ch.AccessHash)
			}
		}
		return nil
	})

	changed := false
	result := make([]forumGroupInfo, 0)
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		norm := normalizePeerChannelID(g.ChatID)
		if isForum, known := forumByID[norm]; known {
			if g.IsForum != isForum {
				g.IsForum = isForum
				changed = true
			}
			if g.Title == "" && titleByID[norm] != "" {
				g.Title = titleByID[norm]
				changed = true
			}
			if g.AccessHash == 0 && hashByID[norm] != 0 {
				g.AccessHash = hashByID[norm]
				changed = true
			}
		}
		if g.IsForum {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				title = g.Target
			}
			result = append(result, forumGroupInfo{
				ChatID:     g.ChatID,
				Title:      title,
				Target:     g.Target,
				AccessHash: g.AccessHash,
			})
		}
	}

	if changed {
		if s.db != nil {
			_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(ctx, bson.M{"_id": ownerID}, bson.M{
				"$set": bson.M{"groups": cfg.Groups, "updated_at": time.Now()},
			}, options.Update().SetUpsert(true))
			s.publishConfigUpdated(ctx, ownerID)
		}
		if s.sqlite != nil {
			raw, _ := json.Marshal(cfg.Groups)
			_, _ = s.sqlite.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET groups = ?, updated_at = CURRENT_TIMESTAMP WHERE owner_id = ?`, string(raw), ownerID)
		}
	}

	encoded, _ := json.Marshal(result)
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{
		ChatID:  chatID,
		OwnerID: ownerID,
		Type:    "forum_groups",
		TaskID:  taskID,
		Text:    string(encoded),
	})
}

func (s *Service) handleFetchGroupTopics(ctx context.Context, client *gobot.Client, chatID, ownerID int64, taskID, groupIDStr string) {
	targetID, err := strconv.ParseInt(strings.TrimSpace(groupIDStr), 10, 64)
	if err != nil || targetID == 0 {
		s.reply(ctx, chatID, 0, `<h1>❌ Grup Tidak Valid</h1><blockquote expandable>ID grup forum tidak valid.</blockquote>`, "panel_notice")
		return
	}
	cfg := s.getJasebConfig(ctx, ownerID)
	var grp jasebGroupDoc
	grp.ChatID = targetID
	grp.Target = groupIDStr
	grp.PeerType = "channel"
	for _, g := range cfg.Groups {
		if sameGroupID(g.ChatID, targetID) {
			grp = g
			break
		}
	}
	targetPeer, err := s.resolveTargetPeer(ctx, client, grp.ChatID, grp.Target, grp.AccessHash, grp.PeerType)
	if err != nil {
		s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ Gagal Memuat Topic</h1><blockquote expandable>Tidak dapat mengakses grup forum: <code>%s</code></blockquote>`, html.EscapeString(err.Error())), "panel_notice")
		return
	}

	res, err := client.API().MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{
		Peer:  targetPeer,
		Limit: 100,
	})
	if err != nil {
		s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ Gagal Mengambil Topic Forum</h1><blockquote expandable>Telegram menolak permintaan topic: <code>%s</code></blockquote>`, html.EscapeString(err.Error())), "panel_notice")
		return
	}

	topics := make([]forumTopicInfo, 0, len(res.Topics))
	for _, item := range res.Topics {
		ft, ok := item.(*tg.ForumTopic)
		if !ok || ft == nil {
			continue
		}
		title := strings.TrimSpace(ft.Title)
		if title == "" {
			if ft.ID == 1 {
				title = "General"
			} else {
				title = fmt.Sprintf("Topic #%d", ft.ID)
			}
		}
		topics = append(topics, forumTopicInfo{
			ID:     ft.ID,
			Title:  title,
			Closed: ft.Closed,
		})
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].ID == 1 {
			return true
		}
		if topics[j].ID == 1 {
			return false
		}
		return topics[i].ID < topics[j].ID
	})

	groupTitle := strings.TrimSpace(grp.Title)
	if groupTitle == "" {
		groupTitle = grp.Target
	}
	encoded, _ := json.Marshal(forumTopicsPayload{
		ChatID:     grp.ChatID,
		GroupTitle: groupTitle,
		Topics:     topics,
	})
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{
		ChatID:  chatID,
		OwnerID: ownerID,
		Type:    "forum_topics",
		TaskID:  taskID,
		Text:    string(encoded),
	})
}

func (s *Service) handleCacheTopicListPost(ctx context.Context, client *gobot.Client, chatID, ownerID int64, listName, text string, entities []models.MessageEntity, mediaIDs []int, sourceCount int) {
	cfg := s.getJasebConfig(ctx, ownerID)
	listIdx := -1
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == listName {
			listIdx = i
			break
		}
	}
	if listIdx < 0 {
		s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ List Topic Tidak Ditemukan</h1><blockquote expandable>List Topic <code>%s</code> sudah dihapus atau tidak tersedia.</blockquote>`, html.EscapeString(listName)), "panel_notice")
		return
	}
	if len(cfg.TopicLists[listIdx].Posts) >= 20 {
		s.reply(ctx, chatID, 0, `<h1>⚠️ Batas Post Tercapai</h1><blockquote expandable>Maksimal <b>20 post</b> per List Topic. Hapus post lama terlebih dahulu.</blockquote>`, "panel_notice")
		return
	}

	topicID := getTeksCacheTopicID()
	cachedMsgID := 0
	var err error
	if sourceCount > 0 {
		mediaIDs, err = s.relayPanelMediaToCache(ctx, client, sourceCount, topicID)
		if err != nil {
			s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ Gagal Menyimpan Media</h1><blockquote expandable><code>%s</code></blockquote>`, html.EscapeString(err.Error())), "panel_notice")
			return
		}
	}
	mediaTypeLabel := "Teks"
	if len(mediaIDs) > 0 {
		cachedMsgID = mediaIDs[0]
		if len(mediaIDs) > 1 {
			s.saveAlbumMapping(ctx, cachedMsgID, mediaIDs)
			mediaTypeLabel = fmt.Sprintf("Album (%d media)", len(mediaIDs))
		} else {
			mediaTypeLabel = "Media Single"
		}
	} else {
		cachedMsgID, err = s.sendTextToAiotdCachesTopic(ctx, client, topicID, text, toMTProtoEntities(entities))
		mediaTypeLabel = "Teks"
	}
	if err != nil || cachedMsgID <= 0 {
		errText := "ID pesan cache tidak valid"
		if err != nil {
			errText = err.Error()
		}
		s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ Gagal Menyimpan Post</h1><blockquote expandable><code>%s</code></blockquote>`, html.EscapeString(errText)), "panel_notice")
		return
	}

	cfg.TopicLists[listIdx].Posts = append(cfg.TopicLists[listIdx].Posts, cachedMsgID)
	s.saveTopicLists(ctx, ownerID, cfg.TopicLists)

	report := fmt.Sprintf(`<h1>✅ Post List Topic Disimpan</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>📂 List Topic</td><td><code>%s</code></td></tr>
  <tr><td>📦 Tipe Materi</td><td>%s</td></tr>
  <tr><td>📄 Total Post</td><td><code>%d/20 post</code></td></tr>
  <tr><td>💾 Cache ID</td><td><code>#%d</code></td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:posts:%s" style="primary">🔍 Lihat Post</tg-button>
  <tg-button type="callback_data" data="panel:tlist:open:%s">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`, html.EscapeString(listName), mediaTypeLabel, len(cfg.TopicLists[listIdx].Posts), cachedMsgID, listName, listName)
	s.reply(ctx, chatID, 0, report, "panel_notice")
}

func (s *Service) handleTopicListPosts(ctx context.Context, client *gobot.Client, chatID, ownerID int64, listName string) {
	cfg := s.getJasebConfig(ctx, ownerID)
	var tl *jasebTopicListDoc
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == listName {
			tl = &cfg.TopicLists[i]
			break
		}
	}
	if tl == nil {
		s.reply(ctx, chatID, 0, fmt.Sprintf(`<h1>❌ List Topic Tidak Ditemukan</h1><blockquote expandable>List Topic <code>%s</code> tidak ditemukan.</blockquote>`, html.EscapeString(listName)), "panel_notice")
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>🔍 Post List Topic %s</h1>\n", html.EscapeString(tl.Name))
	fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>📂 List Topic</th><th>📄 Total Post</th></tr>\n  <tr><td><code>%s</code></td><td><code>%d/20</code></td></tr>\n</table>\n", html.EscapeString(tl.Name), len(tl.Posts))

	if len(tl.Posts) == 0 {
		sb.WriteString("<blockquote expandable>Belum ada post pada List Topic ini. Klik <b>➕ Tambah Post</b> untuk menambahkan materi.</blockquote>\n")
	} else {
		var batch map[int]*tg.Message
		if cachePeer, err := s.resolveCachePeer(ctx, client); err == nil {
			batch = s.fetchCacheMessagesBatch(ctx, client, cachePeer, tl.Posts)
		}
		sb.WriteString("<blockquote expandable>")
		for idx, msgID := range tl.Posts {
			tag := "💬 [Teks] "
			message := batch[msgID]
			if album := s.getAlbumMapping(ctx, msgID); len(album) > 1 {
				tag = fmt.Sprintf("🖼️ [Album %d] ", len(album))
			} else if message != nil && message.Media != nil {
				switch message.Media.(type) {
				case *tg.MessageMediaPhoto:
					tag = "📷 [Foto] "
				case *tg.MessageMediaDocument:
					tag = "📄 [Media] "
				default:
					tag = "📦 [Media] "
				}
			}
			content := ""
			if message != nil && strings.TrimSpace(message.Message) != "" {
				content = message.Message
			}
			words := strings.Fields(content)
			if len(words) > 8 {
				content = strings.Join(words[:8], " ") + "…"
			}
			if content == "" {
				content = "<i>(Tanpa caption)</i>"
			} else {
				content = html.EscapeString(content)
			}
			fmt.Fprintf(&sb, "%d. %s%s\n", idx+1, tag, content)
		}
		sb.WriteString("</blockquote>\n")
		for start := 0; start < len(tl.Posts); start += 5 {
			sb.WriteString("<tg-button-row>\n")
			for i := start; i < start+5 && i < len(tl.Posts); i++ {
				fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="panel:tlist:postdetail:%s:%d">%d</tg-button>`+"\n", tl.Name, i, i+1)
			}
			sb.WriteString("</tg-button-row>\n")
		}
	}
	fmt.Fprintf(&sb, "<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:addpost:%s\" style=\"primary\">➕ Tambah Post</tg-button>\n</tg-button-row>\n", tl.Name)
	fmt.Fprintf(&sb, "<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:open:%s\">◀️ Kembali ◀️</tg-button>\n</tg-button-row>\n", tl.Name)
	sb.WriteString("<hr/>\n<p>GotdBot • Topic List Manager</p>")

	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{
		ChatID:  chatID,
		OwnerID: ownerID,
		Type:    "tlist_posts",
		TaskID:  tl.Name,
		Text:    sb.String(),
	})
}

func (s *Service) handleTopicListPostDetail(ctx context.Context, client *gobot.Client, chatID, ownerID int64, listName, idxStr string) {
	idx, err := strconv.Atoi(strings.TrimSpace(idxStr))
	if err != nil || idx < 0 {
		s.reply(ctx, chatID, 0, `<h1>❌ Index Post Tidak Valid</h1><blockquote expandable>Nomor post tidak valid.</blockquote>`, "panel_notice")
		return
	}
	cfg := s.getJasebConfig(ctx, ownerID)
	var tl *jasebTopicListDoc
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == listName {
			tl = &cfg.TopicLists[i]
			break
		}
	}
	if tl == nil || idx >= len(tl.Posts) {
		s.reply(ctx, chatID, 0, `<h1>❌ Post Tidak Ditemukan</h1><blockquote expandable>Post sudah dihapus atau tidak tersedia.</blockquote>`, "panel_notice")
		return
	}
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		s.reply(ctx, chatID, 0, `<h1>❌ Gagal Akses Cache</h1><blockquote expandable>Userbot tidak dapat terhubung ke channel cache.</blockquote>`, "panel_notice")
		return
	}
	msgID := tl.Posts[idx]
	message, err := s.fetchCacheMessage(ctx, client, cachePeer, msgID)
	if err != nil || message == nil {
		s.reply(ctx, chatID, 0, `<h1>❌ Gagal Memuat Post</h1><blockquote expandable>Detail post tidak dapat diambil dari cache.</blockquote>`, "panel_notice")
		return
	}

	displayContent := "<i>(Tidak ada teks / caption)</i>"
	if strings.TrimSpace(message.Message) != "" {
		displayContent = convertEntitiesToHTML(message.Message, message.Entities)
	}
	mediaType := "Teks"
	var mediaCacheIDs []int
	if message.Media != nil {
		if album := s.getAlbumMapping(ctx, msgID); len(album) > 1 {
			mediaCacheIDs = album
			mediaType = fmt.Sprintf("Album (%d Media)", len(album))
		} else {
			mediaCacheIDs = []int{msgID}
			switch message.Media.(type) {
			case *tg.MessageMediaPhoto:
				mediaType = "Foto"
			case *tg.MessageMediaDocument:
				mediaType = "Media"
			default:
				mediaType = "Media"
			}
		}
	}
	cacheGroup := strings.TrimPrefix(strings.TrimSpace(os.Getenv("GROUP_CACHE")), "@")
	if cacheGroup == "" {
		cacheGroup = "AiotdCaches"
	}
	var mediaTags strings.Builder
	for _, mid := range mediaCacheIDs {
		fmt.Fprintf(&mediaTags, `<img src="https://t.me/%s/%d"/>`+"\n", cacheGroup, mid)
	}

	text := fmt.Sprintf(`%s<h1>🔍 Detail Post #%d — List %s</h1>
<blockquote expandable>%s</blockquote>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>📂 List Topic</td><td><code>%s</code></td></tr>
  <tr><td>📦 Tipe</td><td>%s</td></tr>
  <tr><td>💾 Cache ID</td><td><code>#%d</code></td></tr>
</table>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:delpost:%s:%d" style="danger">🗑️ Hapus Post</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:posts:%s">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`, mediaTags.String(), idx+1, html.EscapeString(tl.Name), displayContent, html.EscapeString(tl.Name), mediaType, msgID, tl.Name, idx, tl.Name)

	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{
		ChatID:  chatID,
		OwnerID: ownerID,
		Type:    "tlist_post_detail",
		TaskID:  tl.Name,
		Text:    text,
	})
}

type topicPlayTracker struct {
	mu          sync.Mutex
	seqIndex    map[string]int
	shuffleDeck map[string][]int
	rng         *rand.Rand
}

func newTopicPlayTracker() *topicPlayTracker {
	return &topicPlayTracker{
		seqIndex:    make(map[string]int),
		shuffleDeck: make(map[string][]int),
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (t *topicPlayTracker) nextPosts(tl jasebTopicListDoc, scopeKey string) []int {
	if len(tl.Posts) == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	key := tl.Name + ":" + scopeKey
	switch tl.PlayMode {
	case "all":
		limit := len(tl.Posts)
		if limit > 20 {
			limit = 20
		}
		out := make([]int, limit)
		copy(out, tl.Posts[:limit])
		return out
	case "shuffle_free":
		return []int{tl.Posts[t.rng.Intn(len(tl.Posts))]}
	case "shuffle_even":
		deck := t.shuffleDeck[key]
		if len(deck) == 0 {
			deck = make([]int, len(tl.Posts))
			copy(deck, tl.Posts)
			t.rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
		}
		pick := deck[0]
		t.shuffleDeck[key] = deck[1:]
		return []int{pick}
	default:
		idx := t.seqIndex[key] % len(tl.Posts)
		t.seqIndex[key] = (idx + 1) % len(tl.Posts)
		return []int{tl.Posts[idx]}
	}
}

type groupTopicDelivery struct {
	TopicID   int
	TopicList *jasebTopicListDoc
}

func resolveGroupTopicDeliveries(cfg jasebConfigDoc, grp jasebGroupDoc) []groupTopicDelivery {
	linkedByTopic := make(map[int]*jasebTopicListDoc)
	for i := range cfg.TopicLists {
		tl := &cfg.TopicLists[i]
		for _, lk := range tl.LinkedTopics {
			if sameGroupID(lk.ChatID, grp.ChatID) {
				linkedByTopic[lk.TopicID] = tl
			}
		}
	}

	seen := make(map[int]bool)
	plan := make([]groupTopicDelivery, 0)
	for _, t := range cfg.Topics {
		if sameGroupID(t.ChatID, grp.ChatID) && t.TopicID > 0 && !seen[t.TopicID] {
			seen[t.TopicID] = true
			plan = append(plan, groupTopicDelivery{
				TopicID:   t.TopicID,
				TopicList: linkedByTopic[t.TopicID],
			})
		}
	}
	for tid, tl := range linkedByTopic {
		if tid > 0 && !seen[tid] {
			seen[tid] = true
			plan = append(plan, groupTopicDelivery{
				TopicID:   tid,
				TopicList: tl,
			})
		}
	}
	return plan
}

func (s *Service) deliverGroupBroadcast(ctx context.Context, client *gobot.Client, cachePeer, targetPeer tg.InputPeerClass, cfg jasebConfigDoc, grp jasebGroupDoc, mainMsgID int, rotator *topicPlayTracker) bool {
	deliveries := resolveGroupTopicDeliveries(cfg, grp)
	if len(deliveries) == 0 {
		return s.forwardSinglePanelPost(ctx, client, cachePeer, targetPeer, cfg, grp, mainMsgID, 0)
	}

	for i, d := range deliveries {
		if i > 0 {
			timer := time.NewTimer(1 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
		}
		var postIDs []int
		if d.TopicList != nil {
			scopeKey := fmt.Sprintf("%d:%d", normalizePeerChannelID(grp.ChatID), d.TopicID)
			postIDs = rotator.nextPosts(*d.TopicList, scopeKey)
			if len(postIDs) == 0 {
				continue
			}
		} else {
			postIDs = []int{mainMsgID}
		}
		for pi, pid := range postIDs {
			if pi > 0 {
				timer := time.NewTimer(1 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return false
				case <-timer.C:
				}
			}
			if !s.forwardSinglePanelPost(ctx, client, cachePeer, targetPeer, cfg, grp, pid, d.TopicID) {
				return false
			}
		}
	}
	return true
}

func (s *Service) forwardSinglePanelPost(ctx context.Context, client *gobot.Client, cachePeer, targetPeer tg.InputPeerClass, cfg jasebConfigDoc, grp jasebGroupDoc, msgID, topicID int) bool {
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
	if topicID > 0 {
		req.SetTopMsgID(topicID)
	}

	_, err := client.API().MessagesForwardMessages(ctx, req)
	if err != nil {
		log.Printf("[panelBroadcast] forward to %s (%d, topic=%d) error: %v", grp.Title, grp.ChatID, topicID, err)
		if flood, ok := tgerr.AsFloodWait(err); ok && !cfg.SkipFloodWait {
			sleepDur := flood + time.Second
			timer := time.NewTimer(sleepDur)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false
			case <-timer.C:
			}
			_, err = client.API().MessagesForwardMessages(ctx, req)
			if err != nil {
				log.Printf("[panelBroadcast] retry forward to %s (%d, topic=%d) error: %v", grp.Title, grp.ChatID, topicID, err)
			}
		}
	}
	return true
}
