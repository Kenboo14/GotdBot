package simulation

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

type GeneratorJob struct {
	VirtualID int64
	Updates   *tg.Updates
	CreatedAt time.Time
	IsError   bool
}

type SyntheticGenerator struct {
	transport *FakeTransport
	metrics   *MetricsCollector
	jobQueue  chan GeneratorJob
	wg        sync.WaitGroup
	rnd       *rand.Rand
	rndMu     sync.Mutex
}

func NewSyntheticGenerator(transport *FakeTransport, metrics *MetricsCollector, queueSize int) *SyntheticGenerator {
	if queueSize <= 0 {
		queueSize = 1000
	}
	return &SyntheticGenerator{
		transport: transport,
		metrics:   metrics,
		jobQueue:  make(chan GeneratorJob, queueSize),
		rnd:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (g *SyntheticGenerator) Queue() <-chan GeneratorJob {
	return g.jobQueue
}

func (g *SyntheticGenerator) QueueLen() int {
	return len(g.jobQueue)
}

func (g *SyntheticGenerator) StartUserLoop(ctx context.Context, u *VirtualUser) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()

		g.rndMu.Lock()
		initialJitter := time.Duration(g.rnd.Intn(3000)) * time.Millisecond
		g.rndMu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-u.Ctx.Done():
			return
		case <-time.After(initialJitter):
		}

		ticker := time.NewTicker(u.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-u.Ctx.Done():
				return
			case <-ticker.C:
				if u.GetState() != StateVirtualActive {
					continue
				}
				g.dispatchSyntheticUpdate(ctx, u)
			}
		}
	}()
}

func (g *SyntheticGenerator) dispatchSyntheticUpdate(ctx context.Context, u *VirtualUser) {
	g.rndMu.Lock()
	rVal := g.rnd.Float64()
	g.rndMu.Unlock()

	var cmdType string
	isError := false

	switch {
	case rVal < 0.02:
		isError = true
		cmdType = "error"
	case rVal < 0.05:
		cmdType = "burst"
	case rVal < 0.30:
		cmdType = "ping"
	case rVal < 0.50:
		cmdType = "help"
	default:
		cmdType = "status"
	}

	upd := g.transport.GenerateUpdate(u.ID, cmdType)
	job := GeneratorJob{
		VirtualID: u.ID,
		Updates:   upd,
		CreatedAt: time.Now(),
		IsError:   isError,
	}

	u.IncGenerated()
	g.metrics.IncGenerated()

	select {
	case g.jobQueue <- job:
		g.metrics.SetPendingQueue(len(g.jobQueue))
	default:
		u.IncFailed()
		g.metrics.IncFailed()
		g.metrics.IncError()
	}
}

func (g *SyntheticGenerator) TriggerBurst(count int, users []*VirtualUser) {
	if len(users) == 0 {
		return
	}
	for i := 0; i < count; i++ {
		g.rndMu.Lock()
		targetUser := users[g.rnd.Intn(len(users))]
		g.rndMu.Unlock()

		upd := g.transport.GenerateUpdate(targetUser.ID, "burst")
		job := GeneratorJob{
			VirtualID: targetUser.ID,
			Updates:   upd,
			CreatedAt: time.Now(),
			IsError:   false,
		}

		targetUser.IncGenerated()
		g.metrics.IncGenerated()

		select {
		case g.jobQueue <- job:
			g.metrics.SetPendingQueue(len(g.jobQueue))
		default:
			targetUser.IncFailed()
			g.metrics.IncFailed()
			g.metrics.IncError()
		}
	}
}

func (g *SyntheticGenerator) Wait() {
	g.wg.Wait()
}
