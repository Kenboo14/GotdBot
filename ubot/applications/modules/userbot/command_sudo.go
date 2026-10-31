package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type sudoRecord struct {
	OwnerID int64 `bson:"owner_id"`
	UserID  int64 `bson:"user_id"`
}

func sendUserText(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, text string) error {
	_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, text)
	return err
}

func (s *Service) userbotOwner(ctx context.Context, client *telegram.Client) (int64, error) {
	self, err := client.Self(ctx)
	if err != nil {
		return 0, err
	}
	return self.ID, nil
}

func messageActor(msg *tg.Message, ownerID int64) (int64, bool) {
	if msg == nil {
		return 0, false
	}
	if msg.Out {
		return ownerID, true
	}
	if sender, ok := msg.FromID.(*tg.PeerUser); ok {
		return sender.UserID, true
	}
	return 0, false
}

func (s *Service) isSudo(ctx context.Context, ownerID, userID int64) bool {
	if ownerID == 0 || userID == 0 {
		return false
	}
	var row sudoRecord
	err := s.db.Collection("userbot_sudo").FindOne(ctx, bson.M{"owner_id": ownerID, "user_id": userID}).Decode(&row)
	return err == nil
}

func (s *Service) commandAllowed(ctx context.Context, client *telegram.Client, msg *tg.Message) bool {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return false
	}
	actorID, ok := messageActor(msg, ownerID)
	if !ok {
		return false
	}
	return actorID == ownerID || s.isSudo(ctx, ownerID, actorID)
}

func (s *Service) sudoCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, args string, add bool) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat mengubah sudo.")
	}
	value := strings.TrimSpace(args)
	if value == "" {
		return sendUserText(ctx, client, target, "Gunakan: !addsudo <user_id> atau !rmsudo <user_id>")
	}
	userID, err := strconv.ParseInt(strings.TrimPrefix(value, "@"), 10, 64)
	if err != nil || userID <= 0 {
		return sendUserText(ctx, client, target, "User ID tidak valid. Gunakan ID numerik Telegram.")
	}
	collection := s.db.Collection("userbot_sudo")
	if add {
		_, err = collection.UpdateOne(ctx, bson.M{"owner_id": ownerID, "user_id": userID}, bson.M{"$set": bson.M{"owner_id": ownerID, "user_id": userID}}, options.Update().SetUpsert(true))
		if err == nil {
			err = sendUserText(ctx, client, target, fmt.Sprintf("Sudo ditambahkan: %d", userID))
		}
		return err
	}
	result, removeErr := collection.DeleteOne(ctx, bson.M{"owner_id": ownerID, "user_id": userID})
	if removeErr != nil {
		return removeErr
	}
	text := fmt.Sprintf("Sudo dihapus: %d", userID)
	if result.DeletedCount == 0 {
		text = "User tersebut tidak ada di daftar sudo."
	}
	return sendUserText(ctx, client, target, text)
}
