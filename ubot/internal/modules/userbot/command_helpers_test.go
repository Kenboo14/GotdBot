package userbot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestBuildReplyToAndCommandContext(t *testing.T) {
	ctx := withCommandReplyContext(context.Background(), &tg.Message{ID: 777})
	if got := commandReplyMsgID(ctx); got != 777 {
		t.Fatalf("commandReplyMsgID() = %d, want 777", got)
	}

	// Normal chat / supergroup: replies directly to command message ID
	rt := buildReplyTo(0, commandReplyMsgID(ctx))
	if rt == nil || rt.ReplyToMsgID != 777 || rt.TopMsgID != 0 {
		t.Fatalf("unexpected normal replyTo: %+v", rt)
	}

	// Forum topic: replies to command message ID while preserving TopMsgID topic
	rtTopic := buildReplyTo(42, commandReplyMsgID(ctx))
	if rtTopic == nil || rtTopic.ReplyToMsgID != 777 || rtTopic.TopMsgID != 42 {
		t.Fatalf("unexpected topic replyTo: %+v", rtTopic)
	}

	// Background message inside topic without command ID: falls back to threadID
	rtFallback := buildReplyTo(42, 0)
	if rtFallback == nil || rtFallback.ReplyToMsgID != 42 || rtFallback.TopMsgID != 42 {
		t.Fatalf("unexpected fallback topic replyTo: %+v", rtFallback)
	}

	// Neither command ID nor thread ID
	if rtNil := buildReplyTo(0, 0); rtNil != nil {
		t.Fatalf("expected nil replyTo, got %+v", rtNil)
	}
}

func TestExtractUserIDFromMessage(t *testing.T) {
	ownerID := int64(9001)

	// Group message from user
	uid, ok := extractUserIDFromMessage(&tg.Message{
		ID:     10,
		FromID: &tg.PeerUser{UserID: 555},
		PeerID: &tg.PeerChat{ChatID: 123},
	}, ownerID)
	if !ok || uid != 555 {
		t.Fatalf("group FromID = (%d, %v), want (555, true)", uid, ok)
	}

	// Incoming PM where FromID is nil
	uid, ok = extractUserIDFromMessage(&tg.Message{
		ID:     11,
		Out:    false,
		PeerID: &tg.PeerUser{UserID: 666},
	}, ownerID)
	if !ok || uid != 666 {
		t.Fatalf("PM incoming = (%d, %v), want (666, true)", uid, ok)
	}

	// Outgoing message uses ownerID
	uid, ok = extractUserIDFromMessage(&tg.Message{
		ID:     12,
		Out:    true,
		PeerID: &tg.PeerUser{UserID: 666},
	}, ownerID)
	if !ok || uid != ownerID {
		t.Fatalf("outgoing message = (%d, %v), want (%d, true)", uid, ok, ownerID)
	}

	// Anonymous channel post has no user ID
	if _, ok := extractUserIDFromMessage(&tg.Message{
		ID:     13,
		FromID: &tg.PeerChannel{ChannelID: 888},
		PeerID: &tg.PeerChannel{ChannelID: 888},
	}, ownerID); ok {
		t.Fatalf("expected false for channel/anonymous message")
	}
}

func TestResolveTargetUserID(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	entities := tg.Entities{
		Users: map[int64]*tg.User{
			12345: {ID: 12345, FirstName: "Budi", Username: "budi_dev"},
		},
	}

	// Numeric ID
	uid, name, err := s.resolveTargetUserID(ctx, nil, nil, &tg.Message{ID: 1}, entities, "12345")
	if err != nil || uid != 12345 || name != "Budi" {
		t.Fatalf("numeric ID got (%d, %q, %v), want (12345, Budi, nil)", uid, name, err)
	}

	// @username from entities
	uid, name, err = s.resolveTargetUserID(ctx, nil, nil, &tg.Message{ID: 2}, entities, "@budi_dev")
	if err != nil || uid != 12345 || name != "Budi" {
		t.Fatalf("@username got (%d, %q, %v), want (12345, Budi, nil)", uid, name, err)
	}

	// Inline text mention entity (MessageEntityMentionName)
	cmdWithMention := &tg.Message{
		ID: 3,
		Entities: []tg.MessageEntityClass{
			&tg.MessageEntityMentionName{Offset: 9, Length: 4, UserID: 12345},
		},
	}
	uid, _, err = s.resolveTargetUserID(ctx, nil, nil, cmdWithMention, entities, "Budi")
	if err != nil || uid != 12345 {
		t.Fatalf("mention entity got (%d, %v), want (12345, nil)", uid, err)
	}

	// Forum topic message without actual message reply -> errNoTargetUser
	topicMsg := &tg.Message{
		ID: 4,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   true,
			ReplyToTopID: 0,
			ReplyToMsgID: 100,
		},
	}
	_, _, err = s.resolveTargetUserID(ctx, nil, nil, topicMsg, entities, "")
	if !errors.Is(err, errNoTargetUser) {
		t.Fatalf("expected errNoTargetUser for un-replied topic message, got %v", err)
	}
}

