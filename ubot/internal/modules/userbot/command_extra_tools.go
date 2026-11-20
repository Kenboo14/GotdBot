package userbot

import (
	"bytes"
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

var toolHTTPClient = &http.Client{Timeout: 12 * time.Second}

func (s *Service) resolveArgOrReply(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string) string {
	if text := strings.TrimSpace(args); text != "" {
		return text
	}
	if replyTxt, _, err := repliedText(ctx, client, target, msg); err == nil {
		return strings.TrimSpace(replyTxt)
	}
	return ""
}

func extractJoinTarget(raw string) (string, string) {
	s := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "https://"), "http://"), "/")
	if idx := strings.IndexAny(s, "?#"); idx >= 0 {
		s = s[:idx]
	}
	low := strings.ToLower(s)
	for _, p := range []string{"t.me/joinchat/", "telegram.me/joinchat/", "t.me/+", "telegram.me/+"} {
		if strings.HasPrefix(low, p) {
			return strings.TrimSpace(s[len(p):]), ""
		}
	}
	for _, p := range []string{"t.me/", "telegram.me/"} {
		if strings.HasPrefix(low, p) {
			s = s[len(p):]
			break
		}
	}
	s = strings.TrimPrefix(s, "@")
	if slash := strings.IndexByte(s, '/'); slash >= 0 {
		s = s[:slash]
	}
	if strings.HasPrefix(s, "+") {
		return strings.TrimPrefix(s, "+"), ""
	}
	return "", strings.TrimSpace(s)
}

func (s *Service) joinCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, args string, threadID int) error {
	raw := strings.Fields(args)
	if len(raw) == 0 {
		return sendUserText(ctx, client, target, "Gunakan: !join <@username|link_t.me>", threadID)
	}
	hash, uname := extractJoinTarget(raw[0])
	if hash != "" {
		if _, err := client.API().MessagesImportChatInvite(ctx, hash); err != nil {
			return sendUserText(ctx, client, target, "Gagal bergabung via link undangan: "+err.Error(), threadID)
		}
		return sendUserText(ctx, client, target, "Berhasil bergabung ke grup/channel.", threadID)
	}
	if uname == "" {
		return sendUserText(ctx, client, target, "Target username atau link undangan tidak valid.", threadID)
	}
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: uname})
	if err != nil || resolved == nil {
		return sendUserText(ctx, client, target, "Gagal menemukan @"+uname, threadID)
	}
	s.peerCache().ApplyUsersChats(resolved.Users, resolved.Chats)
	peerCh, ok := resolved.Peer.(*tg.PeerChannel)
	if !ok {
		return sendUserText(ctx, client, target, "@"+uname+" bukan grup atau channel publik.", threadID)
	}
	accessHash, _ := s.peerCache().ChannelHash(peerCh.ChannelID)
	if _, err := client.API().ChannelsJoinChannel(ctx, &tg.InputChannel{ChannelID: peerCh.ChannelID, AccessHash: accessHash}); err != nil {
		return sendUserText(ctx, client, target, "Gagal bergabung ke @"+uname+": "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Berhasil bergabung ke @"+uname, threadID)
}

func (s *Service) kickmeCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	var err error
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		_, err = client.API().ChannelsLeaveChannel(ctx, &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash})
	case *tg.InputPeerChat:
		_, err = client.API().MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{ChatID: p.ChatID, UserID: &tg.InputUserSelf{}})
	default:
		return sendUserText(ctx, client, target, "Perintah !kickme hanya dapat digunakan di grup atau channel.", threadID)
	}
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal keluar dari grup/channel: "+err.Error(), threadID)
	}
	return nil
}

func (s *Service) inviteCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, threadID int) error {
	_, label, inputUser, err := s.resolveTargetInputUser(ctx, client, target, msg, entities, args)
	if err != nil {
		return sendUserText(ctx, client, target, err.Error(), threadID)
	}
	switch p := target.(type) {
	case *tg.InputPeerChannel:
		_, err = client.API().ChannelsInviteToChannel(ctx, &tg.ChannelsInviteToChannelRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			Users:   []tg.InputUserClass{inputUser},
		})
	case *tg.InputPeerChat:
		_, err = client.API().MessagesAddChatUser(ctx, &tg.MessagesAddChatUserRequest{ChatID: p.ChatID, UserID: inputUser, FwdLimit: 20})
	default:
		return sendUserText(ctx, client, target, "Perintah !invite hanya dapat digunakan di dalam grup/channel.", threadID)
	}
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengundang pengguna: "+err.Error(), threadID)
	}
	return sendUserText(ctx, client, target, "Berhasil mengundang: "+label, threadID)
}

