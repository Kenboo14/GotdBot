package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestManualGroupTokensAndSyncedMerge(t *testing.T) {
	tokens := ParseManualGroupTokens("-1001234567890\n@grup_promo, https://t.me/+SecretInvite123\n-987654\n@grup_promo")
	if len(tokens) != 4 {
		t.Fatalf("expected 4 unique tokens, got %d (%v)", len(tokens), tokens)
	}

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const ownerID int64 = 7771

	if err := h.db.AddGroup(ctx, ownerID, "-100555666777", "Grup -100555666777", -100555666777); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	payload, err := json.Marshal([]databases.GroupEntry{
		{
			Target:     "-100555666777",
			Title:      "Forum Komunitas",
			ChatID:     -100555666777,
			AccessHash: 998877,
			PeerType:   "channel",
			IsForum:    true,
		},
		{
			Target:     "-100999111222",
			Title:      "Grup Auto Join",
			ChatID:     -100999111222,
			AccessHash: 445566,
			PeerType:   "channel",
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	h.applySyncedManualGroups(ctx, ownerID, string(payload))

	groups := h.db.GetGroups(ctx, ownerID)
	if len(groups) != 2 {
		t.Fatalf("expected 2 merged groups, got %d (%+v)", len(groups), groups)
	}
	if groups[0].Title != "Forum Komunitas" || groups[0].AccessHash != 998877 || !groups[0].IsForum {
		t.Fatalf("expected first group metadata updated, got %+v", groups[0])
	}
	if groups[1].ChatID != -100999111222 || groups[1].DisplayTarget() != "-100999111222" {
		t.Fatalf("expected second group normalized with -100, got %+v", groups[1])
	}
}

func TestCekGrupDisplaysNormalizedSupergroupID(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 5501

	cfg := h.db.GetUserConfig(ctx, chatID)
	cfg.Groups = []databases.GroupEntry{
		{Target: "2345678901", Title: "Lapak Resmi", ChatID: 2345678901, PeerType: "channel"},
		{Target: "987654", Title: "Grup Lama", ChatID: -987654, PeerType: "chat"},
	}
	if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	h.CekGrup(ctx, b, chatID, chatID)

	msgs := extractRPCMessages(rpcLog)
	if len(msgs) == 0 {
		t.Fatalf("expected RPC message to be sent")
	}
	combined := strings.Join(msgs, "\n")
	if !strings.Contains(combined, "-1002345678901") {
		t.Fatalf("expected CekGrup output to contain -1002345678901, got: %s", combined)
	}
	if !strings.Contains(combined, "-987654") {
		t.Fatalf("expected CekGrup output to contain -987654, got: %s", combined)
	}
}

func TestHandlePanelInput_WaitingManualGroupFallbackWithoutBroker(t *testing.T) {
	server, b, _ := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := newTestHandlerWithSQLite(t)
	ctx := context.Background()
	const chatID int64 = 6601

	h.setPanelState(chatID, "waiting_add_group_manual")
	handled := h.HandlePanelInput(ctx, b, chatID, chatID, "-100888777666\n-444333", &botgotd.Message{ID: 12, Chat: botgotd.Chat{ID: chatID}})
	if !handled {
		t.Fatalf("expected HandlePanelInput to handle waiting_add_group_manual")
	}

	groups := h.db.GetGroups(ctx, chatID)
	if len(groups) != 2 {
		t.Fatalf("expected 2 saved groups, got %d (%+v)", len(groups), groups)
	}
	if groups[0].ChatID != -100888777666 || groups[0].PeerType != "channel" {
		t.Fatalf("unexpected supergroup: %+v", groups[0])
	}
	if groups[1].ChatID != -444333 || groups[1].PeerType != "chat" {
		t.Fatalf("unexpected basic group: %+v", groups[1])
	}
}
