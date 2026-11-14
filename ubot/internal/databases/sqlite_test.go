package databases

import (
	"context"
	"os"
	"testing"
)

func TestLogRouteSaveAndGet(t *testing.T) {
	dbPath := "test_log_route.db"
	defer os.Remove(dbPath)

	client, err := NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("failed to init sqlite: %v", err)
	}
	defer client.Close()

	// Verify Enterprise WAL and Cache pragmas
	var journalMode string
	if err := client.DB.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode 'wal', got '%s'", journalMode)
	}

	var cacheSize int
	if err := client.DB.QueryRow("PRAGMA cache_size;").Scan(&cacheSize); err != nil {
		t.Fatalf("failed to query cache_size: %v", err)
	}
	if cacheSize != -64000 {
		t.Fatalf("expected cache_size -64000, got %d", cacheSize)
	}

	ctx := context.Background()
	botID := int64(9240263696)
	logMsgID := 1241
	userID := int64(7420876266)
	sourceChatID := int64(-100123456789)
	sourceMsgID := 555
	peerType := "channel"
	accessHash := int64(987654321)

	err = client.SaveLogRoute(ctx, botID, logMsgID, userID, sourceChatID, sourceMsgID, peerType, accessHash)
	if err != nil {
		t.Fatalf("SaveLogRoute failed: %v", err)
	}

	route, err := client.GetLogRoute(ctx, botID, logMsgID)
	if err != nil {
		t.Fatalf("GetLogRoute failed with error: %v", err)
	}
	if route == nil {
		t.Fatalf("GetLogRoute returned nil route")
	}

	if route.LogMessageID != logMsgID || route.SourceChatID != sourceChatID || route.SourceMessageID != sourceMsgID {
		t.Fatalf("Route mismatch: got %+v", route)
	}
}
