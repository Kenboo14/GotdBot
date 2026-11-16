package botgotd

import (
	"testing"
)

func TestPeerStorage_Deduplication(t *testing.T) {
	ps := NewPeerStorage(nil)

	// User deduplication
	ps.SaveUser(1001, 12345)
	ps.mu.RLock()
	hash, ok := ps.users[1001]
	ps.mu.RUnlock()
	if !ok || hash != 12345 {
		t.Fatalf("expected hash 12345, got %d", hash)
	}

	// Repeated save with same access hash (deduplication check)
	ps.SaveUser(1001, 12345)
	ps.mu.RLock()
	hash = ps.users[1001]
	ps.mu.RUnlock()
	if hash != 12345 {
		t.Fatalf("expected hash 12345, got %d", hash)
	}

	// Update with new access hash
	ps.SaveUser(1001, 99999)
	ps.mu.RLock()
	hash = ps.users[1001]
	ps.mu.RUnlock()
	if hash != 99999 {
		t.Fatalf("expected updated hash 99999, got %d", hash)
	}

	// Channel deduplication
	ps.SaveChannel(2001, 54321)
	ps.mu.RLock()
	cHash, ok := ps.chats[2001]
	ps.mu.RUnlock()
	if !ok || cHash != 54321 {
		t.Fatalf("expected chat hash 54321, got %d", cHash)
	}

	ps.SaveChannel(2001, 54321)
	ps.mu.RLock()
	cHash = ps.chats[2001]
	ps.mu.RUnlock()
	if cHash != 54321 {
		t.Fatalf("expected chat hash 54321, got %d", cHash)
	}

	// Invalid IDs
	ps.SaveUser(0, 111)
	ps.SaveUser(-5, 222)
	ps.SaveChannel(0, 333)
	ps.SaveChannel(-10, 444)
	ps.mu.RLock()
	if len(ps.users) != 1 || len(ps.chats) != 1 {
		t.Errorf("invalid IDs should not be saved: users=%d, chats=%d", len(ps.users), len(ps.chats))
	}
	ps.mu.RUnlock()
}
