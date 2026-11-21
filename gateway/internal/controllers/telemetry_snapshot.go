package controllers

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

type adminSessionItem struct {
	ChatID int64  `json:"chat_id"`
	State  string `json:"state"`
}

type adminOverviewSnapshot struct {
	Online            int                  `json:"online"`
	Connecting        int                  `json:"connecting"`
	Reconnecting      int                  `json:"reconnecting"`
	Revoked           int                  `json:"revoked"`
	TotalSessions     int                  `json:"total_sessions"`
	Sessions          []adminSessionItem   `json:"sessions"`
	CPUAllImage       float64              `json:"cpu_all_image"`
	RAMAllImageMB     float64              `json:"ram_all_image_mb"`
	GatewayAllocMB    uint64               `json:"gateway_alloc_mb"`
	GatewaySysMB      uint64               `json:"gateway_sys_mb"`
	GatewayGoroutines int                  `json:"gateway_goroutines"`
	UptimeSec         int64                `json:"uptime_sec"`
	SQLiteBytes       int64                `json:"sqlite_bytes"`
	SQLiteWALBytes    int64                `json:"sqlite_wal_bytes"`
	RedisOnline       bool                 `json:"redis_online"`
	Simulasi          simulationMetricsDoc `json:"simulasi"`
	UpdatedAt         int64                `json:"updated_at"`
}

func fileSizeBytes(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (h *Handler) buildAdminOverviewSnapshot(ctx context.Context) adminOverviewSnapshot {
	snap := adminOverviewSnapshot{
		UpdatedAt: time.Now().Unix(),
	}

	if globalTelemetry != nil {
		globalTelemetry.mu.RLock()
		snap.Sessions = make([]adminSessionItem, 0, len(globalTelemetry.sessionStates))
		for chatID, st := range globalTelemetry.sessionStates {
			snap.Sessions = append(snap.Sessions, adminSessionItem{
				ChatID: chatID,
				State:  st,
			})
			switch strings.ToUpper(st) {
			case "CONNECTED":
				snap.Online++
			case "CONNECTING":
				snap.Connecting++
			case "RECONNECTING", "SUSPECTED":
				snap.Reconnecting++
			case "SESSION_INVALID":
				snap.Revoked++
			}
		}
		snap.TotalSessions = len(globalTelemetry.sessionStates)
		snap.CPUAllImage = globalTelemetry.cpuAllImage
		snap.RAMAllImageMB = globalTelemetry.ramAllImage
		snap.UptimeSec = int64(time.Since(globalTelemetry.bootTime).Seconds())
		snap.Simulasi = globalTelemetry.simulasiMetrics
		globalTelemetry.mu.RUnlock()

		sort.Slice(snap.Sessions, func(i, j int) bool {
			return snap.Sessions[i].ChatID < snap.Sessions[j].ChatID
		})
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	snap.GatewayAllocMB = ms.Alloc / 1024 / 1024
	snap.GatewaySysMB = ms.Sys / 1024 / 1024
	snap.GatewayGoroutines = runtime.NumGoroutine()

	sqlitePath := strings.TrimSpace(os.Getenv("SQLITE_PATH"))
	if sqlitePath == "" {
		sqlitePath = "data/userbot.db"
	}
	snap.SQLiteBytes = fileSizeBytes(sqlitePath)
	snap.SQLiteWALBytes = fileSizeBytes(sqlitePath + "-wal")

	if h != nil && h.broker != nil && h.broker.Client != nil {
		pCtx, cancel := context.WithTimeout(ctx, time.Second)
		if h.broker.Client.Ping(pCtx).Err() == nil {
			snap.RedisOnline = true
		}
		if raw, err := h.broker.Client.Get(pCtx, "simulasi:metrics").Result(); err == nil && raw != "" {
			var simDoc simulationMetricsDoc
			if json.Unmarshal([]byte(raw), &simDoc) == nil {
				snap.Simulasi = simDoc
			}
		}
		cancel()
	}

	return snap
}
