package databases

import (
	"context"
	"os"
	"testing"
)

func TestDataManagerSQLiteOnly(t *testing.T) {
	dbPath := "test_panel.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	ctx := context.Background()
	dm, err := NewDataManager(ctx, "", "", dbPath)
	if err != nil {
		t.Fatalf("failed to init DataManager: %v", err)
	}

	ownerID := int64(123456789)

	// 1. Test Session Check (initial: false)
	if dm.HasActiveSession(ctx, ownerID) {
		t.Errorf("expected false, got true")
	}

	// Insert mock session into SQLite
	_, err = dm.sqlite.DB.ExecContext(ctx, `INSERT INTO sessions (chat_id, owner_id, account_id, session_blob) VALUES (?, ?, ?, ?);`, ownerID, ownerID, ownerID, "dummy_blob")
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}
	if !dm.HasActiveSession(ctx, ownerID) {
		t.Errorf("expected true after insert, got false")
	}

	// 2. Test Config Default & Save
	cfg := dm.GetUserConfig(ctx, ownerID)
	if cfg == nil || cfg.Delay != 5 || cfg.Status != "Berhenti" {
		t.Fatalf("unexpected default config: %+v", cfg)
	}

	cfg.Delay = 10
	cfg.Safemode = true
	cfg.Mode = "Fast"
	cfg.Watermark = "@test_watermark"
	cfg.WatermarkCacheID = 6847
	if err := dm.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("save config failed: %v", err)
	}

	updated := dm.GetUserConfig(ctx, ownerID)
	if updated.Delay != 10 || !updated.Safemode || updated.Mode != "Fast" || updated.Watermark != "@test_watermark" || updated.WatermarkCacheID != 6847 {
		t.Errorf("updated config mismatch: %+v", updated)
	}

	// 3. Test AddListCacheID & GetLists
	if err := dm.AddListCacheID(ctx, ownerID, 1001); err != nil {
		t.Fatalf("AddListCacheID failed: %v", err)
	}
	if err := dm.AddListCacheID(ctx, ownerID, 1002); err != nil {
		t.Fatalf("AddListCacheID 2 failed: %v", err)
	}

	lists := dm.GetLists(ctx, ownerID)
	if len(lists) != 2 || lists[0] != 1001 || lists[1] != 1002 {
		t.Fatalf("expected [1001, 1002], got %+v", lists)
	}

	if err := dm.DeleteListByIndex(ctx, ownerID, 0); err != nil {
		t.Fatalf("DeleteListByIndex failed: %v", err)
	}
	listsAfterDel := dm.GetLists(ctx, ownerID)
	if len(listsAfterDel) != 1 || listsAfterDel[0] != 1002 {
		t.Fatalf("expected 1 list remaining (1002), got %+v", listsAfterDel)
	}

	_ = dm.ClearLists(ctx, ownerID)
	if len(dm.GetLists(ctx, ownerID)) != 0 {
		t.Fatalf("expected 0 lists after clear")
	}

	// 4. Test AddGroup & GetGroups
	err = dm.AddGroup(ctx, ownerID, "@group1", "Grup Promosi 1", -100111)
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}
	groups := dm.GetGroups(ctx, ownerID)
	if len(groups) != 1 || groups[0].Target != "@group1" {
		t.Fatalf("expected group1, got %+v", groups)
	}

	gCount, lCount := dm.CountGroupsAndLists(ctx, ownerID)
	if gCount != 1 || lCount != 0 {
		t.Fatalf("expected gCount 1, lCount 0, got %d, %d", gCount, lCount)
	}

	_ = dm.ClearGroups(ctx, ownerID)
	if len(dm.GetGroups(ctx, ownerID)) != 0 {
		t.Fatalf("expected 0 groups after clear")
	}

	// 5. Test AddTopic & GetTopics
	err = dm.AddTopic(ctx, ownerID, -100111, 42, "Forum Topic #42")
	if err != nil {
		t.Fatalf("AddTopic failed: %v", err)
	}
	topics := dm.GetTopics(ctx, ownerID)
	if len(topics) != 1 || topics[0].TopicID != 42 {
		t.Fatalf("expected 1 topic with ID 42, got %+v", topics)
	}
	_ = dm.ClearTopics(ctx, ownerID)
	if len(dm.GetTopics(ctx, ownerID)) != 0 {
		t.Fatalf("expected 0 topics after clear")
	}
}
