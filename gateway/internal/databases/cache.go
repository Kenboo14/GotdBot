package databases

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cacheInvChannel     = "cache:inv"
	cfgCacheTTL         = 60 * time.Second
	licCacheTTL         = 60 * time.Second
	redisCfgCacheTTL    = 5 * time.Minute
	redisLicCacheTTL    = 2 * time.Minute
	maxGatewayCacheSize = 256
)

type cacheEntry[V any] struct {
	val       V
	expiresAt time.Time
}

type boundedCache[K comparable, V any] struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	items      map[K]cacheEntry[V]
	order      []K
}

func newBoundedCache[K comparable, V any](maxEntries int, ttl time.Duration) *boundedCache[K, V] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &boundedCache[K, V]{
		maxEntries: maxEntries,
		ttl:        ttl,
		items:      make(map[K]cacheEntry[V], maxEntries),
	}
}

func (c *boundedCache[K, V]) Get(key K) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	ent, ok := c.items[key]
	if !ok {
		return zero, false
	}
	if now.After(ent.expiresAt) {
		delete(c.items, key)
		return zero, false
	}
	return ent.val, true
}

func (c *boundedCache[K, V]) Set(key K, val V) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; !exists {
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
		c.order = append(c.order, key)
	}
	c.items[key] = cacheEntry[V]{val: val, expiresAt: time.Now().Add(c.ttl)}
}

func (c *boundedCache[K, V]) Delete(key K) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

func (c *boundedCache[K, V]) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[K]cacheEntry[V], c.maxEntries)
	c.order = nil
	c.mu.Unlock()
}

type cacheInvalidationEvent struct {
	OwnerID int64  `json:"owner_id"`
	Kind    string `json:"kind"`
}

func (dm *DataManager) AttachRedisCache(rdb *redis.Client) {
	if dm == nil || rdb == nil {
		return
	}
	dm.mu.Lock()
	dm.rdb = rdb
	dm.mu.Unlock()
}

func (dm *DataManager) getRedis() *redis.Client {
	if dm == nil {
		return nil
	}
	dm.mu.RLock()
	rdb := dm.rdb
	dm.mu.RUnlock()
	return rdb
}

func (dm *DataManager) publishCacheInv(ctx context.Context, ownerID int64, kind string) {
	rdb := dm.getRedis()
	if rdb == nil {
		return
	}
	raw, err := json.Marshal(cacheInvalidationEvent{OwnerID: ownerID, Kind: kind})
	if err != nil {
		return
	}
	pubCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = rdb.Publish(pubCtx, cacheInvChannel, string(raw)).Err()
}

func (dm *DataManager) handleCacheInvalidation(evt cacheInvalidationEvent) {
	if dm == nil {
		return
	}
	switch evt.Kind {
	case "cfg":
		if evt.OwnerID == 0 {
			dm.cfgCache.Clear()
		} else {
			dm.cfgCache.Delete(evt.OwnerID)
		}
	case "lic":
		if evt.OwnerID == 0 {
			dm.licCache.Clear()
		} else {
			dm.licCache.Delete(evt.OwnerID)
		}
	case "all":
		if evt.OwnerID == 0 {
			dm.cfgCache.Clear()
			dm.licCache.Clear()
		} else {
			dm.cfgCache.Delete(evt.OwnerID)
			dm.licCache.Delete(evt.OwnerID)
		}
	}
}

func (dm *DataManager) cacheGetConfig(ctx context.Context, ownerID int64) *JasebConfig {
	if dm == nil || ownerID == 0 {
		return nil
	}
	if cfg, ok := dm.cfgCache.Get(ownerID); ok && cfg != nil {
		return cloneJasebConfig(cfg)
	}
	rdb := dm.getRedis()
	if rdb == nil {
		return nil
	}
	key := fmt.Sprintf("cache:cfg:%d", ownerID)
	raw, err := rdb.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var cfg JasebConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	dm.cfgCache.Set(ownerID, cloneJasebConfig(&cfg))
	return &cfg
}

