package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type monitorPriority int

const (
	monitorPriorityNormal monitorPriority = iota
	monitorPriorityHigh
)

type monitorJob struct {
	topicKey   string
	topicID    int
	text       string
	priority   monitorPriority
	allowRetry bool
	retries    int
}

type dashboardEditJob struct {
	topicID int
	msgID   int
	text    string
	onMsgID func(int)
}

type topicBatch struct {
	topicKey   string
	header     string
	priority   monitorPriority
	entries    []string
	singleText string
	dropped    int
}

type monitorDispatcherConfig struct {
	burst          int
	refillEvery    time.Duration
	minGap         time.Duration
	flushInterval  time.Duration
	sendTimeout    time.Duration
	highQueueCap   int
	normQueueCap   int
	maxBatchItems  int
	maxMsgBytes    int
	badTopicTTL    time.Duration
	maxRetries     int
	maxWaitCap     time.Duration
	floodExtraWait time.Duration
}

func defaultMonitorDispatcherConfig() monitorDispatcherConfig {
	return monitorDispatcherConfig{
		burst:          2,
		refillEvery:    3500 * time.Millisecond,
		minGap:         1200 * time.Millisecond,
		flushInterval:  8 * time.Second,
		sendTimeout:    8 * time.Second,
		highQueueCap:   64,
		normQueueCap:   64,
		maxBatchItems:  15,
		maxMsgBytes:    3800,
		badTopicTTL:    10 * time.Minute,
		maxRetries:     2,
		maxWaitCap:     5 * time.Minute,
		floodExtraWait: time.Second,
	}
}

type monitorDispatcher struct {
	cfg              monitorDispatcherConfig
	mu               sync.Mutex
	chatTarget       any
	fallbackTop      int
	b                *botgotd.Bot
	sendFn           func(context.Context, *botgotd.SendMessageParams) (*botgotd.Message, error)
	editFn           func(context.Context, *botgotd.EditMessageTextParams) (*botgotd.Message, error)
	tokens           int
	lastRefill       time.Time
	lastSend         time.Time
	cooldownUntil    time.Time
	badTopics        map[int]time.Time
	sendSem          chan struct{}
	highQueue        chan monitorJob
	normalQueue      chan monitorJob
	dashboardMu      sync.Mutex
	pendingDashboard *dashboardEditJob
	dashboardNotify  chan struct{}
	batchMu          sync.Mutex
	batches          map[string]*topicBatch
	batchOrder       []string
	wakeCh           chan struct{}
}

func newMonitorDispatcher(cfg monitorDispatcherConfig, b *botgotd.Bot, target any, fallbackTop int) *monitorDispatcher {
	if cfg.burst < 1 {
		cfg.burst = 1
	}
	if cfg.highQueueCap < 1 {
		cfg.highQueueCap = 64
	}
	if cfg.normQueueCap < 1 {
		cfg.normQueueCap = 64
	}
	if cfg.maxBatchItems < 1 {
		cfg.maxBatchItems = 15
	}
	if cfg.maxMsgBytes < 512 {
		cfg.maxMsgBytes = 3800
	}
	if cfg.sendTimeout <= 0 {
		cfg.sendTimeout = 8 * time.Second
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = 8 * time.Second
	}
	if cfg.maxWaitCap <= 0 {
		cfg.maxWaitCap = 5 * time.Minute
	}
	if cfg.badTopicTTL <= 0 {
		cfg.badTopicTTL = 10 * time.Minute
	}

	return &monitorDispatcher{
		cfg:             cfg,
		chatTarget:      target,
		fallbackTop:     fallbackTop,
		b:               b,
		tokens:          cfg.burst,
		lastRefill:      time.Now(),
		badTopics:       make(map[int]time.Time),
		sendSem:         make(chan struct{}, 1),
		highQueue:       make(chan monitorJob, cfg.highQueueCap),
		normalQueue:     make(chan monitorJob, cfg.normQueueCap),
		dashboardNotify: make(chan struct{}, 1),
		batches:         make(map[string]*topicBatch),
		wakeCh:          make(chan struct{}, 1),
	}
}

func (d *monitorDispatcher) updateTarget(b *botgotd.Bot, target any, fallbackTop int) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.b = b
	d.chatTarget = target
	d.fallbackTop = fallbackTop
	d.mu.Unlock()
}

func (d *monitorDispatcher) signalWake() {
	select {
	case d.wakeCh <- struct{}{}:
	default:
	}
}

func (d *monitorDispatcher) hasTarget() bool {
	d.mu.Lock()
	target := d.chatTarget
	d.mu.Unlock()
	return target != nil
}

