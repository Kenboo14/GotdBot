package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tgerr"
)

func TestMonitorDispatcherSemaphoreSerialization(t *testing.T) {
	topics := map[string]int{
		"MONITOR_TOPIC":             18514,
		"MONITOR_USERBOT_CONNECTED": 18509,
		"MONITOR_TOKEN_COLLECTED":   22444,
		"MONITOR_USER_START":        18519,
		"MONITOR_TOKEN_EXPIRED":     19501,
		"MONITOR_BACKUP_TOKEN":      21875,
		"MONITOR_BOT_LOADER":        18508,
		"MONITOR_TOKEN_CREATED":     18492,
		"MONITOR_SESSION_TRACK":     19207,
		"MONITOR_TRACEBACK":         16857,
		"MONITOR_INVOICE":           25351,
		"MONITOR_DATABASE":          18834,
	}

	cfg := monitorDispatcherConfig{
		burst:         100,
		refillEvery:   0,
		minGap:        0,
		flushInterval: 25 * time.Millisecond,
		sendTimeout:   2 * time.Second,
		highQueueCap:  64,
		normQueueCap:  64,
		maxBatchItems: 15,
		maxMsgBytes:   3800,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123456), 18514)

	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	recordMax := func() {
		cur := inFlight.Add(1)
		for {
			prev := maxInFlight.Load()
			if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
	}

	var recMu sync.Mutex
	sentTopics := make(map[int]int)
	totalOps := atomic.Int64{}

	d.mu.Lock()
	d.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		recordMax()
		time.Sleep(1 * time.Millisecond)
		recMu.Lock()
		sentTopics[p.MessageThreadID]++
		recMu.Unlock()
		totalOps.Add(1)
		inFlight.Add(-1)
		return &botgotd.Message{ID: 10}, nil
	}
	d.editFn = func(_ context.Context, p *botgotd.EditMessageTextParams) (*botgotd.Message, error) {
		recordMax()
		time.Sleep(1 * time.Millisecond)
		totalOps.Add(1)
		inFlight.Add(-1)
		return &botgotd.Message{ID: p.MessageID}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx, func(k string) int { return topics[k] })

	var keys []string
	for k := range topics {
		keys = append(keys, k)
	}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			k := keys[idx%len(keys)]
			d.Enqueue(monitorJob{
				topicKey: k,
				topicID:  topics[k],
				text:     fmt.Sprintf("msg-%d", idx),
				priority: monitorPriority(idx % 2),
			})
			if idx%5 == 0 {
				d.EnqueueDashboard(dashboardEditJob{
					topicID: 18514,
					msgID:   10,
					text:    fmt.Sprintf("dash-%d", idx),
				})
			}
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recMu.Lock()
		nTopics := len(sentTopics)
		recMu.Unlock()
		if totalOps.Load() >= 41 && nTopics == len(topics) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("expected strict semaphore maxInFlight=1, got %d", got)
	}
	recMu.Lock()
	defer recMu.Unlock()
	for k, id := range topics {
		if sentTopics[id] == 0 {
			t.Fatalf("expected topic %s (%d) to receive at least 1 message", k, id)
		}
	}
}

func TestMonitorDispatcherPacingRateLimit(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:         2,
		refillEvery:   50 * time.Millisecond,
		minGap:        20 * time.Millisecond,
		flushInterval: time.Second,
		sendTimeout:   2 * time.Second,
		highQueueCap:  16,
		normQueueCap:  16,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	var times []time.Time
	var mu sync.Mutex
	d.mu.Lock()
	d.sendFn = func(_ context.Context, _ *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		return &botgotd.Message{ID: 1}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx, func(_ string) int { return 18514 })

	for i := 0; i < 5; i++ {
		d.Enqueue(monitorJob{topicKey: "MONITOR_TOKEN_CREATED", topicID: 18492, text: fmt.Sprintf("t%d", i), priority: monitorPriorityHigh})
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(times)
		mu.Unlock()
		if n == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(times) != 5 {
		t.Fatalf("expected 5 paced deliveries, got %d", len(times))
	}
	for i := 1; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		if gap < 16*time.Millisecond {
			t.Fatalf("expected minGap >= 20ms (with jitter tolerance), got %v at index %d", gap, i)
		}
	}
	total := times[4].Sub(times[0])
	if total < 120*time.Millisecond {
		t.Fatalf("expected token bucket refill pacing for 5 msgs (burst 2, refill 50ms) >= 120ms, got %v", total)
	}
}

