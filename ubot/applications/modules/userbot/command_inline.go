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
	raw := client.API()
	inputBot, err := s.inlineBotUser(ctx, client)
	if err != nil {
		return err
	}
	results, err := raw.MessagesGetInlineBotResults(ctx, &tg.MessagesGetInlineBotResultsRequest{Bot: inputBot, Peer: target, Query: query, Offset: ""})
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

// inlineBotUser resolves the gateway bot once and reuses its input user for
// subsequent commands. Resolving the same bot on every !help added two extra
// network round trips to each command.
func (s *Service) inlineBotUser(ctx context.Context, client *telegram.Client) (*tg.InputUser, error) {
	username, err := s.botUsername(ctx)
	if err != nil {
		return nil, err
	}
	s.inlineMu.Lock()
	if cachedBot := s.inlineBots[client]; cachedBot != nil {
		cached := *cachedBot
		s.inlineMu.Unlock()
		return &cached, nil
	}
	s.inlineMu.Unlock()
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, err
	}
	botPeer, err := peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
	if err != nil {
		return nil, err
	}
	inputPeer, ok := botPeer.(*tg.InputPeerUser)
	if !ok {
		return nil, fmt.Errorf("BOT_TOKEN bukan akun bot")
	}
	resolvedBot := &tg.InputUser{UserID: inputPeer.UserID, AccessHash: inputPeer.AccessHash}
	s.inlineMu.Lock()
	if s.inlineBots == nil {
		s.inlineBots = make(map[*telegram.Client]*tg.InputUser)
	}
	if s.inlineBots[client] == nil {
		s.inlineBots[client] = resolvedBot
	}
	cached := *s.inlineBots[client]
	s.inlineMu.Unlock()
	return &cached, nil
}

func (s *Service) botUsername(ctx context.Context) (string, error) {
	if strings.TrimSpace(s.botToken) == "" {
		return "", fmt.Errorf("BOT_TOKEN belum dikonfigurasi")
	}
	s.inlineMu.Lock()
	if s.botUsernameCache != "" {
		username := s.botUsernameCache
		s.inlineMu.Unlock()
		return username, nil
	}
	s.inlineMu.Unlock()
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
	username := strings.TrimPrefix(strings.TrimSpace(answer.Result.Username), "@")
	s.inlineMu.Lock()
	if s.botUsernameCache == "" {
		s.botUsernameCache = username
	}
	username = s.botUsernameCache
	s.inlineMu.Unlock()
	return username, nil
}
