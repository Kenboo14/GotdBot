package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"strconv"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

func (s *Service) hydrateBlacklistMetadata(ctx context.Context, client *gobot.Client, ownerID int64, items []blacklistRecord) {
	if client == nil || len(items) == 0 {
		return
	}
	missing := false
	for _, item := range items {
		if item.Title == "" && item.Username == "" {
			missing = true
			break
		}
	}
	if !missing {
		return
	}

	needGroups := map[int64]bool{}
	needPrivates := map[int64]bool{}
	for _, item := range items {
		if item.Title != "" || item.Username != "" {
			continue
		}
		raw := normalizePeerChannelID(item.ChatID)
		if item.Type == "private" {
			needPrivates[raw] = true
		} else {
			needGroups[raw] = true
		}
	}

	chatTitles := map[int64]string{}
	chatUsernames := map[int64]string{}
	chatKinds := map[int64]string{}
	userTitles := map[int64]string{}
	userUsernames := map[int64]string{}

	scanCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(scanCtx, func(_ context.Context, elem dialogs.Elem) error {
		for _, ch := range elem.Entities.Channels() {
			if needGroups[ch.ID] {
				chatTitles[ch.ID] = strings.TrimSpace(ch.Title)
				chatUsernames[ch.ID] = normalizeBlacklistUsername(ch.Username)
				if ch.Broadcast {
					chatKinds[ch.ID] = "channel"
				} else {
					chatKinds[ch.ID] = "supergroup"
				}
			}
		}
		for _, c := range elem.Entities.Chats() {
			if needGroups[c.ID] {
				chatTitles[c.ID] = strings.TrimSpace(c.Title)
				chatKinds[c.ID] = "basic"
			}
		}
		for _, u := range elem.Entities.Users() {
			if needPrivates[u.ID] {
				userTitles[u.ID] = formatUserFullName(u)
				userUsernames[u.ID] = normalizeBlacklistUsername(u.Username)
			}
		}
		return nil
	})

	for i := range items {
		if items[i].Title != "" || items[i].Username != "" {
			continue
		}
		raw := normalizePeerChannelID(items[i].ChatID)
		changed := false
		if items[i].Type == "private" {
			if t := userTitles[raw]; t != "" {
				items[i].Title = t
				changed = true
			}
			if u := userUsernames[raw]; u != "" {
				items[i].Username = u
				changed = true
			}
			if items[i].PeerKind == "" {
				items[i].PeerKind = "private"
				changed = true
			}
		} else {
			if t := chatTitles[raw]; t != "" {
				items[i].Title = t
				changed = true
			}
			if u := chatUsernames[raw]; u != "" {
				items[i].Username = u
				changed = true
			}
			if k := chatKinds[raw]; k != "" {
				items[i].PeerKind = k
				changed = true
			}
		}
		if changed && s.sqlite != nil {
			_ = s.sqlite.UpsertBlacklist(ctx, databases.BlacklistRecord{
				OwnerID:  ownerID,
				ChatType: items[i].Type,
				ChatID:   items[i].ChatID,
				PeerKind: items[i].PeerKind,
				Title:    items[i].Title,
				Username: items[i].Username,
			})
		}
	}
}

func extractTelegramCChannelID(raw string) (int64, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	idx := strings.Index(lower, "t.me/c/")
	if idx == -1 {
		return 0, false
	}
	rest := strings.TrimSpace(raw[idx+len("t.me/c/"):])
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, false
	}
	chID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || chID <= 0 {
		return 0, false
	}
	canon, err := strconv.ParseInt("-100"+strconv.FormatInt(chID, 10), 10, 64)
	if err != nil {
		return 0, false
	}
	return canon, true
}

func findEntityUserByUsername(entities tg.Entities, uname string) *tg.User {
	for _, u := range entities.Users {
		if u != nil && u.Username != "" && strings.EqualFold(u.Username, uname) {
			return u
		}
	}
	return nil
}

func extractChatTitleFromSlice(chats []tg.ChatClass, targetID int64) string {
	for _, c := range chats {
		if ch, ok := c.(*tg.Chat); ok && ch.ID == targetID {
			return strings.TrimSpace(ch.Title)
		}
	}
	return ""
}

func extractChannelFromSlice(chats []tg.ChatClass, targetID int64) *tg.Channel {
	for _, c := range chats {
		if ch, ok := c.(*tg.Channel); ok && ch.ID == targetID {
			return ch
		}
	}
	return nil
}

func (s *Service) lookupUserMeta(ctx context.Context, client *gobot.Client, entities tg.Entities, userID int64) (string, string) {
	if u, ok := entities.Users[userID]; ok && u != nil {
		return formatUserFullName(u), normalizeBlacklistUsername(u.Username)
	}
	if client != nil {
		if users, err := client.API().UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: userID}}); err == nil {
			for _, uc := range users {
				if u, ok := uc.(*tg.User); ok && u.ID == userID {
					return formatUserFullName(u), normalizeBlacklistUsername(u.Username)
				}
			}
		}
	}
	return "", ""
}

func (s *Service) lookupBasicChatTitle(ctx context.Context, client *gobot.Client, entities tg.Entities, chatID int64) string {
	if c, ok := entities.Chats[chatID]; ok && c != nil && strings.TrimSpace(c.Title) != "" {
		return strings.TrimSpace(c.Title)
	}
	if client != nil {
		if chatsRes, err := client.API().MessagesGetChats(ctx, []int64{chatID}); err == nil && chatsRes != nil {
			if t := extractChatTitleFromSlice(chatsRes.GetChats(), chatID); t != "" {
				return t
			}
		}
	}
	return ""
}

func (s *Service) lookupChannelMeta(ctx context.Context, client *gobot.Client, entities tg.Entities, channelID int64) (string, string, bool) {
	if ch, ok := entities.Channels[channelID]; ok && ch != nil {
		return strings.TrimSpace(ch.Title), normalizeBlacklistUsername(ch.Username), ch.Broadcast
	}
	if title, uname, isBroadcast, found := s.tryFindChannelInDialogs(ctx, client, entities, channelID); found {
		return title, uname, isBroadcast
	}
	return "", "", false
}

func (s *Service) tryFindChannelInDialogs(ctx context.Context, client *gobot.Client, entities tg.Entities, channelID int64) (string, string, bool, bool) {
	if ch, ok := entities.Channels[channelID]; ok && ch != nil {
		return strings.TrimSpace(ch.Title), normalizeBlacklistUsername(ch.Username), ch.Broadcast, true
	}
	if client == nil {
		return "", "", false, false
	}
	if hash, ok := s.peerCache().ChannelHash(channelID); ok && hash != 0 {
		if res, err := client.API().ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: channelID, AccessHash: hash}}); err == nil && res != nil {
			if ch := extractChannelFromSlice(res.GetChats(), channelID); ch != nil {
				return strings.TrimSpace(ch.Title), normalizeBlacklistUsername(ch.Username), ch.Broadcast, true
			}
		}
	}
	scanCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var title, uname string
	var isBroadcast, found bool
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(scanCtx, func(_ context.Context, elem dialogs.Elem) error {
		for _, ch := range elem.Entities.Channels() {
			if ch.ID == channelID {
				title = strings.TrimSpace(ch.Title)
				uname = normalizeBlacklistUsername(ch.Username)
				isBroadcast = ch.Broadcast
				found = true
				return context.Canceled
			}
		}
		return nil
	})
	return title, uname, isBroadcast, found
}
