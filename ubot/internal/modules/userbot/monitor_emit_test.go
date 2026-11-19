package userbot

import (
	"fmt"
	"testing"
	"time"
)

func TestServiceShouldEmitMonitorEventDedupAndCap(t *testing.T) {
	s := &Service{
		monitorSem:   make(chan struct{}, 4),
		monitorDedup: make(map[string]time.Time),
	}

	if !s.shouldEmitMonitorEvent("tb:1001", 30*time.Second) {
		t.Fatalf("expected initial traceback key to pass dedup")
	}
	if s.shouldEmitMonitorEvent("tb:1001", 30*time.Second) {
		t.Fatalf("expected duplicate traceback key within cooldown to be suppressed")
	}
	if !s.shouldEmitMonitorEvent("tb:1002", 30*time.Second) {
		t.Fatalf("expected distinct traceback key to pass dedup")
	}

	for i := 0; i < 1000; i++ {
		if !s.shouldEmitMonitorEvent(fmt.Sprintf("k:%d", i), 5*time.Minute) {
			t.Fatalf("expected unique key k:%d to pass", i)
		}
	}

	s.monitorDedupMu.Lock()
	size := len(s.monitorDedup)
	s.monitorDedupMu.Unlock()
	if size > 512 {
		t.Fatalf("expected monitorDedup capped <= 512, got %d", size)
	}

	s.emitMonitorEvent("traceback", "err", 1001, 1001)
}
