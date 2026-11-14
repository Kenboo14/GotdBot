package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type containerBrief struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

type containerStatsPayload struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

type stackResourceStats struct {
	TotalCPU   float64
	TotalMemMB float64
}

func fetchDockerStackStats(ctx context.Context) (*stackResourceStats, error) {
	socketPath := "/var/run/docker.sock"
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("docker socket not accessible: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 4 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/containers/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create containers request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code listing containers: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read containers list: %w", err)
	}

	var containers []containerBrief
	if err := json.Unmarshal(body, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse containers list: %w", err)
	}

	var targetIDs []string
	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		isTarget := false
		if strings.EqualFold(c.Labels["com.docker.compose.project"], "gotdbot") {
			isTarget = true
		} else {
			for _, n := range c.Names {
				if strings.Contains(strings.ToLower(n), "gotdbot") {
					isTarget = true
					break
				}
			}
		}
		if isTarget {
			targetIDs = append(targetIDs, c.ID)
		}
	}

	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("no matching gotdbot containers found")
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res stackResourceStats
	)

	for _, id := range targetIDs {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			cpu, mem, err := readSingleContainerStats(ctx, client, cid)
			if err != nil {
				return
			}
			mu.Lock()
			res.TotalCPU += cpu
			res.TotalMemMB += mem
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	return &res, nil
}

func readSingleContainerStats(ctx context.Context, client *http.Client, cid string) (float64, float64, error) {
	url := fmt.Sprintf("http://localhost/containers/%s/stats?stream=false", cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("stats req error: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("stats do error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("stats status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("stats read error: %w", err)
	}

	var s containerStatsPayload
	if err := json.Unmarshal(body, &s); err != nil {
		return 0, 0, fmt.Errorf("stats unmarshal error: %w", err)
	}

	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	onlineCPUs := float64(s.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1
	}

	cpuPercent := 0.0
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	memUsage := s.MemoryStats.Usage
	var cache uint64
	if val, ok := s.MemoryStats.Stats["inactive_file"]; ok {
		cache = val
	} else if val, ok := s.MemoryStats.Stats["total_inactive_file"]; ok {
		cache = val
	} else if val, ok := s.MemoryStats.Stats["cache"]; ok {
		cache = val
	}
	if memUsage > cache {
		memUsage -= cache
	}
	memMB := float64(memUsage) / (1024 * 1024)

	return cpuPercent, memMB, nil
}

func parseRedisMemoryMB(ctx context.Context, rdb *redis.Client) float64 {
	if rdb == nil {
		return 0
	}
	info, err := rdb.Info(ctx, "memory").Result()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "used_memory_rss:") {
			valStr := strings.TrimPrefix(line, "used_memory_rss:")
			if bytes, err := strconv.ParseUint(strings.TrimSpace(valStr), 10, 64); err == nil {
				return float64(bytes) / (1024 * 1024)
			}
		}
	}
	return 0
}

func (t *telemetryTracker) updateSystemResourceStats(ctx context.Context) {
	stats, err := fetchDockerStackStats(ctx)
	if err == nil && stats != nil && stats.TotalMemMB > 0 {
		t.mu.Lock()
		t.cpuAllImage = stats.TotalCPU
		t.ramAllImage = stats.TotalMemMB
		t.mu.Unlock()
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	gwSysMB := float64(m.Sys) / (1024 * 1024)

	t.mu.RLock()
	ubotSysMB := float64(t.simulasiMetrics.RAMSysMB)
	t.mu.RUnlock()

	redisMB := parseRedisMemoryMB(ctx, t.rdb)
	totalMem := gwSysMB + ubotSysMB + redisMB

	t.mu.Lock()
	t.ramAllImage = totalMem
	t.mu.Unlock()
}
