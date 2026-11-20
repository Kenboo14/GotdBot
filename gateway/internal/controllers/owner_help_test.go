package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestOwnerHelpPagesAndPagination(t *testing.T) {
	total := len(ownerHelpPages)
	if total != 4 {
		t.Fatalf("expected 4 owner help pages, got %d", total)
	}
	for page := 1; page <= total; page++ {
		text, markup := renderOwnerHelpPage(page)
		assertMonitorCardLayout(t, ownerHelpPages[page-1].TabLabel, text)

		rm, ok := buildStripedRichMessage(text)
		if !ok || rm == nil {
			t.Fatalf("page %d buildStripedRichMessage failed", page)
		}
		tableChecked := false
		for _, blk := range rm.Blocks {
			if tbl, isTbl := blk.(*tg.PageBlockTable); isTbl {
				tableChecked = true
				if !tbl.Striped || tbl.Bordered || !tbl.Compact {
					t.Fatalf("page %d table style mismatch: striped=%v bordered=%v compact=%v", page, tbl.Striped, tbl.Bordered, tbl.Compact)
				}
			}
		}
		if !tableChecked {
			t.Fatalf("page %d missing PageBlockTable", page)
		}

		var hasPrev, hasNext bool
		for _, row := range markup.InlineKeyboard {
			for _, btn := range row {
				if len(btn.CallbackData) > 64 {
					t.Fatalf("callback_data exceeds 64 bytes: %q", btn.CallbackData)
				}
				if btn.Text == "⬅️ Prev" {
					hasPrev = true
				}
				if btn.Text == "Next ➡️" {
					hasNext = true
				}
			}
		}
		if page == 1 && (hasPrev || !hasNext) {
			t.Fatalf("page 1 pagination flags invalid: hasPrev=%v hasNext=%v", hasPrev, hasNext)
		}
		if page == total && (!hasPrev || hasNext) {
			t.Fatalf("last page pagination flags invalid: hasPrev=%v hasNext=%v", hasPrev, hasNext)
		}
		if page > 1 && page < total && (!hasPrev || !hasNext) {
			t.Fatalf("middle page %d pagination flags invalid: hasPrev=%v hasNext=%v", page, hasPrev, hasNext)
		}
	}
}

func TestHelpCommandOwnerVsNonOwnerAndThreadID(t *testing.T) {
	t.Setenv("OWNER", "9001")
	h := NewHandler(nil, nil)

	userSrv, userBot, userLog := setupMockTelegramServer(t, nil)
	h.HelpCommandMenu(context.Background(), userBot, &botgotd.Update{
		Message: &botgotd.Message{
			Chat: botgotd.Chat{ID: 9002, Type: "private"},
			From: &botgotd.User{ID: 9002},
			Text: "/help",
		},
	})
	userSrv.Close()
	userMsgs := extractRPCMessages(userLog)
	if len(userMsgs) != 1 || userMsgs[0] != helpInitialPromptText {
		t.Fatalf("expected non-owner /help initial prompt, got %+v", userMsgs)
	}

	ownerSrv, ownerBot, ownerLog := setupMockTelegramServer(t, nil)
	h.HelpCommandMenu(context.Background(), ownerBot, &botgotd.Update{
		Message: &botgotd.Message{
			Chat:            botgotd.Chat{ID: -1005551234, Type: "supergroup"},
			From:            &botgotd.User{ID: 9001},
			MessageThreadID: 8888,
			Text:            "/help",
		},
	})
	ownerSrv.Close()
	reqs := ownerLog.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 RPC for owner /help, got %d", len(reqs))
	}
	if reqs[0].Body["message_thread_id"] != float64(8888) {
		t.Fatalf("expected message_thread_id 8888, got %#v", reqs[0].Body["message_thread_id"])
	}
	rm, _ := reqs[0].Body["reply_markup"].(map[string]any)
	if rm == nil || rm["inline_keyboard"] == nil || rm["keyboard"] != nil {
		t.Fatalf("expected inline_keyboard only in reply_markup, got %#v", reqs[0].Body["reply_markup"])
	}
	ownerMsgs := extractRPCMessages(ownerLog)
	if len(ownerMsgs) != 1 {
		t.Fatalf("expected 1 message text, got %d", len(ownerMsgs))
	}
	assertMonitorCardLayout(t, "ownerHelp", ownerMsgs[0])

	ownerUserSrv, ownerUserBot, ownerUserLog := setupMockTelegramServer(t, nil)
	h.HelpCommandMenu(context.Background(), ownerUserBot, &botgotd.Update{
		Message: &botgotd.Message{
			Chat: botgotd.Chat{ID: 9001, Type: "private"},
			From: &botgotd.User{ID: 9001},
			Text: "/help user",
		},
	})
	h.HelpCommandMenu(context.Background(), ownerUserBot, &botgotd.Update{
		Message: &botgotd.Message{
			Chat: botgotd.Chat{ID: 9001, Type: "private"},
			From: &botgotd.User{ID: 9001},
			Text: "/helper",
		},
	})
	ownerUserSrv.Close()
	ownerUserMsgs := extractRPCMessages(ownerUserLog)
	if len(ownerUserMsgs) != 1 || ownerUserMsgs[0] != helpInitialPromptText {
		t.Fatalf("expected /help user prompt only and /helper ignored, got %+v", ownerUserMsgs)
	}
}