var smallCapsMap = map[rune]rune{
	'a': 'ᴀ', 'b': 'ʙ', 'c': 'ᴄ', 'd': 'ᴅ', 'e': 'ᴇ', 'f': 'ғ', 'g': 'ɢ',
	'h': 'ʜ', 'i': 'ɪ', 'j': 'ᴊ', 'k': 'ᴋ', 'l': 'ʟ', 'm': 'ᴍ', 'n': 'ɴ',
	'o': 'ᴏ', 'p': 'ᴘ', 'q': 'ǫ', 'r': 'ʀ', 's': 's', 't': 'ᴛ', 'u': 'ᴜ',
	'v': 'ᴠ', 'w': 'ᴡ', 'x': 'x', 'y': 'ʏ', 'z': 'ᴢ',
}

func mapFontRune(r, upperBase, lowerBase, digitBase rune) rune {
	switch {
	case r >= 'A' && r <= 'Z' && upperBase != 0:
		return upperBase + (r - 'A')
	case r >= 'a' && r <= 'z' && lowerBase != 0:
		return lowerBase + (r - 'a')
	case r >= '0' && r <= '9' && digitBase != 0:
		return digitBase + (r - '0')
	default:
		return r
	}
}

func applyUnicodeFont(style, text string) (string, bool) {
	norm := strings.ToLower(strings.TrimSpace(style))
	var out strings.Builder
	for _, r := range text {
		switch norm {
		case "bold":
			out.WriteRune(mapFontRune(r, 0x1D5D4, 0x1D5EE, 0x1D7EC))
		case "italic":
			out.WriteRune(mapFontRune(r, 0x1D608, 0x1D622, 0))
		case "mono":
			out.WriteRune(mapFontRune(r, 0x1D670, 0x1D68A, 0x1D7F6))
		case "script":
			out.WriteRune(mapFontRune(r, 0x1D4D0, 0x1D4EA, 0))
		case "double":
			out.WriteRune(mapFontRune(r, 0x1D538, 0x1D552, 0x1D7D8))
		case "bubble":
			out.WriteRune(mapFontRune(r, 0x24B6, 0x24D0, 0))
		case "square":
			out.WriteRune(mapFontRune(r, 0x1F130, 0x1F130, 0))
		case "smallcaps":
			low := r
			if r >= 'A' && r <= 'Z' {
				low = r + ('a' - 'A')
			}
			if mapped, ok := smallCapsMap[low]; ok {
				out.WriteRune(mapped)
			} else {
				out.WriteRune(r)
			}
		default:
			return "", false
		}
	}
	return out.String(), true
}

func reverseTextRunes(text string) string {
	runes := []rune(text)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func (s *Service) fontListCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	return sendUserText(ctx, client, target, "Daftar Gaya Font Unicode (!font <gaya> <teks>):\n1. bold\n2. italic\n3. mono\n4. script\n5. double\n6. smallcaps\n7. bubble\n8. square", threadID)
}

func (s *Service) fontCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return sendUserText(ctx, client, target, "Gunakan: !font <gaya> <teks>. Ketik !fontlist untuk melihat daftar gaya.", threadID)
	}
	style := fields[0]
	body := s.resolveArgOrReply(ctx, client, target, msg, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), style)))
	if body == "" {
		return sendUserText(ctx, client, target, "Berikan teks yang ingin diubah atau balas sebuah pesan.", threadID)
	}
	styled, ok := applyUnicodeFont(style, body)
	if !ok {
		return sendUserText(ctx, client, target, "Gaya font tidak dikenal. Ketik !fontlist untuk melihat pilihan gaya.", threadID)
	}
	return sendUserText(ctx, client, target, styled, threadID)
}

func (s *Service) mirrorCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	body := s.resolveArgOrReply(ctx, client, target, msg, args)
	if body == "" {
		return sendUserText(ctx, client, target, "Balas pesan teks atau tulis teks setelah !mirror.", threadID)
	}
	return sendUserText(ctx, client, target, reverseTextRunes(body), threadID)
}

