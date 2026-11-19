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

func TestTopicPlayTracker_Modes(t *testing.T) {
	tr := newTopicPlayTracker()
	posts := []int{10, 20, 30}

	seqList := jasebTopicListDoc{Name: "seq", Posts: posts, PlayMode: "sequential"}
	for i, want := range []int{10, 20, 30, 10} {
		got := tr.nextPosts(seqList, "g1")
		if len(got) != 1 || got[0] != want {
			t.Fatalf("seq step %d: got %v, want [%d]", i, got, want)
		}
	}

	evenList := jasebTopicListDoc{Name: "even", Posts: posts, PlayMode: "shuffle_even"}
	seen := make(map[int]bool)
	for i := 0; i < len(posts); i++ {
		got := tr.nextPosts(evenList, "g1")
		if len(got) != 1 {
			t.Fatalf("shuffle_even step %d: expected 1 post, got %v", i, got)
		}
		seen[got[0]] = true
	}
	if len(seen) != len(posts) {
		t.Fatalf("shuffle_even did not cover all posts in one cycle: %v", seen)
	}

	allList := jasebTopicListDoc{Name: "all", Posts: posts, PlayMode: "all"}
	gotAll := tr.nextPosts(allList, "g1")
	if len(gotAll) != 3 || gotAll[0] != 10 || gotAll[2] != 30 {
		t.Fatalf("all mode: got %v, want [10 20 30]", gotAll)
	}

	freeList := jasebTopicListDoc{Name: "free", Posts: posts, PlayMode: "shuffle_free"}
	gotFree := tr.nextPosts(freeList, "g1")
	if len(gotFree) != 1 || (gotFree[0] != 10 && gotFree[0] != 20 && gotFree[0] != 30) {
		t.Fatalf("shuffle_free: unexpected %v", gotFree)
	}
}

func TestResolveGroupTopicDeliveries(t *testing.T) {
	cfg := jasebConfigDoc{
		Topics: []jasebTopicDoc{
			{ChatID: -100123456, TopicID: 1, Title: "General"},
			{ChatID: -100123456, TopicID: 2, Title: "Promo"},
		},
		TopicLists: []jasebTopicListDoc{
			{
				Name:     "makanan",
				Posts:    []int{701, 702},
				PlayMode: "sequential",
				LinkedTopics: []jasebTopicLinkDoc{
					{ChatID: 123456, TopicID: 2, TopicTitle: "Promo"},
					{ChatID: -100123456, TopicID: 5, TopicTitle: "Khusus"},
				},
			},
		},
	}
	grp := jasebGroupDoc{ChatID: 123456, Title: "Aio Topic"}
	plan := resolveGroupTopicDeliveries(cfg, grp)
	if len(plan) != 3 {
		t.Fatalf("expected 3 topic deliveries, got %d: %+v", len(plan), plan)
	}
	if plan[0].TopicID != 1 || plan[0].TopicList != nil {
		t.Fatalf("topic 1 should use main list, got %+v", plan[0])
	}
	if plan[1].TopicID != 2 || plan[1].TopicList == nil || plan[1].TopicList.Name != "makanan" {
		t.Fatalf("topic 2 should use makanan list, got %+v", plan[1])
	}
	if plan[2].TopicID != 5 || plan[2].TopicList == nil || plan[2].TopicList.Name != "makanan" {
		t.Fatalf("topic 5 should use makanan list, got %+v", plan[2])
	}
}
