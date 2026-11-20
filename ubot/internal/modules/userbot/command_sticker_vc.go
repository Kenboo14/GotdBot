package userbot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
)

type vcSessionState struct {
	call   *tg.InputGroupCall
	ssrc   int
	cancel context.CancelFunc
}

var (
	vcSessionMu sync.Mutex
	vcSessions  = make(map[string]*vcSessionState)
)

func vcSessionKey(ownerID int64, target tg.InputPeerClass) string {
	return fmt.Sprintf("%d:%s", ownerID, vcTargetKey(target))
}

func vcTargetKey(target tg.InputPeerClass) string {
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		return fmt.Sprintf("c:%d", p.ChannelID)
	case *tg.InputPeerChat:
		return fmt.Sprintf("g:%d", p.ChatID)
	default:
		return "unknown"
	}
}

func isVCChatPeer(target tg.InputPeerClass) bool {
	switch target.(type) {
	case *tg.InputPeerChannel, *tg.InputPeerChat:
		return true
	default:
		return false
	}
}

func storeVCSession(key string, call *tg.InputGroupCall, ssrc int, cancel context.CancelFunc) {
	if key == "" || call == nil {
		return
	}
	vcSessionMu.Lock()
	if prev, ok := vcSessions[key]; ok && prev != nil && prev.cancel != nil {
		prev.cancel()
	}
	vcSessions[key] = &vcSessionState{
		call:   call,
		ssrc:   ssrc,
		cancel: cancel,
	}
	vcSessionMu.Unlock()
}

func getStoredVCCall(key string) *tg.InputGroupCall {
	vcSessionMu.Lock()
	defer vcSessionMu.Unlock()
	if st, ok := vcSessions[key]; ok && st != nil && st.call != nil {
		return st.call
	}
	return nil
}

func popVCSession(key string) *vcSessionState {
	vcSessionMu.Lock()
	defer vcSessionMu.Unlock()
	st := vcSessions[key]
	delete(vcSessions, key)
	if st != nil && st.cancel != nil {
		st.cancel()
	}
	return st
}

func vcRandomPositive32() int {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		v := int(binary.BigEndian.Uint32(b[:]) & 0x7ffffffe)
		if v > 0 {
			return v
		}
	}
	v := int(time.Now().UnixNano() & 0x7ffffffe)
	if v <= 0 {
		return 100001
	}
	return v
}

func vcRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		binary.BigEndian.PutUint64(b, uint64(time.Now().UnixNano()))
	}
	return hex.EncodeToString(b)
}

func buildVCJoinParams() (tg.DataJSON, int, error) {
	ssrc := vcRandomPositive32()
	fpBytes := make([]byte, 32)
	if _, err := rand.Read(fpBytes); err != nil {
		return tg.DataJSON{}, 0, fmt.Errorf("buat fingerprint WebRTC: %w", err)
	}
	parts := make([]string, len(fpBytes))
	for i, bt := range fpBytes {
		parts[i] = fmt.Sprintf("%02X", bt)
	}
	payload := map[string]any{
		"ufrag": vcRandomHex(4),
		"pwd":   vcRandomHex(12),
		"fingerprints": []map[string]any{
			{
				"hash":        "sha-256",
				"setup":       "active",
				"fingerprint": strings.Join(parts, ":"),
			},
		},
		"ssrc": ssrc,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return tg.DataJSON{}, 0, fmt.Errorf("marshal parameter WebRTC: %w", err)
	}
	return tg.DataJSON{Data: string(raw)}, ssrc, nil
}

