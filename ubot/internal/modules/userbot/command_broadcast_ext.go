package userbot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func (s *Service) cancelAllBroadcastsCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	s.mu.Lock()
	bJob := s.broadcasts[ownerID]
	pJob := s.panelBroadcasts[ownerID]
	if pJob != nil {
		delete(s.panelBroadcasts, ownerID)
	}
	s.mu.Unlock()

	stopped := 0
	if bJob != nil {
		bJob.cancel()
		stopped++
	}
	if pJob != nil {
		pJob.cancel()
		s.setPanelBroadcastStatus(ctx, ownerID, "Berhenti", "Dibatalkan melalui !cancelall")
		stopped++
	}
	if stopped == 0 {
		return sendUserText(ctx, client, target, "Tidak ada antrean broadcast yang sedang berjalan.", threadID)
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Berhasil menghentikan %d antrean broadcast aktif.", stopped), threadID)
}

func (s *Service) forwardBroadcastCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args, kind string, threadID int) error {
	replyID := broadcastReplyMessageID(msg)
	if replyID <= 0 {
		return sendUserText(ctx, client, target, fmt.Sprintf("Balas pesan yang ingin diteruskan. Contoh: !%s (reply pesan)", kind), threadID)
	}
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	targetType := "group"
	if kind == "fucast" {
		targetType = "private"
	}
	excludeBL := strings.Contains(strings.ToLower(args), "-bl")
	targets, err := s.broadcastTargets(ctx, client, map[string]bool{targetType: true}, excludeBL, ownerID)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengambil target forward: "+err.Error(), threadID)
	}
	if len(targets) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada target forward yang sesuai.", threadID)
	}

	taskID := fmt.Sprintf("%d-%d", ownerID, time.Now().UnixNano())
	monitorChatID := ownerID
	s.mu.Lock()
	var accountCtx context.Context
	for cid, oid := range s.owners {
		if oid == ownerID {
			accountCtx = s.accountCtxs[cid]
			break
		}
	}
	if accountCtx == nil {
		accountCtx = s.root
	}
	if s.broadcasts[ownerID] != nil {
		s.mu.Unlock()
		return sendUserText(ctx, client, target, "Broadcast akun ini sedang berjalan. Gunakan !cancel.", threadID)
	}
	jobCtx, cancel := context.WithCancel(accountCtx)
	job := &broadcastJob{id: taskID, cancel: cancel}
	s.broadcasts[ownerID] = job
	s.mu.Unlock()

	if monitorChatID != 0 && s.redis != nil {
		_ = s.notifyBroadcast(ctx, monitorChatID, ownerID, taskID, "broadcast_start", len(targets), 0, 0, 0, nil)
	} else {
		_ = sendUserText(ctx, client, target, fmt.Sprintf("Forward broadcast (%s) dimulai ke %d target.", kind, len(targets)), threadID)
	}

	go func() {
		s.broadcastSem <- struct{}{}
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { <-s.broadcastSem })
		s.runForwardBroadcast(jobCtx, client, ownerID, job, targets, target, replyID, monitorChatID, taskID, threadID)
	}()
	return nil
}

func (s *Service) runForwardBroadcast(ctx context.Context, client *gobot.Client, ownerID int64, job *broadcastJob, targets []broadcastTarget, fromPeer tg.InputPeerClass, replyID int, monitorChatID int64, taskID string, threadID int) {
	success, failed := 0, 0
	lastBucket := 0
	lastProgress := 0
	failures := make([]string, 0)
	for index, item := range targets {
		if ctx.Err() != nil {
			break
		}
		rid, rErr := crypto.RandInt64(crypto.DefaultRand())
		if rErr != nil {
			rid = randInt64()
		}
		_, sendErr := client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
			FromPeer: fromPeer,
			ID:       []int{replyID},
			RandomID: []int64{rid},
			ToPeer:   item.Peer,
		})
		if sendErr != nil {
			if flood, ok := tgerr.AsFloodWait(sendErr); ok {
				waitDur := flood + 500*time.Millisecond
				select {
				case <-ctx.Done():
				case <-time.After(waitDur):
					_, sendErr = client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
						FromPeer: fromPeer,
						ID:       []int{replyID},
						RandomID: []int64{rid},
						ToPeer:   item.Peer,
					})
				}
			}
		}
		if sendErr != nil {
			failed++
			if len(failures) < 100 {
				failures = append(failures, fmt.Sprintf("%s: %s", item.Name, compactBroadcastError(sendErr)))
			}
		} else {
			success++
		}
		processed := index + 1
		bucket := (processed*5 + len(targets) - 1) / len(targets)
		if bucket > 5 {
			bucket = 5
		}
		if bucket > lastBucket {
			lastBucket = bucket
			lastProgress = (processed*100 + len(targets) - 1) / len(targets)
			if lastProgress > 100 {
				lastProgress = 100
			}
			if monitorChatID != 0 && s.redis != nil {
				_ = s.notifyBroadcast(context.Background(), monitorChatID, ownerID, taskID, "broadcast_progress", len(targets), lastProgress, success, failed, nil)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(350 * time.Millisecond):
		}
	}
	if monitorChatID != 0 && s.redis != nil {
		progress := lastProgress
		if ctx.Err() == nil {
			progress = 100
		}
		_ = s.notifyBroadcast(context.Background(), monitorChatID, ownerID, taskID, "broadcast_done", len(targets), progress, success, failed, failures)
	} else {
		_ = sendUserText(context.Background(), client, fromPeer, fmt.Sprintf("Forward broadcast selesai: %d berhasil, %d gagal.", success, failed), threadID)
	}
	s.mu.Lock()
	if s.broadcasts[ownerID] == job {
		delete(s.broadcasts, ownerID)
	}
	s.mu.Unlock()
}