func TestMonitorDispatcherFloodWaitNoFallbackAndRetry(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:          5,
		refillEvery:    0,
		minGap:         0,
		flushInterval:  time.Second,
		sendTimeout:    2 * time.Second,
		highQueueCap:   16,
		normQueueCap:   16,
		maxRetries:     2,
		maxWaitCap:     10 * time.Second,
		floodExtraWait: 0,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	var mu sync.Mutex
	var attempts []int
	var attemptTimes []time.Time
	d.mu.Lock()
	d.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		attempts = append(attempts, p.MessageThreadID)
		attemptTimes = append(attemptTimes, time.Now())
		idx := len(attempts)
		mu.Unlock()
		if idx == 1 {
			return nil, errors.New(`telegram API status 429: {"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`)
		}
		if idx == 2 {
			return nil, tgerr.New(420, "FLOOD_WAIT_1")
		}
		return &botgotd.Message{ID: 77}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx, func(_ string) int { return 18509 })

	d.Enqueue(monitorJob{
		topicKey:   "MONITOR_USERBOT_CONNECTED",
		topicID:    18509,
		text:       "critical event",
		priority:   monitorPriorityHigh,
		allowRetry: true,
	})

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(attempts)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 3 {
		t.Fatalf("expected 3 attempts (429 + FLOOD_WAIT + success), got %d (%v)", len(attempts), attempts)
	}
	for i, tid := range attempts {
		if tid != 18509 {
			t.Fatalf("attempt %d fell back to thread %d on FloodWait; expected no fallback (18509)", i, tid)
		}
	}
	if gap := attemptTimes[1].Sub(attemptTimes[0]); gap < 900*time.Millisecond {
		t.Fatalf("expected >= 900ms cooldown after 429 retry_after=1, got %v", gap)
	}
	if gap := attemptTimes[2].Sub(attemptTimes[1]); gap < 900*time.Millisecond {
		t.Fatalf("expected >= 900ms cooldown after FLOOD_WAIT_1, got %v", gap)
	}
}

func TestMonitorDispatcherPermanentTopicFallbackAndCache(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:         10,
		refillEvery:   0,
		minGap:        0,
		flushInterval: time.Second,
		sendTimeout:   2 * time.Second,
		badTopicTTL:   time.Minute,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	var mu sync.Mutex
	var attempts []int
	d.mu.Lock()
	d.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		attempts = append(attempts, p.MessageThreadID)
		mu.Unlock()
		if p.MessageThreadID == 18509 {
			return nil, errors.New("telegram API status 400: Bad Request: message thread not found")
		}
		return &botgotd.Message{ID: 55}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.deliverJob(ctx, monitorJob{topicKey: "MONITOR_USERBOT_CONNECTED", topicID: 18509, text: "first", priority: monitorPriorityHigh})
	d.deliverJob(ctx, monitorJob{topicKey: "MONITOR_USERBOT_CONNECTED", topicID: 18509, text: "second", priority: monitorPriorityHigh})

	mu.Lock()
	defer mu.Unlock()
	expected := []int{18509, 18514, 18514}
	if len(attempts) != len(expected) {
		t.Fatalf("expected attempts %v, got %v", expected, attempts)
	}
	for i := range expected {
		if attempts[i] != expected[i] {
			t.Fatalf("expected attempts %v, got %v", expected, attempts)
		}
	}
}

func TestMonitorDispatcherBatchCoalescing(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:         20,
		refillEvery:   0,
		minGap:        1 * time.Millisecond,
		flushInterval: 35 * time.Millisecond,
		sendTimeout:   2 * time.Second,
		highQueueCap:  32,
		normQueueCap:  32,
		maxBatchItems: 15,
		maxMsgBytes:   3800,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	topicMap := map[string]int{
		"MONITOR_SESSION_TRACK": 19207,
		"MONITOR_TRACEBACK":     16857,
		"MONITOR_USER_START":    18519,
		"MONITOR_TOKEN_EXPIRED": 19501,
	}

	var mu sync.Mutex
	delivered := make(map[int][]string)
	d.mu.Lock()
	d.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		delivered[p.MessageThreadID] = append(delivered[p.MessageThreadID], p.Text)
		mu.Unlock()
		return &botgotd.Message{ID: 1}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx, func(k string) int { return topicMap[k] })

	for i := 0; i < 50; i++ {
		d.EnqueueBatchEntry("MONITOR_SESSION_TRACK", "🔄 <b>[SESSION TRACK DIGEST]</b>", monitorPriorityNormal, fmt.Sprintf("• session %d", i), "single")
		d.EnqueueBatchEntry("MONITOR_TRACEBACK", "🐞 <b>[TRACEBACK DIGEST]</b>", monitorPriorityNormal, fmt.Sprintf("• err %d", i), "single")
		d.EnqueueBatchEntry("MONITOR_USER_START", "👤 <b>[USER START DIGEST]</b>", monitorPriorityNormal, fmt.Sprintf("• user %d", i), "single")
		d.EnqueueBatchEntry("MONITOR_TOKEN_EXPIRED", "⏳ <b>[TOKEN EXPIRED DIGEST]</b>", monitorPriorityHigh, fmt.Sprintf("• tok %d", i), "single")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(delivered)
		mu.Unlock()
		if count == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 4 {
		t.Fatalf("expected 4 batched topic deliveries, got %d", len(delivered))
	}
	for name, tid := range topicMap {
		msgs := delivered[tid]
		if len(msgs) != 1 {
			t.Fatalf("expected 1 coalesced digest for %s (%d), got %d", name, tid, len(msgs))
		}
		if len(msgs[0]) > 4096 {
			t.Fatalf("digest for %s exceeds 4096 bytes: %d", name, len(msgs[0]))
		}
		if !strings.Contains(msgs[0], "+35 event lainnya") {
			t.Fatalf("expected +35 summary in digest for %s, got: %s", name, msgs[0])
		}
	}
}

