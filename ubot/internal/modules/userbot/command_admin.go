package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) resolveTargetInputUser(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string) (int64, string, tg.InputUserClass, error) {
	uid, displayName, err := s.resolveTargetUserID(ctx, client, target, msg, entities, args)
	if err != nil {
		return 0, "", nil, err
	}
	ownerID, _ := s.userbotOwner(ctx, client)
	var inputUser tg.InputUserClass
	if uid == ownerID {
		inputUser = &tg.InputUserSelf{}
	} else {
		hash, _ := s.peerCache().UserHash(uid)
		inputUser = &tg.InputUser{UserID: uid, AccessHash: hash}
	}
	label := strconv.FormatInt(uid, 10)
	if displayName != "" {
		label = fmt.Sprintf("%s (%d)", displayName, uid)
	}
	return uid, label, inputUser, nil
}

func (s *Service) targetParticipantPeer(uid int64) tg.InputPeerClass {
	hash, _ := s.peerCache().UserHash(uid)
	return &tg.InputPeerUser{UserID: uid, AccessHash: hash}
}

func deletePeerMessages(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if ch, ok := target.(*tg.InputPeerChannel); ok {
		_, err := client.API().ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      ids,
		})
		return err
	}
	_, err := client.API().MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
		Revoke: true,
		ID:     ids,
	})
	return err
}

func extractMessagesSlice(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch val := res.(type) {
	case *tg.MessagesMessages:
		return val.Messages
	case *tg.MessagesMessagesSlice:
		return val.Messages
	case *tg.MessagesChannelMessages:
		return val.Messages
	default:
		return nil
	}
}

func (s *Service) banCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, unban bool, threadID int) error {
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		rights := tg.ChatBannedRights{}
		if !unban {
			rights = tg.ChatBannedRights{ViewMessages: true, SendMessages: true, SendMedia: true}
		}
		_, err = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
			Channel:      &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			Participant:  s.targetParticipantPeer(uid),
			BannedRights: rights,
		})
	case *tg.InputPeerChat:
		if unban {
			_, err = client.API().MessagesAddChatUser(ctx, &tg.MessagesAddChatUserRequest{
				ChatID:   p.ChatID,
				UserID:   inputUser,
				FwdLimit: 10,
			})
		} else {
			_, err = client.API().MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{
				ChatID: p.ChatID,
				UserID: inputUser,
			})
		}
	default:
		return sendUserText(ctx, client, target, "Perintah ini hanya dapat digunakan di dalam grup atau channel.", threadID)
	}
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal memproses ban/unban: "+err.Error(), threadID)
	}
	if unban {
		return sendUserText(ctx, client, target, "Berhasil membuka blokir: "+label, threadID)
	}
	return sendUserText(ctx, client, target, "Berhasil memblokir: "+label, threadID)
}

func (s *Service) kickCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		ch := &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash}
		part := s.targetParticipantPeer(uid)
		if _, err = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
			Channel:      ch,
			Participant:  part,
			BannedRights: tg.ChatBannedRights{ViewMessages: true},
		}); err != nil {
			return sendUserText(ctx, client, target, "Gagal mengeluarkan pengguna: "+err.Error(), threadID)
		}
		_, _ = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
			Channel:      ch,
			Participant:  part,
			BannedRights: tg.ChatBannedRights{},
		})
	case *tg.InputPeerChat:
		if _, err = client.API().MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{
			ChatID: p.ChatID,
			UserID: inputUser,
		}); err != nil {
			return sendUserText(ctx, client, target, "Gagal mengeluarkan pengguna: "+err.Error(), threadID)
		}
	default:
		return sendUserText(ctx, client, target, "Perintah kick hanya dapat digunakan di dalam grup.", threadID)
	}
	return sendUserText(ctx, client, target, "Pengguna dikeluarkan: "+label, threadID)
}

