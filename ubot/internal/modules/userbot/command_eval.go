package userbot

import (
	"context"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

func (s *Service) evaluatorCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, prefix, args string, threadID int) error {
	return sendUserText(ctx, client, target, "Fitur !eval dinonaktifkan untuk menghemat RAM.", threadID)
}
