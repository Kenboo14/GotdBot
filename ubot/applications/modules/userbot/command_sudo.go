package userbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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
	s.mu.Lock()
	if id, ok := s.clientOwners[client]; ok && id > 0 {
		s.mu.Unlock()
		return id, nil
	}
	s.mu.Unlock()

	self, err := client.Self(ctx)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.clientOwners[client] = self.ID
	s.mu.Unlock()
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
	cacheKey := fmt.Sprintf("%d:%d", ownerID, userID)
	s.sudoMu.RLock()
	entry, found := s.sudoCache[cacheKey]
	s.sudoMu.RUnlock()
	if found {
		if time.Now().Before(entry.expiresAt) {
			return entry.allowed
		}
		// Entri expired langsung dihapus dan dianggap cache miss
		s.sudoMu.Lock()
		if cur, ok := s.sudoCache[cacheKey]; ok && cur.expiresAt == entry.expiresAt {
			delete(s.sudoCache, cacheKey)
		}
		s.sudoMu.Unlock()
	}

	var allowed bool
	var queryErr error
	if s.sqlite != nil {
		allowed, queryErr = s.sqlite.IsSudo(ctx, ownerID, userID)
	} else if s.db != nil {
		var row sudoRecord
		err := s.db.Collection("userbot_sudo").FindOne(ctx, bson.M{"owner_id": ownerID, "user_id": userID}).Decode(&row)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				allowed = false
			} else {
				queryErr = err
			}
		} else {
			allowed = true
		}
	}
	if queryErr != nil {
		log.Printf("[sudo %d:%d] Database query error: %v", ownerID, userID, queryErr)
		return false
	}

	s.sudoMu.Lock()
	cur, exists := s.sudoCache[cacheKey]
	if !exists || time.Now().After(cur.expiresAt) {
		s.sudoCache[cacheKey] = sudoCacheEntry{
			allowed:   allowed,
			expiresAt: time.Now().Add(30 * time.Minute),
		}
	} else {
		s.sudoMu.Unlock()
		return cur.allowed
	}
	s.sudoMu.Unlock()
	return allowed
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
	cacheKey := fmt.Sprintf("%d:%d", ownerID, userID)
	if add {
		if s.sqlite != nil {
			err = s.sqlite.AddSudo(ctx, ownerID, userID)
		}
		if s.db != nil {
			go func(oid, uid int64) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = s.db.Collection("userbot_sudo").UpdateOne(bgCtx, bson.M{"owner_id": oid, "user_id": uid}, bson.M{"$set": bson.M{"owner_id": oid, "user_id": uid}}, options.Update().SetUpsert(true))
			}(ownerID, userID)
		}
		if err == nil {
			s.sudoMu.Lock()
			s.sudoCache[cacheKey] = sudoCacheEntry{allowed: true, expiresAt: time.Now().Add(30 * time.Minute)}
			s.sudoMu.Unlock()
			err = sendUserText(ctx, client, target, fmt.Sprintf("Sudo ditambahkan: %d", userID))
		}
		return err
	}
	if s.sqlite != nil {
		err = s.sqlite.RemoveSudo(ctx, ownerID, userID)
	}
	if s.db != nil {
		go func(oid, uid int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sudo").DeleteOne(bgCtx, bson.M{"owner_id": oid, "user_id": uid})
		}(ownerID, userID)
	}
	if err != nil {
		return err
	}
	s.sudoMu.Lock()
	s.sudoCache[cacheKey] = sudoCacheEntry{allowed: false, expiresAt: time.Now().Add(30 * time.Minute)}
	s.sudoMu.Unlock()
	return sendUserText(ctx, client, target, fmt.Sprintf("Sudo dihapus: %d", userID))
}
