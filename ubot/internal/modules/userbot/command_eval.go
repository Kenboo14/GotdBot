package userbot

import (
	"context"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) evaluatorCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, prefix, args string, threadID int) error {
	return sendUserText(ctx, client, target, "Fitur !eval dinonaktifkan untuk menghemat RAM.", threadID)
}
