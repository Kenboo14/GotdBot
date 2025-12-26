package broadcast

import (
	"assterisk/ubot/applications/models"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Service struct {
	repo *Repository
}

func NewService(r *Repository) *Service {
	return &Service{repo: r}
}

func (s *Service) Broadcast(c context.Context, msg redis.XMessage) {

	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	var job map[string]interface{}
	json.Unmarshal([]byte(msg.Values["payload"].(string)), &job)

	// --- LOGIKA BISNIS ---
	input := job["text"].(string)
	chat_id := job["chat_id"].(float64)

	reply_text := fmt.Sprintf("Halo, pesan '%s' sudah diproses!", input)

	s.repo.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: int64(chat_id), Text: reply_text})

}
