package broker

import (
	"assterisk/gateway/applications/models"
	"assterisk/gateway/applications/utils"
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-telegram/bot"
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

func (r *Redis) To(ctx context.Context, stream string, data models.PToService) error {

	jsonData, err := json.Marshal(data)
	if err != nil {
		utils.LogError("JSON Marshal failed", err, nil)
		return err
	}

	payload := map[string]interface{}{
		"payload": string(jsonData), // Simpan sebagai string JSON
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

func (r *Redis) Listener(ctx context.Context, b *bot.Bot, group string, worker string, stream string, callback func(context.Context, *bot.Bot, redis.XMessage)) {
	delay := 0 * time.Second

	err := r.Client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		utils.LogError("Create Group failed", err, map[string]interface{}{"group": group, "stream": stream})
	}

	processMessage := func(msg redis.XMessage) {
		defer func() {
			if rec := recover(); rec != nil {
				// Tangkap stack trace agar tahu baris mana yang bikin crash
				stack := string(debug.Stack())

				// Format pesan panic agar mudah dibaca di log
				log.Printf("🔥 [PANIC RECOVERED] Worker: %s | MsgID: %s\nError: %v\nStack: %s", worker, msg.ID, rec, stack)

				// Opsional: Di sini bisa tambah logic kirim alert ke admin Telegram jika Panic terjadi
			}
		}()

		// Jalankan callback user
		callback(ctx, b, msg)

		// Jika sukses sampai sini (tidak panic), ACK pesan
		// Note: Jika callback error logic biasa, kita tetap ACK agar tidak loop forever (kecuali mau retry mechanism)
		if err := r.Client.XAck(ctx, stream, group, msg.ID).Err(); err != nil {
			utils.LogError("XAck failed", err, map[string]interface{}{"msg_id": msg.ID, "group": group})
		}
	}

	log.Printf("🔄 Worker %s checking for pending messages...", worker)
	for {
		pendingRes, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: worker,
			Streams:  []string{stream, "0"},
			Count:    10,
			Block:    0,
		}).Result()

		if err != nil {
			utils.LogError("Recovery Read failed", err, map[string]interface{}{"worker": worker})
			break
		}

		if len(pendingRes) == 0 || len(pendingRes[0].Messages) == 0 {
			log.Printf("✅ Recovery complete for %s. Starting live listener.", worker)
			break
		}

		log.Printf("🔥 Recovering %d pending messages...", len(pendingRes[0].Messages))

		for _, msg := range pendingRes[0].Messages {
			processMessage(msg)

			// Delay Recovery
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}
	}

	for {
		// 1. Cek apakah context sudah dibatalkan (Shutdown)
		select {
		case <-ctx.Done():
			log.Printf("🛑 Worker %s stopping...", worker)
			return
		default:
		}

		// 2. Baca pesan (Blocking)
		res, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: worker,
			Streams:  []string{stream, ">"}, // ">" artinya pesan baru
			Count:    50,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			utils.LogError("Live Read failed", err, map[string]interface{}{"worker": worker})
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range res[0].Messages {
			processMessage(msg)

			// Delay Live
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
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