func (s *Service) muteCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, unmute bool, threadID int) error {
	ch, ok := target.(*tg.InputPeerChannel)
	if !ok {
		return sendUserText(ctx, client, target, "Perintah mute/unmute hanya didukung pada supergroup.", threadID)
	}
	uid, label, _, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	rights := tg.ChatBannedRights{}
	if !unmute {
		rights = tg.ChatBannedRights{
			SendMessages: true,
			SendMedia:    true,
			SendStickers: true,
			SendGifs:     true,
			SendGames:    true,
			SendInline:   true,
		}
	}
	if _, err = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
		Channel:      &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
		Participant:  s.targetParticipantPeer(uid),
		BannedRights: rights,
	}); err != nil {
		return sendUserText(ctx, client, target, "Gagal mengubah izin kirim pesan: "+err.Error(), threadID)
	}
	if unmute {
		return sendUserText(ctx, client, target, "Izin bicara dipulihkan: "+label, threadID)
	}
	return sendUserText(ctx, client, target, "Pengguna dibisukan: "+label, threadID)
}

func (s *Service) adminRoleCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, mode string, threadID int) error {
	uid, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	_ = uid
	fields := strings.Fields(args)
	var rank string
	if len(fields) > 1 {
		rank = strings.Join(fields[1:], " ")
	}
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		rights := tg.ChatAdminRights{}
		if mode == "promote" || mode == "fullpromote" {
			rights = tg.ChatAdminRights{
				ChangeInfo:     true,
				DeleteMessages: true,
				BanUsers:       true,
				InviteUsers:    true,
				PinMessages:    true,
				ManageCall:     true,
			}
			if mode == "fullpromote" {
				rights.PostMessages = true
				rights.EditMessages = true
				rights.AddAdmins = true
			}
		}
		req := &tg.ChannelsEditAdminRequest{
			Channel:     &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			UserID:      inputUser,
			AdminRights: rights,
		}
		if rank != "" && mode != "demote" {
			req.SetRank(rank)
		}
		_, err = client.API().ChannelsEditAdmin(ctx, req)
	case *tg.InputPeerChat:
		_, err = client.API().MessagesEditChatAdmin(ctx, &tg.MessagesEditChatAdminRequest{
			ChatID:  p.ChatID,
			UserID:  inputUser,
			IsAdmin: mode != "demote",
		})
	default:
		return sendUserText(ctx, client, target, "Perintah admin hanya dapat digunakan di dalam grup atau channel.", threadID)
	}
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengubah status admin: "+err.Error(), threadID)
	}
	if mode == "demote" {
		return sendUserText(ctx, client, target, "Hak admin dicabut: "+label, threadID)
	}
	if mode == "fullpromote" {
		return sendUserText(ctx, client, target, "Diangkat menjadi full admin: "+label, threadID)
	}
	return sendUserText(ctx, client, target, "Diangkat menjadi admin: "+label, threadID)
}

