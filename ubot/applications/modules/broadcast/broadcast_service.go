package broadcast

import (
	"assterisk/ubot/applications/models"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Service struct{ repo *Repository }

func NewService(r *Repository) *Service { return &Service{repo: r} }

func (s *Service) Broadcast(parent context.Context, msg redis.XMessage) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return
	}
	var job models.PToService
	if err := json.Unmarshal([]byte(payload), &job); err != nil || job.ChatID == 0 {
		return
	}
	input := strings.TrimSpace(job.Text)
	if input == "" {
		return
	}
	reply := fmt.Sprintf("Pesan diterima worker: %s", input)
	if err := s.repo.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: job.ChatID, Text: reply}); err != nil {
		fmt.Printf("publish response failed: %v\n", err)
	}
}
