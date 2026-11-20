package userbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

func (s *Service) startGroupCall(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, title, key string) (*tg.InputGroupCall, bool, error) {
	req := &tg.PhoneCreateGroupCallRequest{
		Peer:     target,
		RandomID: vcRandomPositive32(),
	}
	if title = strings.TrimSpace(title); title != "" {
		req.SetTitle(title)
	}
	upd, err := client.API().PhoneCreateGroupCall(ctx, req)
	if err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "GROUPCALL_ALREADY_STARTED") {
			call, gErr := s.resolveActiveGroupCall(ctx, client, target, key)
			if gErr == nil && call != nil {
				if title != "" {
					_, _ = client.API().PhoneEditGroupCallTitle(ctx, &tg.PhoneEditGroupCallTitleRequest{Call: call, Title: title})
				}
				return call, true, nil
			}
			return nil, true, nil
		}
		return nil, false, err
	}
	call := extractGroupCallFromUpdates(upd)
	if call == nil {
		call, _ = getActiveGroupCall(ctx, client, target)
	}
	if call != nil {
		storeVCSession(key, call, 0, nil)
	}
	return call, false, nil
}

func (s *Service) startVCKeepalive(client *gobot.Client, key string, call *tg.InputGroupCall, ssrc int) context.CancelFunc {
	baseCtx := context.Background()
	if s != nil && s.root != nil {
		baseCtx = s.root
	}
	kCtx, cancel := context.WithCancel(baseCtx)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kCtx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(kCtx, 5*time.Second)
				_, err := client.API().PhoneCheckGroupCall(checkCtx, &tg.PhoneCheckGroupCallRequest{
					Call:    call,
					Sources: []int{ssrc},
				})
				checkCancel()
				if err != nil {
					up := strings.ToUpper(err.Error())
					if strings.Contains(up, "GROUPCALL_INVALID") || strings.Contains(up, "GROUPCALL_FORBIDDEN") {
						popVCSession(key)
						return
					}
				}
			}
		}
	}()
	return cancel
}

func (s *Service) handleStartVC(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, args, key string, threadID int) error {
	if !isVCChatPeer(target) {
		return sendUserText(ctx, client, target, "Perintah voice chat hanya dapat digunakan di grup/channel.", threadID)
	}
	call, already, err := s.startGroupCall(ctx, client, target, args, key)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal memulai Voice Chat: "+err.Error(), threadID)
	}
	if already {
		if call != nil {
			return sendUserText(ctx, client, target, fmt.Sprintf("Voice Chat sudah aktif (call_id=%d).", call.ID), threadID)
		}
		return sendUserText(ctx, client, target, "Voice Chat sudah aktif di obrolan ini.", threadID)
	}
	if call != nil {
		return sendUserText(ctx, client, target, fmt.Sprintf("Voice Chat berhasil dimulai (call_id=%d).", call.ID), threadID)
	}
	return sendUserText(ctx, client, target, "Voice Chat berhasil dimulai.", threadID)
}

func (s *Service) handleJoinVC(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, key string, threadID int) error {
	if !isVCChatPeer(target) {
		return sendUserText(ctx, client, target, "Perintah voice chat hanya dapat digunakan di grup/channel.", threadID)
	}
	call, err := s.resolveActiveGroupCall(ctx, client, target, key)
	if err != nil || call == nil {
		createdCall, _, createErr := s.startGroupCall(ctx, client, target, "", key)
		if createErr != nil || createdCall == nil {
			msg := "Tidak ada Voice Chat aktif saat ini."
			if createErr != nil {
				msg = "Gagal memulai/bergabung Voice Chat: " + createErr.Error()
			} else if err != nil {
				msg = err.Error()
			}
			return sendUserText(ctx, client, target, msg, threadID)
		}
		call = createdCall
	}

	joinAs := s.resolveVCJoinAsPeer(ctx, client, target)
	var joinErr error
	var joinedSSRC int
	for attempt := 0; attempt < 2; attempt++ {
		params, ssrc, pErr := buildVCJoinParams()
		if pErr != nil {
			return sendUserText(ctx, client, target, "Gagal menyiapkan parameter Voice Chat: "+pErr.Error(), threadID)
		}
		_, joinErr = client.API().PhoneJoinGroupCall(ctx, &tg.PhoneJoinGroupCallRequest{
			Muted:        true,
			VideoStopped: true,
			Call:         call,
			JoinAs:       joinAs,
			Params:       params,
		})
		if joinErr == nil {
			joinedSSRC = ssrc
			break
		}
		if !strings.Contains(strings.ToUpper(joinErr.Error()), "GROUPCALL_SSRC_DUPLICATE_MUCH") {
			break
		}
	}
	if joinErr != nil {
		return sendUserText(ctx, client, target, fmt.Sprintf("Gagal bergabung ke Voice Chat (call_id=%d): %v", call.ID, joinErr), threadID)
	}
	cancel := s.startVCKeepalive(client, key, call, joinedSSRC)
	storeVCSession(key, call, joinedSSRC, cancel)
	return sendUserText(ctx, client, target, fmt.Sprintf("Berhasil bergabung ke Voice Chat (call_id=%d).", call.ID), threadID)
}

