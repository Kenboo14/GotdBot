package controllers

import (
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisKeyWorkersSet   = "lb:workers"
	redisKeyWorkerPrefix = "lb:worker:"
	redisKeyWorkerDrain  = "lb:worker_drain:"
	redisKeyOwnerWorker  = "lb:owner_worker"
	redisKeyOwnerPool    = "lb:owner_pool"
)

type WorkerStatus struct {
	WorkerID       string `json:"worker_id"`
	Pool           string `json:"pool"`
	ActiveSessions int    `json:"active_sessions"`
	MaxSessions    int    `json:"max_sessions"`
	RAMAllocMB     uint64 `json:"ram_alloc_mb"`
	RAMSysMB       uint64 `json:"ram_sys_mb"`
	Status         string `json:"status"`
	UpdatedAt      int64  `json:"updated_at"`
}

type WorkerLoadBalancer struct {
	rdb         *redis.Client
	broker      *broker.Redis
	db          *databases.DataManager
	mu          sync.Mutex
	missedBeats map[string]int
}

func NewWorkerLoadBalancer(brk *broker.Redis, db *databases.DataManager) *WorkerLoadBalancer {
	var rdb *redis.Client
	if brk != nil {
		rdb = brk.Client
	}
	return &WorkerLoadBalancer{
		rdb:         rdb,
		broker:      brk,
		db:          db,
		missedBeats: make(map[string]int),
	}
}

func normalizeWorkerPool(pool string) string {
	if strings.EqualFold(strings.TrimSpace(pool), "hell") {
		return "hell"
	}
	return "normal"
}

func (lb *WorkerLoadBalancer) GetKnownWorkerIDs(ctx context.Context) ([]string, error) {
	if lb == nil || lb.rdb == nil {
		return nil, nil
	}
	ids, err := lb.rdb.SMembers(ctx, redisKeyWorkersSet).Result()
	if err != nil {
		return nil, fmt.Errorf("smembers %s: %w", redisKeyWorkersSet, err)
	}
	sort.Strings(ids)
	return ids, nil
}

func (lb *WorkerLoadBalancer) ListWorkers(ctx context.Context) ([]WorkerStatus, error) {
	ids, err := lb.GetKnownWorkerIDs(ctx)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisKeyWorkerPrefix + id
	}
	vals, err := lb.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("mget worker statuses: %w", err)
	}
	live := make([]WorkerStatus, 0, len(vals))
	for _, raw := range vals {
		str, ok := raw.(string)
		if !ok || strings.TrimSpace(str) == "" {
			continue
		}
		var ws WorkerStatus
		if json.Unmarshal([]byte(str), &ws) == nil && ws.WorkerID != "" {
			ws.Pool = normalizeWorkerPool(ws.Pool)
			live = append(live, ws)
		}
	}
	sort.Slice(live, func(i, j int) bool {
		return live[i].WorkerID < live[j].WorkerID
	})
	return live, nil
}

func workerLoadScore(w WorkerStatus) float64 {
	if w.MaxSessions <= 0 {
		return math.MaxFloat64
	}
	sessionRatio := float64(w.ActiveSessions) / float64(w.MaxSessions)
	ramRatio := float64(w.RAMAllocMB) / 512.0
	return sessionRatio*0.7 + ramRatio*0.3
}

func SelectLeastLoaded(workers []WorkerStatus, pool string) (WorkerStatus, bool) {
	targetPool := normalizeWorkerPool(pool)
	var best WorkerStatus
	bestScore := math.MaxFloat64
	found := false
	for _, w := range workers {
		if normalizeWorkerPool(w.Pool) != targetPool || w.Status != "ready" {
			continue
		}
		if w.MaxSessions <= 0 || w.ActiveSessions >= w.MaxSessions {
			continue
		}
		score := workerLoadScore(w)
		if !found || score < bestScore || (score == bestScore && w.WorkerID < best.WorkerID) {
			best = w
			bestScore = score
			found = true
		}
	}
	return best, found
}

func selectWorkerWithFallback(workers []WorkerStatus, desiredPool string) (WorkerStatus, bool) {
	if target, ok := SelectLeastLoaded(workers, desiredPool); ok {
		return target, true
	}
	altPool := "hell"
	if normalizeWorkerPool(desiredPool) == "hell" {
		altPool = "normal"
	}
	return SelectLeastLoaded(workers, altPool)
}

