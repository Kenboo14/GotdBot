package databases

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGroupIDNormalizationAndDisplay(t *testing.T) {
	if got := ChannelBotID(2345678901); got != -1002345678901 {
		t.Fatalf("ChannelBotID = %d, want -1002345678901", got)
	}
	if got := NormalizePeerRawID(-1002345678901); got != 2345678901 {
		t.Fatalf("NormalizePeerRawID = %d, want 2345678901", got)
	}
	if !SameGroupID(-1002345678901, 2345678901) {
		t.Fatalf("expected -1002345678901 and 2345678901 to match SameGroupID")
	}

	legacySuper := GroupEntry{Target: "2345678901", Title: "Lapak Resmi", ChatID: 2345678901, PeerType: "channel"}
	if got := legacySuper.DisplayTarget(); got != "-1002345678901" {
		t.Fatalf("legacy supergroup DisplayTarget = %q, want -1002345678901", got)
	}
	if got := legacySuper.DisplayChatID(); got != -1002345678901 {
		t.Fatalf("legacy supergroup DisplayChatID = %d, want -1002345678901", got)
	}

	basic := GroupEntry{Target: "987654", Title: "Grup Lama", ChatID: -987654, PeerType: "chat"}
	if got := basic.DisplayTarget(); got != "-987654" {
		t.Fatalf("basic DisplayTarget = %q, want -987654", got)
	}

	uname := GroupEntry{Target: "@lapak_resmi", Title: "Lapak Resmi", ChatID: -100333444, PeerType: "channel"}
	if got := uname.DisplayTarget(); got != "@lapak_resmi" {
		t.Fatalf("username DisplayTarget = %q, want @lapak_resmi", got)
	}
}

func TestAddGroupCanonicalPersistenceAndDedup(t *testing.T) {
	ctx := context.Background()
	dm, err := NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "gw.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer dm.sqlite.DB.Close()

	const ownerID int64 = 4242

	if err := dm.AddGroup(ctx, ownerID, "-1001234567890", "Grup Jualan", -1001234567890); err != nil {
		t.Fatalf("AddGroup supergroup: %v", err)
	}
	if err := dm.AddGroup(ctx, ownerID, "-1001234567890", "Grup Jualan Baru", -1001234567890); err != nil {
		t.Fatalf("AddGroup dedup: %v", err)
	}
	if err := dm.AddGroup(ctx, ownerID, "-987654", "Grup Dasar", -987654); err != nil {
		t.Fatalf("AddGroup basic: %v", err)
	}

	groups := dm.GetGroups(ctx, ownerID)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups after dedup, got %d (%+v)", len(groups), groups)
	}
	if groups[0].ChatID != -1001234567890 || groups[0].Target != "-1001234567890" || groups[0].PeerType != "channel" {
		t.Fatalf("unexpected supergroup entry: %+v", groups[0])
	}
	if groups[1].ChatID != -987654 || groups[1].Target != "-987654" || groups[1].PeerType != "chat" {
		t.Fatalf("unexpected basic group entry: %+v", groups[1])
	}
}
