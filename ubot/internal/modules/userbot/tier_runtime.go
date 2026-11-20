package userbot

import (
	"context"
	"strings"
	"time"
)

func normalizeUbotTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "vip":
		return "Vip"
	case "pro++", "proplus", "pro+":
		return "Pro++"
	case "pro":
		return "Pro"
	default:
		return "Free"
	}
}

func ubotTierRank(tier string) int {
	switch normalizeUbotTier(tier) {
	case "Vip":
		return 4
	case "Pro++":
		return 3
	case "Pro":
		return 2
	default:
		return 1
	}
}

func (s *Service) getActiveTier(ctx context.Context, ownerID int64) string {
	if s == nil || s.sqlite == nil || s.sqlite.DB == nil || ownerID == 0 {
		return ""
	}
	now := time.Now()
	var activeSHA string
	if err := s.sqlite.DB.QueryRowContext(ctx,
		`SELECT COALESCE(active_token_sha, '') FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`,
		ownerID,
	).Scan(&activeSHA); err == nil && strings.TrimSpace(activeSHA) != "" {
		var tier string
		var exp time.Time
		if qErr := s.sqlite.DB.QueryRowContext(ctx,
			`SELECT tier, expires_at FROM user_licenses WHERE token_sha = ? AND (owner_chat_id = ? OR userbot_id = ?) AND status = 'active' LIMIT 1;`,
			strings.TrimSpace(activeSHA), ownerID, ownerID,
		).Scan(&tier, &exp); qErr == nil && exp.After(now) {
			return normalizeUbotTier(tier)
		}
	}
	rows, err := s.sqlite.DB.QueryContext(ctx,
		`SELECT tier, expires_at FROM user_licenses WHERE (owner_chat_id = ? OR userbot_id = ?) AND status = 'active';`,
		ownerID, ownerID,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()

	bestTier := ""
	bestRank := 0
	var bestExp time.Time
	for rows.Next() {
		var tier string
		var exp time.Time
		if scanErr := rows.Scan(&tier, &exp); scanErr != nil {
			continue
		}
		if !exp.After(now) {
			continue
		}
		rank := ubotTierRank(tier)
		if rank > bestRank || (rank == bestRank && exp.After(bestExp)) {
			bestRank = rank
			bestTier = normalizeUbotTier(tier)
			bestExp = exp
		}
	}
	return bestTier
}

func freeDailyLimitForTier(tier string) int {
	if strings.EqualFold(strings.TrimSpace(tier), "Free") {
		return 50
	}
	return 0
}

func enforceTierBroadcastConfig(cfg jasebConfigDoc, tier string) jasebConfigDoc {
	if !strings.EqualFold(strings.TrimSpace(tier), "Free") {
		return cfg
	}
	cfg.DelayMode = "aman"
	cfg.Delay = 0
	cfg.DelayAllGroups = 0
	cfg.SkipFloodWait = false
	cfg.SkipFloodWaitSec = 0
	if len(cfg.Groups) > 30 {
		cfg.Groups = cfg.Groups[:30]
	}
	if len(cfg.Lists) > 1 {
		cfg.Lists = cfg.Lists[:1]
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "Forward") {
		cfg.Mode = "Normal"
	}
	return cfg
}

func todayWIBKey(now time.Time) string {
	return now.In(jakartaLocation()).Format("2006-01-02")
}

func nextMidnightWIB(now time.Time) time.Time {
	loc := jakartaLocation()
	t := now.In(loc)
	nextDay := t.AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, loc)
}

func (s *Service) waitUntilQuotaResetOrUpgrade(ctx context.Context, ownerID int64, initialTier, dateWIB string, resetAt time.Time) bool {
	for {
		remaining := time.Until(resetAt)
		if remaining <= 0 {
			return true
		}
		wait := remaining
		if wait > 2500*time.Millisecond {
			wait = 2500 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		cfg := s.getJasebConfig(ctx, ownerID)
		if !strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan") {
			return false
		}
		if curTier := s.getActiveTier(ctx, ownerID); curTier != "" && !strings.EqualFold(curTier, initialTier) {
			return true
		}
		if todayWIBKey(time.Now()) != dateWIB {
			return true
		}
	}
}
