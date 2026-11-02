package userbot

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// ──────────────────────────────────────────────────────
// Inline Bot Cache Tests
// ──────────────────────────────────────────────────────

func newServiceWithInlineCache() *Service {
	return &Service{
		inlineBots: make(map[*telegram.Client]*tg.InputUser),
	}
}

// TestInlineBotCache_HitAndMiss verifies that a cached bot InputUser
// is returned on hit, and nil on miss.
func TestInlineBotCache_HitAndMiss(t *testing.T) {
	svc := newServiceWithInlineCache()
	client := &telegram.Client{}
	otherClient := &telegram.Client{}

	bot := &tg.InputUser{UserID: 123, AccessHash: 456}

	// Cache for `client`
	svc.inlineMu.Lock()
	svc.inlineBots[client] = bot
	svc.inlineMu.Unlock()

	// Hit
	svc.inlineMu.RLock()
	cached := svc.inlineBots[client]
	svc.inlineMu.RUnlock()

	if cached == nil {
		t.Fatal("expected cache hit, got nil")
	}
	if cached.UserID != 123 || cached.AccessHash != 456 {
		t.Fatalf("expected UserID=123, AccessHash=456; got %+v", cached)
	}

	// Miss — different client
	svc.inlineMu.RLock()
	missing := svc.inlineBots[otherClient]
	svc.inlineMu.RUnlock()

	if missing != nil {
		t.Fatalf("expected cache miss, got %+v", missing)
	}
}

// TestInlineBotCache_Eviction verifies that evictInlineBot removes the entry.
func TestInlineBotCache_Eviction(t *testing.T) {
	svc := newServiceWithInlineCache()
	client := &telegram.Client{}
	bot := &tg.InputUser{UserID: 100, AccessHash: 200}

	svc.inlineMu.Lock()
	svc.inlineBots[client] = bot
	svc.inlineMu.Unlock()

	// Verify cached
	svc.inlineMu.RLock()
	if svc.inlineBots[client] == nil {
		t.Fatal("expected cached bot")
	}
	svc.inlineMu.RUnlock()

	// Evict
	svc.evictInlineBot(client)

	// Verify evicted
	svc.inlineMu.RLock()
	if svc.inlineBots[client] != nil {
		t.Fatal("expected nil after eviction")
	}
	svc.inlineMu.RUnlock()
}

// TestInlineBotCache_SessionIsolation verifies that different clients
// maintain separate cache entries (no cross-account data leak).
func TestInlineBotCache_SessionIsolation(t *testing.T) {
	svc := newServiceWithInlineCache()
	clientA := &telegram.Client{}
	clientB := &telegram.Client{}

	botA := &tg.InputUser{UserID: 1, AccessHash: 100}
	botB := &tg.InputUser{UserID: 2, AccessHash: 200}

	svc.inlineMu.Lock()
	svc.inlineBots[clientA] = botA
	svc.inlineBots[clientB] = botB
	svc.inlineMu.Unlock()

	svc.inlineMu.RLock()
	gotA := svc.inlineBots[clientA]
	gotB := svc.inlineBots[clientB]
	svc.inlineMu.RUnlock()

	if gotA.UserID != 1 || gotA.AccessHash != 100 {
		t.Fatalf("clientA expected UserID=1 AccessHash=100, got %+v", gotA)
	}
	if gotB.UserID != 2 || gotB.AccessHash != 200 {
		t.Fatalf("clientB expected UserID=2 AccessHash=200, got %+v", gotB)
	}

	// Evicting A shouldn't affect B
	svc.evictInlineBot(clientA)
	svc.inlineMu.RLock()
	if svc.inlineBots[clientA] != nil {
		t.Fatal("clientA should be evicted")
	}
	if svc.inlineBots[clientB] == nil {
		t.Fatal("clientB should still be cached")
	}
	svc.inlineMu.RUnlock()
}

// ──────────────────────────────────────────────────────
// isAccessHashError Tests
// ──────────────────────────────────────────────────────

