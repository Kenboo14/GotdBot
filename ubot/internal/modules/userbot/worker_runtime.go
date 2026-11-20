package userbot

import (
	"assterisk/ubot/internal/databases"
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
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

type workerHeartbeatDoc struct {
	WorkerID       string `json:"worker_id"`
	Pool           string `json:"pool"`
	ActiveSessions int    `json:"active_sessions"`
	MaxSessions    int    `json:"max_sessions"`
	RAMAllocMB     uint64 `json:"ram_alloc_mb"`
	RAMSysMB       uint64 `json:"ram_sys_mb"`
	Status         string `json:"status"`
	UpdatedAt      int64  `json:"updated_at"`
}

func (s *Service) ConfigureWorker(id, pool string, maxSessions int) {
	if s == nil {
		return
	}
	normPool := "normal"
	if strings.EqualFold(strings.TrimSpace(pool), "hell") {
		normPool = "hell"
	}
	s.mu.Lock()
	s.workerID = strings.TrimSpace(id)
	s.workerPool = normPool
	if maxSessions > 0 {
		s.workerMaxSessions = maxSessions
	}
	s.mu.Unlock()
}

func (s *Service) effectiveMaxSessionsLocked() int {
	if s.workerMaxSessions > 0 {
		return s.workerMaxSessions
	}
	return maxActiveSessions
}

func (s *Service) workerIdentity() (string, string) {
	if s == nil {
		return "", "normal"
	}
	s.mu.RLock()
	id := s.workerID
	pool := s.workerPool
	s.mu.RUnlock()
	if pool == "" {
		pool = "normal"
	}
	return id, pool
}

func (s *Service) isSessionOwnedByThisWorker(ctx context.Context, ownerID int64) bool {
	wid, pool := s.workerIdentity()
	if wid == "" || ownerID == 0 {
		return true
	}
	field := strconv.FormatInt(ownerID, 10)
	if s.redis != nil && s.redis.Client != nil {
		assigned, err := s.redis.Client.HGet(ctx, redisKeyOwnerWorker, field).Result()
		if err == nil && strings.TrimSpace(assigned) != "" {
			return strings.TrimSpace(assigned) == wid
		}
	}
	if s.sqlite != nil {
		if a, err := s.sqlite.GetWorkerAssignment(ctx, ownerID); err == nil && a != nil && strings.TrimSpace(a.WorkerID) != "" {
			if s.redis != nil && s.redis.Client != nil {
				_ = s.redis.Client.HSet(ctx, redisKeyOwnerWorker, field, a.WorkerID).Err()
				_ = s.redis.Client.HSet(ctx, redisKeyOwnerPool, field, a.Pool).Err()
			}
			return strings.TrimSpace(a.WorkerID) == wid
		}
	}
	if pool != "normal" {
		return false
	}
	s.mu.RLock()
	curLoad := len(s.active) + len(s.connecting)
	maxCap := s.effectiveMaxSessionsLocked()
	s.mu.RUnlock()
	if curLoad >= maxCap {
		return false
	}
	if s.redis != nil && s.redis.Client != nil {
		won, err := s.redis.Client.HSetNX(ctx, redisKeyOwnerWorker, field, wid).Result()
		if err != nil {
			return false
		}
		if !won {
			actual, _ := s.redis.Client.HGet(ctx, redisKeyOwnerWorker, field).Result()
			return strings.TrimSpace(actual) == wid
		}
		_ = s.redis.Client.HSet(ctx, redisKeyOwnerPool, field, pool).Err()
	}
	if s.sqlite != nil {
		_ = s.sqlite.SaveWorkerAssignment(ctx, &databases.WorkerAssignment{
			OwnerID:   ownerID,
			WorkerID:  wid,
			Pool:      pool,
			UpdatedAt: time.Now(),
		})
	}
	return true
}

func (s *Service) buildWorkerHeartbeatPayload(ctx context.Context) (string, []byte) {
	wid, pool := s.workerIdentity()
	if wid == "" {
		return "", nil
	}
	s.mu.RLock()
	activeCount := len(s.active) + len(s.connecting)
	maxCap := s.effectiveMaxSessionsLocked()
	s.mu.RUnlock()
	status := "ready"
	if s.redis != nil && s.redis.Client != nil {
		if val, err := s.redis.Client.Get(ctx, redisKeyWorkerDrain+wid).Result(); err == nil && val != "" {
			status = "draining"
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	doc := workerHeartbeatDoc{
		WorkerID:       wid,
		Pool:           pool,
		ActiveSessions: activeCount,
		MaxSessions:    maxCap,
		RAMAllocMB:     m.Alloc / 1024 / 1024,
		RAMSysMB:       m.Sys / 1024 / 1024,
		Status:         status,
		UpdatedAt:      time.Now().Unix(),
	}
	raw, _ := json.Marshal(doc)
	return wid, raw
}

func (s *Service) startWorkerHeartbeatLoop(ctx context.Context) {
	wid, _ := s.workerIdentity()
	if wid == "" || s.redis == nil || s.redis.Client == nil {
		return
	}
	publish := func() {
		id, raw := s.buildWorkerHeartbeatPayload(ctx)
		if id == "" || len(raw) == 0 {
			return
		}
		pCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = s.redis.Client.SAdd(pCtx, redisKeyWorkersSet, id).Err()
		_ = s.redis.Client.SetEx(pCtx, redisKeyWorkerPrefix+id, raw, 20*time.Second).Err()
		cancel()
	}
	publish()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}

func (s *Service) migrateSessionToWorker(ctx context.Context, chatID, ownerID int64, targetWorkerID, targetPool string, score int, reason string) error {
	if s == nil || strings.TrimSpace(targetWorkerID) == "" {
		return nil
	}
	if ownerID == 0 {
		ownerID = chatID
	}
	if chatID == 0 {
		chatID = ownerID
	}
	targetID := strings.TrimSpace(targetWorkerID)
	srcID, _ := s.workerIdentity()
	if targetPool == "" {
		targetPool = "normal"
		if strings.Contains(strings.ToLower(targetID), "hell") {
			targetPool = "hell"
		}
	}

	var targets []int64
	seen := make(map[int64]bool)
	addTarget := func(id int64) {
		if id != 0 && !seen[id] {
			seen[id] = true
			targets = append(targets, id)
		}
	}
	addTarget(chatID)
	addTarget(ownerID)

	var cancels []context.CancelFunc
	s.mu.Lock()
	for cid, oid := range s.owners {
		if cid == chatID || oid == ownerID {
			addTarget(cid)
		}
	}
	for _, uid := range targets {
		if pJob := s.panelBroadcasts[uid]; pJob != nil {
			delete(s.panelBroadcasts, uid)
			pJob.cancel()
		}
		if bJob := s.broadcasts[uid]; bJob != nil {
			delete(s.broadcasts, uid)
			bJob.cancel()
		}
		if c := s.activeCancels[uid]; c != nil {
			cancels = append(cancels, c)
			delete(s.activeCancels, uid)
		}
		if client := s.active[uid]; client != nil {
			delete(s.clientOwners, client)
		}
		delete(s.accountGens, uid)
		delete(s.connecting, uid)
		delete(s.active, uid)
		delete(s.accountCtxs, uid)
		delete(s.lastUpdate, uid)
		delete(s.lastVerified, uid)
		delete(s.verifying, uid)
		delete(s.reconnectAttempts, uid)
		s.accountStates[uid] = StateStopped
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	s.heavyMu.Lock()
	for _, uid := range targets {
		delete(s.heavyStates, uid)
	}
	s.heavyMu.Unlock()

	field := strconv.FormatInt(ownerID, 10)
	if s.redis != nil && s.redis.Client != nil {
		_ = s.redis.Client.HSet(ctx, redisKeyOwnerWorker, field, targetID).Err()
		_ = s.redis.Client.HSet(ctx, redisKeyOwnerPool, field, targetPool).Err()
	}
	if s.sqlite != nil {
		_ = s.sqlite.SaveWorkerAssignment(ctx, &databases.WorkerAssignment{
			OwnerID:    ownerID,
			WorkerID:   targetID,
			Pool:       targetPool,
			HeavyScore: score,
			MigratedAt: time.Now().Unix(),
			UpdatedAt:  time.Now(),
		})
	}

	if s.redis != nil && s.redis.Client != nil {
		raw, err := json.Marshal(models.PToService{
			ChatID:  chatID,
			UserID:  ownerID,
			OwnerID: ownerID,
			Type:    "activate_migrated_session",
			TaskID:  reason,
		})
		if err == nil {
			_ = s.redis.Client.XAdd(ctx, &redis.XAddArgs{
				Stream: "to:ubot:command:" + targetID,
				MaxLen: 1000,
				Approx: true,
				ID:     "*",
				Values: map[string]any{
					"payload": string(raw),
					"ts":      time.Now().Unix(),
				},
			}).Err()
		}
	}

	s.emitMonitorEvent("bot_loader", fmt.Sprintf("Migrasi sesi <code>%d</code>: <code>%s</code> → <code>%s</code> (pool <b>%s</b>, %s).", ownerID, srcID, targetID, targetPool, reason), chatID, ownerID)
	return nil
}
