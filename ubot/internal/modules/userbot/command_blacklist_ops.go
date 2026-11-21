package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (s *Service) blacklistCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args, operation string, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	if operation == "remove" {
		handled, remErr := s.tryRemoveBlacklistByIndex(ctx, client, target, ownerID, args, threadID)
		if handled {
			if remErr != nil {
				return sendUserText(ctx, client, target, remErr.Error(), threadID)
			}
			return nil
		}
	}

	info, err := s.resolveBlacklistTarget(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal: "+err.Error(), threadID)
	}

	if operation == "add" {
		if s.sqlite != nil {
			if info.LegacyID != 0 && info.LegacyID != info.CanonicalID {
				_ = s.sqlite.RemoveBlacklist(ctx, ownerID, info.Kind, info.LegacyID)
			}
			err = s.sqlite.UpsertBlacklist(ctx, databases.BlacklistRecord{
				OwnerID:  ownerID,
				ChatType: info.Kind,
				ChatID:   info.CanonicalID,
				PeerKind: info.PeerKind,
				Title:    info.Title,
				Username: info.Username,
			})
			if err != nil {
				return sendUserText(ctx, client, target, "Gagal menambah blacklist: "+err.Error(), threadID)
			}
		}
		if s.db != nil {
			go func(oid int64, item blacklistTargetInfo) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if item.LegacyID != 0 && item.LegacyID != item.CanonicalID {
					_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteMany(bgCtx, bson.M{
						"owner_id": oid,
						"type":     item.Kind,
						"chat_id":  item.LegacyID,
					})
				}
				_, _ = s.db.Collection("userbot_broadcast_blacklist").UpdateOne(
					bgCtx,
					bson.M{"owner_id": oid, "type": item.Kind, "chat_id": item.CanonicalID},
					bson.M{"$set": bson.M{
						"owner_id":   oid,
						"type":       item.Kind,
						"chat_id":    item.CanonicalID,
						"peer_kind":  item.PeerKind,
						"title":      item.Title,
						"username":   item.Username,
						"created_at": time.Now(),
					}},
					options.Update().SetUpsert(true),
				)
			}(ownerID, info)
		}
		label := formatBlacklistEntryLabel(info.Kind, info.Title, info.Username, info.CanonicalID)
		return sendUserText(ctx, client, target, fmt.Sprintf("Berhasil menambah <code>%s</code> (%s | <code>%d</code>) ke blacklist broadcast.", html.EscapeString(label), strings.ToUpper(info.Kind), info.CanonicalID), threadID)
	}

	idCandidates := uniqueNonZeroIDs(info.CanonicalID, info.LegacyID, -info.LegacyID)
	if s.sqlite != nil {
		if err := s.sqlite.RemoveBlacklistAny(ctx, ownerID, info.Kind, idCandidates); err != nil {
			return sendUserText(ctx, client, target, "Gagal menghapus blacklist: "+err.Error(), threadID)
		}
	}
	if s.db != nil {
		go func(oid int64, k string, ids []int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteMany(bgCtx, bson.M{
				"owner_id": oid,
				"type":     k,
				"chat_id":  bson.M{"$in": ids},
			})
		}(ownerID, info.Kind, idCandidates)
	}
	label := formatBlacklistEntryLabel(info.Kind, info.Title, info.Username, info.CanonicalID)
	return sendUserText(ctx, client, target, fmt.Sprintf("Berhasil menghapus <code>%s</code> (%s | <code>%d</code>) dari blacklist broadcast.", html.EscapeString(label), strings.ToUpper(info.Kind), info.CanonicalID), threadID)
}

func (s *Service) tryRemoveBlacklistByIndex(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, ownerID int64, args string, threadID int) (bool, error) {
	flagKind, rawTarget := parseBlacklistArgs(args)
	if flagKind == "" || rawTarget == "" {
		return false, nil
	}
	idx, err := strconv.Atoi(rawTarget)
	if err != nil || idx <= 0 {
		return false, nil
	}

	sections, loadErr := s.loadGroupedBlacklist(ctx, client, ownerID)
	if loadErr != nil {
		return true, fmt.Errorf("Gagal memuat daftar blacklist: %v", loadErr)
	}

	var list []blacklistRecord
	sectionTitle := "GROUP"
	switch flagKind {
	case "group":
		list = sections.groups
		sectionTitle = "GROUP"
	case "private":
		list = sections.privates
		sectionTitle = "PRIVATE"
	case "channel":
		list = sections.channels
		sectionTitle = "CHANNEL"
	}

	if flagKind == "private" && idx > len(list) && idx > 1000 {
		return false, nil
	}
	if idx < 1 || idx > len(list) {
		return true, fmt.Errorf("Nomor %d pada kategori %s tidak ditemukan (total %d). Cek !lsbl.", idx, sectionTitle, len(list))
	}

	item := list[idx-1]
	rawID := normalizePeerChannelID(item.ChatID)
	candidates := uniqueNonZeroIDs(item.ChatID, rawID, -rawID, canonicalBlacklistID(item.Type, item.PeerKind, rawID))
	if s.sqlite != nil {
		if err := s.sqlite.RemoveBlacklistAny(ctx, ownerID, item.Type, candidates); err != nil {
			return true, fmt.Errorf("Gagal menghapus blacklist: %v", err)
		}
	}
	if s.db != nil {
		go func(oid int64, k string, ids []int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteMany(bgCtx, bson.M{
				"owner_id": oid,
				"type":     k,
				"chat_id":  bson.M{"$in": ids},
			})
		}(ownerID, item.Type, candidates)
	}

	dispID := displayChatID(item.Type, item.PeerKind, item.ChatID)
	label := formatBlacklistEntryLabel(item.Type, item.Title, item.Username, dispID)
	msgText := fmt.Sprintf("Berhasil menghapus [%s #%d] <code>%s</code> (<code>%d</code>) dari blacklist broadcast.", sectionTitle, idx, html.EscapeString(label), dispID)
	return true, sendUserText(ctx, client, target, msgText, threadID)
}

