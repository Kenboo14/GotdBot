package userbot

import (
	"assterisk/ubot/internal/databases"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/redis/go-redis/v9"
)

func startWorkerMockRedis(t *testing.T) (*databases.Redis, map[string]string, map[string]map[string]string, map[string][]string) {
	t.Helper()
	r, _, strMap, hashMap, streamMap := startWorkerMockRedisLocked(t)
	return r, strMap, hashMap, streamMap
}

func startWorkerMockRedisLocked(t *testing.T) (*databases.Redis, *sync.Mutex, map[string]string, map[string]map[string]string, map[string][]string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	var mu sync.Mutex
	strMap := make(map[string]string)
	setMap := make(map[string]map[string]bool)
	hashMap := make(map[string]map[string]string)
	streamMap := make(map[string][]string)

	writeBulk := func(w *bufio.Writer, s string) {
		_, _ = fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s)
	}
	writeNull := func(w *bufio.Writer) {
		_, _ = w.WriteString("$-1\r\n")
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := bufio.NewWriter(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil || !strings.HasPrefix(line, "*") {
						return
					}
					n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
					if err != nil || n <= 0 {
						return
					}
					args := make([]string, n)
					for i := 0; i < n; i++ {
						szLine, err := r.ReadString('\n')
						if err != nil || !strings.HasPrefix(szLine, "$") {
							return
						}
						sz, err := strconv.Atoi(strings.TrimSpace(szLine[1:]))
						if err != nil || sz < 0 {
							return
						}
						buf := make([]byte, sz+2)
						if _, err := io.ReadFull(r, buf); err != nil {
							return
						}
						args[i] = string(buf[:sz])
					}
					cmd := strings.ToUpper(args[0])
					mu.Lock()
					switch cmd {
					case "HELLO":
						_, _ = w.WriteString("-ERR unknown command 'HELLO'\r\n")
					case "GET":
						if v, ok := strMap[args[1]]; ok {
							writeBulk(w, v)
						} else {
							writeNull(w)
						}
					case "MGET":
						keys := args[1:]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(keys))
						for _, k := range keys {
							if v, ok := strMap[k]; ok {
								writeBulk(w, v)
							} else {
								writeNull(w)
							}
						}
					case "SADD":
						if setMap[args[1]] == nil {
							setMap[args[1]] = make(map[string]bool)
						}
						for _, m := range args[2:] {
							setMap[args[1]][m] = true
						}
						_, _ = w.WriteString(":1\r\n")
					case "SMEMBERS":
						s := setMap[args[1]]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(s))
						for m := range s {
							writeBulk(w, m)
						}
					case "SET", "SETEX":
						if cmd == "SETEX" && len(args) >= 4 {
							strMap[args[1]] = args[3]
						} else if len(args) >= 3 {
							strMap[args[1]] = args[2]
						}
						_, _ = w.WriteString("+OK\r\n")
					case "DEL":
						delete(strMap, args[1])
						_, _ = w.WriteString(":1\r\n")
					case "HSET":
						if hashMap[args[1]] == nil {
							hashMap[args[1]] = make(map[string]string)
						}
						hashMap[args[1]][args[2]] = args[3]
						_, _ = w.WriteString(":1\r\n")
					case "HSETNX":
						if hashMap[args[1]] == nil {
							hashMap[args[1]] = make(map[string]string)
						}
						if _, exists := hashMap[args[1]][args[2]]; exists {
							_, _ = w.WriteString(":0\r\n")
						} else {
							hashMap[args[1]][args[2]] = args[3]
							_, _ = w.WriteString(":1\r\n")
						}
					case "HGET":
						if v, ok := hashMap[args[1]][args[2]]; ok {
							writeBulk(w, v)
						} else {
							writeNull(w)
						}
					case "HGETALL":
						h := hashMap[args[1]]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(h)*2)
						for k, v := range h {
							writeBulk(w, k)
							writeBulk(w, v)
						}
					case "XADD":
						for i := 2; i+1 < len(args); i++ {
							if args[i] == "payload" {
								streamMap[args[1]] = append(streamMap[args[1]], args[i+1])
								break
							}
						}
						writeBulk(w, "1700000000000-0")
					default:
						_, _ = w.WriteString("+OK\r\n")
					}
					mu.Unlock()
					_ = w.Flush()
				}
			}(conn)
		}
	}()

	rdb := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), Protocol: 2, DisableIdentity: true})
	t.Cleanup(func() {
		_ = rdb.Close()
		_ = ln.Close()
	})
	return &databases.Redis{Client: rdb}, &mu, strMap, hashMap, streamMap
}

