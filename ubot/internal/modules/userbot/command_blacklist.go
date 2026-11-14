package userbot

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type blacklistRecord struct {
	OwnerID int64  `bson:"owner_id"`
	Type    string `bson:"type"`
	ChatID  int64  `bson:"chat_id"`
	Title   string `bson:"title,omitempty"`
}

var blacklistFlags = map[string]string{"-pv": "private", "-gc": "group", "-ch": "channel"}

func blacklistTarget(msg *tg.Message, entities tg.Entities, args string) (int64, string, string, error) {
	parts := strings.Fields(args)
	kind := ""
	for _, part := range parts {
		if value, ok := blacklistFlags[strings.ToLower(part)]; ok {
			if kind != "" && kind != value {
				return 0, "", "", fmt.Errorf("gunakan maksimal satu flag")
			}
			kind = value
		} else {
			return 0, "", "", fmt.Errorf("target blacklist menggunakan chat saat command dikirim")
		}
	}
	peer, err := extractPeer(entities, msg.PeerID)
	if err != nil {
		return 0, "", "", err
	}
	title := ""
	switch value := peer.(type) {
	case *tg.InputPeerUser:
		if kind != "" && kind != "private" {
			return 0, "", "", fmt.Errorf("flag tidak cocok dengan chat ini")
		}
		kind = "private"
		if user := entities.Users[value.UserID]; user != nil {
			title = strings.TrimSpace(user.FirstName + " " + user.LastName)
		}
		return value.UserID, kind, title, nil
	case *tg.InputPeerChat:
		if kind != "" && kind != "group" {
			return 0, "", "", fmt.Errorf("flag tidak cocok dengan chat ini")
		}
		kind = "group"
		if chat := entities.Chats[value.ChatID]; chat != nil {
			title = chat.Title
		}
		return value.ChatID, kind, title, nil
	case *tg.InputPeerChannel:
		// Telegram represents supergroups as channels with Megagroup=true.
		// Keep their blacklist type aligned with broadcastTargets (-gc).
		storedKind := "channel"
		if channel := entities.Channels[value.ChannelID]; channel != nil && channel.Megagroup {
			storedKind = "group"
		}
		if kind != "" && kind != storedKind {
			return 0, "", "", fmt.Errorf("flag tidak cocok dengan chat ini")
		}
		kind = storedKind
		if channel := entities.Channels[value.ChannelID]; channel != nil {
			title = channel.Title
		}
		return value.ChannelID, kind, title, nil
	default:
		return 0, "", "", fmt.Errorf("jenis chat tidak didukung")
	}
}

func extractPeer(entities tg.Entities, value tg.PeerClass) (tg.InputPeerClass, error) {
	return entitiesFrom(entities).ExtractPeer(value)
}

// entitiesFrom is kept local so command files do not depend on update-only
// helpers in the message dispatcher.
func entitiesFrom(entities tg.Entities) interface {
	ExtractPeer(tg.PeerClass) (tg.InputPeerClass, error)
} {
	return peerEntities{entities: entities}
}

type peerEntities struct{ entities tg.Entities }

func (p peerEntities) ExtractPeer(value tg.PeerClass) (tg.InputPeerClass, error) {
	switch peer := value.(type) {
	case *tg.PeerUser:
		user := p.entities.Users[peer.UserID]
		if user == nil {
			return &tg.InputPeerUser{UserID: peer.UserID}, nil
		}
		return &tg.InputPeerUser{UserID: peer.UserID, AccessHash: user.AccessHash}, nil
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: peer.ChatID}, nil
	case *tg.PeerChannel:
		channel := p.entities.Channels[peer.ChannelID]
		if channel == nil {
			return nil, fmt.Errorf("channel tidak ada di entities")
		}
		return &tg.InputPeerChannel{ChannelID: peer.ChannelID, AccessHash: channel.AccessHash}, nil
	default:
		return nil, fmt.Errorf("peer tidak dikenal")
	}
}

func (s *Service) blacklistCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args, operation string) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	chatID, kind, title, err := blacklistTarget(msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, "Format blacklist: !addbl [-pv|-gc|-ch] di chat target.")
	}
	switch operation {
	case "add":
		if s.sqlite != nil {
			err = s.sqlite.AddBlacklist(ctx, ownerID, kind, chatID)
		}
		if s.db != nil {
			go func(oid int64, k string, cid int64, t string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				filter := bson.M{"owner_id": oid, "type": k, "chat_id": cid}
				_, _ = s.db.Collection("userbot_broadcast_blacklist").UpdateOne(bgCtx, filter, bson.M{"$set": bson.M{"owner_id": oid, "type": k, "chat_id": cid, "title": t}}, options.Update().SetUpsert(true))
			}(ownerID, kind, chatID, title)
		}
		if err != nil {
			return err
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("Blacklist %s ditambahkan: %d", kind, displayChatID(kind, chatID)))
	case "remove":
		if s.sqlite != nil {
			err = s.sqlite.RemoveBlacklist(ctx, ownerID, kind, chatID)
		}
		if s.db != nil {
			go func(oid int64, k string, cid int64) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteOne(bgCtx, bson.M{"owner_id": oid, "type": k, "chat_id": cid})
			}(ownerID, kind, chatID)
		}
		if err != nil {
			return err
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("Blacklist %s dihapus: %d", kind, displayChatID(kind, chatID)))
	default:
		return fmt.Errorf("operasi blacklist tidak dikenal")
	}
}

func (s *Service) listBlacklist(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	var entries []blacklistRecord
	if s.sqlite != nil {
		bls, err := s.sqlite.ListBlacklist(ctx, ownerID)
		if err != nil {
			return err
		}
		for _, b := range bls {
			entries = append(entries, blacklistRecord{OwnerID: b.OwnerID, Type: b.ChatType, ChatID: b.ChatID})
		}
	} else if s.db != nil {
		cur, err := s.db.Collection("userbot_broadcast_blacklist").Find(ctx, bson.M{"owner_id": ownerID})
		if err != nil {
			return err
		}
		defer cur.Close(ctx)
		if err := cur.All(ctx, &entries); err != nil {
			return err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ChatID < entries[j].ChatID })
	if len(entries) == 0 {
		return sendUserText(ctx, client, target, "Blacklist masih kosong.")
	}
	lines := []string{"Blacklist broadcast:"}
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("- %s: %d | %s", strings.ToUpper(entry.Type), displayChatID(entry.Type, entry.ChatID), html.EscapeString(entry.Title)))
	}
	return sendUserText(ctx, client, target, strings.Join(lines, "\n"))
}

// Chat IDs in Mongo stay in MTProto form (positive channel/chat IDs) so they
// match InputPeer values used by the userbot. This helper only formats IDs in
// responses using the familiar Bot API representation.
func displayChatID(kind string, id int64) int64 {
	switch kind {
	case "group":
		return -id
	case "channel":
		return -1000000000000 - id
	default:
		return id
	}
}

func (s *Service) clearBlacklist(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if s.sqlite != nil {
		err = s.sqlite.ClearBlacklist(ctx, ownerID)
	}
	if s.db != nil {
		go func(oid int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteMany(bgCtx, bson.M{"owner_id": oid})
		}(ownerID)
	}
	if err != nil {
		return err
	}
	return sendUserText(ctx, client, target, "Semua blacklist broadcast dihapus.")
}
