package userbot

import (
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

func TestDialogKind_SkipsDeletedUsers(t *testing.T) {
	entities := tg.Entities{
		Users: map[int64]*tg.User{
			101: {ID: 101, Deleted: true},
			102: {ID: 102, Bot: true},
			103: {ID: 103},
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
}
