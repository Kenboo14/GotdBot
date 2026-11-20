package userbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type sudoRecord struct {
	OwnerID int64 `bson:"owner_id"`
	UserID  int64 `bson:"user_id"`
}

func randInt64() int64 {
	id, err := crypto.RandInt64(crypto.DefaultRand())
	if err == nil && id != 0 {
		return id
	}
	return time.Now().UnixNano()
}

func (s *Service) userbotOwner(ctx context.Context, client *gobot.Client) (int64, error) {
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

func (s *Service) commandAllowed(ctx context.Context, client *gobot.Client, msg *tg.Message) bool {
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

func (s *Service) sudoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, add bool, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat mengubah sudo.", threadID)
	}
	userID, displayName, resolveErr := s.resolveTargetUserID(ctx, client, target, msg, entities, args)
	if resolveErr != nil {
		if errors.Is(resolveErr, errNoTargetUser) {
			return sendUserText(ctx, client, target, "Balas pesan pengguna atau gunakan: !addsudo <user_id|@username> / !rmsudo <user_id|@username>", threadID)
		}
		return sendUserText(ctx, client, target, resolveErr.Error(), threadID)
	}
	targetLabel := fmt.Sprintf("%d", userID)
	if displayName != "" {
		targetLabel = fmt.Sprintf("%s (%d)", displayName, userID)
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
			err = sendUserText(ctx, client, target, "Sudo ditambahkan: "+targetLabel, threadID)
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
	return sendUserText(ctx, client, target, "Sudo dihapus: "+targetLabel, threadID)
}

func (s *Service) listSudoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if s.sqlite == nil {
		return sendUserText(ctx, client, target, "Database lokal tidak tersedia.", threadID)
	}
	ids, err := s.sqlite.ListSudo(ctx, ownerID)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membaca daftar sudo: "+err.Error(), threadID)
	}
	if len(ids) == 0 {
		return sendUserText(ctx, client, target, "Daftar sudo kosong.", threadID)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Daftar Sudo (%d pengguna):\n", len(ids))
	for i, uid := range ids {
		fmt.Fprintf(&sb, "%d. %d\n", i+1, uid)
	}
	return sendUserText(ctx, client, target, strings.TrimSpace(sb.String()), threadID)
}

func (s *Service) clearSudoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if msg == nil || !msg.Out {
		return sendUserText(ctx, client, target, "Hanya pemilik userbot yang dapat menghapus seluruh sudo.", threadID)
	}
	var removed int64
	if s.sqlite != nil {
		n, err := s.sqlite.ClearSudo(ctx, ownerID)
		if err != nil {
			return sendUserText(ctx, client, target, "Gagal menghapus daftar sudo: "+err.Error(), threadID)
		}
		removed = n
	}
	if s.db != nil {
		go func(oid int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sudo").DeleteMany(bgCtx, bson.M{"owner_id": oid})
		}(ownerID)
	}
	prefix := fmt.Sprintf("%d:", ownerID)
	s.sudoMu.Lock()
	for k := range s.sudoCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.sudoCache, k)
		}
	}
	s.sudoMu.Unlock()
	return sendUserText(ctx, client, target, fmt.Sprintf("Seluruh akses sudo dihapus (%d entri).", removed), threadID)
}