func TestOwnerHelpCallbackPaginationAndAuth(t *testing.T) {
	t.Setenv("OWNER", "9001")
	h := NewHandler(nil, nil)
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h.OwnerHelpCallback(context.Background(), b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID: "cb_non_owner", From: botgotd.User{ID: 9002}, Data: "owner_help:page:2",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 10, Chat: botgotd.Chat{ID: 9002}}},
		},
	})
	if rpcLog.countByMethod("answerCallbackQuery") != 1 || rpcLog.countByMethod("editMessageText") != 0 {
		t.Fatalf("expected non-owner callback ACK with show_alert only, got ack=%d edit=%d",
			rpcLog.countByMethod("answerCallbackQuery"), rpcLog.countByMethod("editMessageText"))
	}
	nonOwnerReqs := rpcLog.getRequests()
	if showAlert, _ := nonOwnerReqs[0].Body["show_alert"].(bool); !showAlert {
		t.Fatalf("expected show_alert=true for non-owner click")
	}

	h.OwnerHelpCallback(context.Background(), b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID: "cb_owner_page2", From: botgotd.User{ID: 9001}, Data: "owner_help:page:2",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 10, Chat: botgotd.Chat{ID: -1005551234}}},
		},
	})
	if rpcLog.countByMethod("editMessageText") != 1 {
		t.Fatalf("expected 1 editMessageText for owner page 2, got %d", rpcLog.countByMethod("editMessageText"))
	}
	allReqs := rpcLog.getRequests()
	lastEdit := allReqs[len(allReqs)-1]
	if txt, _ := lastEdit.Body["text"].(string); !strings.Contains(txt, "Multi-Worker") {
		t.Fatalf("expected page 2 Multi-Worker content, got %q", txt)
	}

	h.OwnerHelpCallback(context.Background(), b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID: "cb_owner_close", From: botgotd.User{ID: 9001}, Data: "owner_help:close",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 10, Chat: botgotd.Chat{ID: -1005551234}}},
		},
	})
	if rpcLog.countByMethod("deleteMessages") != 1 {
		t.Fatalf("expected 1 deleteMessages on owner_help:close, got %d", rpcLog.countByMethod("deleteMessages"))
	}
}

func TestAllMonitorsUseStripedRichCard(t *testing.T) {
	srvText := serverStatusTextRich(9001, 5*time.Millisecond)
	assertMonitorCardLayout(t, "serverStatusTextRich", srvText)
	rm, ok := buildStripedRichMessage(srvText)
	if !ok || rm == nil {
		t.Fatalf("expected buildStripedRichMessage to parse serverStatusTextRich")
	}
	foundTable := false
	for _, blk := range rm.Blocks {
		if tbl, isTbl := blk.(*tg.PageBlockTable); isTbl {
			foundTable = true
			if !tbl.Striped || tbl.Bordered || !tbl.Compact {
				t.Fatalf("expected striped=true bordered=false compact=true on server status table")
			}
		}
	}
	if !foundTable {
		t.Fatalf("expected PageBlockTable in serverStatusTextRich")
	}

	t.Setenv("OWNER", "9001")
	h := NewHandler(nil, nil)
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()
	h.SimulasiCommand(context.Background(), b, &botgotd.Update{
		Message: &botgotd.Message{
			Chat:            botgotd.Chat{ID: -1005551234, Type: "supergroup"},
			From:            &botgotd.User{ID: 9001},
			MessageThreadID: 3333,
			Text:            "/simulasi",
		},
	})
	reqs := rpcLog.getRequests()
	if len(reqs) != 1 || reqs[0].Body["message_thread_id"] != float64(3333) {
		t.Fatalf("expected simulasi reply in thread 3333, got %+v", reqs)
	}
	msgs := extractRPCMessages(rpcLog)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 simulasi message, got %d", len(msgs))
	}
	assertMonitorCardLayout(t, "simulasiHelp", msgs[0])
}
