package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
)

type statefulMockRedis struct {
	mu      sync.Mutex
	strings map[string]string
	sets    map[string]map[string]bool
	hashes  map[string]map[string]string
	streams map[string][]models.PToService
}

func startStatefulMockRedis(t *testing.T) (*broker.Redis, *statefulMockRedis) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	st := &statefulMockRedis{
		strings: make(map[string]string),
		sets:    make(map[string]map[string]bool),
		hashes:  make(map[string]map[string]string),
		streams: make(map[string][]models.PToService),
	}

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
					st.mu.Lock()
					switch cmd {
					case "HELLO":
						_, _ = w.WriteString("-ERR unknown command 'HELLO'\r\n")
					case "PING":
						_, _ = w.WriteString("+PONG\r\n")
					case "SET":
						if len(args) >= 3 {
							st.strings[args[1]] = args[2]
						}
						_, _ = w.WriteString("+OK\r\n")
					case "SETEX":
						if len(args) >= 4 {
							st.strings[args[1]] = args[3]
						}
						_, _ = w.WriteString("+OK\r\n")
					case "GET":
						if len(args) >= 2 {
							if v, ok := st.strings[args[1]]; ok {
								writeBulk(w, v)
							} else {
								writeNull(w)
							}
						} else {
							writeNull(w)
						}
					case "MGET":
						keys := args[1:]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(keys))
						for _, k := range keys {
							if v, ok := st.strings[k]; ok {
								writeBulk(w, v)
							} else {
								writeNull(w)
							}
						}
					case "DEL":
						deleted := 0
						for _, k := range args[1:] {
							if _, ok := st.strings[k]; ok {
								delete(st.strings, k)
								deleted++
							}
						}
						_, _ = fmt.Fprintf(w, ":%d\r\n", deleted)
					case "SADD":
						s := st.sets[args[1]]
						if s == nil {
							s = make(map[string]bool)
							st.sets[args[1]] = s
						}
						added := 0
						for _, m := range args[2:] {
							if !s[m] {
								s[m] = true
								added++
							}
						}
						_, _ = fmt.Fprintf(w, ":%d\r\n", added)
					case "SMEMBERS":
						s := st.sets[args[1]]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(s))
						for m := range s {
							writeBulk(w, m)
						}
					case "HSET":
						h := st.hashes[args[1]]
						if h == nil {
							h = make(map[string]string)
							st.hashes[args[1]] = h
						}
						for i := 2; i+1 < len(args); i += 2 {
							h[args[i]] = args[i+1]
						}
						_, _ = w.WriteString(":1\r\n")
					case "HGET":
						if h := st.hashes[args[1]]; h != nil {
							if v, ok := h[args[2]]; ok {
								writeBulk(w, v)
							} else {
								writeNull(w)
							}
						} else {
							writeNull(w)
						}
					case "HGETALL":
						h := st.hashes[args[1]]
						_, _ = fmt.Fprintf(w, "*%d\r\n", len(h)*2)
						for k, v := range h {
							writeBulk(w, k)
							writeBulk(w, v)
						}
					case "XADD":
						stream := args[1]
						for i := 2; i+1 < len(args); i++ {
							if args[i] == "payload" {
								var p models.PToService
								if json.Unmarshal([]byte(args[i+1]), &p) == nil {
									st.streams[stream] = append(st.streams[stream], p)
								}
								break
							}
						}
						writeBulk(w, "1700000000000-0")
					default:
						_, _ = w.WriteString("+OK\r\n")
					}
					st.mu.Unlock()
					_ = w.Flush()
				}
			}(conn)
		}
	}()

	rdb := redis.NewClient(&redis.Options{
		Addr:            ln.Addr().String(),
		Protocol:        2,
		DisableIdentity: true,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
		_ = ln.Close()
	})
	return &broker.Redis{Client: rdb}, st
}

func (st *statefulMockRedis) registerWorker(ws WorkerStatus) {
	raw, _ := json.Marshal(ws)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sets[redisKeyWorkersSet] == nil {
		st.sets[redisKeyWorkersSet] = make(map[string]bool)
	}
	st.sets[redisKeyWorkersSet][ws.WorkerID] = true
	st.strings[redisKeyWorkerPrefix+ws.WorkerID] = string(raw)
}

