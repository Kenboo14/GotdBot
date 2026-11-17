package userbot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"assterisk/ubot/internal/databases"
	"github.com/gotd/td/tg"
)

func TestIsKnownBotEntityAnd777000(t *testing.T) {
	s := &Service{}

	// 1. Akun resmi Telegram 777000 harus di-skip
	if known, isBot := s.isKnownBotEntity(tg.Entities{}, 777000); !known || !isBot {
		t.Fatalf("777000 got (%v, %v), want (true, true)", known, isBot)
	}

	entities := tg.Entities{
		Users: map[int64]*tg.User{
			101: {ID: 101, Bot: true, Username: "somehelper"},
			102: {ID: 102, Bot: false, Username: "MyCoolBot"},
			103: {ID: 103, Bot: false, Username: "andi_dev"},
			104: {ID: 104, Bot: false, Min: true, Username: "min_user"},
		},
	}

	if known, isBot := s.isKnownBotEntity(entities, 101); !known || !isBot {
		t.Fatalf("Bot=true got (%v, %v), want (true, true)", known, isBot)
	}
	if known, isBot := s.isKnownBotEntity(entities, 102); !known || !isBot {
		t.Fatalf("*bot username got (%v, %v), want (true, true)", known, isBot)
	}
	if known, isBot := s.isKnownBotEntity(entities, 103); !known || isBot {
		t.Fatalf("normal human got (%v, %v), want (true, false)", known, isBot)
	}
	// Min user without Bot flag must stay unknown so fallback UsersGetUsers can verify
	if known, _ := s.isKnownBotEntity(entities, 104); known {
		t.Fatalf("expected Min user without Bot=true to require fallback verification")
	}

	// Cached bot entry works even when entities are empty (e.g. UpdateShortMessage)
	s.storeBotCache(202, true)
	if known, isBot := s.isKnownBotEntity(tg.Entities{}, 202); !known || !isBot {
		t.Fatalf("cached bot got (%v, %v), want (true, true)", known, isBot)
	}
}

func TestMassMentionTagAllAndBurst(t *testing.T) {
	s := &Service{}

	// Normal mention of 2 people (with emoji prefix verifying UTF-16 offset) -> not Tag-All
	normalMsg := &tg.Message{
		ID:      1,
		Message: "🔥 halo @budi dan @siti",
		Entities: []tg.MessageEntityClass{
			&tg.MessageEntityMention{Offset: 8, Length: 5},
			&tg.MessageEntityMention{Offset: 18, Length: 5},
		},
	}
	if isMassMentionOrTagAll(normalMsg) {
		t.Fatalf("expected 2 mentions not to trigger tag-all")
	}

	// Tag-All with 5 unique targets (mixed @mention, MentionName, tg://user URL)
	tagAllMsg := &tg.Message{
		ID:      2,
		Message: "@a @b @c halo",
		Entities: []tg.MessageEntityClass{
			&tg.MessageEntityMention{Offset: 0, Length: 2},
			&tg.MessageEntityMention{Offset: 3, Length: 2},
			&tg.MessageEntityMention{Offset: 6, Length: 2},
			&tg.MessageEntityMentionName{Offset: 9, Length: 2, UserID: 1001},
			&tg.MessageEntityTextURL{Offset: 11, Length: 2, URL: "tg://user?id=1002"},
		},
	}
	if !isMassMentionOrTagAll(tagAllMsg) {
		t.Fatalf("expected >= 5 unique mentions to trigger tag-all detection")
	}

	// Mention burst: 4 rapid messages within 15 seconds triggers burst
	base := time.Now()
	for i := 0; i < 3; i++ {
		if s.recordAndCheckMentionBurst(1, 999, 500, 1, base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("did not expect burst trigger at message %d", i+1)
		}
	}
	if !s.recordAndCheckMentionBurst(1, 999, 500, 1, base.Add(4*time.Second)) {
		t.Fatalf("expected 4th rapid mention to trigger burst detection")
	}
}

func TestDuplicateAutoReplyDetection(t *testing.T) {
	s := &Service{}
	now := time.Now()

	// Short human greetings (< 15 chars) must never be treated as duplicate auto-reply
	if s.isDuplicateAutoReply(1, 555, "halo", now) || s.isDuplicateAutoReply(1, 555, "halo", now.Add(time.Second)) {
		t.Fatalf("short human message should not be flagged as duplicate auto-reply")
	}

	// Long template auto-reply (>= 15 chars): first allowed, second within 15m suppressed
	tpl := "Halo, saya sedang AFK, nanti saya balas."
	if s.isDuplicateAutoReply(1, 555, tpl, now) {
		t.Fatalf("first auto-reply template should be logged once")
	}
	if !s.isDuplicateAutoReply(1, 555, "  HALO, SAYA SEDANG AFK, NANTI SAYA BALAS. ", now.Add(2*time.Minute)) {
		t.Fatalf("repeated identical auto-reply template should be suppressed")
	}
	// Different sender with same text should not be suppressed
	if s.isDuplicateAutoReply(1, 666, tpl, now.Add(2*time.Minute)) {
		t.Fatalf("different sender should not inherit prior sender's duplicate filter")
	}
}

func TestLogBlockSQLitePersistenceAndRelink(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_log_block.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.DB.Close()

	ownerOld := int64(10001)
	ownerNew := int64(20002)
	target := int64(888777)

	if err := sq.AddLogBlock(ctx, ownerOld, target, "Spam Userbot"); err != nil {
		t.Fatalf("AddLogBlock failed: %v", err)
	}
	blocked, err := sq.IsLogBlocked(ctx, ownerOld, target)
	if err != nil || !blocked {
		t.Fatalf("IsLogBlocked = (%v, %v), want (true, nil)", blocked, err)
	}

	rows, err := sq.ListLogBlocks(ctx, ownerOld)
	if err != nil || len(rows) != 1 || rows[0].Label != "Spam Userbot" {
		t.Fatalf("ListLogBlocks got %+v (err=%v)", rows, err)
	}

	// Relink should transfer log_blocked_users to new owner account ID
	if err := sq.RelinkUserbotData(ctx, ownerOld, ownerNew, ownerOld, ownerNew); err != nil {
		t.Fatalf("RelinkUserbotData failed: %v", err)
	}
	if blockedNew, _ := sq.IsLogBlocked(ctx, ownerNew, target); !blockedNew {
		t.Fatalf("expected relinked ownerNew to keep blocked user")
	}

	if err := sq.RemoveLogBlock(ctx, ownerNew, target); err != nil {
		t.Fatalf("RemoveLogBlock failed: %v", err)
	}
	if blockedAfter, _ := sq.IsLogBlocked(ctx, ownerNew, target); blockedAfter {
		t.Fatalf("expected user to be unblocked after RemoveLogBlock")
	}

	_ = sq.AddLogBlock(ctx, ownerNew, 111, "A")
	_ = sq.AddLogBlock(ctx, ownerNew, 222, "B")
	if err := sq.ClearLogBlocks(ctx, ownerNew); err != nil {
		t.Fatalf("ClearLogBlocks failed: %v", err)
	}
	if remaining, _ := sq.ListLogBlocks(ctx, ownerNew); len(remaining) != 0 {
		t.Fatalf("expected 0 remaining blocks after ClearLogBlocks, got %d", len(remaining))
	}
}
