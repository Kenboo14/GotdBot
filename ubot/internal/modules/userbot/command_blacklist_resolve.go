package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) resolveRemoteBlacklistTarget(
	ctx context.Context,
	client *gobot.Client,
	entities tg.Entities,
	flagKind string,
	raw string,
) (blacklistTargetInfo, error) {
	cleaned := strings.TrimSpace(raw)
	if chSlugID, ok := extractTelegramCChannelID(cleaned); ok {
		cleaned = strconv.FormatInt(chSlugID, 10)
	}

	if idVal, err := strconv.ParseInt(cleaned, 10, 64); err == nil && idVal != 0 {
		return s.resolveNumericBlacklistTarget(ctx, client, entities, flagKind, cleaned, idVal)
	}

	inviteHash, uname := extractJoinTarget(cleaned)
	if inviteHash != "" {
		return s.resolveInviteBlacklistTarget(ctx, client, inviteHash, flagKind)
	}
	if uname == "" {
		uname = strings.TrimPrefix(cleaned, "@")
	}
	if uname == "" {
		return blacklistTargetInfo{}, fmt.Errorf("target %q tidak valid", raw)
	}

	if u := findEntityUserByUsername(entities, uname); u != nil {
		if flagKind != "" && flagKind != "private" {
			return blacklistTargetInfo{}, fmt.Errorf("@%s adalah akun private, gunakan -pv", uname)
		}
		return blacklistTargetInfo{
			CanonicalID: u.ID,
			LegacyID:    u.ID,
			Kind:        "private",
			PeerKind:    "private",
			Title:       formatUserFullName(u),
			Username:    normalizeBlacklistUsername(u.Username),
		}, nil
	}

	if client == nil {
		return blacklistTargetInfo{}, fmt.Errorf("client userbot tidak aktif untuk resolve @%s", uname)
	}
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: uname})
	if err != nil {
		return blacklistTargetInfo{}, fmt.Errorf("gagal resolve @%s: %w", uname, err)
	}
	s.peerCache().ApplyUsersChats(resolved.Users, resolved.Chats)

	switch p := resolved.Peer.(type) {
	case *tg.PeerUser:
		if flagKind != "" && flagKind != "private" {
			return blacklistTargetInfo{}, fmt.Errorf("@%s adalah akun private, gunakan -pv", uname)
		}
		var u *tg.User
		for _, item := range resolved.Users {
			if cand, ok := item.(*tg.User); ok && cand.ID == p.UserID {
				u = cand
				break
			}
		}
		title := "@" + uname
		username := "@" + uname
		if u != nil {
			if n := formatUserFullName(u); n != "" {
				title = n
			}
			if u.Username != "" {
				username = normalizeBlacklistUsername(u.Username)
			}
		}
		return blacklistTargetInfo{
			CanonicalID: p.UserID,
			LegacyID:    p.UserID,
			Kind:        "private",
			PeerKind:    "private",
			Title:       title,
			Username:    username,
		}, nil

	case *tg.PeerChat:
		if flagKind != "" && flagKind != "group" {
			return blacklistTargetInfo{}, fmt.Errorf("@%s adalah group, gunakan -gc", uname)
		}
		title := extractChatTitleFromSlice(resolved.Chats, p.ChatID)
		return blacklistTargetInfo{
			CanonicalID: -p.ChatID,
			LegacyID:    p.ChatID,
			Kind:        "group",
			PeerKind:    "basic",
			Title:       title,
		}, nil

	case *tg.PeerChannel:
		ch := extractChannelFromSlice(resolved.Chats, p.ChannelID)
		isBroadcast := ch != nil && ch.Broadcast
		kind := "group"
		peerKind := "supergroup"
		if isBroadcast {
			kind = "channel"
			peerKind = "channel"
		}
		if flagKind != "" && flagKind != kind {
			return blacklistTargetInfo{}, fmt.Errorf("@%s berjenis %s, bukan %s", uname, kind, flagKind)
		}
		title := ""
		username := "@" + uname
		if ch != nil {
			title = strings.TrimSpace(ch.Title)
			if ch.Username != "" {
				username = normalizeBlacklistUsername(ch.Username)
			}
		}
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID(kind, peerKind, p.ChannelID),
			LegacyID:    p.ChannelID,
			Kind:        kind,
			PeerKind:    peerKind,
			Title:       title,
			Username:    username,
		}, nil
	}

	return blacklistTargetInfo{}, fmt.Errorf("target @%s tidak ditemukan", uname)
}