func TestIsAccessHashError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil", nil, false},
		{"generic", errStr("something went wrong"), false},
		{"BOT_INVALID", errStr("BOT_INVALID"), true},
		{"bot_invalid lower", errStr("bot_invalid"), true},
		{"ACCESS_HASH_INVALID", errStr("ACCESS_HASH_INVALID"), true},
		{"USER_ID_INVALID", errStr("USER_ID_INVALID (code 400)"), true},
		{"partial access_hash", errStr("rpc error: ACCESS_HASH_EXPIRED"), true},
		{"flood_wait", errStr("FLOOD_WAIT_5"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAccessHashError(tc.err)
			if got != tc.expect {
				t.Errorf("isAccessHashError(%q) = %v, want %v", tc.err, got, tc.expect)
			}
		})
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }

// ──────────────────────────────────────────────────────
// truncateQuery Tests
// ──────────────────────────────────────────────────────

func TestTruncateQuery(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"short", "short"},
		{"", ""},
		{strings.Repeat("a", 30), strings.Repeat("a", 30)},
		{strings.Repeat("a", 31), strings.Repeat("a", 27) + "..."},
		{strings.Repeat("a", 100), strings.Repeat("a", 27) + "..."},
	}
	for _, tc := range tests {
		got := truncateQuery(tc.input)
		if got != tc.expect {
			t.Errorf("truncateQuery(%q) = %q, want %q", tc.input, got, tc.expect)
		}
	}
}

// ──────────────────────────────────────────────────────
// Concurrent Cache Access Test (for -race)
// ──────────────────────────────────────────────────────

func TestInlineBotCache_ConcurrentAccess(t *testing.T) {
	svc := newServiceWithInlineCache()
	clients := make([]*telegram.Client, 10)
	for i := range clients {
		clients[i] = &telegram.Client{}
	}

	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bot := &tg.InputUser{UserID: int64(idx), AccessHash: int64(idx * 1000)}
			svc.inlineMu.Lock()
			svc.inlineBots[clients[idx]] = bot
			svc.inlineMu.Unlock()
		}(i)
	}
	wg.Wait()

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			svc.inlineMu.RLock()
			cached := svc.inlineBots[clients[idx]]
			svc.inlineMu.RUnlock()
			if cached == nil {
				t.Errorf("client[%d] cache miss", idx)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent evictors + readers
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			svc.evictInlineBot(clients[idx])
		}(i)
		go func(idx int) {
			defer wg.Done()
			svc.inlineMu.RLock()
			_ = svc.inlineBots[clients[idx]] // just access, value may or may not be nil
			svc.inlineMu.RUnlock()
		}(i)
	}
	wg.Wait()
}

// ──────────────────────────────────────────────────────
// BotInlineResultClass.GetID() Tests — ensures both result
// types return proper IDs
// ──────────────────────────────────────────────────────

func TestInlineResultGetID(t *testing.T) {
	// BotInlineResult
	result1 := &tg.BotInlineResult{ID: "article_1"}
	var iface1 tg.BotInlineResultClass = result1
	if iface1.GetID() != "article_1" {
		t.Errorf("BotInlineResult GetID() = %q, want %q", iface1.GetID(), "article_1")
	}

	// BotInlineMediaResult
	result2 := &tg.BotInlineMediaResult{ID: "media_2"}
	var iface2 tg.BotInlineResultClass = result2
	if iface2.GetID() != "media_2" {
		t.Errorf("BotInlineMediaResult GetID() = %q, want %q", iface2.GetID(), "media_2")
	}
}

// ──────────────────────────────────────────────────────
// botUsernameCache Tests
// ──────────────────────────────────────────────────────

func TestBotUsernameCache_EmptyToken(t *testing.T) {
	svc := &Service{botToken: ""}
	_, err := svc.botUsername(nil)
	if err == nil {
		t.Fatal("expected error for empty botToken")
	}
	if !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Errorf("expected BOT_TOKEN error, got: %v", err)
	}
}