type groupedBlacklist struct {
	groups   []blacklistRecord
	privates []blacklistRecord
	channels []blacklistRecord
}

func (s *Service) loadGroupedBlacklist(ctx context.Context, client *gobot.Client, ownerID int64) (groupedBlacklist, error) {
	var all []blacklistRecord
	if s.sqlite != nil {
		rows, err := s.sqlite.ListBlacklist(ctx, ownerID)
		if err != nil {
			return groupedBlacklist{}, err
		}
		for _, r := range rows {
			all = append(all, blacklistRecord{
				OwnerID:   r.OwnerID,
				Type:      r.ChatType,
				ChatID:    r.ChatID,
				PeerKind:  r.PeerKind,
				Title:     r.Title,
				Username:  r.Username,
				CreatedAt: r.CreatedAt,
			})
		}
	} else if s.db != nil {
		cur, err := s.db.Collection("userbot_broadcast_blacklist").Find(
			ctx,
			bson.M{"owner_id": ownerID},
			options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}),
		)
		if err != nil {
			return groupedBlacklist{}, err
		}
		defer cur.Close(ctx)
		if err := cur.All(ctx, &all); err != nil {
			return groupedBlacklist{}, err
		}
		sort.SliceStable(all, func(i, j int) bool {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		})
	}

	s.hydrateBlacklistMetadata(ctx, client, ownerID, all)

	var grouped groupedBlacklist
	for _, r := range all {
		switch r.Type {
		case "private":
			grouped.privates = append(grouped.privates, r)
		case "channel":
			grouped.channels = append(grouped.channels, r)
		default:
			grouped.groups = append(grouped.groups, r)
		}
	}
	return grouped, nil
}

func formatBlacklistListHTML(grouped groupedBlacklist) string {
	var b strings.Builder
	b.WriteString("<b>List Blacklist:</b>\n")

	renderSection := func(header string, items []blacklistRecord) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n<b>%s</b>\n<blockquote>", header)
		for i, item := range items {
			dispID := displayChatID(item.Type, item.PeerKind, item.ChatID)
			label := formatBlacklistEntryLabel(item.Type, item.Title, item.Username, dispID)
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%d. <code>%d</code> | %s", i+1, dispID, html.EscapeString(label))
		}
		b.WriteString("</blockquote>")
	}

	renderSection("GROUP", grouped.groups)
	renderSection("PRIVATE", grouped.privates)
	renderSection("CHANNEL", grouped.channels)
	b.WriteString("\n\n<i>Hapus via nomor:</i> <code>!rmbl -gc|-pv|-ch &lt;no&gt;</code>")
	return b.String()
}

func (s *Service) listBlacklist(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	grouped, err := s.loadGroupedBlacklist(ctx, client, ownerID)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengambil blacklist.", threadID)
	}

	if len(grouped.groups) == 0 && len(grouped.privates) == 0 && len(grouped.channels) == 0 {
		return sendUserText(ctx, client, target, "Daftar blacklist broadcast masih kosong.", threadID)
	}

	return sendUserText(ctx, client, target, formatBlacklistListHTML(grouped), threadID)
}

func (s *Service) clearBlacklist(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	if s.sqlite != nil {
		if err := s.sqlite.ClearBlacklist(ctx, ownerID); err != nil {
			return sendUserText(ctx, client, target, "Gagal menghapus seluruh blacklist.", threadID)
		}
	}
	if s.db != nil {
		go func(oid int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_broadcast_blacklist").DeleteMany(bgCtx, bson.M{"owner_id": oid})
		}(ownerID)
	}
	return sendUserText(ctx, client, target, "Semua blacklist broadcast dihapus.", threadID)
}

func formatBlacklistEntryLabel(kind, title, username string, fallbackID int64) string {
	raw := strings.TrimSpace(title)
	if kind == "private" {
		if uname := strings.TrimSpace(username); uname != "" {
			raw = "@" + strings.TrimPrefix(uname, "@")
		}
	}
	if raw == "" {
		raw = strconv.FormatInt(fallbackID, 10)
	}
	return truncateBlacklistLabel(raw, 10)
}

func truncateBlacklistLabel(raw string, maxRunes int) string {
	trimmed := strings.TrimSpace(raw)
	if maxRunes <= 0 || utf8.RuneCountInString(trimmed) <= maxRunes {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxRunes])
}

func uniqueNonZeroIDs(ids ...int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
