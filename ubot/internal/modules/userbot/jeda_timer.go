package userbot

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dailyTimerWindow struct {
	StartMin int
	EndMin   int
	StartStr string
	EndStr   string
}

var (
	jakartaOnce sync.Once
	jakartaLoc  *time.Location
)

func jakartaLocation() *time.Location {
	jakartaOnce.Do(func() {
		if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil && loc != nil {
			jakartaLoc = loc
			return
		}
		jakartaLoc = time.FixedZone("WIB", 7*3600)
	})
	return jakartaLoc
}

func computeAutoDelay(groupCount int) (int, int) {
	switch {
	case groupCount <= 0:
		return 30, 0
	case groupCount < 10:
		return 5, 25
	case groupCount < 30:
		return 10, 20
	case groupCount < 50:
		return 30, 10
	case groupCount < 100:
		return 35, 0
	case groupCount < 200:
		return 45, 0
	default:
		return 60, 0
	}
}

func computeNextBroadcastWait(cfg jasebConfigDoc, groupIndex, totalGroups int, randIntn func(int) int) time.Duration {
	isEndOfRound := totalGroups > 0 && groupIndex == totalGroups-1
	mode := strings.ToLower(strings.TrimSpace(cfg.DelayMode))

	var wait time.Duration
	switch mode {
	case "aman":
		if randIntn == nil {
			randIntn = rand.IntN
		}
		wait = time.Duration(200+randIntn(201)) * time.Second
	case "otomatis":
		pg, pa := computeAutoDelay(totalGroups)
		if isEndOfRound && pa > 0 {
			wait = time.Duration(pa) * time.Minute
		} else if pg > 0 {
			wait = time.Duration(pg) * time.Second
		}
	default:
		pg := cfg.Delay
		if pg < 0 {
			pg = 0
		}
		pa := cfg.DelayAllGroups
		if pa < 0 {
			pa = 0
		}
		if pg == 0 && pa == 0 {
			wait = 0
		} else if isEndOfRound && pa > 0 {
			wait = time.Duration(pa) * time.Minute
		} else if pg > 0 {
			wait = time.Duration(pg) * time.Second
		}
	}

	if cfg.Safemode {
		wait += 500 * time.Millisecond
	}
	return wait
}

func shouldSkipFloodWait(cfg jasebConfigDoc, flood time.Duration) bool {
	if cfg.SkipFloodWaitSec > 0 {
		return flood > time.Duration(cfg.SkipFloodWaitSec)*time.Second
	}
	if cfg.SkipFloodWait {
		return true
	}
	return false
}

func parseClockHHMM(raw string) (int, int, string, bool) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.TrimSuffix(clean, "wib")
	clean = strings.TrimSpace(clean)
	clean = strings.ReplaceAll(clean, ".", ":")
	parts := strings.Split(clean, ":")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 2 || len(parts[1]) != 2 {
		return 0, 0, "", false
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, "", false
	}
	return h, m, fmt.Sprintf("%02d:%02d", h, m), true
}

func parseDailyTimerWindow(raw string) (dailyTimerWindow, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" || strings.EqualFold(clean, "Belum diatur") {
		return dailyTimerWindow{}, false
	}
	clean = strings.TrimPrefix(clean, "⏰")
	clean = strings.TrimPrefix(clean, "⏱️")
	clean = strings.TrimSpace(clean)
	lower := strings.ToLower(clean)
	lower = strings.TrimSpace(strings.TrimSuffix(lower, "wib"))

	for _, sep := range []string{"-", "–", "—", "s/d", "sampai"} {
		if strings.Contains(lower, sep) {
			parts := strings.SplitN(lower, sep, 2)
			if len(parts) == 2 {
				sh, sm, sCanon, okS := parseClockHHMM(parts[0])
				eh, em, eCanon, okE := parseClockHHMM(parts[1])
				if okS && okE && sCanon != eCanon {
					return dailyTimerWindow{
						StartMin: sh*60 + sm,
						EndMin:   eh*60 + em,
						StartStr: sCanon,
						EndStr:   eCanon,
					}, true
				}
			}
			return dailyTimerWindow{}, false
		}
	}
	return dailyTimerWindow{}, false
}

func isWithinDailyTimer(w dailyTimerWindow, now time.Time) bool {
	t := now.In(jakartaLocation())
	curMin := t.Hour()*60 + t.Minute()
	if w.StartMin < w.EndMin {
		return curMin >= w.StartMin && curMin < w.EndMin
	}
	return curMin >= w.StartMin || curMin < w.EndMin
}

func nextDailyTimerStart(w dailyTimerWindow, now time.Time) time.Time {
	loc := jakartaLocation()
	t := now.In(loc)
	startH := w.StartMin / 60
	startM := w.StartMin % 60
	candidate := time.Date(t.Year(), t.Month(), t.Day(), startH, startM, 0, 0, loc)
	if !candidate.After(t) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

func shouldPauseForDailyTimer(timerRaw string, now time.Time) (bool, time.Time, dailyTimerWindow) {
	win, ok := parseDailyTimerWindow(timerRaw)
	if !ok {
		return false, time.Time{}, dailyTimerWindow{}
	}
	if isWithinDailyTimer(win, now) {
		return false, time.Time{}, win
	}
	return true, nextDailyTimerStart(win, now), win
}

func jedaConfigFingerprint(cfg jasebConfigDoc) string {
	return fmt.Sprintf("%s|%d|%d|%s", strings.ToLower(strings.TrimSpace(cfg.DelayMode)), cfg.Delay, cfg.DelayAllGroups, strings.TrimSpace(cfg.Timer))
}

func formatUbotManualJeda(perGroupSec, perAllGroupsMin int) string {
	parts := make([]string, 0, 2)
	if perGroupSec > 0 {
		parts = append(parts, fmt.Sprintf("%d detik · antar-grup", perGroupSec))
	}
	if perAllGroupsMin > 0 {
		switch perAllGroupsMin {
		case 240:
			parts = append(parts, "4 jam · antar-putaran")
		case 360:
			parts = append(parts, "6 jam · antar-putaran")
		default:
			parts = append(parts, fmt.Sprintf("%d menit · antar-putaran", perAllGroupsMin))
		}
	}
	if len(parts) == 0 {
		return "Tanpa jeda"
	}
	return strings.Join(parts, ", ")
}

func formatUbotJedaSummary(cfg jasebConfigDoc) string {
	switch strings.ToLower(strings.TrimSpace(cfg.DelayMode)) {
	case "aman":
		return "Jeda Aman (200–400 detik)"
	case "otomatis":
		if len(cfg.Groups) > 0 {
			pg, pa := computeAutoDelay(len(cfg.Groups))
			return fmt.Sprintf("Jeda Otomatis (%s)", formatUbotManualJeda(pg, pa))
		}
		return "Jeda Otomatis"
	default:
		return formatUbotManualJeda(cfg.Delay, cfg.DelayAllGroups)
	}
}

func (s *Service) isDailyTimerActiveNow(ctx context.Context, ownerID int64) bool {
	cfg := s.getJasebConfig(ctx, ownerID)
	paused, _, _ := shouldPauseForDailyTimer(cfg.Timer, time.Now())
	return !paused
}