func extractGroupCallFromUpdates(updates tg.UpdatesClass) *tg.InputGroupCall {
	scan := func(items []tg.UpdateClass) *tg.InputGroupCall {
		for _, item := range items {
			ug, ok := item.(*tg.UpdateGroupCall)
			if !ok {
				continue
			}
			if gc, ok := ug.Call.(*tg.GroupCall); ok && gc != nil && gc.ID != 0 {
				return gc.AsInput()
			}
		}
		return nil
	}
	switch u := updates.(type) {
	case *tg.Updates:
		return scan(u.Updates)
	case *tg.UpdatesCombined:
		return scan(u.Updates)
	}
	return nil
}

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
	var htmlLines []string
	fmt.Fprintf(&sb, "Sticker Info:\nDocument ID: %d\nMIME: %s\n", doc.ID, doc.MimeType)
	htmlLines = append(htmlLines,
		"<b>Sticker Info</b>",
		fmt.Sprintf("<b>Document ID:</b> <i>%d</i>", doc.ID),
		fmt.Sprintf("<b>MIME:</b> <i>%s</i>", html.EscapeString(doc.MimeType)),
	)
	var shortName string
	if stAttr != nil {
		if stAttr.Alt != "" {
			fmt.Fprintf(&sb, "Emoji: %s\n", stAttr.Alt)
			htmlLines = append(htmlLines, fmt.Sprintf("<b>Emoji:</b> <i>%s</i>", html.EscapeString(stAttr.Alt)))
		}
		if stAttr.Stickerset != nil {
			res, sErr := client.API().MessagesGetStickerSet(ctx, &tg.MessagesGetStickerSetRequest{
				Stickerset: stAttr.Stickerset,
			})
			if sErr == nil {
				if ms, ok := res.(*tg.MessagesStickerSet); ok {
					shortName = ms.Set.ShortName
					fmt.Fprintf(&sb, "Pack Title: %s\nShortName: %s\nTotal Stickers: %d\nLink: https://t.me/addstickers/%s\n",
						ms.Set.Title, ms.Set.ShortName, ms.Set.Count, ms.Set.ShortName)
					htmlLines = append(htmlLines,
						fmt.Sprintf("<b>Pack Title:</b> <i>%s</i>", html.EscapeString(ms.Set.Title)),
						fmt.Sprintf("<b>ShortName:</b> <i>%s</i>", html.EscapeString(ms.Set.ShortName)),
						fmt.Sprintf("<b>Total Stickers:</b> <i>%d</i>", ms.Set.Count),
					)
				}
			}
		}
	}
	fallbackText := strings.TrimSpace(sb.String())
	if shortName != "" {
		packURL := "https://t.me/addstickers/" + shortName
		card := ubotInlineCardPayload{
			Title: "Sticker Info",
			HTML:  strings.Join(htmlLines, "\n"),
			Buttons: [][]ubotInlineCardButton{
				{{Text: "Link!", URL: packURL}},
			},
		}
		return s.sendInlineCardWithFallback(ctx, client, target, card, fallbackText, threadID)
	}
	return sendUserText(ctx, client, target, fallbackText, threadID)
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

func (s *Service) resolveActiveGroupCall(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, key string) (*tg.InputGroupCall, error) {
	call, err := getActiveGroupCall(ctx, client, target)
	if err == nil && call != nil {
		return call, nil
	}
	if cached := getStoredVCCall(key); cached != nil {
		return cached, nil
	}
	return nil, err
}

func (s *Service) resolveVCJoinAsPeer(ctx context.Context, client *gobot.Client, target tg.InputPeerClass) tg.InputPeerClass {
	self, selfErr := client.Self(ctx)
	if joinPeers, err := client.API().PhoneGetGroupCallJoinAs(ctx, target); err == nil && joinPeers != nil {
		userHashes := make(map[int64]int64, len(joinPeers.Users))
		for _, u := range joinPeers.Users {
			if userObj, ok := u.(*tg.User); ok {
				userHashes[userObj.ID] = userObj.AccessHash
			}
		}
		chanHashes := make(map[int64]int64, len(joinPeers.Chats))
		for _, c := range joinPeers.Chats {
			if chObj, ok := c.(*tg.Channel); ok {
				chanHashes[chObj.ID] = chObj.AccessHash
			}
		}
		if selfErr == nil && self != nil {
			for _, p := range joinPeers.Peers {
				if pu, ok := p.(*tg.PeerUser); ok && pu.UserID == self.ID {
					ah := userHashes[pu.UserID]
					if ah == 0 {
						ah = self.AccessHash
					}
					return &tg.InputPeerUser{UserID: self.ID, AccessHash: ah}
				}
			}
		}
		for _, p := range joinPeers.Peers {
			switch v := p.(type) {
			case *tg.PeerUser:
				return &tg.InputPeerUser{UserID: v.UserID, AccessHash: userHashes[v.UserID]}
			case *tg.PeerChannel:
				return &tg.InputPeerChannel{ChannelID: v.ChannelID, AccessHash: chanHashes[v.ChannelID]}
			case *tg.PeerChat:
				return &tg.InputPeerChat{ChatID: v.ChatID}
			}
		}
	}
	if selfErr == nil && self != nil {
		return &tg.InputPeerUser{UserID: self.ID, AccessHash: self.AccessHash}
	}
	return &tg.InputPeerSelf{}
}