func TestBotUsernameCache_CachedReturn(t *testing.T) {
	svc := &Service{
		botToken:         "fake:token",
		botUsernameCache: "MyBot",
	}
	username, err := svc.botUsername(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "MyBot" {
		t.Errorf("expected MyBot, got %q", username)
	}
}

// ──────────────────────────────────────────────────────
// RandomID Correctness & Collision Safety Tests
// ──────────────────────────────────────────────────────

// TestInlineRandomID_Uniqueness runs 5,000 concurrent calls across 10 goroutines
// and verifies that all generated IDs are strictly unique (0 collision).
func TestInlineRandomID_Uniqueness(t *testing.T) {
	const total = 5000
	const workers = 10
	perWorker := total / workers

	var mu sync.Mutex
	seen := make(map[int64]bool, total)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int64, perWorker)
			for i := 0; i < perWorker; i++ {
				id := inlineRandomID()
				if id == 0 {
					t.Errorf("got zero random ID")
				}
				local[i] = id
			}

			mu.Lock()
			for _, id := range local {
				if seen[id] {
					t.Errorf("collision detected for random ID: %d", id)
				}
				seen[id] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(seen) != total {
		t.Fatalf("expected %d unique IDs, got %d", total, len(seen))
	}
}

// TestPreWarmInlineBot_Cancellation verifies that preWarmInlineBot stops
// immediately when the client context is canceled.
func TestPreWarmInlineBot_Cancellation(t *testing.T) {
	svc := newServiceWithInlineCache()
	client := &telegram.Client{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should not block or panic
	svc.preWarmInlineBot(ctx, client)
}

// ──────────────────────────────────────────────────────
// BENCHMARKS: Local Helpers & Concurrency
// ──────────────────────────────────────────────────────

// Global sinks to prevent compiler dead-code elimination.
var (
	benchSinkID     int64
	benchSinkString string
	benchSinkBot    *tg.InputUser
)

// BenchmarkRandomID_GotdCryptoRand measures the current gotd/td/crypto generator.
func BenchmarkRandomID_GotdCryptoRand(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkID = inlineRandomID()
	}
}

// BenchmarkRandomID_TimeUnixNano measures the raw timestamp approach.
func BenchmarkRandomID_TimeUnixNano(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkID = time.Now().UnixNano()
	}
}

// BenchmarkRandomID_LegacyBinaryRead measures the previous crypto/rand + binary.Read approach.
func BenchmarkRandomID_LegacyBinaryRead(b *testing.B) {
	var dummy int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = binary.Read(crand.Reader, binary.BigEndian, &dummy)
		benchSinkID = dummy
	}
}

// BenchmarkInlineCache_RLockConcurrent measures concurrent read throughput on RWMutex cache.
func BenchmarkInlineCache_RLockConcurrent(b *testing.B) {
	svc := newServiceWithInlineCache()
	client := &telegram.Client{}
	svc.inlineMu.Lock()
	svc.inlineBots[client] = &tg.InputUser{UserID: 1, AccessHash: 2}
	svc.inlineMu.Unlock()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			svc.inlineMu.RLock()
			benchSinkBot = svc.inlineBots[client]
			svc.inlineMu.RUnlock()
		}
	})
}

// BenchmarkInlineCache_LockConcurrent measures throughput with standard Mutex (the old implementation).
func BenchmarkInlineCache_LockConcurrent(b *testing.B) {
	var mu sync.Mutex
	cache := make(map[*telegram.Client]*tg.InputUser)
	client := &telegram.Client{}
	mu.Lock()
	cache[client] = &tg.InputUser{UserID: 1, AccessHash: 2}
	mu.Unlock()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()
			benchSinkBot = cache[client]
			mu.Unlock()
		}
	})
}

// BenchmarkTruncateQuery measures the safety string truncation helper.
func BenchmarkTruncateQuery(b *testing.B) {
	query := "this_is_a_very_long_inline_query_for_notes_or_help_command"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkString = truncateQuery(query)
	}
}

// BenchmarkResultGetID measures interface GetID() dispatch with package-level sink.
func BenchmarkResultGetID(b *testing.B) {
	var iface tg.BotInlineResultClass = &tg.BotInlineResult{ID: "query_result_12345"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSinkString = iface.GetID()
	}
}

