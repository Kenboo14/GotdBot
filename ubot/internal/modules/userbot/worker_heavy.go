package userbot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	heavyPromoteScore = 70
	heavyDemoteScore  = 25
	heavyMinStreak    = 3
	heavyMinHellStay  = 6 * time.Hour
)

type heavyMinuteBucket struct {
	minute   int64
	updates  int
	floods   int
	okSends  int
	errSends int
}

type heavyChatState struct {
	buckets          [5]heavyMinuteBucket
	heavyStreak      int
	lightStreak      int
	enteredHellAt    time.Time
	consecutiveScore int
}

func (s *Service) getOrCreateHeavyStateLocked(chatID int64) *heavyChatState {
	if s.heavyStates == nil {
		s.heavyStates = make(map[int64]*heavyChatState)
	}
	st := s.heavyStates[chatID]
	if st == nil {
		st = &heavyChatState{}
		s.heavyStates[chatID] = st
	}
	return st
}

func (s *Service) updateHeavyBucket(chatID int64, fn func(b *heavyMinuteBucket)) {
	if s == nil || chatID == 0 {
		return
	}
	minute := time.Now().Unix() / 60
	idx := int(minute % 5)
	s.heavyMu.Lock()
	st := s.getOrCreateHeavyStateLocked(chatID)
	if st.buckets[idx].minute != minute {
		st.buckets[idx] = heavyMinuteBucket{minute: minute}
	}
	fn(&st.buckets[idx])
	s.heavyMu.Unlock()
}

func (s *Service) recordHeavyUpdate(chatID int64) {
	s.updateHeavyBucket(chatID, func(b *heavyMinuteBucket) { b.updates++ })
}

func (s *Service) recordHeavyFlood(chatID int64) {
	s.updateHeavyBucket(chatID, func(b *heavyMinuteBucket) { b.floods++ })
}

func (s *Service) recordHeavyBroadcastResult(chatID int64, ok bool) {
	s.updateHeavyBucket(chatID, func(b *heavyMinuteBucket) {
		if ok {
			b.okSends++
		} else {
			b.errSends++
		}
	})
}

func (s *Service) snapshotHeavyMetrics(chatID int64, now time.Time) (int, int, float64) {
	if s == nil || chatID == 0 {
		return 0, 0, 0
	}
	curMin := now.Unix() / 60
	s.heavyMu.Lock()
	defer s.heavyMu.Unlock()
	st := s.heavyStates[chatID]
	if st == nil {
		return 0, 0, 0
	}
	var totalUpdates, maxUpdatesPerMin, floods5m, okSends, errSends int
	for _, b := range st.buckets {
		if b.minute == 0 || curMin-b.minute >= 5 {
			continue
		}
		totalUpdates += b.updates
		if b.updates > maxUpdatesPerMin {
			maxUpdatesPerMin = b.updates
		}
		floods5m += b.floods
		okSends += b.okSends
		errSends += b.errSends
	}
	avgPerMin := totalUpdates / 5
	updatesPerMin := maxUpdatesPerMin
	if avgPerMin > updatesPerMin {
		updatesPerMin = avgPerMin
	}
	totalSends := okSends + errSends
	var errRate float64
	if totalSends >= 10 {
		errRate = float64(errSends) / float64(totalSends)
	}
	return updatesPerMin, floods5m, errRate
}

func computeHeavyScore(cfg jasebConfigDoc, updatesPerMin, floods5m int, errRate5m float64) int {
	score := 0
	gCount := len(cfg.Groups)
	if gCount >= 250 {
		score += 60
	} else if gCount >= 150 {
		score += 35
	}
	if len(cfg.Topics)+len(cfg.TopicLists) >= 20 {
		score += 20
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan") {
		delayMode := strings.TrimSpace(cfg.DelayMode)
		if (delayMode == "" || delayMode == "manual") && cfg.Delay <= 10 && gCount >= 50 {
			score += 35
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "Autoreply") && gCount >= 50 {
		score += 25
	}
	if updatesPerMin >= 120 {
		score += 40
	}
	if floods5m >= 3 {
		score += 35
	}
	if errRate5m >= 0.40 {
		score += 20
	}
	return score
}

func (s *Service) listWorkersFromRedis(ctx context.Context) []workerHeartbeatDoc {
	if s == nil || s.redis == nil || s.redis.Client == nil {
		return nil
	}
	ids, err := s.redis.Client.SMembers(ctx, redisKeyWorkersSet).Result()
	if err != nil || len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisKeyWorkerPrefix + id
	}
	vals, err := s.redis.Client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil
	}
	out := make([]workerHeartbeatDoc, 0, len(vals))
	for _, raw := range vals {
		str, ok := raw.(string)
		if !ok || strings.TrimSpace(str) == "" {
			continue
		}
		var doc workerHeartbeatDoc
		if json.Unmarshal([]byte(str), &doc) == nil && doc.WorkerID != "" {
			out = append(out, doc)
		}
	}
	return out
}