func TestMonitorDispatcherPriorityProtection(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:         20,
		refillEvery:   0,
		minGap:        0,
		flushInterval: time.Second,
		sendTimeout:   2 * time.Second,
		highQueueCap:  16,
		normQueueCap:  4,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	for i := 0; i < 6; i++ {
		d.Enqueue(monitorJob{topicKey: "MONITOR_USER_START", topicID: 18519, text: fmt.Sprintf("normal-%d", i), priority: monitorPriorityNormal})
	}
	highItems := []struct {
		key string
		id  int
		txt string
	}{
		{"MONITOR_BACKUP_TOKEN", 21875, "high-backup"},
		{"MONITOR_TOKEN_CREATED", 18492, "high-created"},
		{"MONITOR_INVOICE", 25351, "high-invoice"},
	}
	for _, hi := range highItems {
		if ok := d.Enqueue(monitorJob{topicKey: hi.key, topicID: hi.id, text: hi.txt, priority: monitorPriorityHigh}); !ok {
			t.Fatalf("expected high priority enqueue %s to succeed when normal queue is full", hi.key)
		}
	}

	var mu sync.Mutex
	var order []string
	d.mu.Lock()
	d.sendFn = func(_ context.Context, p *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		order = append(order, p.Text)
		mu.Unlock()
		return &botgotd.Message{ID: 1}, nil
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx, func(_ string) int { return 18514 })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n == 7 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 7 {
		t.Fatalf("expected 3 high + 4 normal deliveries, got %d (%v)", len(order), order)
	}
	for i := 0; i < 3; i++ {
		if !strings.HasPrefix(order[i], "high-") {
			t.Fatalf("expected high priority item at position %d, got %q (order=%v)", i, order[i], order)
		}
	}
}

func TestMonitorDispatcherDashboardCoalescingAndNoSpamOnError(t *testing.T) {
	cfg := monitorDispatcherConfig{
		burst:         20,
		refillEvery:   0,
		minGap:        0,
		flushInterval: time.Second,
		sendTimeout:   2 * time.Second,
	}
	d := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)

	var mu sync.Mutex
	var editedTexts []string
	var sentCount int
	var editErr error

	d.mu.Lock()
	d.editFn = func(_ context.Context, p *botgotd.EditMessageTextParams) (*botgotd.Message, error) {
		mu.Lock()
		editedTexts = append(editedTexts, p.Text)
		err := editErr
		mu.Unlock()
		if err != nil {
			return nil, err
		}
		return &botgotd.Message{ID: p.MessageID}, nil
	}
	d.sendFn = func(_ context.Context, _ *botgotd.SendMessageParams) (*botgotd.Message, error) {
		mu.Lock()
		sentCount++
		mu.Unlock()
		return &botgotd.Message{ID: 999}, nil
	}
	d.mu.Unlock()

	for i := 1; i <= 10; i++ {
		d.EnqueueDashboard(dashboardEditJob{topicID: 18514, msgID: 100, text: fmt.Sprintf("v%d", i)})
	}
	if dash := d.popDashboard(); dash == nil || dash.text != "v10" {
		t.Fatalf("expected coalesced dashboard v10, got %+v", dash)
	} else {
		d.deliverDashboard(context.Background(), dash)
	}

	mu.Lock()
	if len(editedTexts) != 1 || editedTexts[0] != "v10" || sentCount != 0 {
		mu.Unlock()
		t.Fatalf("expected 1 edit of v10 and 0 sends, got edits=%v sends=%d", editedTexts, sentCount)
	}
	editErr = errors.New("context deadline exceeded")
	mu.Unlock()

	d.deliverDashboard(context.Background(), &dashboardEditJob{topicID: 18514, msgID: 100, text: "v11"})

	mu.Lock()
	if sentCount != 0 {
		mu.Unlock()
		t.Fatalf("transient edit error must not trigger SendMessage, got sentCount=%d", sentCount)
	}
	editErr = errors.New("telegram API status 429: retry after 1")
	mu.Unlock()

	d.deliverDashboard(context.Background(), &dashboardEditJob{topicID: 18514, msgID: 100, text: "v12"})

	mu.Lock()
	if sentCount != 0 {
		mu.Unlock()
		t.Fatalf("429 edit error must not trigger SendMessage, got sentCount=%d", sentCount)
	}
	mu.Unlock()

	d2 := newMonitorDispatcher(cfg, nil, int64(-100123), 18514)
	var gotNewID int
	var d2Sent int
	d2.mu.Lock()
	d2.editFn = func(_ context.Context, _ *botgotd.EditMessageTextParams) (*botgotd.Message, error) {
		return nil, errors.New("Bad Request: message to edit not found")
	}
	d2.sendFn = func(_ context.Context, _ *botgotd.SendMessageParams) (*botgotd.Message, error) {
		d2Sent++
		return &botgotd.Message{ID: 999}, nil
	}
	d2.mu.Unlock()

	d2.deliverDashboard(context.Background(), &dashboardEditJob{
		topicID: 18514,
		msgID:   100,
		text:    "recreate",
		onMsgID: func(id int) { gotNewID = id },
	})
	if d2Sent != 1 || gotNewID != 999 {
		t.Fatalf("expected message-not-found to recreate once with new ID 999, got sent=%d newID=%d", d2Sent, gotNewID)
	}
}

