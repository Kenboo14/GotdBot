package userbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func (s *Service) resolveAndJoinManualGroupToken(
	ctx context.Context,
	client *gobot.Client,
	tok string,
	cachedChannels map[int64]*tg.Channel,
	cachedChats map[int64]*tg.Chat,
) manualGroupOutcome {
	if canonChID, ok := extractTelegramCChannelID(tok); ok {
		return s.resolveNumericGroupToken(ctx, client, strconv.FormatInt(canonChID, 10), canonChID, cachedChannels, cachedChats)
	}
	if numID, err := strconv.ParseInt(tok, 10, 64); err == nil && numID != 0 {
		return s.resolveNumericGroupToken(ctx, client, tok, numID, cachedChannels, cachedChats)
	}

	inviteHash, uname := extractJoinTarget(tok)
	if inviteHash != "" {
		return s.resolveInviteGroupToken(ctx, client, tok, inviteHash)
	}
	if uname == "" {
		uname = strings.TrimPrefix(tok, "@")
	}
	if uname == "" || strings.Contains(uname, "/") {
		return manualGroupOutcome{Target: tok, ErrMsg: "Format target tidak dikenali"}
	}
	return s.resolveUsernameGroupToken(ctx, client, tok, uname)
}

func (s *Service) resolveInviteGroupToken(ctx context.Context, client *gobot.Client, rawTok, hash string) manualGroupOutcome {
	inv, err := client.API().MessagesCheckChatInvite(ctx, hash)
	if err != nil {
		return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Invite tidak valid: %v", err)}
	}

	switch v := inv.(type) {
	case *tg.ChatInviteAlready:
		doc := groupDocFromChatClass(v.Chat)
		if doc == nil {
			return manualGroupOutcome{Target: rawTok, ErrMsg: "Tipe chat tidak didukung"}
		}
		return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Sudah bergabung"}

	case *tg.ChatInvitePeek:
		updates, impErr := client.API().MessagesImportChatInvite(ctx, hash)
		if impErr != nil && !tgerr.Is(impErr, "USER_ALREADY_PARTICIPANT") {
			return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Gagal auto-join: %v", impErr)}
		}
		if impErr == nil {
			doc, needsWebView := groupDocFromJoinResult(updates)
			if needsWebView {
				return manualGroupOutcome{Target: rawTok, ErrMsg: "Grup membutuhkan verifikasi WebView sebelum join"}
			}
			if doc != nil {
				return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Berhasil auto-join"}
			}
		}
		if doc := groupDocFromChatClass(v.Chat); doc != nil {
			return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Sudah bergabung"}
		}
		return manualGroupOutcome{Target: rawTok, ErrMsg: "Gagal membaca metadata chat setelah join"}

	case *tg.ChatInvite:
		if v.RequestNeeded {
			return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Grup %q membutuhkan persetujuan admin (join request)", v.Title)}
		}
		updates, impErr := client.API().MessagesImportChatInvite(ctx, hash)
		if impErr != nil {
			if tgerr.Is(impErr, "USER_ALREADY_PARTICIPANT") {
				if reInv, rErr := client.API().MessagesCheckChatInvite(ctx, hash); rErr == nil {
					if already, ok := reInv.(*tg.ChatInviteAlready); ok {
						if doc := groupDocFromChatClass(already.Chat); doc != nil {
							return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Sudah bergabung"}
						}
					}
				}
			}
			return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Gagal auto-join: %v", impErr)}
		}
		doc, needsWebView := groupDocFromJoinResult(updates)
		if needsWebView {
			return manualGroupOutcome{Target: rawTok, ErrMsg: "Grup membutuhkan verifikasi WebView sebelum join"}
		}
		if doc != nil {
			return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Berhasil auto-join"}
		}
		if reInv, rErr := client.API().MessagesCheckChatInvite(ctx, hash); rErr == nil {
			if already, ok := reInv.(*tg.ChatInviteAlready); ok {
				if doc := groupDocFromChatClass(already.Chat); doc != nil {
					return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Berhasil auto-join"}
				}
			}
		}
		return manualGroupOutcome{Target: rawTok, ErrMsg: "Auto-join berhasil tetapi gagal mengambil detail grup"}
	}

	return manualGroupOutcome{Target: rawTok, ErrMsg: "Respon invite link tidak dikenal"}
}

