package simulation

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	Status           string  `json:"status"`
	TargetUsers      int     `json:"target_users"`
	ActiveUsers      int     `json:"active_users"`
	LowProfile       int     `json:"low_profile"`
	MedProfile       int     `json:"med_profile"`
	HighProfile      int     `json:"high_profile"`
	GeneratedUpdates int64   `json:"generated_updates"`
	ProcessedUpdates int64   `json:"processed_updates"`
	FailedUpdates    int64   `json:"failed_updates"`
	PendingQueue     int     `json:"pending_queue"`
	ThroughputSec    float64 `json:"throughput_sec"`
	LatencyP50Ms     float64 `json:"latency_p50_ms"`
	LatencyP95Ms     float64 `json:"latency_p95_ms"`
	LatencyP99Ms     float64 `json:"latency_p99_ms"`
	UptimeSec        int64   `json:"uptime_sec"`
	Goroutines       int     `json:"goroutines"`
	RAMAllocMB       uint64  `json:"ram_alloc_mb"`
	RAMSysMB         uint64  `json:"ram_sys_mb"`
	ErrorsCount      int64   `json:"errors_count"`
	UpdatedAt        int64   `json:"updated_at"`
}

type MetricsCollector struct {
	mu           sync.RWMutex
	status       string
	targetUsers  int
	activeUsers  int
	lowProfile   int
	medProfile   int
	highProfile  int
	pendingQueue int
	goroutines   int
	ramAllocMB   uint64
	ramSysMB     uint64
	startTime    time.Time

	generated atomic.Int64
	processed atomic.Int64
	failed    atomic.Int64
	errors    atomic.Int64

	lastProcessed int64
	lastCalcTime  time.Time
	throughput    float64

	latencies []float64
	maxSample int
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		status:       "STOPPED",
		lastCalcTime: time.Now(),
		maxSample:    2000,
		latencies:    make([]float64, 0, 2000),
	}
}

func (m *MetricsCollector) Start(target int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = "RUNNING"
	m.targetUsers = target
	m.startTime = time.Now()
	m.lastCalcTime = time.Now()
	m.lastProcessed = 0
	m.latencies = m.latencies[:0]
	m.generated.Store(0)
	m.processed.Store(0)
	m.failed.Store(0)
	m.errors.Store(0)
}

func (m *MetricsCollector) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = "STOPPED"
	m.targetUsers = 0
	m.activeUsers = 0
	m.lowProfile = 0
	m.medProfile = 0
	m.highProfile = 0
	m.pendingQueue = 0
	m.goroutines = 0
}

func (m *MetricsCollector) SetProfiles(active, low, med, high int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeUsers = active
	m.lowProfile = low
	m.medProfile = med
	m.highProfile = high
}

func (m *MetricsCollector) SetPendingQueue(val int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingQueue = val
}

func (m *MetricsCollector) SetGoroutines(val int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goroutines = val
}

func (m *MetricsCollector) SetRuntimeStats(goroutines int, allocMB, sysMB uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goroutines = goroutines
	m.ramAllocMB = allocMB
	m.ramSysMB = sysMB
}

func (m *MetricsCollector) IncGenerated() {
	m.generated.Add(1)
}

func (m *MetricsCollector) IncProcessed() {
	m.processed.Add(1)
}

func (m *MetricsCollector) IncFailed() {
	m.failed.Add(1)
}

func (m *MetricsCollector) IncError() {
	m.errors.Add(1)
}

func (m *MetricsCollector) RecordLatency(d time.Duration) {
	ms := float64(d.Microseconds()) / 1000.0
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.latencies) < m.maxSample {
		m.latencies = append(m.latencies, ms)
	} else {
		idx := int(time.Now().UnixNano()) % m.maxSample
		m.latencies[idx] = ms
	}
}

func (m *MetricsCollector) CalculateThroughput() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(m.lastCalcTime).Seconds()
	if elapsed <= 0.2 {
		return
	}

	currProcessed := m.processed.Load()
	delta := currProcessed - m.lastProcessed
	if delta < 0 {
		delta = 0
	}

	m.throughput = float64(delta) / elapsed
	m.lastProcessed = currProcessed
	m.lastCalcTime = now
}

func (m *MetricsCollector) Snapshot() Snapshot {
	m.CalculateThroughput()

	m.mu.RLock()
	defer m.mu.RUnlock()

	var uptime int64
	if !m.startTime.IsZero() && m.status == "RUNNING" {
		uptime = int64(time.Since(m.startTime).Seconds())
	}

	p50, p95, p99 := calculatePercentiles(m.latencies)

	return Snapshot{
		Status:           m.status,
		TargetUsers:      m.targetUsers,
		ActiveUsers:      m.activeUsers,
		LowProfile:       m.lowProfile,
		MedProfile:       m.medProfile,
		HighProfile:      m.highProfile,
		GeneratedUpdates: m.generated.Load(),
		ProcessedUpdates: m.processed.Load(),
		FailedUpdates:    m.failed.Load(),
		PendingQueue:     m.pendingQueue,
		ThroughputSec:    m.throughput,
		LatencyP50Ms:     p50,
		LatencyP95Ms:     p95,
		LatencyP99Ms:     p99,
		UptimeSec:        uptime,
		Goroutines:       m.goroutines,
		RAMAllocMB:       m.ramAllocMB,
		RAMSysMB:         m.ramSysMB,
		ErrorsCount:      m.errors.Load(),
		UpdatedAt:        time.Now().Unix(),
	}
}

func calculatePercentiles(samples []float64) (p50, p95, p99 float64) {
	n := len(samples)
	if n == 0 {
		return 0, 0, 0
	}

	sorted := make([]float64, n)
	copy(sorted, samples)
	sort.Float64s(sorted)

	idx50 := int(float64(n) * 0.50)
	idx95 := int(float64(n) * 0.95)
	idx99 := int(float64(n) * 0.99)

	if idx50 >= n {
		idx50 = n - 1
	}
	if idx95 >= n {
		idx95 = n - 1
	}
	if idx99 >= n {
		idx99 = n - 1
	}

	return sorted[idx50], sorted[idx95], sorted[idx99]
}
