package userbot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	ubotCfgCacheMaxEntries = 150
	ubotCfgCacheTTL        = 60 * time.Second
	cacheInvChannel        = "cache:inv"
)

type ubotCfgCacheEntry struct {
	doc       jasebConfigDoc
	expiresAt time.Time
}

type ubotConfigCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	items      map[int64]ubotCfgCacheEntry
	order      []int64
}

func newUbotConfigCache(maxEntries int, ttl time.Duration) *ubotConfigCache {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &ubotConfigCache{
		maxEntries: maxEntries,
		ttl:        ttl,
		items:      make(map[int64]ubotCfgCacheEntry, maxEntries),
	}
}

func cloneJasebConfigDoc(doc jasebConfigDoc) jasebConfigDoc {
	cp := doc
	if doc.Lists != nil {
		cp.Lists = make([]int, len(doc.Lists))
		copy(cp.Lists, doc.Lists)
	}
	if doc.Groups != nil {
		cp.Groups = make([]jasebGroupDoc, len(doc.Groups))
		copy(cp.Groups, doc.Groups)
	}
	if doc.Topics != nil {
		cp.Topics = make([]jasebTopicDoc, len(doc.Topics))
		copy(cp.Topics, doc.Topics)
	}
	if doc.TopicLists != nil {
		cp.TopicLists = make([]jasebTopicListDoc, len(doc.TopicLists))
		for i, tl := range doc.TopicLists {
			cpTL := tl
			if tl.Posts != nil {
				cpTL.Posts = make([]int, len(tl.Posts))
				copy(cpTL.Posts, tl.Posts)
			}
			if tl.LinkedTopics != nil {
				cpTL.LinkedTopics = make([]jasebTopicLinkDoc, len(tl.LinkedTopics))
				copy(cpTL.LinkedTopics, tl.LinkedTopics)
			}
			cp.TopicLists[i] = cpTL
		}
	}
	return cp
}

func (c *ubotConfigCache) Get(ownerID int64) (jasebConfigDoc, bool) {
	if c == nil || ownerID == 0 {
		return jasebConfigDoc{}, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	ent, ok := c.items[ownerID]
	if !ok {
		return jasebConfigDoc{}, false
	}
	if now.After(ent.expiresAt) {
		delete(c.items, ownerID)
		return jasebConfigDoc{}, false
	}
	return cloneJasebConfigDoc(ent.doc), true
}

func (c *ubotConfigCache) Set(ownerID int64, doc jasebConfigDoc) {
	if c == nil || ownerID == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[ownerID]; !exists {
		if len(c.items) >= c.maxEntries {
			evicted := false
			for len(c.order) > 0 {
				oldest := c.order[0]
				c.order = c.order[1:]
				if _, ok := c.items[oldest]; ok {
					delete(c.items, oldest)
					evicted = true
					break
				}
			}
			if !evicted {
				for k := range c.items {
					delete(c.items, k)
					break
				}
			}
		}
		c.order = append(c.order, ownerID)
	}
	c.items[ownerID] = ubotCfgCacheEntry{
		doc:       cloneJasebConfigDoc(doc),
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *ubotConfigCache) Delete(ownerID int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if ownerID == 0 {
		c.items = make(map[int64]ubotCfgCacheEntry, c.maxEntries)
		c.order = nil
	} else {
		delete(c.items, ownerID)
	}
	c.mu.Unlock()
}

func (s *Service) invalidateConfigCache(ctx context.Context, ownerID int64, publish bool) {
	if s == nil {
		return
	}
	s.cfgCache.Delete(ownerID)
	if s.redis != nil && s.redis.Client != nil {
		if ownerID != 0 {
			_ = s.redis.Client.Del(ctx, fmt.Sprintf("cache:cfg:%d", ownerID)).Err()
		}
		if publish {
			raw, err := json.Marshal(map[string]any{"owner_id": ownerID, "kind": "cfg"})
			if err == nil {
				pubCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = s.redis.Client.Publish(pubCtx, cacheInvChannel, string(raw)).Err()
				cancel()
			}
		}
	}
}

func (s *Service) startCacheInvalidationLoop(ctx context.Context) {
	if s == nil || s.redis == nil || s.redis.Client == nil {
		return
	}
	pubsub := s.redis.Client.Subscribe(ctx, cacheInvChannel, "config:updated")
	defer pubsub.Close()
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var evt struct {
				OwnerID int64  `json:"owner_id"`
				Kind    string `json:"kind"`
			}
			if json.Unmarshal([]byte(msg.Payload), &evt) != nil {
				continue
			}
			if msg.Channel == cacheInvChannel && evt.Kind == "lic" {
				continue
			}
			s.cfgCache.Delete(evt.OwnerID)
		}
	}
}
