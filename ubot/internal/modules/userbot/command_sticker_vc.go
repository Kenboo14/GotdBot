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

func extractStickerDocument(reply *tg.Message) (*tg.Document, *tg.DocumentAttributeSticker) {
	if reply == nil {
		return nil, nil
	}
	med, ok := reply.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil, nil
	}
	doc, ok := med.Document.(*tg.Document)
	if !ok || doc == nil {
		return nil, nil
	}
	for _, attr := range doc.Attributes {
		if stAttr, ok := attr.(*tg.DocumentAttributeSticker); ok {
			return doc, stAttr
		}
	}
	return doc, nil
}

func (s *Service) strinfoCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return sendUserText(ctx, client, target, "Balas sebuah stiker untuk melihat informasinya (!strinfo).", threadID)
	}
	doc, stAttr := extractStickerDocument(reply)
	if doc == nil {
		return sendUserText(ctx, client, target, "Pesan yang dibalas bukan stiker atau dokumen.", threadID)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Sticker Info:\nDocument ID: %d\nMIME: %s\n", doc.ID, doc.MimeType)
	if stAttr != nil {
		if stAttr.Alt != "" {
			fmt.Fprintf(&sb, "Emoji: %s\n", stAttr.Alt)
		}
		if stAttr.Stickerset != nil {
			res, sErr := client.API().MessagesGetStickerSet(ctx, &tg.MessagesGetStickerSetRequest{
				Stickerset: stAttr.Stickerset,
			})
			if sErr == nil {
				if ms, ok := res.(*tg.MessagesStickerSet); ok {
					fmt.Fprintf(&sb, "Pack Title: %s\nShortName: %s\nTotal Stickers: %d\nLink: https://t.me/addstickers/%s\n",
						ms.Set.Title, ms.Set.ShortName, ms.Set.Count, ms.Set.ShortName)
				}
			}
		}
	}
	return sendUserText(ctx, client, target, strings.TrimSpace(sb.String()), threadID)
}

func (s *Service) kangCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return sendUserText(ctx, client, target, "Balas stiker atau dokumen stiker untuk disimpan ke pack (!kang [emoji]).", threadID)
	}
	doc, stAttr := extractStickerDocument(reply)
	if doc == nil {
		return sendUserText(ctx, client, target, "Balas stiker yang valid untuk !kang.", threadID)
	}
	emoji := "😎"
	if fields := strings.Fields(args); len(fields) > 0 {
		emoji = fields[0]
	} else if stAttr != nil && strings.TrimSpace(stAttr.Alt) != "" {
		emoji = stAttr.Alt
	}
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal membaca identitas akun.", threadID)
	}
	shortName := fmt.Sprintf("gotdbot_%d_pack", ownerID)
	item := tg.InputStickerSetItem{
		Document: &tg.InputDocument{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		},
		Emoji: emoji,
	}
	_, addErr := client.API().StickersAddStickerToSet(ctx, &tg.StickersAddStickerToSetRequest{
		Stickerset: &tg.InputStickerSetShortName{ShortName: shortName},
		Sticker:    item,
	})
	if addErr != nil {
		_, createErr := client.API().StickersCreateStickerSet(ctx, &tg.StickersCreateStickerSetRequest{
			UserID:    &tg.InputUserSelf{},
			Title:     fmt.Sprintf("GotdBot Pack %d", ownerID),
			ShortName: shortName,
			Stickers:  []tg.InputStickerSetItem{item},
		})
		if createErr != nil {
			return sendUserText(ctx, client, target, fmt.Sprintf("Gagal menambahkan ke pack stiker (%s): %v", addErr.Error(), createErr), threadID)
		}
	}
	return sendUserText(ctx, client, target, fmt.Sprintf("Stiker ditambahkan ke pack (%s): https://t.me/addstickers/%s", emoji, shortName), threadID)
}

