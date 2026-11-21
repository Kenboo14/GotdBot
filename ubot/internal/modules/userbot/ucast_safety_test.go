package userbot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestIsSessionRevokedError_ExcludesTargetUserErrors(t *testing.T) {
	nonRevoked := []error{
		nil,
		errors.New("rpcDoRequest: rpc error code 400: INPUT_USER_DEACTIVATED"),
		errors.New("rpcDoRequest: rpc error code 403: USER_IS_BLOCKED"),
		errors.New("rpcDoRequest: rpc error code 400: USER_IS_BOT"),
		errors.New("rpcDoRequest: rpc error code 403: USER_PRIVACY_RESTRICTED"),
		errors.New("rpcDoRequest: rpc error code 400: USER_ID_INVALID"),
		errors.New("rpcDoRequest: rpc error code 400: PEER_FLOOD"),
	}
	for _, err := range nonRevoked {
		if isSessionRevokedError(err) {
			t.Fatalf("expected non-revoked error for %v", err)
		}
	}

	revoked := []error{
		errors.New("rpcDoRequest: rpc error code 401: AUTH_KEY_UNREGISTERED"),
		errors.New("rpcDoRequest: rpc error code 401: SESSION_REVOKED"),
		errors.New("rpcDoRequest: rpc error code 401: USER_DEACTIVATED"),
	}
	for _, err := range revoked {
		if !isSessionRevokedError(err) {
			t.Fatalf("expected revoked error for %v", err)
		}
	}
}

func TestDialogKind_SkipsDeletedUsersAndInaccessibleChats(t *testing.T) {
	entities := tg.Entities{
		Users: map[int64]*tg.User{
			101: {ID: 101, Deleted: true},
			102: {ID: 102, Bot: true},
			103: {ID: 103},
		},
		Chats: map[int64]*tg.Chat{
			201: {ID: 201, Left: true},
			202: {ID: 202, Deactivated: true},
			203: {ID: 203},
		},
		Channels: map[int64]*tg.Channel{
			301: {ID: 301, Left: true, Megagroup: true},
			302: {ID: 302, Megagroup: true},
			303: {ID: 303}, // read-only broadcast channel
			304: {ID: 304, Creator: true},
			305: {ID: 305, AdminRights: tg.ChatAdminRights{PostMessages: true}},
		},
	}

	if _, _, skip := dialogKind(&tg.InputPeerUser{UserID: 101}, entities, 999); !skip {
		t.Fatalf("expected deleted user 101 to be skipped")
	}
	if _, _, skip := dialogKind(&tg.InputPeerUser{UserID: 102}, entities, 999); !skip {
		t.Fatalf("expected bot user 102 to be skipped")
	}
	if _, _, skip := dialogKind(&tg.InputPeerUser{UserID: 999}, entities, 999); !skip {
		t.Fatalf("expected owner 999 to be skipped")
	}
	kind, id, skip := dialogKind(&tg.InputPeerUser{UserID: 103}, entities, 999)
	if skip || kind != "private" || id != 103 {
		t.Fatalf("expected active private user 103, got kind=%q id=%d skip=%v", kind, id, skip)
	}

	if _, _, skip := dialogKind(&tg.InputPeerChat{ChatID: 201}, entities, 999); !skip {
		t.Fatalf("expected left chat 201 to be skipped")
	}
	if _, _, skip := dialogKind(&tg.InputPeerChat{ChatID: 202}, entities, 999); !skip {
		t.Fatalf("expected deactivated chat 202 to be skipped")
	}
	if kind, id, skip = dialogKind(&tg.InputPeerChat{ChatID: 203}, entities, 999); skip || kind != "group" || id != 203 {
		t.Fatalf("expected active group 203, got kind=%q id=%d skip=%v", kind, id, skip)
	}

	if _, _, skip := dialogKind(&tg.InputPeerChannel{ChannelID: 301}, entities, 999); !skip {
		t.Fatalf("expected left supergroup 301 to be skipped")
	}
	if kind, id, skip = dialogKind(&tg.InputPeerChannel{ChannelID: 302}, entities, 999); skip || kind != "group" || id != 302 {
		t.Fatalf("expected active supergroup 302, got kind=%q id=%d skip=%v", kind, id, skip)
	}
	if _, _, skip := dialogKind(&tg.InputPeerChannel{ChannelID: 303}, entities, 999); !skip {
		t.Fatalf("expected read-only channel 303 to be skipped")
	}
	if kind, id, skip = dialogKind(&tg.InputPeerChannel{ChannelID: 304}, entities, 999); skip || kind != "channel" || id != 304 {
		t.Fatalf("expected creator channel 304, got kind=%q id=%d skip=%v", kind, id, skip)
	}
	if kind, id, skip = dialogKind(&tg.InputPeerChannel{ChannelID: 305}, entities, 999); skip || kind != "channel" || id != 305 {
		t.Fatalf("expected admin-post channel 305, got kind=%q id=%d skip=%v", kind, id, skip)
	}
}

