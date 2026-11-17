package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"
)

func TestUserbotPrefixPersistsAcrossSessionDelete(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlite, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sqlite.Close()

	chatID := int64(10001)
	accountID := int64(20001)

	if err := sqlite.SaveSession(ctx, chatID, chatID, accountID, "encrypted-blob", "!", false); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	store := &sqliteStorage{
		sqlite:    sqlite,
		chatID:    chatID,
		ownerID:   chatID,
		accountID: accountID,
	}
	if err := store.setPrefix(ctx, ".", true); err != nil {
		t.Fatalf("setPrefix failed: %v", err)
	}

	if err := sqlite.DeleteSession(ctx, chatID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	reinstallStore := &sqliteStorage{
		sqlite:  sqlite,
		chatID:  chatID,
		ownerID: chatID,
	}
	pfx, ok := reinstallStore.loadUserbotPrefix(ctx, accountID)
	if !ok || pfx != "." {
		t.Fatalf("expected persisted prefix '.' (ok=true), got %q (ok=%v)", pfx, ok)
	}

	// Simulate fresh install check: calling setPrefix with loaded values keeps "."
	if err := reinstallStore.setPrefix(ctx, pfx, ok); err != nil {
		t.Fatalf("setPrefix after reinstall failed: %v", err)
	}
	st, err := sqlite.GetUserbotSettings(ctx, chatID)
	if err != nil || !st.PrefixSet || st.Prefix != "." {
		t.Fatalf("expected userbot_settings prefix '.', got %+v (err=%v)", st, err)
	}
}

func TestUserbotRelinkUserDataAcrossTokenRecovery(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "relink.db")
	sqlite, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sqlite.Close()

	oldChatID := int64(1111)
	oldAccID := int64(2222)
	newChatID := int64(3333)
	newAccID := int64(4444)

	if err := sqlite.SaveUserbotPrefix(ctx, oldChatID, oldChatID, oldAccID, ",", true); err != nil {
		t.Fatalf("SaveUserbotPrefix failed: %v", err)
	}
	if err := sqlite.AddSudo(ctx, oldAccID, 999); err != nil {
		t.Fatalf("AddSudo failed: %v", err)
	}
	if err := sqlite.AddBlacklist(ctx, oldAccID, "group", 555); err != nil {
		t.Fatalf("AddBlacklist failed: %v", err)
	}
	if err := sqlite.SaveNote(ctx, databases.NoteRecord{OwnerID: oldAccID, Name: "promo", Text: "Halo"}); err != nil {
		t.Fatalf("SaveNote failed: %v", err)
	}
	if err := sqlite.SetLogStatus(ctx, oldAccID, true); err != nil {
		t.Fatalf("SetLogStatus failed: %v", err)
	}
	if err := sqlite.SaveLogRoute(ctx, 777, 888, oldAccID, 123, 456, "user", 42); err != nil {
		t.Fatalf("SaveLogRoute failed: %v", err)
	}

	svc := NewService(ctx, sqlite, nil, nil, 0, "", nil, "", "", 0)
	svc.relinkUserData(ctx, oldChatID, newChatID, oldAccID, newAccID)

	svc.mu.RLock()
	inMemPrefix := svc.prefixes[newChatID]
	svc.mu.RUnlock()
	if inMemPrefix != "," {
		t.Fatalf("expected in-memory prefix ',' for newChatID, got %q", inMemPrefix)
	}

	st, err := sqlite.GetUserbotSettings(ctx, newChatID)
	if err != nil || !st.PrefixSet || st.Prefix != "," {
		t.Fatalf("expected relinked prefix ',', got %+v (err=%v)", st, err)
	}
	if isSudo, err := sqlite.IsSudo(ctx, newAccID, 999); err != nil || !isSudo {
		t.Fatalf("expected sudo 999 relinked to %d (got %v, err=%v)", newAccID, isSudo, err)
	}
	if isBL, err := sqlite.IsBlacklisted(ctx, newAccID, "group", 555); err != nil || !isBL {
		t.Fatalf("expected blacklist 555 relinked to %d (got %v, err=%v)", newAccID, isBL, err)
	}
	if note, err := sqlite.GetNote(ctx, newAccID, "promo"); err != nil || note.Text != "Halo" {
		t.Fatalf("expected note 'promo' relinked to %d (got %+v, err=%v)", newAccID, note, err)
	}
	if enabled, err := sqlite.GetLogStatus(ctx, newAccID); err != nil || !enabled {
		t.Fatalf("expected log status true relinked to %d (got %v, err=%v)", newAccID, enabled, err)
	}
	if route, err := sqlite.GetLogRoute(ctx, 777, 888); err != nil || route == nil || route.UserID != newAccID {
		t.Fatalf("expected log route relinked to user_id=%d, got %+v (err=%v)", newAccID, route, err)
	}
}