func (s *Service) resolveUsernameGroupToken(ctx context.Context, client *gobot.Client, rawTok, uname string) manualGroupOutcome {
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: uname})
	if err != nil {
		return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Gagal resolve @%s: %v", uname, err)}
	}
	s.peerCache().ApplyUsersChats(resolved.Users, resolved.Chats)

	switch p := resolved.Peer.(type) {
	case *tg.PeerUser:
		return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("@%s adalah akun pengguna, bukan grup/channel", uname)}

	case *tg.PeerChat:
		for _, c := range resolved.Chats {
			if chat, ok := c.(*tg.Chat); ok && chat.ID == p.ChatID {
				doc := groupDocFromChatClass(chat)
				return manualGroupOutcome{Target: rawTok, Group: doc, Status: "Sudah bergabung"}
			}
		}
		canon := canonicalGroupID("chat", p.ChatID)
		return manualGroupOutcome{
			Target: rawTok,
			Group: &jasebGroupDoc{
				Target:   strconv.FormatInt(canon, 10),
				Title:    "@" + uname,
				ChatID:   canon,
				PeerType: "chat",
			},
			Status: "Tersimpan",
		}

	case *tg.PeerChannel:
		ch := extractChannelFromSlice(resolved.Chats, p.ChannelID)
		if ch == nil {
			return manualGroupOutcome{Target: rawTok, ErrMsg: "Metadata channel tidak ditemukan"}
		}
		status := "Sudah bergabung"
		if ch.Left {
			updates, jErr := client.API().ChannelsJoinChannel(ctx, &tg.InputChannel{
				ChannelID:  ch.ID,
				AccessHash: ch.AccessHash,
			})
			if jErr != nil && !tgerr.Is(jErr, "USER_ALREADY_PARTICIPANT") {
				return manualGroupOutcome{Target: rawTok, ErrMsg: fmt.Sprintf("Gagal auto-join: %v", jErr)}
			}
			if jErr == nil {
				status = "Berhasil auto-join"
				joinedDoc, needsWebView := groupDocFromJoinResult(updates)
				if needsWebView {
					return manualGroupOutcome{Target: rawTok, ErrMsg: "Grup membutuhkan verifikasi WebView sebelum join"}
				}
				if joinedDoc != nil {
					return manualGroupOutcome{Target: rawTok, Group: joinedDoc, Status: status}
				}
			}
		}
		doc := groupDocFromChatClass(ch)
		return manualGroupOutcome{Target: rawTok, Group: doc, Status: status}
	}

	return manualGroupOutcome{Target: rawTok, ErrMsg: "Target username tidak merujuk ke grup/channel"}
}

func (s *Service) resolveNumericGroupToken(
	ctx context.Context,
	client *gobot.Client,
	rawTok string,
	numID int64,
	cachedChannels map[int64]*tg.Channel,
	cachedChats map[int64]*tg.Chat,
) manualGroupOutcome {
	isChannelPrefix := strings.HasPrefix(rawTok, "-100")
	rawNorm := normalizePeerChannelID(numID)

	if isChannelPrefix {
		if ch := cachedChannels[rawNorm]; ch != nil {
			return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(ch), Status: "Sudah bergabung"}
		}
		if hash, ok := s.peerCache().ChannelHash(rawNorm); ok && hash != 0 {
			if res, err := client.API().ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: rawNorm, AccessHash: hash}}); err == nil && res != nil {
				if ch := extractChannelFromSlice(res.GetChats(), rawNorm); ch != nil {
					return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(ch), Status: "Terverifikasi"}
				}
			}
		}
		canon := canonicalGroupID("channel", rawNorm)
		canonStr := strconv.FormatInt(canon, 10)
		return manualGroupOutcome{
			Target: rawTok,
			Group: &jasebGroupDoc{
				Target:   canonStr,
				Title:    "Grup " + canonStr,
				ChatID:   canon,
				PeerType: "channel",
			},
			Status: "Tersimpan (pastikan userbot sudah bergabung)",
		}
	}

	if numID < 0 {
		if c := cachedChats[rawNorm]; c != nil {
			return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(c), Status: "Sudah bergabung"}
		}
		if chatsRes, err := client.API().MessagesGetChats(ctx, []int64{rawNorm}); err == nil && chatsRes != nil {
			for _, item := range chatsRes.GetChats() {
				if c, ok := item.(*tg.Chat); ok && c.ID == rawNorm {
					return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(c), Status: "Terverifikasi"}
				}
			}
		}
		canon := canonicalGroupID("chat", rawNorm)
		canonStr := strconv.FormatInt(canon, 10)
		return manualGroupOutcome{
			Target: rawTok,
			Group: &jasebGroupDoc{
				Target:   canonStr,
				Title:    "Grup " + canonStr,
				ChatID:   canon,
				PeerType: "chat",
			},
			Status: "Tersimpan (group dasar)",
		}
	}

	if ch := cachedChannels[numID]; ch != nil {
		return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(ch), Status: "Sudah bergabung"}
	}
	if c := cachedChats[numID]; c != nil {
		return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(c), Status: "Sudah bergabung"}
	}
	if hash, ok := s.peerCache().ChannelHash(numID); ok && hash != 0 {
		if res, err := client.API().ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: numID, AccessHash: hash}}); err == nil && res != nil {
			if ch := extractChannelFromSlice(res.GetChats(), numID); ch != nil {
				return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(ch), Status: "Terverifikasi"}
			}
		}
	}
	if chatsRes, err := client.API().MessagesGetChats(ctx, []int64{numID}); err == nil && chatsRes != nil {
		for _, item := range chatsRes.GetChats() {
			if c, ok := item.(*tg.Chat); ok && c.ID == numID {
				return manualGroupOutcome{Target: rawTok, Group: groupDocFromChatClass(c), Status: "Terverifikasi"}
			}
		}
	}

	canon := canonicalGroupID("channel", numID)
	if numID < 1000000000 {
		canon = canonicalGroupID("chat", numID)
	}
	peerType := "chat"
	if canon != -numID {
		peerType = "channel"
	}
	canonStr := strconv.FormatInt(canon, 10)
	return manualGroupOutcome{
		Target: rawTok,
		Group: &jasebGroupDoc{
			Target:   canonStr,
			Title:    "Grup " + canonStr,
			ChatID:   canon,
			PeerType: peerType,
		},
		Status: "Tersimpan (belum terverifikasi di dialog)",
	}
}