func TestBroadcastJobAcquireReleaseAndCancelPropagation(t *testing.T) {
	accountCtx, accountCancel := context.WithCancel(context.Background())
	defer accountCancel()

	svc := &Service{
		root:        context.Background(),
		owners:      map[int64]int64{10: 999},
		accountCtxs: map[int64]context.Context{10: accountCtx},
		broadcasts:  make(map[int64]*broadcastJob),
	}

	jobCtx, job, ok := svc.acquireBroadcastJob(999)
	if !ok || job == nil || jobCtx == nil {
		t.Fatalf("expected first broadcast job acquisition to succeed")
	}
	if _, _, dupOK := svc.acquireBroadcastJob(999); dupOK {
		t.Fatalf("expected concurrent broadcast acquisition for same owner to be rejected")
	}

	accountCancel()
	select {
	case <-jobCtx.Done():
	case <-time.After(time.Second):
		t.Fatalf("expected account cancellation to cancel acquired broadcast job")
	}

	svc.releaseBroadcastJob(999, job)
	if _, _, reacquiredOK := svc.acquireBroadcastJob(999); !reacquiredOK {
		t.Fatalf("expected broadcast slot to be available after releaseBroadcastJob")
	}
}

func TestIsCancelArgAndClassifyFatalError(t *testing.T) {
	if !isCancelArg("cancel") || !isCancelArg(" STOP ") {
		t.Fatalf("expected cancel/stop args to match")
	}
	if isCancelArg("halo") {
		t.Fatalf("did not expect non-cancel arg to match")
	}
	if _, fatal := classifyBroadcastFatalError(errors.New("rpcDoRequest: rpc error code 400: INPUT_USER_DEACTIVATED")); fatal {
		t.Fatalf("did not expect INPUT_USER_DEACTIVATED to trigger fatal broadcast stop")
	}
	if _, fatal := classifyBroadcastFatalError(errors.New("rpcDoRequest: rpc error code 400: PEER_FLOOD")); !fatal {
		t.Fatalf("expected PEER_FLOOD to trigger fatal broadcast stop")
	}
	if _, fatal := classifyBroadcastFatalError(errors.New("rpcDoRequest: rpc error code 401: SESSION_REVOKED")); !fatal {
		t.Fatalf("expected SESSION_REVOKED to trigger fatal broadcast stop")
	}
}

func TestBroadcastStepDelayAndPeerFlood(t *testing.T) {
	pmDelay := broadcastStepDelay("private")
	if pmDelay < 800*time.Millisecond || pmDelay >= 1200*time.Millisecond {
		t.Fatalf("expected private delay in [800ms, 1200ms), got %v", pmDelay)
	}
	if grpDelay := broadcastStepDelay("group"); grpDelay != 350*time.Millisecond {
		t.Fatalf("expected group delay 350ms, got %v", grpDelay)
	}
	if !isPeerFloodError(errors.New("rpcDoRequest: rpc error code 400: PEER_FLOOD")) {
		t.Fatalf("expected PEER_FLOOD to be detected")
	}
	if isPeerFloodError(errors.New("rpcDoRequest: rpc error code 400: INPUT_USER_DEACTIVATED")) {
		t.Fatalf("did not expect INPUT_USER_DEACTIVATED to be detected as PEER_FLOOD")
	}

	svc := &Service{
		logins:    make(map[int64]*login),
		sudoCache: make(map[string]sudoCacheEntry),
	}
	svc.reapStaleResources()
}
