package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

type blacklistRecord struct {
	OwnerID   int64     `bson:"owner_id"`
	Type      string    `bson:"type"`
	ChatID    int64     `bson:"chat_id"`
	PeerKind  string    `bson:"peer_kind,omitempty"`
	Title     string    `bson:"title,omitempty"`
	Username  string    `bson:"username,omitempty"`
	CreatedAt time.Time `bson:"created_at,omitempty"`
}

type blacklistTargetInfo struct {
	CanonicalID int64
	LegacyID    int64
	Kind        string
	PeerKind    string
	Title       string
	Username    string
}

var blacklistFlags = map[string]string{"-pv": "private", "-gc": "group", "-ch": "channel"}

func canonicalBlacklistID(kind, peerKind string, rawID int64) int64 {
	if rawID == 0 {
		return 0
	}
	if rawID < 0 {
		s := strconv.FormatInt(rawID, 10)
		if strings.HasPrefix(s, "-100") {
			return rawID
		}
		if peerKind == "supergroup" || peerKind == "channel" || kind == "channel" {
			if parsed, err := strconv.ParseInt("-100"+strconv.FormatInt(-rawID, 10), 10, 64); err == nil {
				return parsed
			}
		}
		if kind == "private" {
			return -rawID
		}
		return rawID
	}
	if peerKind == "supergroup" || peerKind == "channel" || kind == "channel" {
		if parsed, err := strconv.ParseInt("-100"+strconv.FormatInt(rawID, 10), 10, 64); err == nil {
			return parsed
		}
	}
	if kind == "group" {
		return -rawID
	}
	return rawID
}

func inferBlacklistPeerKind(kind, peerKind string, id int64) string {
	if peerKind != "" {
		return peerKind
	}
	if kind == "channel" {
		return "channel"
	}
	if kind == "private" {
		return "private"
	}
	if id < 0 {
		if strings.HasPrefix(strconv.FormatInt(id, 10), "-100") {
			return "supergroup"
		}
		return "basic"
	}
	if id >= 1000000000 {
		return "supergroup"
	}
	return "basic"
}

func displayChatID(kind, peerKind string, id int64) int64 {
	canon := canonicalBlacklistID(kind, inferBlacklistPeerKind(kind, peerKind, id), id)
	if canon != 0 {
		return canon
	}
	return id
}

func normalizeBlacklistUsername(raw string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
	if trimmed == "" {
		return ""
	}
	return "@" + trimmed
}

func formatUserFullName(u *tg.User) string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" && u.Username != "" {
		return "@" + u.Username
	}
	return name
}

func (s *Service) blacklistTarget(
	ctx context.Context,
	client *gobot.Client,
	target tg.InputPeerClass,
	msg *tg.Message,
	entities tg.Entities,
	args string,
) (int64, string, string, error) {
	info, err := s.resolveBlacklistTarget(ctx, client, target, msg, entities, args)
	if err != nil {
		return 0, "", "", err
	}
	id := info.LegacyID
	if id == 0 {
		id = info.CanonicalID
	}
	return id, info.Kind, info.Title, nil
}

func parseBlacklistArgs(args string) (string, string) {
	flagKind := ""
	rest := make([]string, 0, 2)
	for _, part := range strings.Fields(args) {
		if kind, ok := blacklistFlags[strings.ToLower(part)]; ok {
			flagKind = kind
			continue
		}
		rest = append(rest, part)
	}
	rawTarget := ""
	if len(rest) > 0 {
		rawTarget = strings.TrimSpace(rest[0])
	}
	return flagKind, rawTarget
}

func (s *Service) resolveBlacklistTarget(
	ctx context.Context,
	client *gobot.Client,
	target tg.InputPeerClass,
	msg *tg.Message,
	entities tg.Entities,
	args string,
) (blacklistTargetInfo, error) {
	flagKind, rawTarget := parseBlacklistArgs(args)
	if rawTarget != "" {
		return s.resolveRemoteBlacklistTarget(ctx, client, entities, flagKind, rawTarget)
	}
	return s.resolveInPlaceBlacklistTarget(ctx, client, target, msg, entities, flagKind)
}

