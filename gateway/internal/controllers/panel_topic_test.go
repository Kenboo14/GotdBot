package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

func newTestHandlerWithSQLite(t *testing.T) *Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dm.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return NewHandler(nil, dm)
}

func TestPanelCallback_DeleteGroupRemovesMessageAndData(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 1001
	const msgID = 77

	if err := h.db.AddGroup(ctx, chatID, "43", "Grup 43", 43); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	h.trackDetailMessageIDs(ctx, chatID, []int{msgID})

	update := &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_del_grp",
			Data: "panel:group:delete:0",
			From: botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{
					ID:   msgID,
					Chat: botgotd.Chat{ID: chatID},
				},
			},
		},
	}

	h.PanelCallback(ctx, b, update)

	if got := len(h.db.GetGroups(ctx, chatID)); got != 0 {
		t.Fatalf("expected 0 groups after delete, got %d", got)
	}
	delReq, ok := rpcLog.findRequestByMethod("deleteMessages")
	if !ok {
		t.Fatalf("expected deleteMessages RPC to be called")
	}
	ids, _ := delReq.Body["message_ids"].([]any)
	if len(ids) != 1 || int(ids[0].(float64)) != msgID {
		t.Fatalf("expected deleteMessages for ID %d, got %+v", msgID, delReq.Body["message_ids"])
	}
}

func TestPanelCallback_DeleteListRemovesMessageAndData(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 1002
	const msgID = 88

	if err := h.db.AddListCacheID(ctx, chatID, 555); err != nil {
		t.Fatalf("AddListCacheID: %v", err)
	}
	h.trackDetailMessageIDs(ctx, chatID, []int{msgID})

	update := &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_del_list",
			Data: "panel:list:delete:0",
			From: botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{
					ID:   msgID,
					Chat: botgotd.Chat{ID: chatID},
				},
			},
		},
	}

	h.PanelCallback(ctx, b, update)

	if got := len(h.db.GetLists(ctx, chatID)); got != 0 {
		t.Fatalf("expected 0 lists after delete, got %d", got)
	}
	if rpcLog.countByMethod("deleteMessages") != 1 {
		t.Fatalf("expected deleteMessages to be called once, got %d", rpcLog.countByMethod("deleteMessages"))
	}
}

func TestPanelCallback_DeleteWatermarkRemovesMessageAndClearsConfig(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 1003
	const msgID = 99

	cfg := h.db.GetUserConfig(ctx, chatID)
	cfg.Watermark = "Promo by GotdBot"
	cfg.WatermarkCacheID = 9001
	if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	h.trackDetailMessageIDs(ctx, chatID, []int{msgID})

	update := &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_del_wm",
			Data: "panel:watermark:delete",
			From: botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{
					ID:   msgID,
					Chat: botgotd.Chat{ID: chatID},
				},
			},
		},
	}

	h.PanelCallback(ctx, b, update)

	updated := h.db.GetUserConfig(ctx, chatID)
	if updated.Watermark != "" || updated.WatermarkCacheID != 0 {
		t.Fatalf("expected watermark cleared, got %q / %d", updated.Watermark, updated.WatermarkCacheID)
	}
	if rpcLog.countByMethod("deleteMessages") == 0 {
		t.Fatalf("expected deleteMessages to be called for watermark prompt")
	}
}

func TestParseTopicIndices(t *testing.T) {
	got, err := parseTopicIndices("1 3 5", 10, 10)
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("unexpected parseTopicIndices(1 3 5): %v, err=%v", got, err)
	}
	got, err = parseTopicIndices("2-5", 10, 10)
	if err != nil || len(got) != 4 || got[0] != 2 || got[3] != 5 {
		t.Fatalf("unexpected parseTopicIndices(2-5): %v, err=%v", got, err)
	}
	if _, err := parseTopicIndices("1-11", 12, 10); err == nil {
		t.Fatalf("expected error when exceeding max 10 topics")
	}
	if _, err := parseTopicIndices("0", 5, 10); err == nil {
		t.Fatalf("expected error for out-of-range topic 0")
	}
}

