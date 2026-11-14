package simulation

import (
	"context"
	"testing"
	"time"
)

func TestSimulationMetrics(t *testing.T) {
	m := NewMetricsCollector()
	m.Start(100)

	for i := 0; i < 100; i++ {
		m.IncGenerated()
		m.IncProcessed()
		m.RecordLatency(time.Duration(i+1) * time.Millisecond)
	}
	m.IncFailed()
	m.IncError()

	snap := m.Snapshot()
	if snap.Status != "RUNNING" {
		t.Fatalf("expected RUNNING, got %s", snap.Status)
	}
	if snap.GeneratedUpdates != 100 {
		t.Fatalf("expected 100 generated, got %d", snap.GeneratedUpdates)
	}
	if snap.ProcessedUpdates != 100 {
		t.Fatalf("expected 100 processed, got %d", snap.ProcessedUpdates)
	}
	if snap.FailedUpdates != 1 {
		t.Fatalf("expected 1 failed, got %d", snap.FailedUpdates)
	}
	if snap.LatencyP50Ms <= 0 || snap.LatencyP95Ms <= 0 {
		t.Fatalf("invalid latency percentiles: p50=%.2f, p95=%.2f", snap.LatencyP50Ms, snap.LatencyP95Ms)
	}

	m.Stop()
	snapStopped := m.Snapshot()
	if snapStopped.Status != "STOPPED" {
		t.Fatalf("expected STOPPED, got %s", snapStopped.Status)
	}
}

func TestFakeTransport(t *testing.T) {
	ft := NewFakeTransport()
	upd := ft.GenerateUpdate(900000001, "ping")
	if upd == nil || len(upd.Updates) == 0 {
		t.Fatal("expected non-nil synthetic updates")
	}
	if len(upd.Users) == 0 {
		t.Fatal("expected synthetic user in update")
	}
}

func TestSimulationLifecycleAndScale(t *testing.T) {
	engine := NewSimulationEngine(nil)

	if err := engine.Scale(10); err != nil {
		t.Fatalf("scale 10 failed: %v", err)
	}
	if !engine.IsRunning() {
		t.Fatal("expected engine to be running")
	}

	time.Sleep(100 * time.Millisecond)

	if err := engine.Scale(50); err != nil {
		t.Fatalf("scale 50 failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := engine.Scale(20); err != nil {
		t.Fatalf("scale down 20 failed: %v", err)
	}

	engine.Stop()
	if engine.IsRunning() {
		t.Fatal("expected engine to be stopped")
	}
}

func TestGeneratorBurst(t *testing.T) {
	metrics := NewMetricsCollector()
	transport := NewFakeTransport()
	gen := NewSyntheticGenerator(transport, metrics, 100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	user := NewVirtualUser(900000001, ProfileHigh, ctx)
	users := []*VirtualUser{user}

	gen.TriggerBurst(20, users)

	if metrics.generated.Load() != 20 {
		t.Fatalf("expected 20 generated from burst, got %d", metrics.generated.Load())
	}
	if gen.QueueLen() != 20 {
		t.Fatalf("expected 20 in queue, got %d", gen.QueueLen())
	}
}

func BenchmarkFakeTransport(b *testing.B) {
	ft := NewFakeTransport()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ft.GenerateUpdate(int64(900000000+i), "ping")
	}
}

func BenchmarkMetricsRecord(b *testing.B) {
	m := NewMetricsCollector()
	m.Start(1000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.IncGenerated()
		m.IncProcessed()
		m.RecordLatency(time.Millisecond)
	}
}