func (s *Service) resolveInPlaceBlacklistTarget(
	ctx context.Context,
	client *gobot.Client,
	target tg.InputPeerClass,
	msg *tg.Message,
	entities tg.Entities,
	flagKind string,
) (blacklistTargetInfo, error) {
	if msg == nil {
		return blacklistTargetInfo{}, fmt.Errorf("target blacklist tidak ditemukan")
	}

	if flagKind == "private" {
		if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && peerUser.UserID > 0 {
			title, uname := s.lookupUserMeta(ctx, client, entities, peerUser.UserID)
			return blacklistTargetInfo{
				CanonicalID: peerUser.UserID,
				LegacyID:    peerUser.UserID,
				Kind:        "private",
				PeerKind:    "private",
				Title:       title,
				Username:    uname,
			}, nil
		}
		if _, isCh := msg.PeerID.(*tg.PeerChannel); isCh {
			if ch, ok := entities.Channels[msg.PeerID.(*tg.PeerChannel).ChannelID]; ok && ch != nil && ch.Broadcast {
				return blacklistTargetInfo{}, fmt.Errorf("target blacklist -pv tidak berlaku di channel")
			}
		}
		if msg.ReplyTo != nil {
			reply, err := s.replyMessage(ctx, client, msg, target)
			if err != nil {
				return blacklistTargetInfo{}, fmt.Errorf("ambil pesan balasan: %w", err)
			}
			if reply != nil {
				var ownerID int64
				if client != nil {
					ownerID, _ = s.userbotOwner(ctx, client)
				}
				if uid, ok := extractUserIDFromMessage(reply, ownerID); ok && uid > 0 {
					title, uname := s.lookupUserMeta(ctx, client, entities, uid)
					return blacklistTargetInfo{
						CanonicalID: uid,
						LegacyID:    uid,
						Kind:        "private",
						PeerKind:    "private",
						Title:       title,
						Username:    uname,
					}, nil
				}
			}
		}
		return blacklistTargetInfo{}, fmt.Errorf("balas pesan pengguna atau sertakan target user/username saat memakai -pv")
	}

	switch peer := msg.PeerID.(type) {
	case *tg.PeerUser:
		if flagKind != "" && flagKind != "private" {
			return blacklistTargetInfo{}, fmt.Errorf("flag %s tidak sesuai untuk chat private", flagKind)
		}
		title, uname := s.lookupUserMeta(ctx, client, entities, peer.UserID)
		return blacklistTargetInfo{
			CanonicalID: peer.UserID,
			LegacyID:    peer.UserID,
			Kind:        "private",
			PeerKind:    "private",
			Title:       title,
			Username:    uname,
		}, nil

	case *tg.PeerChat:
		if flagKind != "" && flagKind != "group" {
			return blacklistTargetInfo{}, fmt.Errorf("chat ini adalah group dasar, bukan %s", flagKind)
		}
		title := s.lookupBasicChatTitle(ctx, client, entities, peer.ChatID)
		return blacklistTargetInfo{
			CanonicalID: -peer.ChatID,
			LegacyID:    peer.ChatID,
			Kind:        "group",
			PeerKind:    "basic",
			Title:       title,
		}, nil

	case *tg.PeerChannel:
		title, uname, isBroadcast := s.lookupChannelMeta(ctx, client, entities, peer.ChannelID)
		kind := "group"
		peerKind := "supergroup"
		if isBroadcast || flagKind == "channel" {
			if isBroadcast {
				kind = "channel"
				peerKind = "channel"
			} else if flagKind == "channel" {
				return blacklistTargetInfo{}, fmt.Errorf("chat ini adalah supergroup, gunakan -gc")
			}
		}
		if flagKind == "group" && isBroadcast {
			return blacklistTargetInfo{}, fmt.Errorf("chat ini adalah channel, gunakan -ch")
		}
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID(kind, peerKind, peer.ChannelID),
			LegacyID:    peer.ChannelID,
			Kind:        kind,
			PeerKind:    peerKind,
			Title:       title,
			Username:    uname,
		}, nil
	}

	return blacklistTargetInfo{}, fmt.Errorf("target blacklist menggunakan chat saat command dikirim (atau gunakan -pv untuk target pengguna)")
}
