package databases

import (
	"context"
	"fmt"
	"strings"
)

const (
	MaxTopicLists       = 20
	MaxTopicListPosts   = 20
	MaxTopicsPerGroup   = 10
	PlayModeSequential  = "sequential"
	PlayModeShuffleEven = "shuffle_even"
	PlayModeShuffleFree = "shuffle_free"
	PlayModeAll         = "all"
)

func NormalizePlayMode(mode string) string {
	switch mode {
	case PlayModeShuffleEven, PlayModeShuffleFree, PlayModeAll:
		return mode
	default:
		return PlayModeSequential
	}
}

func (m *DataManager) SetGroupTopics(ctx context.Context, ownerID, chatID int64, groupTitle string, selected []TopicEntry) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	kept := make([]TopicEntry, 0, len(cfg.Topics)+len(selected))
	for _, t := range cfg.Topics {
		if !SameGroupID(t.ChatID, chatID) {
			kept = append(kept, t)
		}
	}
	seen := make(map[int]bool, len(selected))
	for _, t := range selected {
		if seen[t.TopicID] {
			continue
		}
		seen[t.TopicID] = true
		if t.GroupTitle == "" {
			t.GroupTitle = groupTitle
		}
		t.ChatID = chatID
		kept = append(kept, t)
	}
	cfg.Topics = kept
	cfg.TopicEnabled = len(cfg.Topics) > 0
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) DeleteGroupTopics(ctx context.Context, ownerID, chatID int64) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	kept := make([]TopicEntry, 0, len(cfg.Topics))
	for _, t := range cfg.Topics {
		if !SameGroupID(t.ChatID, chatID) {
			kept = append(kept, t)
		}
	}
	cfg.Topics = kept
	cfg.TopicEnabled = len(cfg.Topics) > 0
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) GetTopicLists(ctx context.Context, ownerID int64) []TopicListEntry {
	cfg := m.GetUserConfig(ctx, ownerID)
	return cfg.TopicLists
}

func (m *DataManager) GetTopicList(ctx context.Context, ownerID int64, name string) *TopicListEntry {
	cfg := m.GetUserConfig(ctx, ownerID)
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == name {
			cp := cfg.TopicLists[i]
			return &cp
		}
	}
	return nil
}

func (m *DataManager) CreateTopicList(ctx context.Context, ownerID int64, name string) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	if len(cfg.TopicLists) >= MaxTopicLists {
		return fmt.Errorf("maksimal %d List Topic tercapai", MaxTopicLists)
	}
	for _, tl := range cfg.TopicLists {
		if strings.EqualFold(tl.Name, name) {
			return fmt.Errorf("nama list %q sudah digunakan", name)
		}
	}
	cfg.TopicLists = append(cfg.TopicLists, TopicListEntry{
		Name:         name,
		Posts:        []int{},
		PlayMode:     PlayModeSequential,
		LinkedTopics: []TopicLink{},
	})
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) DeleteTopicList(ctx context.Context, ownerID int64, name string) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	filtered := make([]TopicListEntry, 0, len(cfg.TopicLists))
	found := false
	for _, tl := range cfg.TopicLists {
		if tl.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, tl)
	}
	if !found {
		return fmt.Errorf("list topic %q tidak ditemukan", name)
	}
	cfg.TopicLists = filtered
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) SetTopicListPlayMode(ctx context.Context, ownerID int64, name, mode string) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	norm := NormalizePlayMode(mode)
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == name {
			cfg.TopicLists[i].PlayMode = norm
			return m.SaveUserConfig(ctx, cfg)
		}
	}
	return fmt.Errorf("list topic %q tidak ditemukan", name)
}

func (m *DataManager) DeleteTopicListPostByIndex(ctx context.Context, ownerID int64, name string, index int) (int, error) {
	cfg := m.GetUserConfig(ctx, ownerID)
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == name {
			posts := cfg.TopicLists[i].Posts
			if index < 0 || index >= len(posts) {
				return 0, fmt.Errorf("index out of range")
			}
			removed := posts[index]
			cfg.TopicLists[i].Posts = append(posts[:index], posts[index+1:]...)
			return removed, m.SaveUserConfig(ctx, cfg)
		}
	}
	return 0, fmt.Errorf("list topic %q tidak ditemukan", name)
}

func (m *DataManager) LinkTopicsToList(ctx context.Context, ownerID int64, listName string, links []TopicLink) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	targetKeys := make(map[string]bool, len(links))
	for _, lk := range links {
		targetKeys[fmt.Sprintf("%d:%d", NormalizePeerRawID(lk.ChatID), lk.TopicID)] = true
	}
	for i := range cfg.TopicLists {
		kept := make([]TopicLink, 0, len(cfg.TopicLists[i].LinkedTopics))
		for _, existing := range cfg.TopicLists[i].LinkedTopics {
			key := fmt.Sprintf("%d:%d", NormalizePeerRawID(existing.ChatID), existing.TopicID)
			if !targetKeys[key] {
				kept = append(kept, existing)
			}
		}
		cfg.TopicLists[i].LinkedTopics = kept
	}
	if listName == "" {
		return m.SaveUserConfig(ctx, cfg)
	}
	found := false
	for i := range cfg.TopicLists {
		if cfg.TopicLists[i].Name == listName {
			cfg.TopicLists[i].LinkedTopics = append(cfg.TopicLists[i].LinkedTopics, links...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("list topic %q tidak ditemukan", listName)
	}
	return m.SaveUserConfig(ctx, cfg)
}
