package userbot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"assterisk/ubot/internal/models"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

type aliveInlinePayload struct {
	OwnerID int64 `json:"owner_id"`
	ChatID  int64 `json:"chat_id"`
}

type alivePingResult struct {
	OK    bool    `json:"ok"`
	MS    float64 `json:"ms,omitempty"`
	Error string  `json:"error,omitempty"`
}

func (s *Service) handleAliveCommand(ctx context.Context, client *gobot.Client, chatID int64, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		ownerID = chatID
	}

	if s.redis != nil && s.redis.Client != nil {
		refBytes := make([]byte, 12)
		if _, rErr := rand.Read(refBytes); rErr == nil {
			ref := base64.RawURLEncoding.EncodeToString(refBytes)
			payload, mErr := json.Marshal(aliveInlinePayload{
				OwnerID: ownerID,
				ChatID:  chatID,
			})
			if mErr == nil {
				if setErr := s.redis.Client.Set(ctx, "inline:alive:"+ref, payload, 5*time.Minute).Err(); setErr == nil {
					if inlineErr := s.sendInlineQuery(ctx, client, target, "alive ref:"+ref, threadID); inlineErr == nil {
						return nil
					} else {
						log.Printf("[userbot %d] !alive inline gagal: %v", chatID, inlineErr)
					}
				}
			}
		}
	}

	started := time.Now()
	pingStr := "Timeout"
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err == nil {
		pingStr = fmt.Sprintf("%.3fms", float64(time.Since(started).Microseconds())/1000)
	}
	fallbackText := fmt.Sprintf("── GO UserBot ──\n  • UserID: %d\n  • Ping: %s", ownerID, pingStr)
	return sendUserText(ctx, client, target, fallbackText, threadID)
}

func (s *Service) handleAlivePingJob(job models.PToService) {
	if job.TaskID == "" || s.redis == nil || s.redis.Client == nil {
		return
	}
	targetOwner := job.OwnerID
	if targetOwner == 0 {
		targetOwner = job.UserID
	}
	if targetOwner == 0 {
		targetOwner = job.ChatID
	}

	s.mu.Lock()
	client, sessionChatID := s.findActiveClientLocked(targetOwner, job.UserID, job.ChatID)
	if client != nil && sessionChatID != 0 {
		s.lastActive[sessionChatID] = time.Now()
	}
	s.mu.Unlock()

	publishResult := func(res alivePingResult) {
		raw, err := json.Marshal(res)
		if err != nil {
			return
		}
		key := "alive:ping:" + job.TaskID
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.redis.Client.Set(bgCtx, key, raw, 30*time.Second).Err()
		_ = s.redis.Client.Publish(bgCtx, key, raw).Err()
	}

	if client == nil {
		publishResult(alivePingResult{OK: false, Error: "offline"})
		return
	}

	go func(c *gobot.Client) {
		pingCtx, cancel := context.WithTimeout(s.root, 3*time.Second)
		defer cancel()
		started := time.Now()
		if err := c.Ping(pingCtx); err != nil {
			publishResult(alivePingResult{OK: false, Error: err.Error()})
			return
		}
		ms := float64(time.Since(started).Microseconds()) / 1000.0
		publishResult(alivePingResult{OK: true, MS: ms})
	}(client)
}