func (s *Service) resolveNumericBlacklistTarget(
	ctx context.Context,
	client *gobot.Client,
	entities tg.Entities,
	flagKind string,
	rawText string,
	idVal int64,
) (blacklistTargetInfo, error) {
	if strings.HasPrefix(rawText, "-100") {
		rawCh := normalizePeerChannelID(idVal)
		title, uname, isBroadcast := s.lookupChannelMeta(ctx, client, entities, rawCh)
		kind := "group"
		peerKind := "supergroup"
		if flagKind == "channel" || (flagKind == "" && isBroadcast) {
			kind = "channel"
			peerKind = "channel"
		}
		if flagKind == "private" {
			return blacklistTargetInfo{}, fmt.Errorf("ID %s adalah channel/supergroup, bukan private user", rawText)
		}
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID(kind, peerKind, rawCh),
			LegacyID:    rawCh,
			Kind:        kind,
			PeerKind:    peerKind,
			Title:       title,
			Username:    uname,
		}, nil
	}

	if idVal < 0 {
		rawChat := -idVal
		if flagKind == "private" || flagKind == "channel" {
			return blacklistTargetInfo{}, fmt.Errorf("ID %s adalah group dasar", rawText)
		}
		title := s.lookupBasicChatTitle(ctx, client, entities, rawChat)
		return blacklistTargetInfo{
			CanonicalID: -rawChat,
			LegacyID:    rawChat,
			Kind:        "group",
			PeerKind:    "basic",
			Title:       title,
		}, nil
	}

	if flagKind == "private" {
		title, uname := s.lookupUserMeta(ctx, client, entities, idVal)
		return blacklistTargetInfo{
			CanonicalID: idVal,
			LegacyID:    idVal,
			Kind:        "private",
			PeerKind:    "private",
			Title:       title,
			Username:    uname,
		}, nil
	}

	if flagKind == "channel" {
		title, uname, _ := s.lookupChannelMeta(ctx, client, entities, idVal)
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID("channel", "channel", idVal),
			LegacyID:    idVal,
			Kind:        "channel",
			PeerKind:    "channel",
			Title:       title,
			Username:    uname,
		}, nil
	}

	if flagKind == "group" {
		if title, uname, isBroadcast, found := s.tryFindChannelInDialogs(ctx, client, entities, idVal); found && !isBroadcast {
			return blacklistTargetInfo{
				CanonicalID: canonicalBlacklistID("group", "supergroup", idVal),
				LegacyID:    idVal,
				Kind:        "group",
				PeerKind:    "supergroup",
				Title:       title,
				Username:    uname,
			}, nil
		}
		title := s.lookupBasicChatTitle(ctx, client, entities, idVal)
		return blacklistTargetInfo{
			CanonicalID: -idVal,
			LegacyID:    idVal,
			Kind:        "group",
			PeerKind:    "basic",
			Title:       title,
		}, nil
	}

	if u, ok := entities.Users[idVal]; ok && u != nil {
		return blacklistTargetInfo{
			CanonicalID: idVal,
			LegacyID:    idVal,
			Kind:        "private",
			PeerKind:    "private",
			Title:       formatUserFullName(u),
			Username:    normalizeBlacklistUsername(u.Username),
		}, nil
	}

	if title, uname, isBroadcast, found := s.tryFindChannelInDialogs(ctx, client, entities, idVal); found {
		kind := "group"
		peerKind := "supergroup"
		if isBroadcast {
			kind = "channel"
			peerKind = "channel"
		}
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID(kind, peerKind, idVal),
			LegacyID:    idVal,
			Kind:        kind,
			PeerKind:    peerKind,
			Title:       title,
			Username:    uname,
		}, nil
	}

	title, uname := s.lookupUserMeta(ctx, client, entities, idVal)
	return blacklistTargetInfo{
		CanonicalID: idVal,
		LegacyID:    idVal,
		Kind:        "private",
		PeerKind:    "private",
		Title:       title,
		Username:    uname,
	}, nil
}

func (s *Service) resolveInviteBlacklistTarget(ctx context.Context, client *gobot.Client, hash, flagKind string) (blacklistTargetInfo, error) {
	if client == nil {
		return blacklistTargetInfo{}, fmt.Errorf("client userbot tidak aktif untuk memeriksa invite link")
	}
	inv, err := client.API().MessagesCheckChatInvite(ctx, hash)
	if err != nil {
		return blacklistTargetInfo{}, fmt.Errorf("gagal memeriksa tautan invite: %w", err)
	}
	var chatObj tg.ChatClass
	switch v := inv.(type) {
	case *tg.ChatInviteAlready:
		chatObj = v.Chat
	case *tg.ChatInvitePeek:
		chatObj = v.Chat
	case *tg.ChatInvite:
		return blacklistTargetInfo{}, fmt.Errorf("userbot belum bergabung ke %q (invite privat tanpa chat ID)", v.Title)
	}
	switch c := chatObj.(type) {
	case *tg.Channel:
		kind := "group"
		peerKind := "supergroup"
		if c.Broadcast {
			kind = "channel"
			peerKind = "channel"
		}
		if flagKind != "" && flagKind != kind {
			return blacklistTargetInfo{}, fmt.Errorf("tautan adalah %s, bukan %s", kind, flagKind)
		}
		return blacklistTargetInfo{
			CanonicalID: canonicalBlacklistID(kind, peerKind, c.ID),
			LegacyID:    c.ID,
			Kind:        kind,
			PeerKind:    peerKind,
			Title:       strings.TrimSpace(c.Title),
			Username:    normalizeBlacklistUsername(c.Username),
		}, nil
	case *tg.Chat:
		if flagKind != "" && flagKind != "group" {
			return blacklistTargetInfo{}, fmt.Errorf("tautan adalah group, bukan %s", flagKind)
		}
		return blacklistTargetInfo{
			CanonicalID: -c.ID,
			LegacyID:    c.ID,
			Kind:        "group",
			PeerKind:    "basic",
			Title:       strings.TrimSpace(c.Title),
		}, nil
	}
	return blacklistTargetInfo{}, fmt.Errorf("tautan invite tidak menghasilkan chat yang valid")
}