func (s *Service) dispatchExtendedCommand(ctx context.Context, client *gobot.Client, chatID int64, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, name, args string, threadID int) (bool, error) {
	switch name {
	case "lsudo":
		return true, s.listSudoCommand(ctx, client, target, threadID)
	case "clearsudo":
		return true, s.clearSudoCommand(ctx, client, target, msg, threadID)
	case "ban":
		return true, s.banCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "unban":
		return true, s.banCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "kick":
		return true, s.kickCommand(ctx, client, target, msg, entities, args, threadID)
	case "mute":
		return true, s.muteCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "unmute":
		return true, s.muteCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "promote", "fullpromote", "demote":
		return true, s.adminRoleCommand(ctx, client, target, msg, entities, args, name, threadID)
	case "pin":
		return true, s.pinCommand(ctx, client, target, msg, args, false, threadID)
	case "unpin":
		return true, s.pinCommand(ctx, client, target, msg, args, true, threadID)
	case "del":
		return true, s.delCommand(ctx, client, target, msg, threadID)
	case "purge":
		return true, s.purgeCommand(ctx, client, target, msg, threadID)
	case "dh":
		return true, s.deleteHistoryCommand(ctx, client, target, msg, args, threadID)
	case "zombies":
		return true, s.zombiesCommand(ctx, client, target, threadID)
	case "gban":
		return true, s.gbanCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "ungban":
		return true, s.gbanCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "gmute":
		return true, s.gmuteCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "ungmute":
		return true, s.gmuteCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "gkick":
		return true, s.gkickCommand(ctx, client, target, msg, entities, args, threadID)
	case "lsgban":
		return true, s.listGlobalModCommand(ctx, client, target, "gban", threadID)
	case "lsgmute":
		return true, s.listGlobalModCommand(ctx, client, target, "gmute", threadID)
	case "cancelall":
		return true, s.cancelAllBroadcastsCommand(ctx, client, target, threadID)
	case "ucast":
		return true, s.broadcastCommand(ctx, client, target, msg, "-pc "+args, threadID)
	case "fgcast", "fucast":
		return true, s.forwardBroadcastCommand(ctx, client, target, msg, args, name, threadID)
	case "spam":
		return true, s.spamCommand(ctx, client, target, msg, args, threadID)
	case "spamcast":
		return true, s.spamcastCommand(ctx, client, target, msg, args, threadID)
	case "memcast":
		return true, s.memcastCommand(ctx, client, target, msg, args, threadID)
	case "id":
		return true, s.idCommand(ctx, client, target, msg, entities, args, threadID)
	case "info":
		return true, s.infoCommand(ctx, client, target, msg, entities, args, threadID)
	case "limit":
		return true, s.limitCommand(ctx, client, target, threadID)
	case "sg":
		return true, s.sgCommand(ctx, client, target, msg, entities, args, threadID)
	case "setbio":
		return true, s.setBioCommand(ctx, client, target, msg, args, threadID)
	case "setname":
		return true, s.setNameCommand(ctx, client, target, msg, args, threadID)
	case "setusername":
		return true, s.setUsernameCommand(ctx, client, target, msg, args, threadID)
	case "join":
		return true, s.joinCommand(ctx, client, target, args, threadID)
	case "kickme":
		return true, s.kickmeCommand(ctx, client, target, threadID)
	case "invite":
		return true, s.inviteCommand(ctx, client, target, msg, entities, args, threadID)
	case "fontlist":
		return true, s.fontListCommand(ctx, client, target, threadID)
	case "font":
		return true, s.fontCommand(ctx, client, target, msg, args, threadID)
	case "azan":
		return true, s.azanCommand(ctx, client, target, args, threadID)
	case "ai":
		return true, s.aiCommand(ctx, client, target, msg, args, threadID)
	case "telegraph":
		return true, s.telegraphCommand(ctx, client, target, msg, args, threadID)
	case "mirror":
		return true, s.mirrorCommand(ctx, client, target, msg, args, threadID)
	case "quotly":
		return true, s.quotlyCommand(ctx, client, target, msg, threadID)
	case "strinfo":
		return true, s.strinfoCommand(ctx, client, target, msg, threadID)
	case "kang":
		return true, s.kangCommand(ctx, client, target, msg, args, threadID)
	case "startvc", "stopvc", "setvc", "joinvc", "leavevc", "play", "video", "ytmusic", "ytvideo":
		return true, s.vcCommand(ctx, client, target, msg, name, args, threadID)
	default:
		return false, nil
	}
}