func (s *Service) parseSpamArgs(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, maxCount int) (int, string, []tg.MessageEntityClass, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return 0, "", nil, fmt.Errorf("jumlah tidak diberikan")
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count <= 0 {
		return 0, "", nil, fmt.Errorf("jumlah harus berupa angka positif")
	}
	if count > maxCount {
		count = maxCount
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), fields[0]))
	var entities []tg.MessageEntityClass
	if text == "" {
		var rErr error
		text, entities, rErr = repliedText(ctx, client, target, msg)
		if rErr != nil || strings.TrimSpace(text) == "" {
			return 0, "", nil, fmt.Errorf("berikan teks pesan atau balas pesan teks")
		}
	}
	return count, text, entities, nil
}

func (s *Service) spamCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	count, text, entities, err := s.parseSpamArgs(ctx, client, target, msg, args, 25)
	if err != nil {
		return sendUserText(ctx, client, target, "Gunakan: !spam <jumlah 1-25> <pesan|reply>", threadID)
	}
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			break
		}
		req := &tg.MessagesSendMessageRequest{
			Peer:     target,
			Message:  text,
			Entities: entities,
			RandomID: randInt64(),
			ReplyTo:  buildReplyTo(threadID, 0),
		}
		if _, sErr := client.API().MessagesSendMessage(ctx, req); sErr != nil {
			if flood, ok := tgerr.AsFloodWait(sErr); ok {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(flood + 500*time.Millisecond):
				}
				continue
			}
			log.Printf("[spam] send err: %v", sErr)
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(350 * time.Millisecond):
		}
	}
	return nil
}

func (s *Service) spamcastCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	count, text, entities, err := s.parseSpamArgs(ctx, client, target, msg, args, 5)
	if err != nil {
		return sendUserText(ctx, client, target, "Gunakan: !spamcast <jumlah 1-5> <pesan|reply>", threadID)
	}
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	targets, err := s.broadcastTargets(ctx, client, map[string]bool{"group": true}, true, ownerID)
	if err != nil || len(targets) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada grup target untuk !spamcast.", threadID)
	}
	if err := sendUserText(ctx, client, target, fmt.Sprintf("Spamcast dimulai: %dx ke %d grup target.", count, len(targets)), threadID); err != nil {
		return err
	}
	go func() {
		s.broadcastSem <- struct{}{}
		defer func() { <-s.broadcastSem }()
		sent, failed := 0, 0
		for _, item := range targets {
			for i := 0; i < count; i++ {
				if s.root.Err() != nil {
					return
				}
				_, sErr := client.API().MessagesSendMessage(s.root, &tg.MessagesSendMessageRequest{
					Peer:     item.Peer,
					Message:  text,
					Entities: entities,
					RandomID: randInt64(),
				})
				if sErr != nil {
					failed++
				} else {
					sent++
				}
				time.Sleep(350 * time.Millisecond)
			}
		}
		_ = sendUserText(context.Background(), client, target, fmt.Sprintf("Spamcast selesai: %d terkirim, %d gagal.", sent, failed), threadID)
	}()
	return nil
}

func (s *Service) memcastCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	ch, ok := target.(*tg.InputPeerChannel)
	if !ok {
		return sendUserText(ctx, client, target, "Perintah !memcast hanya dapat digunakan pada supergroup.", threadID)
	}
	text := strings.TrimSpace(args)
	var entities []tg.MessageEntityClass
	if text == "" {
		var rErr error
		text, entities, rErr = repliedText(ctx, client, target, msg)
		if rErr != nil || strings.TrimSpace(text) == "" {
			return sendUserText(ctx, client, target, "Gunakan: !memcast <pesan> atau reply pesan teks.", threadID)
		}
	}
	res, err := client.API().ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
		Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
		Filter:  &tg.ChannelParticipantsRecent{},
		Limit:   100,
	})
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengambil daftar anggota: "+err.Error(), threadID)
	}
	cp, ok := res.(*tg.ChannelsChannelParticipants)
	if !ok {
		return sendUserText(ctx, client, target, "Daftar anggota tidak tersedia.", threadID)
	}
	var users []*tg.User
	for _, item := range cp.Users {
		u, isUser := item.(*tg.User)
		if isUser && u != nil && !u.Bot && !u.Deleted && !u.Self && u.ID > 0 {
			users = append(users, u)
			if len(users) >= 50 {
				break
			}
		}
	}
	if len(users) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada anggota yang dapat dikirimi pesan.", threadID)
	}
	sent, failed := 0, 0
	for _, u := range users {
		if ctx.Err() != nil {
			break
		}
		_, sErr := client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
			Peer:     &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash},
			Message:  text,
			Entities: entities,
			RandomID: randInt64(),
		})
		if sErr != nil {
			failed++
		} else {
			sent++
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(450 * time.Millisecond):
		}
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Memcast selesai: %d terkirim, %d gagal (maks 50 anggota).", sent, failed), threadID)
}