func TestDevMonitorBoundedDedupCap(t *testing.T) {
	m := &devMonitor{dedup: make(map[string]time.Time)}
	for i := 0; i < 1000; i++ {
		k := fmt.Sprintf("unique-key-%d", i)
		if !m.shouldSendDedup(k, 5*time.Minute) {
			t.Fatalf("expected first call for %s to succeed", k)
		}
	}
	if m.shouldSendDedup("unique-key-999", 5*time.Minute) {
		t.Fatalf("expected recent key to be deduplicated within cooldown")
	}
	m.mu.Lock()
	n := len(m.dedup)
	m.mu.Unlock()
	if n > maxDedupEntries {
		t.Fatalf("expected dedup map capped <= %d, got %d", maxDedupEntries, n)
	}
}

func TestTelemetryPeriodicSyncAndOutOfOrderNoAlert(t *testing.T) {
	testDisp := newMonitorDispatcher(monitorDispatcherConfig{burst: 10, highQueueCap: 16, normQueueCap: 16}, nil, int64(-100123), 18514)
	globalDevMonitor.mu.Lock()
	prevTarget, prevDisp := globalDevMonitor.chatTarget, globalDevMonitor.dispatcher
	globalDevMonitor.chatTarget = int64(-100123)
	globalDevMonitor.dispatcher = testDisp
	globalDevMonitor.mu.Unlock()
	defer func() {
		globalDevMonitor.mu.Lock()
		globalDevMonitor.chatTarget = prevTarget
		globalDevMonitor.dispatcher = prevDisp
		globalDevMonitor.mu.Unlock()
	}()

	tr := &telemetryTracker{
		sessionStates: make(map[int64]string),
		lastEventIDs:  make(map[int64]string),
		alerts:        make(chan alertEventItem, 10),
	}
	nowMs := time.Now().UnixMilli()
	idNew := fmt.Sprintf("%d-0", nowMs)
	idOld := fmt.Sprintf("%d-0", nowMs-5000)

	tr.recordSessionEvent(models.PFromService{ChatID: 101, State: "CONNECTED", Text: "ok"}, idNew)
	tr.recordSessionEvent(models.PFromService{ChatID: 101, State: "SESSION_INVALID", Text: "AUTH_KEY_UNREGISTERED"}, idOld)
	if got := tr.sessionStates[101]; got != "CONNECTED" {
		t.Fatalf("expected out-of-order older event to be ignored, got %q", got)
	}
	if len(tr.alerts) != 0 {
		t.Fatalf("expected 0 alerts after out-of-order event, got %d", len(tr.alerts))
	}

	tr.recordSessionEvent(models.PFromService{ChatID: 102, State: "SESSION_INVALID", Text: "periodic_sync"}, idNew)
	if got := tr.sessionStates[102]; got != "SESSION_INVALID" {
		t.Fatalf("expected periodic_sync to update dashboard state, got %q", got)
	}
	if len(tr.alerts) != 0 {
		t.Fatalf("expected periodic_sync not to enqueue alerts, got %d", len(tr.alerts))
	}
	testDisp.batchMu.Lock()
	batchCount := len(testDisp.batches)
	testDisp.batchMu.Unlock()
	if batchCount != 0 {
		t.Fatalf("expected periodic_sync and out-of-order events not to enqueue MONITOR_SESSION_TRACK, got %d batches", batchCount)
	}
}