func TestResolvePeer_PrivateChatWithControllerAndSQLiteFallback(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "peers.db")
	sqlite, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sqlite.Close()

	chatID := int64(100001)
	ownerID := int64(200002)
	strangerID := int64(300003)

	if err := sqlite.SavePeer(ctx, chatID, "user", 777111); err != nil {
		t.Fatalf("SavePeer chatID failed: %v", err)
	}
	if err := sqlite.SavePeer(ctx, strangerID, "user", 888222); err != nil {
		t.Fatalf("SavePeer strangerID failed: %v", err)
	}

	svc := NewService(ctx, sqlite, nil, nil, 0, "", nil, "", "", 0)
	svc.mu.Lock()
	svc.owners[chatID] = ownerID
	svc.mu.Unlock()

	// Controller user (chatID != ownerID) must resolve to InputPeerUser, not InputPeerSelf
	pController, err := svc.resolvePeer(ctx, nil, chatID, tg.Entities{}, &tg.PeerUser{UserID: chatID}, nil)
	if err != nil {
		t.Fatalf("resolvePeer controller user failed: %v", err)
	}
	if ipu, ok := pController.(*tg.InputPeerUser); !ok || ipu.UserID != chatID || ipu.AccessHash != 777111 {
		t.Fatalf("expected InputPeerUser for controller user %d, got %#v", chatID, pController)
	}

	// Self (ownerID) must resolve to InputPeerSelf
	pSelf, err := svc.resolvePeer(ctx, nil, chatID, tg.Entities{}, &tg.PeerUser{UserID: ownerID}, nil)
	if err != nil {
		t.Fatalf("resolvePeer self failed: %v", err)
	}
	if _, ok := pSelf.(*tg.InputPeerSelf); !ok {
		t.Fatalf("expected InputPeerSelf for ownerID %d, got %#v", ownerID, pSelf)
	}

	// Private chat with another user from SQLite fallback when UpdateShortMessage entities are empty
	pStranger, err := svc.resolvePeer(ctx, nil, chatID, tg.Entities{}, &tg.PeerUser{UserID: strangerID}, &tg.Message{ID: 55})
	if err != nil {
		t.Fatalf("resolvePeer stranger failed: %v", err)
	}
	if ipu, ok := pStranger.(*tg.InputPeerUser); !ok || ipu.UserID != strangerID || ipu.AccessHash != 888222 {
		t.Fatalf("expected InputPeerUser for stranger %d, got %#v", strangerID, pStranger)
	}
}

func TestRevokeExpiredSessionDeletesSessionPreservesSettings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "revoke.db")
	sqlite, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sqlite.Close()

	chatID := int64(50001)
	accID := int64(60001)
	if err := sqlite.SaveSession(ctx, chatID, chatID, accID, "blob", ".", true); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	if err := sqlite.SaveUserbotPrefix(ctx, chatID, chatID, accID, ".", true); err != nil {
		t.Fatalf("SaveUserbotPrefix failed: %v", err)
	}

	svc := NewService(ctx, sqlite, nil, nil, 0, "", nil, "", "", 0)
	_, cancel := context.WithCancel(ctx)
	canceled := false
	svc.mu.Lock()
	svc.owners[chatID] = accID
	svc.accountGens[chatID] = 1
	svc.activeCancels[chatID] = func() {
		canceled = true
		cancel()
	}
	svc.mu.Unlock()

	svc.revokeExpiredSession(ctx, chatID, accID, "17-11-2026 12:00:00")

	if !canceled {
		t.Fatalf("expected active client context to be canceled")
	}
	has, err := sqlite.HasSession(ctx, chatID, accID)
	if err != nil || has {
		t.Fatalf("expected session deleted after expiry, has=%v err=%v", has, err)
	}
	st, err := sqlite.GetUserbotSettings(ctx, chatID)
	if err != nil || st == nil || !st.PrefixSet || st.Prefix != "." {
		t.Fatalf("expected userbot_settings preserved after expiry, got %+v (err=%v)", st, err)
	}
}
