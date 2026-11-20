package controllers

import (
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"time"
)

func (lb *WorkerLoadBalancer) TriggerMigration(ctx context.Context, ownerID int64, targetWorkerID, targetPool, reason string) error {
	if lb == nil || lb.broker == nil {
		return fmt.Errorf("load balancer broker not ready")
	}
	live, err := lb.ListWorkers(ctx)
	if err != nil {
		return err
	}
	targetWS, ok := isWorkerAlive(live, targetWorkerID)
	if !ok {
		return fmt.Errorf("worker tujuan %s sedang offline", targetWorkerID)
	}
	if targetPool == "" {
		targetPool = targetWS.Pool
	}
	normPool := normalizeWorkerPool(targetPool)
	srcWorker, _ := lb.GetOwnerWorker(ctx, ownerID)
	if srcWorker == targetWorkerID {
		return nil
	}
	if srcWorker != "" {
		if _, alive := isWorkerAlive(live, srcWorker); alive {
			return lb.broker.ToStream(ctx, "to:ubot:command:"+srcWorker, models.PToService{
				ChatID:  ownerID,
				UserID:  ownerID,
				OwnerID: ownerID,
				Type:    "migrate_out",
				TaskID:  normPool,
				Text:    targetWorkerID,
			})
		}
	}
	if err := lb.SetOwnerAssignment(ctx, ownerID, targetWorkerID, normPool, 0); err != nil {
		return err
	}
	if err := lb.broker.ToStream(ctx, "to:ubot:command:"+targetWorkerID, models.PToService{
		ChatID:  ownerID,
		UserID:  ownerID,
		OwnerID: ownerID,
		Type:    "activate_migrated_session",
		TaskID:  reason,
	}); err != nil {
		return fmt.Errorf("trigger activate migrated session: %w", err)
	}
	return nil
}

func (lb *WorkerLoadBalancer) SetWorkerDrain(ctx context.Context, workerID string, drain bool) error {
	if lb == nil || lb.rdb == nil {
		return fmt.Errorf("redis tidak tersedia")
	}
	key := redisKeyWorkerDrain + workerID
	if drain {
		return lb.rdb.Set(ctx, key, "1", 0).Err()
	}
	return lb.rdb.Del(ctx, key).Err()
}

func (lb *WorkerLoadBalancer) listAssignedOwners(ctx context.Context, workerID string) []int64 {
	seen := make(map[int64]bool)
	var owners []int64
	if lb.rdb != nil {
		if allMap, err := lb.rdb.HGetAll(ctx, redisKeyOwnerWorker).Result(); err == nil {
			for k, v := range allMap {
				if v == workerID {
					if uid, pErr := strconv.ParseInt(k, 10, 64); pErr == nil && uid > 0 && !seen[uid] {
						seen[uid] = true
						owners = append(owners, uid)
					}
				}
			}
		}
	}
	if lb.db != nil {
		if rows, err := lb.db.ListWorkerAssignmentsByWorker(ctx, workerID); err == nil {
			for _, r := range rows {
				if r != nil && r.OwnerID > 0 && !seen[r.OwnerID] {
					seen[r.OwnerID] = true
					owners = append(owners, r.OwnerID)
				}
			}
		}
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i] < owners[j] })
	return owners
}

func incrementWorkerLoad(live []WorkerStatus, workerID string) {
	for idx := range live {
		if live[idx].WorkerID == workerID {
			live[idx].ActiveSessions++
			return
		}
	}
}

func (lb *WorkerLoadBalancer) DrainWorker(ctx context.Context, workerID string) (int, error) {
	if err := lb.SetWorkerDrain(ctx, workerID, true); err != nil {
		return 0, err
	}
	live, err := lb.ListWorkers(ctx)
	if err != nil {
		return 0, err
	}
	owners := lb.listAssignedOwners(ctx, workerID)
	migrated := 0
	for _, uid := range owners {
		candidates := make([]WorkerStatus, 0, len(live))
		for idx := range live {
			if live[idx].WorkerID != workerID {
				candidates = append(candidates, live[idx])
			}
		}
		_, desiredPool := lb.GetOwnerWorker(ctx, uid)
		target, ok := selectWorkerWithFallback(candidates, desiredPool)
		if !ok {
			continue
		}
		if err := lb.TriggerMigration(ctx, uid, target.WorkerID, target.Pool, "worker_drain"); err == nil {
			migrated++
			incrementWorkerLoad(live, target.WorkerID)
		}
	}
	return migrated, nil
}

func (lb *WorkerLoadBalancer) CheckFailoverOnce(ctx context.Context) int {
	if lb == nil || lb.rdb == nil {
		return 0
	}
	ids, err := lb.GetKnownWorkerIDs(ctx)
	if err != nil || len(ids) == 0 {
		return 0
	}
	live, err := lb.ListWorkers(ctx)
	if err != nil {
		return 0
	}
	liveSet := make(map[string]WorkerStatus, len(live))
	for _, w := range live {
		liveSet[w.WorkerID] = w
	}

	reassigned := 0
	var deadWorkers []string
	lb.mu.Lock()
	for _, id := range ids {
		if _, ok := liveSet[id]; ok {
			delete(lb.missedBeats, id)
			continue
		}
		lb.missedBeats[id]++
		if lb.missedBeats[id] >= 2 {
			deadWorkers = append(deadWorkers, id)
		}
	}
	lb.mu.Unlock()

	for _, deadID := range deadWorkers {
		owners := lb.listAssignedOwners(ctx, deadID)
		if len(owners) == 0 {
			continue
		}
		moved := 0
		for _, uid := range owners {
			_, desiredPool := lb.GetOwnerWorker(ctx, uid)
			target, ok := selectWorkerWithFallback(live, desiredPool)
			if !ok {
				break
			}
			if err := lb.SetOwnerAssignment(ctx, uid, target.WorkerID, target.Pool, 0); err != nil {
				continue
			}
			if lb.broker != nil {
				_ = lb.broker.ToStream(ctx, "to:ubot:command:"+target.WorkerID, models.PToService{
					ChatID:  uid,
					UserID:  uid,
					OwnerID: uid,
					Type:    "activate_migrated_session",
					TaskID:  "failover_from_" + deadID,
				})
			}
			incrementWorkerLoad(live, target.WorkerID)
			moved++
			reassigned++
		}
		if moved > 0 {
			globalDevMonitor.NotifyBotLoader("load-balancer", fmt.Sprintf("Failover otomatis: memindahkan <code>%d</code> user dari worker offline <code>%s</code>.", moved, html.EscapeString(deadID)))
		}
	}
	return reassigned
}

func (lb *WorkerLoadBalancer) StartFailoverLoop(ctx context.Context) {
	if lb == nil || lb.rdb == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lb.CheckFailoverOnce(ctx)
		}
	}
}