func TestBlacklistTargetModes(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	entities := tg.Entities{
		Users: map[int64]*tg.User{
			777: {ID: 777, FirstName: "Target", AccessHash: 11},
		},
		Chats: map[int64]*tg.Chat{
			999: {ID: 999, Title: "Grup Uji"},
		},
	}

	// In group without -pv (even with ReplyTo in a forum/reply) -> targets current group
	groupMsg := &tg.Message{
		ID:     1,
		PeerID: &tg.PeerChat{ChatID: 999},
		ReplyTo: &tg.MessageReplyHeader{
			ReplyToMsgID: 50,
		},
	}
	chatID, kind, _, err := s.blacklistTarget(ctx, nil, nil, groupMsg, entities, "")
	if err != nil || kind != "group" || chatID != 999 {
		t.Fatalf("group without -pv got (%d, %q, %v), want (999, group, nil)", chatID, kind, err)
	}

	// In group with -pv + explicit user ID -> targets user private
	chatID, kind, title, err := s.blacklistTarget(ctx, nil, nil, groupMsg, entities, "-pv 777")
	if err != nil || kind != "private" || chatID != 777 || title != "Target" {
		t.Fatalf("group with -pv 777 got (%d, %q, %q, %v), want (777, private, Target, nil)", chatID, kind, title, err)
	}

	// In PM with -pv -> targets PM user directly
	pmMsg := &tg.Message{
		ID:     2,
		PeerID: &tg.PeerUser{UserID: 777},
	}
	chatID, kind, _, err = s.blacklistTarget(ctx, nil, nil, pmMsg, entities, "-pv")
	if err != nil || kind != "private" || chatID != 777 {
		t.Fatalf("PM with -pv got (%d, %q, %v), want (777, private, nil)", chatID, kind, err)
	}
}

func TestShouldProcessEditedCommand(t *testing.T) {
	s := &Service{}
	now := time.Now().Unix()

	// EditDate == 0 (reaction/view update only) must be ignored
	if s.shouldProcessEditedCommand(100, &tg.Message{
		ID:       50,
		PeerID:   &tg.PeerChat{ChatID: 1},
		Message:  "!p",
		EditDate: 0,
	}, now) {
		t.Fatalf("expected EditDate == 0 to be ignored")
	}

	// Too old edit (> 300s) must be ignored
	if s.shouldProcessEditedCommand(100, &tg.Message{
		ID:       50,
		PeerID:   &tg.PeerChat{ChatID: 1},
		Message:  "!p",
		EditDate: int(now - 600),
	}, now) {
		t.Fatalf("expected stale edit > 300s to be ignored")
	}

	// Fresh edit should be processed once
	edited := &tg.Message{
		ID:       50,
		PeerID:   &tg.PeerChat{ChatID: 1},
		Message:  "!addsudo",
		EditDate: int(now - 2),
	}
	if !s.shouldProcessEditedCommand(100, edited, now) {
		t.Fatalf("expected fresh edit to be processed")
	}
	s.markCommandExecuted(100, edited)

	// Re-delivery with same text/editDate (e.g. reaction added later) must be ignored
	if s.shouldProcessEditedCommand(100, edited, now) {
		t.Fatalf("expected already-executed edit not to re-trigger")
	}

	// New edit changing text & editDate should be processed
	editedAgain := &tg.Message{
		ID:       50,
		PeerID:   &tg.PeerChat{ChatID: 1},
		Message:  "!rmsudo",
		EditDate: int(now),
	}
	if !s.shouldProcessEditedCommand(100, editedAgain, now) {
		t.Fatalf("expected second distinct edit to be processed")
	}
}