func TestComputeHeavyScoreAndConfigCache(t *testing.T) {
	lightCfg := jasebConfigDoc{
		Groups:    make([]jasebGroupDoc, 20),
		Delay:     30,
		DelayMode: "manual",
		Mode:      "Normal",
		Status:    "Berhenti",
	}
	if score := computeHeavyScore(lightCfg, 5, 0, 0); score != 0 {
		t.Fatalf("expected 0 heavy score for light user, got %d", score)
	}

	heavyCfg := jasebConfigDoc{
		Groups:    make([]jasebGroupDoc, 160),
		Topics:    make([]jasebTopicDoc, 20),
		Delay:     5,
		DelayMode: "manual",
		Mode:      "Normal",
		Status:    "Berjalan",
	}
	if score := computeHeavyScore(heavyCfg, 150, 4, 0.50); score < 160 {
		t.Fatalf("expected high combined heavy score, got %d", score)
	}

	cache := newUbotConfigCache(2, 50*time.Millisecond)
	cache.Set(1, jasebConfigDoc{OwnerID: 1, Delay: 10, Lists: []int{5}})
	cache.Set(2, jasebConfigDoc{OwnerID: 2, Delay: 20})
	cache.Set(3, jasebConfigDoc{OwnerID: 3, Delay: 30})
	if _, ok := cache.Get(1); ok {
		t.Fatalf("expected entry 1 evicted at max 2")
	}
	got2, ok := cache.Get(2)
	if !ok || got2.Delay != 20 {
		t.Fatalf("expected entry 2 present with delay 20")
	}
}

func TestWorkerOwnershipMigrationAndHeavyMetrics(t *testing.T) {
	sq, err := databases.NewSQLiteClient(filepath.Join(t.TempDir(), "ubot_worker.db"))
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	rClient, strMap, hashMap, streamMap := startWorkerMockRedis(t)
	svc := NewService(ctx, sq, nil, rClient, 1, "hash", make([]byte, 32), "tok", "", 1)
	svc.ConfigureWorker("ubot-1", "normal", 100)

	if !svc.isSessionOwnedByThisWorker(ctx, 8001) {
		t.Fatalf("expected normal worker to claim unassigned owner 8001")
	}
	a, err := sq.GetWorkerAssignment(ctx, 8001)
	if err != nil || a == nil || a.WorkerID != "ubot-1" {
		t.Fatalf("expected saved assignment for ubot-1, got %+v err=%v", a, err)
	}

	hellSvc := NewService(ctx, sq, nil, rClient, 1, "hash", make([]byte, 32), "tok", "", 1)
	hellSvc.ConfigureWorker("ubot-hell", "hell", 50)
	if hellSvc.isSessionOwnedByThisWorker(ctx, 8002) {
		t.Fatalf("expected hell worker not to auto-claim unassigned owner 8002")
	}

	for i := 0; i < 125; i++ {
		svc.recordHeavyUpdate(8001)
	}
	for i := 0; i < 3; i++ {
		svc.recordHeavyFlood(8001)
	}
	for i := 0; i < 10; i++ {
		svc.recordHeavyBroadcastResult(8001, i < 4)
	}
	upm, fl5, errRate := svc.snapshotHeavyMetrics(8001, time.Now())
	if upm < 120 || fl5 != 3 || errRate < 0.59 {
		t.Fatalf("unexpected rolling metrics: upm=%d fl5=%d errRate=%.2f", upm, fl5, errRate)
	}

	hellRaw, _ := json.Marshal(workerHeartbeatDoc{
		WorkerID: "ubot-hell", Pool: "hell", ActiveSessions: 1, MaxSessions: 50, Status: "ready",
	})
	_ = rClient.Client.SAdd(ctx, redisKeyWorkersSet, "ubot-hell").Err()
	_ = rClient.Client.Set(ctx, redisKeyWorkerPrefix+"ubot-hell", string(hellRaw), 0).Err()

	_ = svc.migrateSessionToWorker(ctx, 8001, 8001, "ubot-hell", "hell", 90, "test_heavy")
	if got := hashMap[redisKeyOwnerWorker]["8001"]; got != "ubot-hell" {
		t.Fatalf("expected owner 8001 migrated to ubot-hell in Redis, got %q", got)
	}
	if len(streamMap["to:ubot:command:ubot-hell"]) != 1 {
		t.Fatalf("expected 1 activate_migrated_session job pushed to ubot-hell, got %d", len(streamMap["to:ubot:command:ubot-hell"]))
	}
	_ = strMap
}