func selectWorkerTarget(workers []workerHeartbeatDoc, pool, excludeID string, maxRatio float64) (workerHeartbeatDoc, bool) {
	var best workerHeartbeatDoc
	bestScore := math.MaxFloat64
	found := false
	for _, w := range workers {
		if w.WorkerID == excludeID || !strings.EqualFold(w.Pool, pool) || w.Status != "ready" {
			continue
		}
		if w.MaxSessions <= 0 || w.ActiveSessions >= w.MaxSessions {
			continue
		}
		ratio := float64(w.ActiveSessions) / float64(w.MaxSessions)
		if maxRatio > 0 && ratio >= maxRatio {
			continue
		}
		score := ratio*0.7 + (float64(w.RAMAllocMB)/512.0)*0.3
		if !found || score < bestScore || (score == bestScore && w.WorkerID < best.WorkerID) {
			best = w
			bestScore = score
			found = true
		}
	}
	return best, found
}

func (s *Service) evaluateHeavyUsers(ctx context.Context) {
	wid, pool := s.workerIdentity()
	if wid == "" {
		return
	}
	s.mu.RLock()
	type sessionPair struct {
		chatID  int64
		ownerID int64
	}
	pairs := make([]sessionPair, 0, len(s.active))
	for cid, c := range s.active {
		if c == nil {
			continue
		}
		oid := s.owners[cid]
		if oid == 0 {
			oid = cid
		}
		pairs = append(pairs, sessionPair{chatID: cid, ownerID: oid})
	}
	s.mu.RUnlock()

	now := time.Now()
	workers := s.listWorkersFromRedis(ctx)
	for _, p := range pairs {
		cfg := s.getJasebConfig(ctx, p.ownerID)
		upm, fl5, errRate := s.snapshotHeavyMetrics(p.chatID, now)
		score := computeHeavyScore(cfg, upm, fl5, errRate)

		var moveTo *workerHeartbeatDoc
		targetPool := ""
		reason := ""

		s.heavyMu.Lock()
		st := s.getOrCreateHeavyStateLocked(p.chatID)
		st.consecutiveScore = score
		if pool == "normal" {
			if score >= heavyPromoteScore {
				st.heavyStreak++
				st.lightStreak = 0
			} else {
				st.heavyStreak = 0
			}
			if st.heavyStreak >= heavyMinStreak {
				if target, ok := selectWorkerTarget(workers, "hell", wid, 0); ok {
					cp := target
					moveTo = &cp
					targetPool = "hell"
					reason = fmt.Sprintf("auto_heavy(score=%d)", score)
					st.heavyStreak = 0
				}
			}
		} else if pool == "hell" {
			if st.enteredHellAt.IsZero() {
				st.enteredHellAt = now
			}
			if score <= heavyDemoteScore {
				st.lightStreak++
			} else {
				st.lightStreak = 0
			}
			if now.Sub(st.enteredHellAt) >= heavyMinHellStay && st.lightStreak >= heavyMinStreak {
				if target, ok := selectWorkerTarget(workers, "normal", wid, 0.85); ok {
					cp := target
					moveTo = &cp
					targetPool = "normal"
					reason = fmt.Sprintf("auto_cooldown(score=%d)", score)
					st.lightStreak = 0
				}
			}
		}
		s.heavyMu.Unlock()

		if moveTo != nil {
			_ = s.migrateSessionToWorker(ctx, p.chatID, p.ownerID, moveTo.WorkerID, targetPool, score, reason)
		}
	}
}

func (s *Service) startHeavyScoreMonitorLoop(ctx context.Context) {
	wid, _ := s.workerIdentity()
	if wid == "" {
		return
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluateHeavyUsers(ctx)
		}
	}
}
