package userbot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tg"
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
	if isCancelArg(args) {
		return s.broadcastCommand(ctx, client, target, msg, "cancel", threadID)
	}
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

	monitorChatID := ownerID
	jobCtx, job, ok := s.acquireBroadcastJob(ownerID)
	if !ok {
		return sendUserText(ctx, client, target, "Broadcast akun ini sedang berjalan. Gunakan !cancel.", threadID)
	}
	taskID := job.id

	if err := sendUserText(ctx, client, target, fmt.Sprintf("Forward broadcast (%s) dimulai ke %d target.", kind, len(targets)), threadID); err != nil {
		s.releaseBroadcastJob(ownerID, job)
		return err
	}
	if monitorChatID != 0 && s.redis != nil {
		if nErr := s.notifyBroadcast(ctx, monitorChatID, ownerID, taskID, "broadcast_start", len(targets), 0, 0, 0, nil); nErr != nil {
			log.Printf("[userbot %d] forward broadcast notify failed: %v", ownerID, nErr)
		}
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
	defer s.releaseBroadcastJob(ownerID, job)
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
		sendErr := sendWithFloodRetry(ctx, ownerID, "Forward broadcast", func() error {
			_, err := client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
				FromPeer: fromPeer,
				ID:       []int{replyID},
				RandomID: []int64{rid},
				ToPeer:   item.Peer,
			})
			return err
		})
		if sendErr != nil && ctx.Err() != nil {
			break
		}
		var fatalStop bool
		if sendErr != nil {
			failed++
			if label, isFatal := classifyBroadcastFatalError(sendErr); isFatal {
				fatalStop = true
				if len(failures) < 100 {
					failures = append(failures, fmt.Sprintf("%s: %s", item.Name, label))
				}
			} else if len(failures) < 100 {
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
		if fatalStop {
			log.Printf("[userbot %d] Forward broadcast stopped early due to %v", ownerID, sendErr)
			break
		}
		if !waitContextDelay(ctx, broadcastStepDelay(item.Kind)) {
			break
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
	if isCancelArg(args) {
		return s.broadcastCommand(ctx, client, target, msg, "cancel", threadID)
	}
	count, text, entities, err := s.parseSpamArgs(ctx, client, target, msg, args, 25)
	if err != nil {
		return sendUserText(ctx, client, target, "Gunakan: !spam <jumlah 1-25> <pesan|reply>", threadID)
	}
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	jobCtx, job, ok := s.acquireBroadcastJob(ownerID)
	if !ok {
		return sendUserText(ctx, client, target, "Broadcast/spam akun ini sedang berjalan. Gunakan !cancel atau !cancelall.", threadID)
	}
	delayKind := "group"
	if _, isPrivate := target.(*tg.InputPeerUser); isPrivate {
		delayKind = "private"
	}
	go func() {
		defer s.releaseBroadcastJob(ownerID, job)
		s.broadcastSem <- struct{}{}
		defer func() { <-s.broadcastSem }()
		for i := 0; i < count; i++ {
			if jobCtx.Err() != nil {
				break
			}
			req := &tg.MessagesSendMessageRequest{
				Peer:     target,
				Message:  text,
				Entities: entities,
				RandomID: randInt64(),
				ReplyTo:  buildReplyTo(threadID, 0),
			}
			if sErr := sendWithFloodRetry(jobCtx, ownerID, "Spam", func() error {
				_, sendErr := client.API().MessagesSendMessage(jobCtx, req)
				return sendErr
			}); sErr != nil {
				if jobCtx.Err() == nil {
					log.Printf("[spam] send stopped (owner=%d): %v", ownerID, sErr)
				}
				break
			}
			if i+1 < count && !waitContextDelay(jobCtx, broadcastStepDelay(delayKind)) {
				break
			}
		}
	}()
	return nil
}

func (s *Service) spamcastCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	if isCancelArg(args) {
		return s.broadcastCommand(ctx, client, target, msg, "cancel", threadID)
	}
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
	jobCtx, job, ok := s.acquireBroadcastJob(ownerID)
	if !ok {
		return sendUserText(ctx, client, target, "Broadcast akun ini sedang berjalan. Gunakan !cancel atau !cancelall.", threadID)
	}
	if err := sendUserText(ctx, client, target, fmt.Sprintf("Spamcast dimulai: %dx ke %d grup target.", count, len(targets)), threadID); err != nil {
		s.releaseBroadcastJob(ownerID, job)
		return err
	}
	go func() {
		defer s.releaseBroadcastJob(ownerID, job)
		s.broadcastSem <- struct{}{}
		defer func() { <-s.broadcastSem }()
		sent, failed := 0, 0
		fatalReason := ""
		stopped := false
		for _, item := range targets {
			if stopped || jobCtx.Err() != nil {
				break
			}
			for i := 0; i < count; i++ {
				if jobCtx.Err() != nil {
					stopped = true
					break
				}
				req := &tg.MessagesSendMessageRequest{
					Peer:     item.Peer,
					Message:  text,
					Entities: entities,
					RandomID: randInt64(),
				}
				sErr := sendWithFloodRetry(jobCtx, ownerID, "Spamcast", func() error {
					_, sendErr := client.API().MessagesSendMessage(jobCtx, req)
					return sendErr
				})
				if sErr != nil && jobCtx.Err() != nil {
					stopped = true
					break
				}
				if sErr != nil {
					failed++
					if label, isFatal := classifyBroadcastFatalError(sErr); isFatal {
						fatalReason = label
						stopped = true
						log.Printf("[userbot %d] Spamcast stopped early due to %v", ownerID, sErr)
						break
					}
				} else {
					sent++
				}
				if !waitContextDelay(jobCtx, broadcastStepDelay(item.Kind)) {
					stopped = true
					break
				}
			}
		}
		summary := fmt.Sprintf("Spamcast selesai: %d terkirim, %d gagal.", sent, failed)
		if fatalReason != "" {
			summary = fmt.Sprintf("Spamcast %s: %d terkirim, %d gagal.", fatalReason, sent, failed)
		} else if jobCtx.Err() != nil {
			summary = fmt.Sprintf("Spamcast dibatalkan: %d terkirim, %d gagal.", sent, failed)
		}
		_ = sendUserText(context.Background(), client, target, summary, threadID)
	}()
	return nil
}

func (s *Service) memcastCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	if isCancelArg(args) {
		return s.broadcastCommand(ctx, client, target, msg, "cancel", threadID)
	}
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
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
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
	jobCtx, job, acquired := s.acquireBroadcastJob(ownerID)
	if !acquired {
		return sendUserText(ctx, client, target, "Broadcast akun ini sedang berjalan. Gunakan !cancel atau !cancelall.", threadID)
	}
	if err := sendUserText(ctx, client, target, fmt.Sprintf("Memcast dimulai: mengirim PM ke %d anggota (maks 50).", len(users)), threadID); err != nil {
		s.releaseBroadcastJob(ownerID, job)
		return err
	}
	go func() {
		defer s.releaseBroadcastJob(ownerID, job)
		s.broadcastSem <- struct{}{}
		defer func() { <-s.broadcastSem }()
		sent, failed := 0, 0
		fatalReason := ""
		for idx, u := range users {
			if jobCtx.Err() != nil {
				break
			}
			req := &tg.MessagesSendMessageRequest{
				Peer:     &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash},
				Message:  text,
				Entities: entities,
				RandomID: randInt64(),
			}
			sErr := sendWithFloodRetry(jobCtx, ownerID, "Memcast", func() error {
				_, sendErr := client.API().MessagesSendMessage(jobCtx, req)
				return sendErr
			})
			if sErr != nil && jobCtx.Err() != nil {
				break
			}
			if sErr != nil {
				failed++
				if label, isFatal := classifyBroadcastFatalError(sErr); isFatal {
					fatalReason = label
					log.Printf("[userbot %d] Memcast stopped early due to %v", ownerID, sErr)
					break
				}
			} else {
				sent++
			}
			if idx+1 < len(users) && !waitContextDelay(jobCtx, broadcastStepDelay("private")) {
				break
			}
		}
		summary := fmt.Sprintf("Memcast selesai: %d terkirim, %d gagal (maks 50 anggota).", sent, failed)
		if fatalReason != "" {
			summary = fmt.Sprintf("Memcast %s: %d terkirim, %d gagal. Hindari terlalu sering mengirim PM ke anggota non-kontak.", fatalReason, sent, failed)
		} else if jobCtx.Err() != nil {
			summary = fmt.Sprintf("Memcast dibatalkan: %d terkirim, %d gagal.", sent, failed)
		}
		_ = sendUserText(context.Background(), client, target, summary, threadID)
	}()
	return nil
}