func TestEvaluateHeavyUsersHysteresis(t *testing.T) {
	ctx := context.Background()
	rClient, stateMu, _, hashMap, streamMap := startWorkerMockRedisLocked(t)

	ubot1Raw, _ := json.Marshal(workerHeartbeatDoc{
		WorkerID: "ubot-1", Pool: "normal", ActiveSessions: 2, MaxSessions: 100, Status: "ready",
	})
	ubotHellRaw, _ := json.Marshal(workerHeartbeatDoc{
		WorkerID: "ubot-hell", Pool: "hell", ActiveSessions: 1, MaxSessions: 50, Status: "ready",
	})
	_ = rClient.Client.SAdd(ctx, redisKeyWorkersSet, "ubot-1", "ubot-hell").Err()
	_ = rClient.Client.Set(ctx, redisKeyWorkerPrefix+"ubot-1", string(ubot1Raw), 0).Err()
	_ = rClient.Client.Set(ctx, redisKeyWorkerPrefix+"ubot-hell", string(ubotHellRaw), 0).Err()

	svc := NewService(ctx, nil, nil, rClient, 1, "hash", make([]byte, 32), "tok", "", 1)
	svc.ConfigureWorker("ubot-1", "normal", 100)
	svc.mu.Lock()
	svc.active[9001] = &gobot.Client{}
	svc.owners[9001] = 9001
	svc.mu.Unlock()
	svc.cfgCache.Set(9001, jasebConfigDoc{
		OwnerID:   9001,
		Groups:    make([]jasebGroupDoc, 260),
		Status:    "Berjalan",
		Delay:     5,
		DelayMode: "manual",
	})

	for i := 1; i <= 2; i++ {
		svc.evaluateHeavyUsers(ctx)
		stateMu.Lock()
		n := len(streamMap["to:ubot:command:ubot-hell"])
		stateMu.Unlock()
		if n != 0 {
			t.Fatalf("expected no hell migration on streak %d, got %d jobs", i, n)
		}
	}

	svc.evaluateHeavyUsers(ctx)
	stateMu.Lock()
	hellJobs := len(streamMap["to:ubot:command:ubot-hell"])
	assignedWorker := hashMap[redisKeyOwnerWorker]["9001"]
	stateMu.Unlock()
	if hellJobs != 1 || assignedWorker != "ubot-hell" {
		t.Fatalf("expected migration to ubot-hell on 3rd streak, jobs=%d assigned=%q", hellJobs, assignedWorker)
	}

	hellSvc := NewService(ctx, nil, nil, rClient, 1, "hash", make([]byte, 32), "tok", "", 1)
	hellSvc.ConfigureWorker("ubot-hell", "hell", 50)
	hellSvc.mu.Lock()
	hellSvc.active[9002] = &gobot.Client{}
	hellSvc.owners[9002] = 9002
	hellSvc.mu.Unlock()
	hellSvc.cfgCache.Set(9002, jasebConfigDoc{
		OwnerID: 9002,
		Groups:  make([]jasebGroupDoc, 10),
		Status:  "Berhenti",
	})

	for i := 1; i <= 3; i++ {
		hellSvc.evaluateHeavyUsers(ctx)
		stateMu.Lock()
		n := len(streamMap["to:ubot:command:ubot-1"])
		stateMu.Unlock()
		if n != 0 {
			t.Fatalf("expected no demotion before 6h min hell stay (iter %d), got %d jobs", i, n)
		}
	}

	hellSvc.heavyMu.Lock()
	st := hellSvc.getOrCreateHeavyStateLocked(9002)
	st.enteredHellAt = time.Now().Add(-7 * time.Hour)
	st.lightStreak = 0
	hellSvc.heavyMu.Unlock()

	for i := 1; i <= 2; i++ {
		hellSvc.evaluateHeavyUsers(ctx)
		stateMu.Lock()
		n := len(streamMap["to:ubot:command:ubot-1"])
		stateMu.Unlock()
		if n != 0 {
			t.Fatalf("expected no cooldown demotion on streak %d, got %d jobs", i, n)
		}
	}

	hellSvc.evaluateHeavyUsers(ctx)
	stateMu.Lock()
	normalJobs := len(streamMap["to:ubot:command:ubot-1"])
	demotedWorker := hashMap[redisKeyOwnerWorker]["9002"]
	stateMu.Unlock()
	if normalJobs != 1 || demotedWorker != "ubot-1" {
		t.Fatalf("expected cooldown migration to ubot-1 on 3rd streak, jobs=%d assigned=%q", normalJobs, demotedWorker)
	}
}
