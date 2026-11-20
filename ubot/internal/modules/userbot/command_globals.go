package userbot

import (
	"context"
	"fmt"
	"strings"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

func (s *Service) gbanCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, remove bool, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	if uid == ownerID {
		return sendUserText(ctx, client, target, "Tidak dapat melakukan gban pada pemilik akun sendiri.", threadID)
	}
	if s.sqlite != nil {
		if remove {
			_ = s.sqlite.RemoveGlobalMod(ctx, ownerID, "gban", uid)
		} else {
			_ = s.sqlite.AddGlobalMod(ctx, ownerID, "gban", uid, label)
		}
	}
	partPeer := s.targetParticipantPeer(uid)
	count := 0
	scanned := 0
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		if scanned >= 150 {
			return fmt.Errorf("limit")
		}
		scanned++
		switch p := item.Peer.(type) {
		case *tg.InputPeerChannel:
			rights := tg.ChatBannedRights{}
			if !remove {
				rights = tg.ChatBannedRights{ViewMessages: true, SendMessages: true, SendMedia: true}
			}
			if _, opErr := client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
				Channel:      &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				Participant:  partPeer,
				BannedRights: rights,
			}); opErr == nil {
				count++
			}
		case *tg.InputPeerChat:
			if !remove {
				if _, opErr := client.API().MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{
					ChatID: p.ChatID,
					UserID: inputUser,
				}); opErr == nil {
					count++
				}
			}
		}
		return nil
	})
	if remove {
		return sendUserText(ctx, client, target, fmt.Sprintf("Global Unban selesai untuk %s pada %d chat.", label, count), threadID)
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Global Ban selesai untuk %s pada %d chat.", label, count), threadID)
}

func (s *Service) gmuteCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, remove bool, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	uid, label, _, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	if uid == ownerID {
		return sendUserText(ctx, client, target, "Tidak dapat melakukan gmute pada pemilik akun sendiri.", threadID)
	}
	if s.sqlite != nil {
		if remove {
			_ = s.sqlite.RemoveGlobalMod(ctx, ownerID, "gmute", uid)
		} else {
			_ = s.sqlite.AddGlobalMod(ctx, ownerID, "gmute", uid, label)
		}
	}
	partPeer := s.targetParticipantPeer(uid)
	count := 0
	scanned := 0
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		if scanned >= 150 {
			return fmt.Errorf("limit")
		}
		scanned++
		ch, ok := item.Peer.(*tg.InputPeerChannel)
		if !ok {
			return nil
		}
		rights := tg.ChatBannedRights{}
		if !remove {
			rights = tg.ChatBannedRights{
				SendMessages: true,
				SendMedia:    true,
				SendStickers: true,
				SendGifs:     true,
				SendGames:    true,
				SendInline:   true,
			}
		}
		if _, opErr := client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
			Channel:      &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			Participant:  partPeer,
			BannedRights: rights,
		}); opErr == nil {
			count++
		}
		return nil
	})
	if remove {
		return sendUserText(ctx, client, target, fmt.Sprintf("Global Unmute selesai untuk %s pada %d grup.", label, count), threadID)
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Global Mute selesai untuk %s pada %d grup.", label, count), threadID)
}

func (s *Service) gkickCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	if uid == ownerID {
		return sendUserText(ctx, client, target, "Tidak dapat melakukan gkick pada pemilik akun sendiri.", threadID)
	}
	partPeer := s.targetParticipantPeer(uid)
	count := 0
	scanned := 0
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(ctx, func(_ context.Context, item dialogs.Elem) error {
		if scanned >= 150 {
			return fmt.Errorf("limit")
		}
		scanned++
		switch p := item.Peer.(type) {
		case *tg.InputPeerChannel:
			ch := &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash}
			if _, opErr := client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
				Channel:      ch,
				Participant:  partPeer,
				BannedRights: tg.ChatBannedRights{ViewMessages: true},
			}); opErr == nil {
				_, _ = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
					Channel:      ch,
					Participant:  partPeer,
					BannedRights: tg.ChatBannedRights{},
				})
				count++
			}
		case *tg.InputPeerChat:
			if _, opErr := client.API().MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{
				ChatID: p.ChatID,
				UserID: inputUser,
			}); opErr == nil {
				count++
			}
		}
		return nil
	})
	return sendUserText(ctx, client, target, fmt.Sprintf("Global Kick selesai untuk %s pada %d grup.", label, count), threadID)
}

func (s *Service) listGlobalModCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, kind string, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	if s.sqlite == nil {
		return sendUserText(ctx, client, target, "Database lokal tidak tersedia.", threadID)
	}
	rows, err := s.sqlite.ListGlobalMod(ctx, ownerID, kind)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membaca daftar "+kind+": "+err.Error(), threadID)
	}
	if len(rows) == 0 {
		return sendUserText(ctx, client, target, fmt.Sprintf("Daftar %s kosong.", kind), threadID)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Daftar %s (%d pengguna):\n", strings.ToUpper(kind), len(rows))
	for i, r := range rows {
		label := r.Label
		if label == "" {
			label = fmt.Sprintf("%d", r.UserID)
		}
		fmt.Fprintf(&sb, "%d. %s\n", i+1, label)
	}
	return sendUserText(ctx, client, target, strings.TrimSpace(sb.String()), threadID)
}