func (dm *DataManager) cacheSetConfig(ctx context.Context, cfg *JasebConfig, publish bool) {
	if dm == nil || cfg == nil || cfg.OwnerID == 0 {
		return
	}
	dm.cfgCache.Set(cfg.OwnerID, cloneJasebConfig(cfg))
	if rdb := dm.getRedis(); rdb != nil {
		if raw, err := json.Marshal(cfg); err == nil {
			key := fmt.Sprintf("cache:cfg:%d", cfg.OwnerID)
			_ = rdb.SetEx(ctx, key, raw, redisCfgCacheTTL).Err()
		}
		if publish {
			dm.publishCacheInv(ctx, cfg.OwnerID, "cfg")
		}
	}
}

func (dm *DataManager) cacheInvalidateConfig(ctx context.Context, ownerID int64, publish bool) {
	if dm == nil {
		return
	}
	if ownerID == 0 {
		dm.cfgCache.Clear()
	} else {
		dm.cfgCache.Delete(ownerID)
	}
	if rdb := dm.getRedis(); rdb != nil {
		if ownerID != 0 {
			_ = rdb.Del(ctx, fmt.Sprintf("cache:cfg:%d", ownerID)).Err()
		}
		if publish {
			dm.publishCacheInv(ctx, ownerID, "cfg")
		}
	}
}

func cloneUserLicense(ul *UserLicense) *UserLicense {
	if ul == nil {
		return nil
	}
	cp := *ul
	return &cp
}

func isCachedLicenseValid(ul *UserLicense) bool {
	if ul == nil || ul.Status != "active" {
		return false
	}
	return ul.ExpiresAt.After(time.Now())
}

func (dm *DataManager) cacheGetActiveLicense(ctx context.Context, ownerChatID int64) (*UserLicense, bool) {
	if dm == nil || ownerChatID == 0 {
		return nil, false
	}
	if ul, ok := dm.licCache.Get(ownerChatID); ok {
		if isCachedLicenseValid(ul) {
			return cloneUserLicense(ul), true
		}
		dm.licCache.Delete(ownerChatID)
	}
	rdb := dm.getRedis()
	if rdb == nil {
		return nil, false
	}
	key := fmt.Sprintf("cache:lic:%d", ownerChatID)
	raw, err := rdb.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var ul UserLicense
	if json.Unmarshal(raw, &ul) != nil || !isCachedLicenseValid(&ul) {
		_ = rdb.Del(ctx, key).Err()
		return nil, false
	}
	dm.licCache.Set(ownerChatID, cloneUserLicense(&ul))
	return &ul, true
}

func (dm *DataManager) cacheSetActiveLicense(ctx context.Context, ownerChatID int64, ul *UserLicense) {
	if dm == nil || ownerChatID == 0 || !isCachedLicenseValid(ul) {
		return
	}
	dm.licCache.Set(ownerChatID, cloneUserLicense(ul))
	if rdb := dm.getRedis(); rdb != nil {
		if raw, err := json.Marshal(ul); err == nil {
			_ = rdb.SetEx(ctx, fmt.Sprintf("cache:lic:%d", ownerChatID), raw, redisLicCacheTTL).Err()
		}
	}
}

func (dm *DataManager) cacheInvalidateLicense(ctx context.Context, ownerChatID int64, publish bool) {
	if dm == nil {
		return
	}
	if ownerChatID == 0 {
		dm.licCache.Clear()
	} else {
		dm.licCache.Delete(ownerChatID)
	}
	if rdb := dm.getRedis(); rdb != nil {
		if ownerChatID != 0 {
			_ = rdb.Del(ctx, fmt.Sprintf("cache:lic:%d", ownerChatID)).Err()
		}
		if publish {
			dm.publishCacheInv(ctx, ownerChatID, "lic")
		}
	}
}

func (dm *DataManager) GetWorkerAssignment(ctx context.Context, ownerID int64) (*WorkerAssignment, error) {
	if dm == nil || dm.sqlite == nil {
		return nil, nil
	}
	return dm.sqlite.GetWorkerAssignment(ctx, ownerID)
}

func (dm *DataManager) SaveWorkerAssignment(ctx context.Context, a *WorkerAssignment) error {
	if dm == nil || dm.sqlite == nil {
		return nil
	}
	return dm.sqlite.SaveWorkerAssignment(ctx, a)
}

func (dm *DataManager) ListWorkerAssignmentsByWorker(ctx context.Context, workerID string) ([]*WorkerAssignment, error) {
	if dm == nil || dm.sqlite == nil {
		return nil, nil
	}
	return dm.sqlite.ListWorkerAssignmentsByWorker(ctx, workerID)
}