func isWorkerAlive(workers []WorkerStatus, workerID string) (WorkerStatus, bool) {
	for _, w := range workers {
		if w.WorkerID == workerID {
			return w, true
		}
	}
	return WorkerStatus{}, false
}

func (lb *WorkerLoadBalancer) GetOwnerWorker(ctx context.Context, ownerID int64) (string, string) {
	if lb == nil || ownerID == 0 {
		return "", ""
	}
	field := strconv.FormatInt(ownerID, 10)
	var wid, pool string
	if lb.rdb != nil {
		wid, _ = lb.rdb.HGet(ctx, redisKeyOwnerWorker, field).Result()
		pool, _ = lb.rdb.HGet(ctx, redisKeyOwnerPool, field).Result()
	}
	if wid == "" && lb.db != nil {
		if a, err := lb.db.GetWorkerAssignment(ctx, ownerID); err == nil && a != nil {
			wid = a.WorkerID
			pool = a.Pool
			if lb.rdb != nil && wid != "" {
				_ = lb.rdb.HSet(ctx, redisKeyOwnerWorker, field, wid).Err()
				_ = lb.rdb.HSet(ctx, redisKeyOwnerPool, field, normalizeWorkerPool(pool)).Err()
			}
		}
	}
	return strings.TrimSpace(wid), normalizeWorkerPool(pool)
}

func (lb *WorkerLoadBalancer) SetOwnerAssignment(ctx context.Context, ownerID int64, workerID, pool string, heavyScore int) error {
	if lb == nil || ownerID == 0 || strings.TrimSpace(workerID) == "" {
		return nil
	}
	wid := strings.TrimSpace(workerID)
	normPool := normalizeWorkerPool(pool)
	field := strconv.FormatInt(ownerID, 10)
	if lb.rdb != nil {
		if err := lb.rdb.HSet(ctx, redisKeyOwnerWorker, field, wid).Err(); err != nil {
			return fmt.Errorf("hset owner worker: %w", err)
		}
		if err := lb.rdb.HSet(ctx, redisKeyOwnerPool, field, normPool).Err(); err != nil {
			return fmt.Errorf("hset owner pool: %w", err)
		}
	}
	if lb.db != nil {
		if err := lb.db.SaveWorkerAssignment(ctx, &databases.WorkerAssignment{
			OwnerID:    ownerID,
			WorkerID:   wid,
			Pool:       normPool,
			HeavyScore: heavyScore,
			MigratedAt: time.Now().Unix(),
			UpdatedAt:  time.Now(),
		}); err != nil {
			return fmt.Errorf("save sqlite worker assignment: %w", err)
		}
	}
	return nil
}

func (lb *WorkerLoadBalancer) ResolveStream(ctx context.Context, stream string, data models.PToService) (string, error) {
	if lb == nil || lb.rdb == nil {
		return stream, nil
	}
	live, err := lb.ListWorkers(ctx)
	if err != nil || len(live) == 0 {
		return stream, nil
	}
	ownerID := data.OwnerID
	if ownerID == 0 {
		ownerID = data.UserID
	}
	if ownerID == 0 {
		ownerID = data.ChatID
	}
	if ownerID == 0 {
		if chosen, ok := SelectLeastLoaded(live, "normal"); ok {
			return stream + ":" + chosen.WorkerID, nil
		}
		return stream + ":" + live[0].WorkerID, nil
	}

	wid, desiredPool := lb.GetOwnerWorker(ctx, ownerID)
	if wid != "" {
		if _, alive := isWorkerAlive(live, wid); alive {
			return stream + ":" + wid, nil
		}
	}

	if stream == "to:ubot:auth" {
		if data.Type == "auth_cancel" {
			return stream + ":" + live[0].WorkerID, nil
		}
		chosen, ok := SelectLeastLoaded(live, "normal")
		if !ok {
			return "", broker.ErrWorkerPoolFull
		}
		_ = lb.SetOwnerAssignment(ctx, ownerID, chosen.WorkerID, chosen.Pool, 0)
		return stream + ":" + chosen.WorkerID, nil
	}

	chosen, ok := selectWorkerWithFallback(live, desiredPool)
	if !ok {
		return "", broker.ErrWorkerPoolFull
	}
	_ = lb.SetOwnerAssignment(ctx, ownerID, chosen.WorkerID, chosen.Pool, 0)
	return stream + ":" + chosen.WorkerID, nil
}