func (s *Service) azanCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, args string, threadID int) error {
	city := strings.TrimSpace(args)
	if city == "" {
		city = "Jakarta"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := "https://api.aladhan.com/v1/timingsByCity?city=" + url.QueryEscape(city) + "&country=Indonesia&method=20"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal menyiapkan permintaan jadwal sholat: "+err.Error(), threadID)
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengambil jadwal sholat: "+err.Error(), threadID)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return sendUserText(ctx, client, target, fmt.Sprintf("Jadwal sholat untuk %s tidak ditemukan.", city), threadID)
	}
	var parsed struct {
		Code int `json:"code"`
		Data struct {
			Date    struct{ Readable string `json:"readable"` } `json:"date"`
			Timings struct {
				Fajr    string `json:"Fajr"`
				Dhuhr   string `json:"Dhuhr"`
				Asr     string `json:"Asr"`
				Maghrib string `json:"Maghrib"`
				Isha    string `json:"Isha"`
			} `json:"timings"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &parsed) != nil || parsed.Code != 200 {
		return sendUserText(ctx, client, target, "Jadwal sholat untuk "+city+" belum tersedia.", threadID)
	}
	t := parsed.Data.Timings
	out := fmt.Sprintf("Jadwal Sholat %s (%s):\nSubuh: %s\nDzuhur: %s\nAshar: %s\nMaghrib: %s\nIsya: %s",
		city, parsed.Data.Date.Readable, t.Fajr, t.Dhuhr, t.Asr, t.Maghrib, t.Isha)
	return sendUserText(ctx, client, target, out, threadID)
}

func (s *Service) aiCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	prompt := s.resolveArgOrReply(ctx, client, target, msg, args)
	if prompt == "" {
		return sendUserText(ctx, client, target, "Gunakan: !ai <pertanyaan> atau balas pesan teks.", threadID)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://text.pollinations.ai/"+url.PathEscape(prompt), nil)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal memproses AI request: "+err.Error(), threadID)
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return sendUserText(ctx, client, target, "Layanan AI sedang tidak dapat dihubungi: "+err.Error(), threadID)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	reply := strings.TrimSpace(string(raw))
	if err != nil || resp.StatusCode != http.StatusOK || reply == "" {
		return sendUserText(ctx, client, target, "Layanan AI belum mengembalikan jawaban yang valid.", threadID)
	}
	if len([]rune(reply)) > 3900 {
		reply = string([]rune(reply)[:3900]) + "..."
	}
	return sendUserText(ctx, client, target, reply, threadID)
}

func (s *Service) telegraphCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	body := s.resolveArgOrReply(ctx, client, target, msg, args)
	if body == "" {
		return sendUserText(ctx, client, target, "Balas pesan teks atau tuliskan isi setelah !telegraph.", threadID)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	postJSON := func(endpoint string, payload map[string]any) ([]byte, error) {
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := toolHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	}
	accRaw, err := postJSON("https://api.telegra.ph/createAccount", map[string]any{"short_name": "GotdBot", "author_name": "GotdBot UserBot"})
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal terhubung ke Telegraph: "+err.Error(), threadID)
	}
	var accParsed struct {
		OK     bool `json:"ok"`
		Result struct{ AccessToken string `json:"access_token"` } `json:"result"`
	}
	if json.Unmarshal(accRaw, &accParsed) != nil || !accParsed.OK || accParsed.Result.AccessToken == "" {
		return sendUserText(ctx, client, target, "Gagal membuat akun Telegraph sementara.", threadID)
	}
	pageRaw, err := postJSON("https://api.telegra.ph/createPage", map[string]any{
		"access_token": accParsed.Result.AccessToken,
		"title":        "GotdBot Note",
		"author_name":  "GotdBot UserBot",
		"content":      []map[string]any{{"tag": "p", "children": []string{body}}},
	})
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membuat halaman Telegraph: "+err.Error(), threadID)
	}
	var pageParsed struct {
		OK     bool `json:"ok"`
		Result struct{ URL string `json:"url"` } `json:"result"`
	}
	if json.Unmarshal(pageRaw, &pageParsed) != nil || !pageParsed.OK || pageParsed.Result.URL == "" {
		return sendUserText(ctx, client, target, "Gagal menerbitkan halaman ke Telegraph.", threadID)
	}
	return sendUserText(ctx, client, target, "Tautan Telegraph: "+pageParsed.Result.URL, threadID)
}

func (s *Service) quotlyCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, threadID int) error {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil || reply.ID <= 0 {
		return sendUserText(ctx, client, target, "Balas pesan yang ingin dijadikan stiker kutipan (!quotly).", threadID)
	}
	if botPeer, rErr := s.resolveBotPeerByUsername(ctx, client, "QuotLyBot"); rErr == nil {
		if _, fErr := client.API().MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
			FromPeer: target, ID: []int{reply.ID}, RandomID: []int64{randInt64()}, ToPeer: botPeer,
		}); fErr == nil {
			for attempt := 0; attempt < 5; attempt++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(700 * time.Millisecond):
				}
				hist, hErr := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: botPeer, Limit: 1})
				if hErr != nil {
					continue
				}
				for _, item := range extractMessagesSlice(hist) {
					m, ok := item.(*tg.Message)
					if !ok || m.Out {
						continue
					}
					if medDoc, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := medDoc.Document.(*tg.Document); ok {
							_, sErr := client.API().MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
								Peer:     target,
								Media:    &tg.InputMediaDocument{ID: &tg.InputDocument{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}},
								RandomID: randInt64(),
								ReplyTo:  buildReplyTo(threadID, commandReplyMsgID(ctx)),
							})
							if sErr == nil {
								return nil
							}
						}
					}
				}
			}
		}
	}
	quoteText := strings.TrimSpace(reply.Message)
	if quoteText == "" {
		return sendUserText(ctx, client, target, "QuotLy bot tidak merespons dan pesan tidak memiliki teks.", threadID)
	}
	return sendUserText(ctx, client, target, "Quote:\n\""+quoteText+"\"", threadID)
}
