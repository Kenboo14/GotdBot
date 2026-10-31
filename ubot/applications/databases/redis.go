package databases

import (
	"assterisk/ubot/applications/models"
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"strings"
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
		log.Printf("Error creating group: %v", err)
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
			Count:    500,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			// redis.Nil hanya menandakan tidak ada pesan baru selama periode Block.
			// Ini kondisi normal pada worker yang sedang idle.
			if err == redis.Nil {
				continue
			}
			log.Printf("⚠️ Redis Error: %v. Retrying in 2s...", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		for _, msg := range res[0].Messages {

			// 3. Jalankan Logic via Callback
			callback(ctx, msg)

			// 4. ACK jika sukses
			r.Client.XAck(ctx, stream, group, msg.ID)
		}
	}
}
