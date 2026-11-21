package controllers

import (
	"context"
	"math"
	"testing"
)

func TestComputeContainerWorkingSetMB_ExcludesAllFileCache(t *testing.T) {
	var pCgroupV2 containerStatsPayload
	pCgroupV2.MemoryStats.Usage = 60 * 1024 * 1024
	pCgroupV2.MemoryStats.Stats = map[string]uint64{
		"active_file":   12 * 1024 * 1024,
		"inactive_file": 8 * 1024 * 1024,
		"file":          20 * 1024 * 1024,
	}
	if got := computeContainerWorkingSetMB(pCgroupV2); math.Abs(got-40.0) > 0.01 {
		t.Fatalf("expected 40.0 MiB after excluding file cache, got %.2f MiB", got)
	}

	var pSplit containerStatsPayload
	pSplit.MemoryStats.Usage = 55 * 1024 * 1024
	pSplit.MemoryStats.Stats = map[string]uint64{
		"active_file":   10 * 1024 * 1024,
		"inactive_file": 5 * 1024 * 1024,
	}
	if got := computeContainerWorkingSetMB(pSplit); math.Abs(got-40.0) > 0.01 {
		t.Fatalf("expected 40.0 MiB after excluding active+inactive file cache, got %.2f MiB", got)
	}

	maybeAutoCleanStackMemory(context.Background(), nil)
}