func TestWorkerLoadBalancerRoutingFailoverAndCommands(t *testing.T) {
	brk, st := startStatefulMockRedis(t)
	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "lb.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()
	h := NewHandler(brk, dm)
	t.Setenv("OWNER", "9001")

	if err := brk.To(ctx, "to:ubot:command", models.PToService{OwnerID: 111, Type: "ping"}); err != nil {
		t.Fatalf("legacy single-worker route failed: %v", err)
	}
	st.mu.Lock()
	legacyLen := len(st.streams["to:ubot:command"])
	st.mu.Unlock()
	if legacyLen != 1 {
		t.Fatalf("expected 1 legacy stream item, got %d", legacyLen)
	}

	st.registerWorker(WorkerStatus{WorkerID: "ubot-1", Pool: "normal", ActiveSessions: 40, MaxSessions: 100, RAMAllocMB: 180, Status: "ready"})
	st.registerWorker(WorkerStatus{WorkerID: "ubot-2", Pool: "normal", ActiveSessions: 10, MaxSessions: 100, RAMAllocMB: 120, Status: "ready"})
	st.registerWorker(WorkerStatus{WorkerID: "ubot-hell", Pool: "hell", ActiveSessions: 5, MaxSessions: 50, RAMAllocMB: 210, Status: "ready"})

	if err := brk.To(ctx, "to:ubot:auth", models.PToService{OwnerID: 2001, Type: "auth_phone"}); err != nil {
		t.Fatalf("auth route failed: %v", err)
	}
	st.mu.Lock()
	assigned := st.hashes[redisKeyOwnerWorker]["2001"]
	authStreamLen := len(st.streams["to:ubot:auth:ubot-2"])
	st.mu.Unlock()
	if assigned != "ubot-2" || authStreamLen != 1 {
		t.Fatalf("expected owner 2001 routed to least-loaded ubot-2, assigned=%q streamLen=%d", assigned, authStreamLen)
	}

	st.registerWorker(WorkerStatus{WorkerID: "ubot-1", Pool: "normal", ActiveSessions: 100, MaxSessions: 100, Status: "ready"})
	st.registerWorker(WorkerStatus{WorkerID: "ubot-2", Pool: "normal", ActiveSessions: 100, MaxSessions: 100, Status: "ready"})
	if err := brk.To(ctx, "to:ubot:auth", models.PToService{OwnerID: 2002, Type: "auth_phone"}); !errors.Is(err, broker.ErrWorkerPoolFull) {
		t.Fatalf("expected ErrWorkerPoolFull when normal pool is full, got %v", err)
	}

	st.registerWorker(WorkerStatus{WorkerID: "ubot-2", Pool: "normal", ActiveSessions: 12, MaxSessions: 100, Status: "ready"})
	st.mu.Lock()
	delete(st.strings, redisKeyWorkerPrefix+"ubot-1")
	st.hashes[redisKeyOwnerWorker]["3001"] = "ubot-1"
	st.hashes[redisKeyOwnerPool]["3001"] = "normal"
	st.mu.Unlock()

	h.lb.CheckFailoverOnce(ctx)
	if got := h.lb.CheckFailoverOnce(ctx); got != 1 {
		t.Fatalf("expected 1 failover reassignment on 2nd missed heartbeat, got %d", got)
	}
	st.mu.Lock()
	reassignedTo := st.hashes[redisKeyOwnerWorker]["3001"]
	failoverLen := len(st.streams["to:ubot:command:ubot-2"])
	st.mu.Unlock()
	if reassignedTo != "ubot-2" || failoverLen == 0 {
		t.Fatalf("expected owner 3001 failed over to ubot-2, got %q (len=%d)", reassignedTo, failoverLen)
	}

	for _, cmd := range []string{"/worker status", "/worker move 3001 ubot-hell", "/worker drain ubot-2"} {
		h.WorkerCommand(ctx, b, &botgotd.Update{
			Message: &botgotd.Message{
				Chat: botgotd.Chat{ID: 9001},
				From: &botgotd.User{ID: 9001},
				Text: cmd,
			},
		})
	}
	msgs := extractRPCMessages(rpcLog)
	if len(msgs) < 3 {
		t.Fatalf("expected >= 3 /worker replies, got %d", len(msgs))
	}
	for i, msg := range msgs {
		assertMonitorCardLayout(t, "workerCmd["+strconv.Itoa(i)+"]", msg)
	}
}
