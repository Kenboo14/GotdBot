package databases

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedCacheAndInvalidation(t *testing.T) {
	c := newBoundedCache[int64, string](2, 50*time.Millisecond)
	c.Set(1, "one")
	c.Set(2, "two")
	c.Set(3, "three")

	if _, ok := c.Get(1); ok {
		t.Fatalf("expected oldest key 1 to be evicted at capacity 2")
	}
	if v, ok := c.Get(2); !ok || v != "two" {
		t.Fatalf("expected key 2 = two, got (%q, %v)", v, ok)
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.Get(2); ok {
		t.Fatalf("expected expired key 2 to miss after TTL")
	}

	dm, err := NewDataManager(context.Background(), context.Background(), "", "", filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer dm.Close()

	ctx := context.Background()
	cfg := dm.GetUserConfig(ctx, 5001)
	cfg.Delay = 45
	cfg.Lists = []int{10, 20}
	if err := dm.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	got1 := dm.GetUserConfig(ctx, 5001)
	got1.Delay = 999
	got1.Lists[0] = 777
	got2 := dm.GetUserConfig(ctx, 5001)
	if got2.Delay != 45 || len(got2.Lists) != 2 || got2.Lists[0] != 10 {
		t.Fatalf("expected deep-copied config from cache, got delay=%d lists=%v", got2.Delay, got2.Lists)
	}

	dm.handleCacheInvalidation(cacheInvalidationEvent{OwnerID: 5001, Kind: "cfg"})
	if _, ok := dm.cfgCache.Get(5001); ok {
		t.Fatalf("expected cache invalidation to clear owner 5001")
	}

	ulActive := &UserLicense{
		TokenSHA:    "GOTD_SHA:active1",
		MasterToken: "GOTD:MASTER1",
		OwnerChatID: 5001,
		UserbotID:   5001,
		Tier:        "Pro",
		ExpiresAt:   time.Now().Add(time.Hour),
		Status:      "active",
	}
	if err := dm.SaveUserLicense(ctx, ulActive); err != nil {
		t.Fatalf("SaveUserLicense: %v", err)
	}
	gotLic, err := dm.GetActiveUserLicense(ctx, 5001)
	if err != nil || gotLic == nil || gotLic.Tier != "Pro" {
		t.Fatalf("expected active Pro license, got %+v err=%v", gotLic, err)
	}
	if cached, ok := dm.licCache.Get(5001); !ok || cached == nil {
		t.Fatalf("expected license cache populated on active fetch")
	}
	if err := dm.MarkUserLicenseExpired(ctx, "GOTD_SHA:active1"); err != nil {
		t.Fatalf("MarkUserLicenseExpired: %v", err)
	}
	if _, ok := dm.licCache.Get(5001); ok {
		t.Fatalf("expected license cache invalidated on expiry")
	}
	if isSQLiteBusyError(context.Canceled) {
		t.Fatalf("expected context.Canceled not to be classified as SQLite busy")
	}
	if !isSQLiteBusyError(errors.New("database is locked")) {
		t.Fatalf("expected database is locked to be classified as SQLite busy")
	}
}

func TestWorkerAssignmentsPersistence(t *testing.T) {
	dm, err := NewDataManager(context.Background(), context.Background(), "", "", filepath.Join(t.TempDir(), "workers.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer dm.Close()
	ctx := context.Background()

	if err := dm.SaveWorkerAssignment(ctx, &WorkerAssignment{
		OwnerID:    1001,
		WorkerID:   "ubot-1",
		Pool:       "normal",
		HeavyScore: 15,
		MigratedAt: 1700000000,
	}); err != nil {
		t.Fatalf("SaveWorkerAssignment 1: %v", err)
	}
	if err := dm.SaveWorkerAssignment(ctx, &WorkerAssignment{
		OwnerID:    1002,
		WorkerID:   "ubot-hell",
		Pool:       "hell",
		HeavyScore: 95,
		MigratedAt: 1700000050,
	}); err != nil {
		t.Fatalf("SaveWorkerAssignment 2: %v", err)
	}
	got, err := dm.GetWorkerAssignment(ctx, 1002)
	if err != nil || got == nil || got.WorkerID != "ubot-hell" || got.Pool != "hell" || got.HeavyScore != 95 {
		t.Fatalf("unexpected assignment: %+v err=%v", got, err)
	}
	rows, err := dm.ListWorkerAssignmentsByWorker(ctx, "ubot-hell")
	if err != nil || len(rows) != 1 || rows[0].OwnerID != 1002 {
		t.Fatalf("unexpected ListWorkerAssignmentsByWorker: %+v err=%v", rows, err)
	}
}