func getActiveGroupCall(ctx context.Context, client *gobot.Client, target tg.InputPeerClass) (*tg.InputGroupCall, error) {
	var chatFull *tg.MessagesChatFull
	var err error
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		chatFull, err = client.API().ChannelsGetFullChannel(ctx, &tg.InputChannel{
			ChannelID:  p.ChannelID,
			AccessHash: p.AccessHash,
		})
	case *tg.InputPeerChat:
		chatFull, err = client.API().MessagesGetFullChat(ctx, p.ChatID)
	default:
		return nil, fmt.Errorf("perintah voice chat hanya dapat digunakan di grup/channel")
	}
	if err != nil || chatFull == nil {
		return nil, fmt.Errorf("gagal memeriksa voice chat aktif: %v", err)
	}
	var callClass tg.InputGroupCallClass
	switch f := chatFull.FullChat.(type) {
	case *tg.ChannelFull:
		if c, ok := f.GetCall(); ok {
			callClass = c
		}
	case *tg.ChatFull:
		if c, ok := f.GetCall(); ok {
			callClass = c
		}
	}
	call, ok := callClass.(*tg.InputGroupCall)
	if !ok || call == nil {
		return nil, fmt.Errorf("tidak ada voice chat aktif saat ini")
	}
	return call, nil
}

