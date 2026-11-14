package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type ControlMessage struct {
	Action string `json:"action"`
	Target int    `json:"target"`
}

type SimulationEngine struct {
	mu         sync.Mutex
	rdb        *redis.Client
	metrics    *MetricsCollector
	generator  *SyntheticGenerator
	users      []*VirtualUser
	ctx        context.Context
	cancel     context.CancelFunc
	workerWg   sync.WaitGroup
	syncCancel context.CancelFunc
	running    bool
	workerNum  int
}

func NewSimulationEngine(rdb *redis.Client) *SimulationEngine {
	metrics := NewMetricsCollector()
	transport := NewFakeTransport()
	generator := NewSyntheticGenerator(transport, metrics, 2000)

	return &SimulationEngine{
		rdb:       rdb,
		metrics:   metrics,
		generator: generator,
		workerNum: 32,
	}
}

func (e *SimulationEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *SimulationEngine) Scale(target int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if target <= 0 {
		return fmt.Errorf("target must be greater than zero")
	}
	if target > 2000 {
		return fmt.Errorf("target exceeds maximum limit of 2000")
	}

	if !e.running {
		e.ctx, e.cancel = context.WithCancel(context.Background())
		e.running = true
		e.metrics.Start(target)
		e.startWorkers()
		e.startPeriodicSync()
	}

	current := len(e.users)
	if target > current {
		for i := current; i < target; i++ {
			id := int64(900000000 + i + 1)
			var prof UserProfile
			switch {
			case i < int(float64(target)*0.40):
				prof = ProfileLow
			case i < int(float64(target)*0.80):
				prof = ProfileMed
			default:
				prof = ProfileHigh
			}

			u := NewVirtualUser(id, prof, e.ctx)
			e.users = append(e.users, u)
			e.generator.StartUserLoop(e.ctx, u)
		}
	} else if target < current {
		for i := target; i < current; i++ {
			e.users[i].Stop()
		}
		e.users = e.users[:target]
	}

	low, med, high := 0, 0, 0
	for _, u := range e.users {
		switch u.Profile {
		case ProfileLow:
			low++
		case ProfileMed:
			med++
		case ProfileHigh:
			high++
		}
	}
	e.metrics.SetProfiles(len(e.users), low, med, high)
	e.metrics.mu.Lock()
	e.metrics.targetUsers = target
	e.metrics.mu.Unlock()

	return nil
}

func (e *SimulationEngine) startWorkers() {
	for i := 0; i < e.workerNum; i++ {
		e.workerWg.Add(1)
		go func() {
			defer e.workerWg.Done()
			rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

			for {
				select {
				case <-e.ctx.Done():
					return
				case job, ok := <-e.generator.Queue():
					if !ok {
						return
					}
					e.processJob(job, rnd)
				}
			}
		}()
	}
}

func (e *SimulationEngine) processJob(job GeneratorJob, rnd *rand.Rand) {
	processDelay := time.Duration(rnd.Intn(10)+2) * time.Millisecond
	time.Sleep(processDelay)

	elapsed := time.Since(job.CreatedAt)
	e.metrics.RecordLatency(elapsed)

	if job.IsError {
		e.metrics.IncFailed()
		e.metrics.IncError()
	} else {
		e.metrics.IncProcessed()
	}
	e.metrics.SetPendingQueue(e.generator.QueueLen())
}

func (e *SimulationEngine) startPeriodicSync() {
	syncCtx, cancel := context.WithCancel(context.Background())
	e.syncCancel = cancel

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-syncCtx.Done():
				return
			case <-ticker.C:
				e.metrics.SetGoroutines(runtime.NumGoroutine())
				e.exportMetrics()
			}
		}
	}()
}

func (e *SimulationEngine) exportMetrics() {
	if e.rdb == nil {
		return
	}
	snap := e.metrics.Snapshot()
	bytes, err := json.Marshal(snap)
	if err == nil {
		_ = e.rdb.Set(context.Background(), "simulasi:metrics", bytes, 1*time.Hour).Err()
	}
}

func (e *SimulationEngine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	if e.cancel != nil {
		e.cancel()
	}
	if e.syncCancel != nil {
		e.syncCancel()
	}

	for _, u := range e.users {
		u.Stop()
	}
	e.users = nil
	e.mu.Unlock()

	e.workerWg.Wait()
	e.generator.Wait()

	e.metrics.Stop()
	e.exportMetrics()

	slog.Info("virtual userbot simulation stopped successfully")
}

func (e *SimulationEngine) ListenControl(ctx context.Context) {
	if e.rdb == nil {
		return
	}
	pubsub := e.rdb.Subscribe(ctx, "simulasi:control")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			e.Stop()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var cmd ControlMessage
			if err := json.Unmarshal([]byte(msg.Payload), &cmd); err != nil {
				continue
			}

			switch cmd.Action {
			case "start", "scale":
				if err := e.Scale(cmd.Target); err != nil {
					slog.Error("failed to scale simulation", slog.String("error", err.Error()))
				}
			case "stop":
				e.Stop()
			}
		}
	}
}
