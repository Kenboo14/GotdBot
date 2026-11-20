package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

func TestBuildStripedRichMessage(t *testing.T) {
	rows := []monitorRichRow{
		{Label: "📂 SQLite", Value: "🟢 OK"},
		{Label: "🍃 MongoDB", Value: "🟢 OK"},
	}

	collapsedCard := renderMonitorRichCard("🟢 Database &amp; VPS Health", rows, "ℹ️ Sehat", false)
	rm, ok := buildStripedRichMessage(collapsedCard)
	if !ok || rm == nil {
		t.Fatalf("expected buildStripedRichMessage to succeed")
	}
	var foundHeading, foundTable, foundQuote bool
	for _, blk := range rm.Blocks {
		switch b := blk.(type) {
		case *tg.PageBlockHeading1:
			foundHeading = true
		case *tg.PageBlockTable:
			foundTable = true
			if !b.Striped || b.Bordered || !b.Compact {
				t.Fatalf("expected striped=true bordered=false compact=true, got striped=%v bordered=%v compact=%v", b.Striped, b.Bordered, b.Compact)
			}
		case *tg.PageBlockBlockquote:
			foundQuote = true
			if b.Collapsed {
				t.Fatalf("expected non-expandable quote to have Collapsed=false")
			}
		}
	}
	if !foundHeading || !foundTable || !foundQuote {
		t.Fatalf("missing expected blocks heading=%v table=%v quote=%v", foundHeading, foundTable, foundQuote)
	}

	expCard := renderMonitorRichCard("⚙️ Bot Loader Status", rows, "ℹ️ Detail", true)
	rmExp, ok := buildStripedRichMessage(expCard)
	if !ok || rmExp == nil {
		t.Fatalf("expected expandable card parse to succeed")
	}
	expQuoteOK := false
	for _, blk := range rmExp.Blocks {
		if b, isQuote := blk.(*tg.PageBlockBlockquote); isQuote {
			expQuoteOK = b.Collapsed
		}
	}
	if !expQuoteOK {
		t.Fatalf("expected expandable blockquote to have Collapsed=true")
	}
}

func TestResolveMonitorChatTarget(t *testing.T) {
	server, b, _ := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getChat") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":-1001234567890,"type":"supergroup"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	defer server.Close()

	got := resolveMonitorChatTarget(context.Background(), b, "@devgroup")
	id, ok := got.(int64)
	if !ok || id != -1001234567890 {
		t.Fatalf("expected resolved chat id -1001234567890, got %T (%v)", got, got)
	}
}

func TestObserveIncomingChatUpgradesUsername(t *testing.T) {
	disp := newMonitorDispatcher(defaultMonitorDispatcherConfig(), nil, "@devgroup", 5)
	dm := &devMonitor{
		chatTarget:        "@devgroup",
		rawTargetUsername: "devgroup",
		fallbackTop:       5,
		dispatcher:        disp,
		topics:            make(map[string]int),
	}
	dm.ObserveIncomingChat(botgotd.Chat{ID: -1009876543210, Username: "DevGroup"})
	dm.mu.Lock()
	targetID := parseMonitorChatID(dm.chatTarget)
	dm.mu.Unlock()
	if targetID != -1009876543210 {
		t.Fatalf("expected chatTarget upgraded to -1009876543210, got %v", dm.chatTarget)
	}
	disp.mu.Lock()
	dispID := parseMonitorChatID(disp.chatTarget)
	disp.mu.Unlock()
	if dispID != -1009876543210 {
		t.Fatalf("expected dispatcher chatTarget upgraded to -1009876543210, got %d", dispID)
	}
}

func TestSendMonitorRichMessagePreservesThreadID(t *testing.T) {
	ctx := context.Background()
	card := renderMonitorRichCard("🟢 Database &amp; VPS Health", []monitorRichRow{
		{Label: "📂 SQLite", Value: "🟢 OK"},
	}, "ℹ️ Sehat", false)

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	msg, err := sendMonitorRichMessage(ctx, b, int64(-1005551234), 4242, card)
	server.Close()
	if err != nil || msg == nil {
		t.Fatalf("expected sendMonitorRichMessage success, got msg=%+v err=%v", msg, err)
	}
	reqs := rpcLog.getRequests()
	if len(reqs) != 1 || !strings.HasSuffix(reqs[0].Path, "/sendRichMessage") {
		t.Fatalf("expected 1 /sendRichMessage RPC, got %+v", reqs)
	}
	if reqs[0].Body["message_thread_id"] != float64(4242) {
		t.Fatalf("expected message_thread_id 4242, got %#v", reqs[0].Body["message_thread_id"])
	}
	rm, ok := reqs[0].Body["rich_message"].(map[string]any)
	if !ok || rm["html"] == "" {
		t.Fatalf("expected rich_message.html payload, got %#v", reqs[0].Body["rich_message"])
	}

	fbServer, fbBot, fbLog := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendRichMessage") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Not Found"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99}}`))
	})
	defer fbServer.Close()

	msgFB, err := sendMonitorRichMessage(ctx, fbBot, int64(-1005551234), 4242, card)
	if err != nil || msgFB == nil || msgFB.ID != 99 {
		t.Fatalf("expected fallback sendMessage id=99, got msg=%+v err=%v", msgFB, err)
	}
	fbReqs := fbLog.getRequests()
	if len(fbReqs) != 2 || !strings.HasSuffix(fbReqs[1].Path, "/sendMessage") {
		t.Fatalf("expected fallback /sendMessage RPC after 404, got %+v", fbReqs)
	}
	if fbReqs[1].Body["message_thread_id"] != float64(4242) {
		t.Fatalf("expected fallback message_thread_id 4242, got %#v", fbReqs[1].Body["message_thread_id"])
	}
	if fbReqs[1].Body["parse_mode"] != "HTML" {
		t.Fatalf("expected fallback parse_mode HTML, got %#v", fbReqs[1].Body["parse_mode"])
	}
}

func TestWorkerCommandPreservesForumThreadID(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	t.Setenv("OWNER", "9001")
	h := NewHandler(nil, nil)
	h.WorkerCommand(context.Background(), b, &botgotd.Update{
		Message: &botgotd.Message{
			Chat:            botgotd.Chat{ID: -1005551234, Type: "supergroup"},
			From:            &botgotd.User{ID: 9001},
			MessageThreadID: 7777,
			Text:            "/worker status",
		},
	})

	reqs := rpcLog.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 RPC from WorkerCommand, got %d", len(reqs))
	}
	if reqs[0].Body["message_thread_id"] != float64(7777) {
		t.Fatalf("expected message_thread_id 7777, got %#v", reqs[0].Body["message_thread_id"])
	}
	msgs := extractRPCMessages(rpcLog)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 extracted rich message, got %d", len(msgs))
	}
	assertMonitorCardLayout(t, "workerForumTopic", msgs[0])
}
