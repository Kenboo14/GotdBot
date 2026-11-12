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
