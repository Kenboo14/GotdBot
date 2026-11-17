package botgotd

import (
	"testing"
)

func TestPeerStorage_Deduplication(t *testing.T) {
	ps := NewPeerStorage(nil)

	// User deduplication
	ps.SaveUser(1001, 12345)
	hash, ok := ps.UserHash(1001)
	if !ok || hash != 12345 {
		t.Fatalf("expected hash 12345, got %d (ok=%v)", hash, ok)
	}

	// Repeated save with same access hash (deduplication check)
	ps.SaveUser(1001, 12345)
	hash, _ = ps.UserHash(1001)
	if hash != 12345 {
		t.Fatalf("expected hash 12345, got %d", hash)
	}

	// Update with new access hash
	ps.SaveUser(1001, 99999)
	hash, _ = ps.UserHash(1001)
	if hash != 99999 {
		t.Fatalf("expected updated hash 99999, got %d", hash)
	}

	// Channel deduplication
	ps.SaveChannel(2001, 54321)
	cHash, ok := ps.ChannelHash(2001)
	if !ok || cHash != 54321 {
		t.Fatalf("expected chat hash 54321, got %d (ok=%v)", cHash, ok)
	}

	ps.SaveChannel(2001, 54321)
	cHash, _ = ps.ChannelHash(2001)
	if cHash != 54321 {
		t.Fatalf("expected chat hash 54321, got %d", cHash)
	}

	// Invalid IDs
	ps.SaveUser(0, 111)
	ps.SaveUser(-5, 222)
	ps.SaveChannel(0, 333)
	ps.SaveChannel(-10, 444)
	users, channels, _ := ps.Counts()
	if users != 1 || channels != 1 {
		t.Errorf("invalid IDs should not be saved: users=%d, channels=%d", users, channels)
	}
}
