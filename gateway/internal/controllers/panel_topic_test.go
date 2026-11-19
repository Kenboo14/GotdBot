package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"path/filepath"
	"strings"
	"testing"
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
	if rpcLog.countByMethod("deleteMessages") == 0 {
		t.Fatalf("expected deleteMessages to be called when deleting list topic")
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
