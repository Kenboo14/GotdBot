package databases

import (
	"assterisk/ubot/internal/models"
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	Client *redis.Client
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
		PoolSize:     100,
		MinIdleConns: 10,
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

func (r *Redis) To(ctx context.Context, stream string, data *models.PFromService) error {

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"payload": string(jsonData), // Simpan sebagai string JSON
		"ts":      time.Now().Unix(),
	}

	return r.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		MaxLen: 1000,
		Approx: true,
		ID:     "*",
		Values: payload,
	}).Err()

}

func (r *Redis) Listener(ctx context.Context, group, worker, stream string, callback func(context.Context, redis.XMessage)) {
	err := r.Client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Printf("Error creating group %s on %s: %v", group, stream, err)
	}

	const workerCount = 16
	jobQueue := make(chan redis.XMessage, workerCount*4)

	// Bounded Worker Pool untuk concurrency stabil tanpa Head-of-Line Blocking
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
								slog.Error("ubot redis worker panic recovered",
									slog.Int("worker_id", workerID),
									slog.String("worker", worker),
									slog.String("msg_id", msg.ID),
									slog.Any("panic", rec),
									slog.String("stack", stack),
								)
							}
						}()

						callback(ctx, msg)
						r.Client.XAck(ctx, stream, group, msg.ID)
					}()
				}
			}
		}(i)
	}

	enqueueJob := func(msg redis.XMessage) bool {
		select {
		case <-ctx.Done():
			return false
		case jobQueue <- msg:
			return true
		}
	}

	// 1. Recovery Fase: Baca pesan pending yang belum di-ACK (PEL) jika container baru restart
	log.Printf("🔄 Ubot worker %s checking for pending messages in %s...", worker, stream)
	for {
		if ctx.Err() != nil {
			break
		}

		pendingRes, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: worker,
			Streams:  []string{stream, "0"},
			Count:    20,
			Block:    0,
		}).Result()

		if err != nil {
			if err != redis.Nil {
				log.Printf("⚠️ Recovery read failed on %s: %v", stream, err)
			}
			break
		}

		if len(pendingRes) == 0 || len(pendingRes[0].Messages) == 0 {
			log.Printf("✅ Recovery complete for %s. Live listener active.", worker)
			break
		}

		log.Printf("🔥 Recovering %d pending messages in %s...", len(pendingRes[0].Messages), stream)
		for _, msg := range pendingRes[0].Messages {
			if !enqueueJob(msg) {
				break
			}
		}
	}

	// 2. Live Reader Fase: Baca pesan baru yang masuk (">")
	defer func() {
		close(jobQueue)
		wg.Wait()
		log.Printf("🛑 Ubot worker %s stopped gracefully.", worker)
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
			log.Printf("⚠️ Redis Error on %s: %v. Retrying in 2s...", stream, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		for _, msg := range res[0].Messages {
			if !enqueueJob(msg) {
				return
			}
		}
	}
}