func (s *Service) handleLeaveVC(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, key string, threadID int) error {
	if !isVCChatPeer(target) {
		return sendUserText(ctx, client, target, "Perintah voice chat hanya dapat digunakan di grup/channel.", threadID)
	}
	state := popVCSession(key)
	var call *tg.InputGroupCall
	ssrc := 0
	if state != nil {
		call = state.call
		ssrc = state.ssrc
	}
	if call == nil {
		activeCall, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
			if state != nil {
				return sendUserText(ctx, client, target, "Voice Chat telah berakhir.", threadID)
			}
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		call = activeCall
	}
	if ssrc == 0 {
		return sendUserText(ctx, client, target, "Userbot sedang tidak berada di Voice Chat.", threadID)
	}
	if _, err := client.API().PhoneLeaveGroupCall(ctx, &tg.PhoneLeaveGroupCallRequest{
		Call:   call,
		Source: ssrc,
	}); err != nil {
		up := strings.ToUpper(err.Error())
		if strings.Contains(up, "GROUPCALL_INVALID") || strings.Contains(up, "GROUPCALL_FORBIDDEN") {
			return sendUserText(ctx, client, target, "Berhasil keluar dari Voice Chat.", threadID)
		}
		return sendUserText(ctx, client, target, "Gagal keluar dari Voice Chat: "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Berhasil keluar dari Voice Chat.", threadID)
}

func (s *Service) vcCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, name, args string, threadID int) error {
	ownerID, _ := s.userbotOwner(ctx, client)
	key := vcSessionKey(ownerID, target)
	switch name {
	case "startvc":
		return s.handleStartVC(ctx, client, target, args, key, threadID)
	case "stopvc":
		call, err := s.resolveActiveGroupCall(ctx, client, target, key)
		if err != nil || call == nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		popVCSession(key)
		if _, err := client.API().PhoneDiscardGroupCall(ctx, call); err != nil {
			return sendUserText(ctx, client, target, "Gagal menghentikan Voice Chat: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Voice Chat dihentikan.", threadID)
	case "setvc":
		title := strings.TrimSpace(args)
		if title == "" {
			return sendUserText(ctx, client, target, "Gunakan: !setvc <judul baru>", threadID)
		}
		call, err := s.resolveActiveGroupCall(ctx, client, target, key)
		if err != nil || call == nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		if _, err := client.API().PhoneEditGroupCallTitle(ctx, &tg.PhoneEditGroupCallTitleRequest{
			Call:  call,
			Title: title,
		}); err != nil {
			return sendUserText(ctx, client, target, "Gagal mengubah judul Voice Chat: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Judul Voice Chat diperbarui: "+title, threadID)
	case "joinvc":
		return s.handleJoinVC(ctx, client, target, key, threadID)
	case "leavevc":
		return s.handleLeaveVC(ctx, client, target, key, threadID)
	case "play", "video", "ytmusic", "ytvideo":
		query := strings.TrimSpace(args)
		if query == "" {
			if replyTxt, _, rErr := repliedText(ctx, client, target, msg); rErr == nil {
				query = strings.TrimSpace(replyTxt)
			}
		}
		if query == "" {
			return sendUserText(ctx, client, target, fmt.Sprintf("Gunakan: !%s <judul|link>", name), threadID)
		}
		call, err := s.resolveActiveGroupCall(ctx, client, target, key)
		if err != nil || call == nil {
			return sendUserText(ctx, client, target, "Pastikan Voice Chat aktif terlebih dahulu (!startvc): "+err.Error(), threadID)
		}
		title := query
		if strings.HasPrefix(strings.ToLower(query), "http") {
			reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			oembedURL := "https://www.youtube.com/oembed?url=" + url.QueryEscape(query) + "&format=json"
			if req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodGet, oembedURL, nil); reqErr == nil {
				if resp, rErr := toolHTTPClient.Do(req); rErr == nil {
					raw, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
					_ = resp.Body.Close()
					var meta struct {
						Title      string `json:"title"`
						AuthorName string `json:"author_name"`
					}
					if json.Unmarshal(raw, &meta) == nil && meta.Title != "" {
						title = fmt.Sprintf("%s (%s)", meta.Title, meta.AuthorName)
					}
				}
			}
			cancel()
		}
		modeLabel := "Audio Stream"
		if name == "video" || name == "ytvideo" {
			modeLabel = "Video Stream"
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("VC Queue (%s | Call %d):\nMedia: %s\nStatus: Terdaftar (catatan: WebRTC media encoder eksternal belum terpasang di container hemat RAM).", modeLabel, call.ID, title), threadID)
	}
	return nil
}
