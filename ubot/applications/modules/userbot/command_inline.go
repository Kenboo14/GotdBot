package userbot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func (s *Service) sendInlineHelp(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	return s.sendInlineQuery(ctx, client, target, "assist_help")
}

func (s *Service) sendInlineQuery(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, query string) error {
	username, err := s.botUsername(ctx)
	if err != nil {
		return err
	}
	raw := client.API()
	resolved, err := raw.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return err
	}
	botPeer, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
	if err != nil {
		return err
	}
	inputPeer, ok := botPeer.(*tg.InputPeerUser)
	if !ok {
		return fmt.Errorf("BOT_TOKEN bukan akun bot")
	}
	results, err := raw.MessagesGetInlineBotResults(ctx, &tg.MessagesGetInlineBotResultsRequest{Bot: &tg.InputUser{UserID: inputPeer.UserID, AccessHash: inputPeer.AccessHash}, Peer: target, Query: query, Offset: ""})
	if err != nil {
		return err
	}
	if len(results.Results) == 0 {
		return fmt.Errorf("bot tidak mengembalikan hasil inline")
	}
	result, ok := results.Results[0].(*tg.BotInlineResult)
	if !ok {
		return fmt.Errorf("format hasil inline bot tidak didukung")
	}
	var randomIDBytes [8]byte
	if _, err := rand.Read(randomIDBytes[:]); err != nil {
		return err
	}
	_, err = raw.MessagesSendInlineBotResult(ctx, &tg.MessagesSendInlineBotResultRequest{Peer: target, RandomID: int64(binary.LittleEndian.Uint64(randomIDBytes[:])), QueryID: results.QueryID, ID: result.ID})
	return err
}

func (s *Service) botUsername(ctx context.Context) (string, error) {
	if strings.TrimSpace(s.botToken) == "" {
		return "", fmt.Errorf("BOT_TOKEN belum dikonfigurasi")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+s.botToken+"/getMe", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var answer struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", err
	}
	if !answer.OK || strings.TrimSpace(answer.Result.Username) == "" {
		return "", fmt.Errorf("BOT_TOKEN tidak dapat di-resolve")
	}
	return strings.TrimPrefix(strings.TrimSpace(answer.Result.Username), "@"), nil
}