func TestTopicListLifecycleAndRichMessages(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 2001

	h.setPanelState(chatID, "tlist_create_name")
	h.handleTopicPanelInput(ctx, b, chatID, chatID, "tlist_create_name", "Invalid Name!", nil)
	if len(h.db.GetTopicLists(ctx, chatID)) != 0 {
		t.Fatalf("invalid list name should not be created")
	}

	h.handleTopicPanelInput(ctx, b, chatID, chatID, "tlist_create_name", "makanan", nil)
	lists := h.db.GetTopicLists(ctx, chatID)
	if len(lists) != 1 || lists[0].Name != "makanan" {
		t.Fatalf("expected list makanan created, got %+v", lists)
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_mode",
			Data: "panel:tlist:setmode:makanan:shuffle_even",
			From: botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{ID: 50, Chat: botgotd.Chat{ID: chatID}},
			},
		},
	})
	tl := h.db.GetTopicList(ctx, chatID, "makanan")
	if tl == nil || tl.PlayMode != databases.PlayModeShuffleEven {
		t.Fatalf("expected play mode shuffle_even, got %+v", tl)
	}

	links := []databases.TopicLink{{ChatID: -100123, GroupTitle: "Aio Topic", TopicID: 1, TopicTitle: "General"}}
	if err := h.db.LinkTopicsToList(ctx, chatID, "makanan", links); err != nil {
		t.Fatalf("LinkTopicsToList: %v", err)
	}
	tl = h.db.GetTopicList(ctx, chatID, "makanan")
	if len(tl.LinkedTopics) != 1 || tl.LinkedTopics[0].TopicTitle != "General" {
		t.Fatalf("expected linked topic General, got %+v", tl.LinkedTopics)
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_del_tl",
			Data: "panel:tlist:delete:makanan",
			From: botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{ID: 55, Chat: botgotd.Chat{ID: chatID}},
			},
		},
	})
	if len(h.db.GetTopicLists(ctx, chatID)) != 0 {
		t.Fatalf("expected makanan deleted")
	}
	if rpcLog.countByMethod("deleteMessages") != 0 {
		t.Fatalf("expected deleteMessages NOT to be called when deleting list topic, got %d", rpcLog.countByMethod("deleteMessages"))
	}
	editedMsg55 := false
	for _, r := range rpcLog.getRequests() {
		if strings.HasSuffix(r.Path, "/editMessageText") {
			if mid, ok := r.Body["message_id"].(float64); ok && int(mid) == 55 {
				editedMsg55 = true
			}
		}
	}
	if !editedMsg55 {
		t.Fatalf("expected editMessageText on message 55 when deleting list topic")
	}

	_ = h.db.SetGroupTopics(ctx, chatID, -100123, "Aio Topic", []databases.TopicEntry{
		{ChatID: -100123, GroupTitle: "Aio Topic", TopicID: 1, Title: "General"},
	})
	h.CekTopic(ctx, b, chatID, chatID)

	foundRich := false
	for _, r := range rpcLog.getRequests() {
		if strings.HasSuffix(r.Path, "/sendRichMessage") {
			if rm, ok := r.Body["rich_message"].(map[string]any); ok {
				if hStr, _ := rm["html"].(string); strings.Contains(hStr, "Topic Tujuan") && strings.Contains(hStr, "Aio Topic") {
					foundRich = true
				}
			}
		}
	}
	if !foundRich {
		t.Fatalf("expected CekTopic to send full rich message containing Topic Tujuan and Aio Topic")
	}
}

func startMockBroker(t *testing.T) (*broker.Redis, func() []models.PToService) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	var mu sync.Mutex
	var payloads []models.PToService

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil || !strings.HasPrefix(line, "*") {
						return
					}
					n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
					if err != nil || n <= 0 {
						return
					}
					args := make([]string, 0, n)
					for i := 0; i < n; i++ {
						szLine, err := r.ReadString('\n')
						if err != nil || !strings.HasPrefix(szLine, "$") {
							return
						}
						sz, err := strconv.Atoi(strings.TrimSpace(szLine[1:]))
						if err != nil || sz < 0 {
							return
						}
						buf := make([]byte, sz+2)
						if _, err := io.ReadFull(r, buf); err != nil {
							return
						}
						args = append(args, string(buf[:sz]))
					}
					cmd := strings.ToUpper(args[0])
					switch cmd {
					case "HELLO":
						_, _ = c.Write([]byte("-ERR unknown command 'HELLO'\r\n"))
					case "XADD":
						for i := 0; i+1 < len(args); i++ {
							if args[i] == "payload" {
								var p models.PToService
								if json.Unmarshal([]byte(args[i+1]), &p) == nil {
									mu.Lock()
									payloads = append(payloads, p)
									mu.Unlock()
								}
								break
							}
						}
						_, _ = c.Write([]byte("$15\r\n1700000000000-0\r\n"))
					case "SET":
						_, _ = c.Write([]byte("+OK\r\n"))
					case "DEL":
						_, _ = c.Write([]byte(":1\r\n"))
					case "GET":
						_, _ = c.Write([]byte("$-1\r\n"))
					default:
						_, _ = c.Write([]byte("+OK\r\n"))
					}
				}
			}(conn)
		}
	}()

	rdb := redis.NewClient(&redis.Options{
		Addr:                     ln.Addr().String(),
		Protocol:                 2,
		DisableIdentity:          true,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	})
	t.Cleanup(func() {
		_ = rdb.Close()
		_ = ln.Close()
	})
	return &broker.Redis{Client: rdb}, func() []models.PToService {
		mu.Lock()
		defer mu.Unlock()
		out := make([]models.PToService, len(payloads))
		copy(out, payloads)
		return out
	}
}