func (s *Service) pinCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, unpin bool, threadID int) error {
	replyID := broadcastReplyMessageID(msg)
	if unpin {
		if replyID > 0 {
			if _, err := client.API().MessagesUpdatePinnedMessage(ctx, &tg.MessagesUpdatePinnedMessageRequest{
				Peer:  target,
				ID:    replyID,
				Unpin: true,
			}); err != nil {
				return sendUserText(ctx, client, target, "Gagal melepas sematan: "+err.Error(), threadID)
			}
			return sendUserText(ctx, client, target, "Sematan pesan dilepas.", threadID)
		}
		req := &tg.MessagesUnpinAllMessagesRequest{Peer: target}
		if threadID > 0 {
			req.SetTopMsgID(threadID)
		}
		if _, err := client.API().MessagesUnpinAllMessages(ctx, req); err != nil {
			return sendUserText(ctx, client, target, "Gagal melepas sematan: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Semua sematan dilepas.", threadID)
	}
	if replyID <= 0 {
		return sendUserText(ctx, client, target, "Balas pesan yang ingin disematkan.", threadID)
	}
	silent := strings.Contains(strings.ToLower(args), "silent")
	if _, err := client.API().MessagesUpdatePinnedMessage(ctx, &tg.MessagesUpdatePinnedMessageRequest{
		Peer:   target,
		ID:     replyID,
		Silent: silent,
	}); err != nil {
		return sendUserText(ctx, client, target, "Gagal menyematkan pesan: "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Pesan berhasil disematkan.", threadID)
}

func (s *Service) delCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	var ids []int
	if replyID := broadcastReplyMessageID(msg); replyID > 0 {
		ids = append(ids, replyID)
	}
	if msg != nil && msg.ID > 0 {
		ids = append(ids, msg.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := deletePeerMessages(ctx, client, target, ids); err != nil {
		return sendUserText(ctx, client, target, "Gagal menghapus pesan: "+err.Error(), threadID)
	}
	return nil
}

func (s *Service) purgeCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	replyID := broadcastReplyMessageID(msg)
	if replyID <= 0 || msg == nil || msg.ID < replyID {
		return sendUserText(ctx, client, target, "Balas pesan awal yang ingin di-purge.", threadID)
	}
	startID := replyID
	if msg.ID-startID+1 > 100 {
		startID = msg.ID - 99
	}
	ids := make([]int, 0, msg.ID-startID+1)
	for id := startID; id <= msg.ID; id++ {
		ids = append(ids, id)
	}
	if err := deletePeerMessages(ctx, client, target, ids); err != nil {
		return sendUserText(ctx, client, target, "Gagal melakukan purge: "+err.Error(), threadID)
	}
	return nil
}

func (s *Service) deleteHistoryCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	limit := 10
	if fields := strings.Fields(args); len(fields) > 0 {
		if n, err := strconv.Atoi(fields[0]); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	res, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  target,
		Limit: 100,
	})
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membaca riwayat chat: "+err.Error(), threadID)
	}
	var ids []int
	if msg != nil && msg.ID > 0 && msg.Out {
		ids = append(ids, msg.ID)
	}
	for _, item := range extractMessagesSlice(res) {
		if m, ok := item.(*tg.Message); ok && m.Out && m.ID > 0 {
			if msg != nil && m.ID == msg.ID {
				continue
			}
			ids = append(ids, m.ID)
			if len(ids) >= limit+1 {
				break
			}
		}
	}
	if len(ids) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada pesan sendiri yang ditemukan untuk dihapus.", threadID)
	}
	if err := deletePeerMessages(ctx, client, target, ids); err != nil {
		return sendUserText(ctx, client, target, "Gagal menghapus riwayat pesan: "+err.Error(), threadID)
	}
	return nil
}

func (s *Service) zombiesCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ch, ok := target.(*tg.InputPeerChannel)
	if !ok {
		return sendUserText(ctx, client, target, "Perintah !zombies hanya dapat digunakan pada supergroup.", threadID)
	}
	inputCh := &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}
	res, err := client.API().ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
		Channel: inputCh,
		Filter:  &tg.ChannelParticipantsRecent{},
		Limit:   200,
	})
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengambil daftar anggota: "+err.Error(), threadID)
	}
	cp, ok := res.(*tg.ChannelsChannelParticipants)
	if !ok {
		return sendUserText(ctx, client, target, "Daftar anggota tidak tersedia.", threadID)
	}
	cleaned := 0
	for _, item := range cp.Users {
		u, ok := item.(*tg.User)
		if !ok || !u.Deleted || u.ID <= 0 {
			continue
		}
		part := &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}
		if _, bErr := client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
			Channel:      inputCh,
			Participant:  part,
			BannedRights: tg.ChatBannedRights{ViewMessages: true},
		}); bErr == nil {
			_, _ = client.API().ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{
				Channel:      inputCh,
				Participant:  part,
				BannedRights: tg.ChatBannedRights{},
			})
			cleaned++
		}
	}
	if cleaned == 0 {
		return sendUserText(ctx, client, target, "Tidak ada akun terhapus (zombies) yang ditemukan.", threadID)
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Berhasil membersihkan %d akun terhapus (zombies).", cleaned), threadID)
}
