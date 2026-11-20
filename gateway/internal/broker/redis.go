package broker

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"assterisk/gateway/internal/utils"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrWorkerPoolFull = errors.New("worker pool full")

type Redis struct {
	Client         *redis.Client
	mu             sync.RWMutex
	streamResolver func(context.Context, string, models.PToService) (string, error)
}

func (r *Redis) SetStreamResolver(fn func(context.Context, string, models.PToService) (string, error)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.streamResolver = fn
	r.mu.Unlock()
}

func NewRedis(ctx context.Context, addr string, pwd string, useTLS bool) (*Redis, error) {
	var tlsConfig *tls.Config
	if useTLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     pwd,
		DB:           0,
		PoolSize:     32,
		MinIdleConns: 2,
		MaxRetries:   3,
		TLSConfig:    tlsConfig,
	})

	// Cek apakah koneksi berhasil
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	log.Println("🔌 Connected to Redis at", addr)

	return &Redis{Client: rdb}, nil
}

func (r *Redis) To(ctx context.Context, stream string, data models.PToService) error {
	targetStream := stream
	if stream == "to:ubot:command" || stream == "to:ubot:auth" {
		r.mu.RLock()
		resolver := r.streamResolver
		r.mu.RUnlock()
		if resolver != nil {
			resolved, err := resolver(ctx, stream, data)
			if err != nil {
				return err
			}
			if resolved != "" {
				targetStream = resolved
			}
		}
	}
	return r.ToStream(ctx, targetStream, data)
}

func (r *Redis) ToStream(ctx context.Context, stream string, data models.PToService) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		utils.LogError("JSON Marshal failed", err, nil)
		return err
	}

	payload := map[string]interface{}{
		"payload": string(jsonData),
		"ts":      time.Now().Unix(),
	}

	_, err = r.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		MaxLen: 1000,
		Approx: true,
		ID:     "*",
		Values: payload,
	}).Result()

	if err != nil {
		utils.LogError("XAdd failed", err, map[string]interface{}{"stream": stream})
		return err
	}

	return nil
}

func (r *Redis) Listener(ctx context.Context, b *botgotd.Bot, group string, worker string, stream string, callback func(context.Context, *botgotd.Bot, redis.XMessage)) {
	r.ListenerWithWorkers(ctx, b, group, worker, stream, 16, callback)
}

func (r *Redis) ListenerWithWorkers(ctx context.Context, b *botgotd.Bot, group string, worker string, stream string, workerCount int, callback func(context.Context, *botgotd.Bot, redis.XMessage)) {
	if workerCount <= 0 {
		workerCount = 1
	}
	// Buat consumer group jika belum ada
	err := r.Client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		utils.LogError("Create Group failed", err, map[string]interface{}{"group": group, "stream": stream})
	}

	jobQueue := make(chan redis.XMessage, workerCount*4)

	// Inisialisasi Bounded Worker Pool untuk mencegah Head-of-Line Blocking
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-jobQueue:
					if !ok {
						return
					}
					func() {
						defer func() {
							if rec := recover(); rec != nil {
								stack := string(debug.Stack())
								slog.Error("redis worker panic recovered",
									slog.Int("worker_id", workerID),
									slog.String("worker", worker),
									slog.String("msg_id", msg.ID),
									slog.Any("panic", rec),
									slog.String("stack", stack),
								)
							}
						}()

						callback(ctx, b, msg)

						var ackErr error
						for attempt := 0; attempt < 3; attempt++ {
							ackErr = r.Client.XAck(ctx, stream, group, msg.ID).Err()
							if ackErr == nil {
								break
							}
							select {
							case <-ctx.Done():
								break
							case <-time.After(100 * time.Millisecond):
							}
						}
						if ackErr != nil {
							utils.LogError("XAck failed after 3 attempts", ackErr, map[string]interface{}{"msg_id": msg.ID, "group": group})
						}
					}()
				}
			}
		}(i)
	}

	// Helper untuk enqueue job dengan context cancellation guard
	enqueueJob := func(msg redis.XMessage) bool {
		select {
		case <-ctx.Done():
			return false
		case jobQueue <- msg:
			return true
		}
	}

	// 1. Recovery Fase: Baca pesan pending yang belum di-ACK (PEL)
	log.Printf("🔄 Worker %s checking for pending messages...", worker)
	lastPendingID := "0"
	for {
		if ctx.Err() != nil {
			break
		}

		pendingRes, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: worker,
			Streams:  []string{stream, lastPendingID},
			Count:    20,
			Block:    0,
		}).Result()

		if err != nil {
			if err != redis.Nil {
				utils.LogError("Recovery Read failed", err, map[string]interface{}{"worker": worker})
			}
			break
		}

		if len(pendingRes) == 0 || len(pendingRes[0].Messages) == 0 {
			log.Printf("✅ Recovery complete for %s. Live listener active.", worker)
			break
		}

		log.Printf("🔥 Recovering %d pending messages in %s...", len(pendingRes[0].Messages), stream)
		enqueuedAll := true
		for _, msg := range pendingRes[0].Messages {
			if !enqueueJob(msg) {
				enqueuedAll = false
				break
			}
		}
		if !enqueuedAll {
			break
		}
		lastPendingID = pendingRes[0].Messages[len(pendingRes[0].Messages)-1].ID
	}

	// 2. Live Reader Fase: Baca pesan baru yang masuk (">")
	defer func() {
		close(jobQueue)
		wg.Wait()
		log.Printf("🛑 Worker %s stopped gracefully.", worker)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		res, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: worker,
			Streams:  []string{stream, ">"},
			Count:    50,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			utils.LogError("Live Read failed", err, map[string]interface{}{"worker": worker})
			time.Sleep(1 * time.Second)
			continue
		}

		for _, msg := range res[0].Messages {
			if !enqueueJob(msg) {
				return
			}
		}
	}
}

var rateLimitLua = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
    redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local limit = tonumber(ARGV[2])
if current > limit then
    local ttl = redis.call('PTTL', KEYS[1])
    return {0, ttl}
end
return {1, 0}
`)

func (r *Redis) CheckLimit(ctx context.Context, identifier string, limit int, window time.Duration) (bool, time.Duration, error) {
	key := "rate_limit:" + identifier
	windowMs := window.Milliseconds()

	res, err := rateLimitLua.Run(ctx, r.Client, []string{key}, windowMs, limit).Slice()
	if err != nil {
		utils.LogError("RateLimit Lua script failed", err, map[string]interface{}{"key": key})
		return false, 0, err
	}

	allowed, _ := res[0].(int64)
	ttlMs, _ := res[1].(int64)

	if allowed == 1 {
		return true, 0, nil
	}
	return false, time.Duration(ttlMs) * time.Millisecond, nil
}