func setupTopicEditMockServer(t *testing.T) (*botgotd.Bot, *mockRPCLog) {
	t.Helper()
	var nextMsgID atomic.Int64
	nextMsgID.Store(100)
	server, b, rpcLog := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendRichMessage"), strings.HasSuffix(r.URL.Path, "/sendMessage"):
			id := nextMsgID.Add(1)
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"date":1700000000,"chat":{"id":1,"type":"private"}}}`, id)
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":101,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	})
	t.Cleanup(server.Close)
	return b, rpcLog
}

func TestTopicListEditInPlaceFlows(t *testing.T) {
	b, rpcLog := setupTopicEditMockServer(t)
	brk, getPayloads := startMockBroker(t)

	dbPath := filepath.Join(t.TempDir(), "test_edit_flow.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dm.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	h := NewHandler(brk, dm)
	ctx := context.Background()
	const chatID int64 = 3001

	h.ShowListTopic(ctx, b, chatID, chatID, 0)
	baseMsgID := h.getTopicListEditID(chatID)
	if baseMsgID == 0 {
		t.Fatalf("expected ShowListTopic to track message ID")
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_create",
			Data:    "panel:tlist:create",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	h.handleTopicPanelInput(ctx, b, chatID, chatID, h.getPanelState(chatID), "Invalid Name!", nil)
	h.handleTopicPanelInput(ctx, b, chatID, chatID, h.getPanelState(chatID), "promo", nil)
	if h.getPanelState(chatID) != "" {
		t.Fatalf("expected panel state cleared after creating list")
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_addpost",
			Data:    "panel:tlist:addpost:promo",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	if h.getPanelState(chatID) != "tlist_add_post:promo" {
		t.Fatalf("expected tlist_add_post:promo state, got %q", h.getPanelState(chatID))
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_back_open",
			Data:    "panel:tlist:open:promo",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	if h.getPanelState(chatID) != "" {
		t.Fatalf("expected returning via open to clear tlist_add_post state, got %q", h.getPanelState(chatID))
	}

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_addpost_2",
			Data:    "panel:tlist:addpost:promo",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	h.handleTopicPanelInput(ctx, b, chatID, chatID, h.getPanelState(chatID), "Konten baru", nil)
	h.handleTopicServiceResponse(ctx, b, models.PFromService{
		ChatID:   chatID,
		ThreadID: baseMsgID,
		OwnerID:  chatID,
		Type:     "tlist_post_saved",
		TaskID:   "promo",
		Text:     "<h1>✅ Post List Topic Disimpan</h1>",
	})

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_posts",
			Data:    "panel:tlist:posts:promo",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	h.handleTopicServiceResponse(ctx, b, models.PFromService{
		ChatID:   chatID,
		ThreadID: baseMsgID,
		OwnerID:  chatID,
		Type:     "tlist_posts",
		TaskID:   "promo",
		Text:     "<h1>🔍 Post List Topic promo</h1>",
	})

	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_postdetail",
			Data:    "panel:tlist:postdetail:promo:0",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})
	h.handleTopicServiceResponse(ctx, b, models.PFromService{
		ChatID:   chatID,
		ThreadID: baseMsgID,
		OwnerID:  chatID,
		Type:     "tlist_post_detail",
		TaskID:   "promo",
		Text:     "<h1>🔍 Detail Post #1 — List promo</h1>",
	})

	cfg := dm.GetUserConfig(ctx, chatID)
	cfg.TopicLists[0].Posts = append(cfg.TopicLists[0].Posts, 9001)
	if err := dm.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_delpost",
			Data:    "panel:tlist:delpost:promo:0",
			From:    botgotd.User{ID: chatID},
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: baseMsgID, Chat: botgotd.Chat{ID: chatID}}},
		},
	})

	if got := rpcLog.countByMethod("deleteMessages"); got != 0 {
		t.Fatalf("expected 0 deleteMessages during edit-in-place List Topic flows, got %d", got)
	}
	if got := rpcLog.countByMethod("sendRichMessage"); got != 1 {
		t.Fatalf("expected exactly 1 initial sendRichMessage, got %d", got)
	}
	if got := rpcLog.countByMethod("editMessageText"); got < 10 {
		t.Fatalf("expected >= 10 editMessageText calls across flows, got %d", got)
	}

	sentPayloads := getPayloads()
	if len(sentPayloads) != 4 {
		t.Fatalf("expected 4 broker payloads, got %d", len(sentPayloads))
	}
	for i, p := range sentPayloads {
		if p.ThreadID != baseMsgID {
			t.Fatalf("payload %d (%s) expected ThreadID=%d, got %d", i, p.Type, baseMsgID, p.ThreadID)
		}
	}
}

func TestTopicFastPathUsesCachedForumGroupsFromSQLite(t *testing.T) {
	b, rpcLog := setupTopicEditMockServer(t)
	brk, getPayloads := startMockBroker(t)

	dbPath := filepath.Join(t.TempDir(), "test_fastpath.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB := dm.GetSQLiteDB(); sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	h := NewHandler(brk, dm)
	ctx := context.Background()
	const chatID int64 = 4001

	cfg := dm.GetUserConfig(ctx, chatID)
	cfg.Groups = []databases.GroupEntry{
		{ChatID: -100555, Target: "-100555", Title: "Forum Cepat", AccessHash: 999888, PeerType: "channel", IsForum: true},
		{ChatID: -100666, Target: "-100666", Title: "Grup Biasa", AccessHash: 111222, PeerType: "channel", IsForum: false},
	}
	if err := dm.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	h.StartTopicMenu(ctx, b, chatID, chatID)
	if got := h.getPanelState(chatID); got != "topic_select_group" {
		t.Fatalf("expected topic_select_group immediately via fast-path, got %q", got)
	}
	if rpcLog.countByMethod("sendMessage") != 0 {
		t.Fatalf("expected 0 progress sendMessage calls on fast-path, got %d", rpcLog.countByMethod("sendMessage"))
	}
	if rpcLog.countByMethod("sendRichMessage") != 1 {
		t.Fatalf("expected 1 sendRichMessage group list on StartTopicMenu fast-path, got %d", rpcLog.countByMethod("sendRichMessage"))
	}

	sentPayloads := getPayloads()
	if len(sentPayloads) != 1 || sentPayloads[0].TaskID != "bg_refresh_forum" {
		t.Fatalf("expected 1 bg_refresh_forum payload, got %+v", sentPayloads)
	}

	bgGroups, _ := json.Marshal([]topicForumGroup{
		{ChatID: -100555, Title: "Forum Cepat Baru", Target: "-100555", AccessHash: 999888},
	})
	h.handleTopicServiceResponse(ctx, b, models.PFromService{
		ChatID:  chatID,
		OwnerID: chatID,
		Type:    "forum_groups",
		TaskID:  "bg_refresh_forum",
		Text:    string(bgGroups),
	})
	if rpcLog.countByMethod("sendRichMessage") != 1 {
		t.Fatalf("expected bg_refresh_forum not to send extra messages")
	}

	h.StartLinkTopic(ctx, b, chatID, chatID, "promo")
	if got := h.getPanelState(chatID); got != "tlist_link_group" {
		t.Fatalf("expected tlist_link_group immediately via fast-path, got %q", got)
	}
	if rpcLog.countByMethod("sendRichMessage") != 2 {
		t.Fatalf("expected StartLinkTopic to send group select message immediately, got %d", rpcLog.countByMethod("sendRichMessage"))
	}
	if len(getPayloads()) != 1 {
		t.Fatalf("expected no duplicate bg_refresh_forum within 5m cooldown, got %d", len(getPayloads()))
	}
}