func (s *Service) vcCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, name, args string, threadID int) error {
	switch name {
	case "startvc":
		req := &tg.PhoneCreateGroupCallRequest{
			Peer:     target,
			RandomID: int(randInt64()),
		}
		if title := strings.TrimSpace(args); title != "" {
			req.SetTitle(title)
		}
		if _, err := client.API().PhoneCreateGroupCall(ctx, req); err != nil {
			return sendUserText(ctx, client, target, "Gagal memulai Voice Chat: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Voice Chat berhasil dimulai.", threadID)
	case "stopvc":
		call, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		if _, err := client.API().PhoneDiscardGroupCall(ctx, call); err != nil {
			return sendUserText(ctx, client, target, "Gagal menghentikan Voice Chat: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Voice Chat dihentikan.", threadID)
	case "setvc":
		title := strings.TrimSpace(args)
		if title == "" {
			return sendUserText(ctx, client, target, "Gunakan: !setvc <judul baru>", threadID)
		}
		call, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
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
		call, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		_, err = client.API().PhoneJoinGroupCall(ctx, &tg.PhoneJoinGroupCallRequest{
			Muted:        true,
			VideoStopped: true,
			Call:         call,
			JoinAs:       &tg.InputPeerSelf{},
			Params:       tg.DataJSON{Data: "{}"},
		})
		if err != nil {
			return sendUserText(ctx, client, target, fmt.Sprintf("Voice Chat aktif (call_id=%d), signaling join: %v", call.ID, err), threadID)
		}
		return sendUserText(ctx, client, target, fmt.Sprintf("Bergabung ke Voice Chat (call_id=%d).", call.ID), threadID)
	case "leavevc":
		call, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
			return sendUserText(ctx, client, target, err.Error(), threadID)
		}
		if _, err := client.API().PhoneLeaveGroupCall(ctx, &tg.PhoneLeaveGroupCallRequest{
			Call:   call,
			Source: 0,
		}); err != nil {
			return sendUserText(ctx, client, target, "Keluar dari Voice Chat: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Berhasil keluar dari Voice Chat.", threadID)
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
		call, err := getActiveGroupCall(ctx, client, target)
		if err != nil {
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

func (s *Service) dispatchExtendedCommand(ctx context.Context, client *gobot.Client, chatID int64, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, name, args string, threadID int) (bool, error) {
	switch name {
	case "lsudo":
		return true, s.listSudoCommand(ctx, client, target, threadID)
	case "clearsudo":
		return true, s.clearSudoCommand(ctx, client, target, msg, threadID)
	case "ban":
		return true, s.banCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "unban":
		return true, s.banCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "kick":
		return true, s.kickCommand(ctx, client, target, msg, entities, args, threadID)
	case "mute":
		return true, s.muteCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "unmute":
		return true, s.muteCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "promote", "fullpromote", "demote":
		return true, s.adminRoleCommand(ctx, client, target, msg, entities, args, name, threadID)
	case "pin":
		return true, s.pinCommand(ctx, client, target, msg, args, false, threadID)
	case "unpin":
		return true, s.pinCommand(ctx, client, target, msg, args, true, threadID)
	case "del":
		return true, s.delCommand(ctx, client, target, msg, threadID)
	case "purge":
		return true, s.purgeCommand(ctx, client, target, msg, threadID)
	case "dh":
		return true, s.deleteHistoryCommand(ctx, client, target, msg, args, threadID)
	case "zombies":
		return true, s.zombiesCommand(ctx, client, target, threadID)
	case "gban":
		return true, s.gbanCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "ungban":
		return true, s.gbanCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "gmute":
		return true, s.gmuteCommand(ctx, client, target, msg, entities, args, false, threadID)
	case "ungmute":
		return true, s.gmuteCommand(ctx, client, target, msg, entities, args, true, threadID)
	case "gkick":
		return true, s.gkickCommand(ctx, client, target, msg, entities, args, threadID)
	case "lsgban":
		return true, s.listGlobalModCommand(ctx, client, target, "gban", threadID)
	case "lsgmute":
		return true, s.listGlobalModCommand(ctx, client, target, "gmute", threadID)
	case "cancelall":
		return true, s.cancelAllBroadcastsCommand(ctx, client, target, threadID)
	case "ucast":
		return true, s.broadcastCommand(ctx, client, target, msg, "-pc "+args, threadID)
	case "fgcast", "fucast":
		return true, s.forwardBroadcastCommand(ctx, client, target, msg, args, name, threadID)
	case "spam":
		return true, s.spamCommand(ctx, client, target, msg, args, threadID)
	case "spamcast":
		return true, s.spamcastCommand(ctx, client, target, msg, args, threadID)
	case "memcast":
		return true, s.memcastCommand(ctx, client, target, msg, args, threadID)
	case "id":
		return true, s.idCommand(ctx, client, chatID, target, msg, threadID)
	case "info":
		return true, s.infoCommand(ctx, client, target, msg, entities, args, threadID)
	case "limit":
		return true, s.limitCommand(ctx, client, target, threadID)
	case "sg":
		return true, s.sgCommand(ctx, client, target, msg, entities, args, threadID)
	case "setbio":
		return true, s.setBioCommand(ctx, client, target, msg, args, threadID)
	case "setname":
		return true, s.setNameCommand(ctx, client, target, msg, args, threadID)
	case "setusername":
		return true, s.setUsernameCommand(ctx, client, target, msg, args, threadID)
	case "join":
		return true, s.joinCommand(ctx, client, target, args, threadID)
	case "kickme":
		return true, s.kickmeCommand(ctx, client, target, threadID)
	case "invite":
		return true, s.inviteCommand(ctx, client, target, msg, entities, args, threadID)
	case "fontlist":
		return true, s.fontListCommand(ctx, client, target, threadID)
	case "font":
		return true, s.fontCommand(ctx, client, target, msg, args, threadID)
	case "azan":
		return true, s.azanCommand(ctx, client, target, args, threadID)
	case "ai":
		return true, s.aiCommand(ctx, client, target, msg, args, threadID)
	case "telegraph":
		return true, s.telegraphCommand(ctx, client, target, msg, args, threadID)
	case "mirror":
		return true, s.mirrorCommand(ctx, client, target, msg, args, threadID)
	case "quotly":
		return true, s.quotlyCommand(ctx, client, target, msg, threadID)
	case "strinfo":
		return true, s.strinfoCommand(ctx, client, target, msg, threadID)
	case "kang":
		return true, s.kangCommand(ctx, client, target, msg, args, threadID)
	case "startvc", "stopvc", "setvc", "joinvc", "leavevc", "play", "video", "ytmusic", "ytvideo":
		return true, s.vcCommand(ctx, client, target, msg, name, args, threadID)
	default:
		return false, nil
	}
}