func (d *monitorDispatcher) Enqueue(job monitorJob) bool {
	if d == nil || strings.TrimSpace(job.text) == "" || !d.hasTarget() {
		return false
	}
	q := d.normalQueue
	if job.priority == monitorPriorityHigh {
		q = d.highQueue
	}
	select {
	case q <- job:
		d.signalWake()
		return true
	default:
		log.Printf("⚠️ [DevMonitor] queue full (priority=%d), dropping event for %s", job.priority, job.topicKey)
		return false
	}
}

func (d *monitorDispatcher) EnqueueBatchEntry(topicKey, header string, priority monitorPriority, entry, singleText string) {
	if d == nil || strings.TrimSpace(entry) == "" || !d.hasTarget() {
		return
	}
	d.batchMu.Lock()
	b, ok := d.batches[topicKey]
	if !ok {
		b = &topicBatch{
			topicKey:   topicKey,
			header:     header,
			priority:   priority,
			singleText: singleText,
		}
		d.batches[topicKey] = b
		d.batchOrder = append(d.batchOrder, topicKey)
	} else if priority > b.priority {
		b.priority = priority
	}
	if len(b.entries) < d.cfg.maxBatchItems {
		b.entries = append(b.entries, entry)
	} else {
		b.dropped++
	}
	d.batchMu.Unlock()
}

func (d *monitorDispatcher) EnqueueDashboard(job dashboardEditJob) {
	if d == nil || strings.TrimSpace(job.text) == "" || !d.hasTarget() {
		return
	}
	d.dashboardMu.Lock()
	copyJob := job
	d.pendingDashboard = &copyJob
	d.dashboardMu.Unlock()

	select {
	case d.dashboardNotify <- struct{}{}:
	default:
	}
	d.signalWake()
}

func (d *monitorDispatcher) popDashboard() *dashboardEditJob {
	d.dashboardMu.Lock()
	job := d.pendingDashboard
	d.pendingDashboard = nil
	d.dashboardMu.Unlock()
	return job
}

func (d *monitorDispatcher) flushBatches(resolveTopic func(string) int) {
	d.batchMu.Lock()
	if len(d.batchOrder) == 0 {
		d.batchMu.Unlock()
		return
	}
	batches := d.batches
	order := d.batchOrder
	d.batches = make(map[string]*topicBatch)
	d.batchOrder = nil
	d.batchMu.Unlock()

	for _, key := range order {
		b := batches[key]
		if b == nil || len(b.entries) == 0 {
			continue
		}
		topicID := 0
		if resolveTopic != nil {
			topicID = resolveTopic(b.topicKey)
		}
		if len(b.entries) == 1 && b.dropped == 0 && b.singleText != "" {
			d.Enqueue(monitorJob{
				topicKey:   b.topicKey,
				topicID:    topicID,
				text:       b.singleText,
				priority:   b.priority,
				allowRetry: b.priority == monitorPriorityHigh,
			})
			continue
		}

		var sb strings.Builder
		sb.WriteString(b.header)
		sb.WriteString("\n────────────────────────\n")
		dropped := b.dropped
		for _, entry := range b.entries {
			if sb.Len()+len(entry)+96 > d.cfg.maxMsgBytes {
				dropped++
				continue
			}
			sb.WriteString(entry)
			if !strings.HasSuffix(entry, "\n") {
				sb.WriteByte('\n')
			}
		}
		if dropped > 0 {
			sb.WriteString(fmt.Sprintf("<i>… dan +%d event lainnya diringkas</i>\n", dropped))
		}
		sb.WriteString(fmt.Sprintf("• 🕒 %s WIB", time.Now().Format("15:04:05")))

		d.Enqueue(monitorJob{
			topicKey:   b.topicKey,
			topicID:    topicID,
			text:       sb.String(),
			priority:   b.priority,
			allowRetry: b.priority == monitorPriorityHigh,
		})
	}
}

func (d *monitorDispatcher) run(ctx context.Context, resolveTopic func(string) int) {
	flushTicker := time.NewTicker(d.cfg.flushInterval)
	defer flushTicker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-flushTicker.C:
			d.flushBatches(resolveTopic)
		default:
		}
		select {
		case job := <-d.highQueue:
			d.deliverJob(ctx, job)
			continue
		default:
		}
		select {
		case job := <-d.normalQueue:
			d.deliverJob(ctx, job)
			continue
		default:
		}
		if dash := d.popDashboard(); dash != nil {
			d.deliverDashboard(ctx, dash)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			d.flushBatches(resolveTopic)
		case <-d.wakeCh:
		case <-d.dashboardNotify:
		}
	}
}
