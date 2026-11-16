package userbot

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestExtractThreadID_TopicFromReplyToTopID(t *testing.T) {
	msg := &tg.Message{
		ID:    100,
		Flags: 1 << 3,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   true,
			ReplyToTopID: 42,
			ReplyToMsgID: 99,
		},
	}
	got := extractThreadID(msg)
	if got != 42 {
		t.Errorf("extractThreadID() = %d, want %d (ReplyToTopID takes priority)", got, 42)
	}
}

func TestExtractThreadID_TopicFallbackToReplyToMsgID(t *testing.T) {
	msg := &tg.Message{
		ID:    100,
		Flags: 1 << 3,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   true,
			ReplyToTopID: 0,
			ReplyToMsgID: 456,
		},
	}
	got := extractThreadID(msg)
	if got != 456 {
		t.Errorf("extractThreadID() = %d, want %d (fallback to ReplyToMsgID when ForumTopic)", got, 456)
	}
}

func TestExtractThreadID_GeneralTopicNoReplyTo(t *testing.T) {
	msg := &tg.Message{ID: 100}
	got := extractThreadID(msg)
	if got != 0 {
		t.Errorf("extractThreadID() = %d, want 0 (general topic has no reply_to)", got)
	}
}

func TestExtractThreadID_ForumTopicZeroIDs(t *testing.T) {
	msg := &tg.Message{
		ID:    100,
		Flags: 1 << 3,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   true,
			ReplyToTopID: 0,
			ReplyToMsgID: 0,
		},
	}
	got := extractThreadID(msg)
	if got != 0 {
		t.Errorf("extractThreadID() = %d, want 0 (ForumTopic but no valid topic ID)", got)
	}
}

func TestExtractThreadID_ReplyToTopIDWithoutForumTopic(t *testing.T) {
	msg := &tg.Message{
		ID:    100,
		Flags: 1 << 3,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   false,
			ReplyToTopID: 77,
			ReplyToMsgID: 0,
		},
	}
	got := extractThreadID(msg)
	if got != 77 {
		t.Errorf("extractThreadID() = %d, want %d (ReplyToTopID is definitive topic ID)", got, 77)
	}
}

func TestExtractThreadID_MessageIDNeverTreatedAsTopic(t *testing.T) {
	msg := &tg.Message{
		ID:    9999,
		Flags: 1 << 3,
		ReplyTo: &tg.MessageReplyHeader{
			ForumTopic:   false,
			ReplyToTopID: 0,
			ReplyToMsgID: 0,
		},
	}
	got := extractThreadID(msg)
	if got != 0 {
		t.Errorf("extractThreadID() = %d, want 0 (Message.ID must never be treated as Topic ID)", got)
	}
}
