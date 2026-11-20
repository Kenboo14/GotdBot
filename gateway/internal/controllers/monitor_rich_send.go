package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message/rich"
	"github.com/gotd/td/tg"
)

var monitorHTTPClient = &http.Client{Timeout: 15 * time.Second}

func buildStripedRichMessage(htmlText string) (*tg.InputRichMessage, bool) {
	formatted := botgotd.FormatRichHTML(htmlText)
	blocks, err := rich.ParseHTML(strings.NewReader(formatted))
	if err != nil || len(blocks) == 0 {
		return nil, false
	}
	expandable := strings.Contains(htmlText, "<blockquote expandable")
	applyStripedRichStyle(blocks, expandable)
	return rich.New(blocks...).Input(), true
}

func applyStripedRichStyle(blocks []tg.PageBlockClass, expandable bool) {
	for _, blk := range blocks {
		switch b := blk.(type) {
		case *tg.PageBlockTable:
			b.Bordered = false
			b.Striped = true
			b.Compact = true
		case *tg.PageBlockBlockquote:
			if expandable {
				b.Collapsed = true
			}
		case *tg.PageBlockBlockquoteBlocks:
			applyStripedRichStyle(b.Blocks, expandable)
		case *tg.PageBlockDetails:
			applyStripedRichStyle(b.Blocks, expandable)
		}
	}
}

func parseMonitorChatID(target any) int64 {
	switch v := target.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0
		}
		return id
	default:
		return 0
	}
}

func isValidMTProtoPeer(chatID int64, peer tg.InputPeerClass) bool {
	if peer == nil {
		return false
	}
	if chatID < -1000000000000 {
		_, ok := peer.(*tg.InputPeerChannel)
		return ok
	}
	return true
}

func botAPIBaseURL(b *botgotd.Bot) string {
	if b != nil && strings.TrimSpace(b.ServerURL) != "" {
		return strings.TrimRight(b.ServerURL, "/")
	}
	return "https://api.telegram.org"
}

func resolveMonitorChatTarget(ctx context.Context, b *botgotd.Bot, target any) any {
	if id := parseMonitorChatID(target); id != 0 {
		return id
	}
	rawStr, ok := target.(string)
	if !ok {
		return target
	}
	rawStr = strings.TrimSpace(rawStr)
	if rawStr == "" || b == nil || (b.Token == "" && b.ServerURL == "") {
		return target
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("%s/bot%s/getChat?chat_id=%s", botAPIBaseURL(b), b.Token, url.QueryEscape(rawStr))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return target
	}
	resp, err := monitorHTTPClient.Do(req)
	if err != nil {
		return target
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return target
	}
	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
			ID int64 `json:"id"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.OK && parsed.Result.ID != 0 {
		return parsed.Result.ID
	}
	return target
}

func extractMTProtoMsgID(updates tg.UpdatesClass) int {
	switch u := updates.(type) {
	case *tg.Updates:
		return extractMsgIDFromUpdateSlice(u.Updates)
	case *tg.UpdatesCombined:
		return extractMsgIDFromUpdateSlice(u.Updates)
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.UpdateShortMessage:
		return u.ID
	default:
		return 0
	}
}

func extractMsgIDFromUpdateSlice(list []tg.UpdateClass) int {
	for _, upd := range list {
		switch m := upd.(type) {
		case *tg.UpdateNewMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		case *tg.UpdateNewChannelMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		case *tg.UpdateEditMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		case *tg.UpdateEditChannelMessage:
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		}
	}
	return 0
}

func randomMTProtoID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func postBotAPIJSON(ctx context.Context, b *botgotd.Bot, method string, body map[string]any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal %s body: %w", method, err)
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", botAPIBaseURL(b), b.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := monitorHTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do %s request: %w", method, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", method, err)
	}
	return resp.StatusCode, respBody, nil
}

func sendMonitorRichMessage(ctx context.Context, b *botgotd.Bot, target any, threadID int, htmlText string) (*botgotd.Message, error) {
	if b == nil {
		return nil, nil
	}
	chatID := parseMonitorChatID(target)
	if b.API != nil && b.Peers != nil && chatID != 0 {
		if peer := b.Peers.GetInputPeer(chatID); isValidMTProtoPeer(chatID, peer) {
			plainText, entities := botgotd.ParseHTML(htmlText)
			newReq := func(rm tg.InputRichMessageClass) *tg.MessagesSendMessageRequest {
				req := &tg.MessagesSendMessageRequest{
					Peer:     peer,
					Message:  plainText,
					Entities: entities,
					RandomID: randomMTProtoID(),
				}
				if threadID > 0 {
					req.SetReplyTo(&tg.InputReplyToMessage{
						ReplyToMsgID: threadID,
						TopMsgID:     threadID,
					})
				}
				req.SetRichMessage(rm)
				return req
			}
			if richMsg, ok := buildStripedRichMessage(htmlText); ok {
				updates, err := b.API.MessagesSendMessage(ctx, newReq(richMsg))
				if err == nil {
					return &botgotd.Message{ID: extractMTProtoMsgID(updates), Chat: botgotd.Chat{ID: chatID}}, nil
				}
				if _, isFlood := parseMonitorRetryAfter(err, time.Hour); isFlood || isPermanentTopicError(err) {
					return nil, err
				}
			}
			updates, err := b.API.MessagesSendMessage(ctx, newReq(rich.HTML(botgotd.FormatRichHTML(htmlText))))
			if err == nil {
				return &botgotd.Message{ID: extractMTProtoMsgID(updates), Chat: botgotd.Chat{ID: chatID}}, nil
			}
			if _, isFlood := parseMonitorRetryAfter(err, time.Hour); isFlood || isPermanentTopicError(err) {
				return nil, err
			}
		}
	}

	if b.Token != "" || b.ServerURL != "" {
		richBody := map[string]any{
			"chat_id": target,
			"rich_message": map[string]any{
				"html": botgotd.FormatRichHTML(htmlText),
			},
		}
		if threadID > 0 {
			richBody["message_thread_id"] = threadID
		}
		status, respBody, err := postBotAPIJSON(ctx, b, "sendRichMessage", richBody)
		if err == nil {
			var envelope struct {
				OK          bool            `json:"ok"`
				Description string          `json:"description"`
				Result      json.RawMessage `json:"result"`
			}
			_ = json.Unmarshal(respBody, &envelope)
			if status == http.StatusOK && (envelope.OK || len(respBody) == 0) {
				var msgRes struct {
					MessageID int `json:"message_id"`
				}
				_ = json.Unmarshal(envelope.Result, &msgRes)
				return &botgotd.Message{ID: msgRes.MessageID, Chat: botgotd.Chat{ID: chatID}}, nil
			}
			if status == http.StatusTooManyRequests || isPermanentTopicError(errors.New(envelope.Description)) {
				return nil, fmt.Errorf("sendRichMessage status %d: %s", status, strings.TrimSpace(string(respBody)))
			}
		}

		stdBody := map[string]any{
			"chat_id":    target,
			"text":       botgotd.CleanHTML(htmlText),
			"parse_mode": "HTML",
		}
		if threadID > 0 {
			stdBody["message_thread_id"] = threadID
		}
		status, respBody, err = postBotAPIJSON(ctx, b, "sendMessage", stdBody)
		if err != nil {
			return nil, fmt.Errorf("sendMessage http: %w", err)
		}
		var envelope struct {
			OK          bool            `json:"ok"`
			Description string          `json:"description"`
			Result      json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(respBody, &envelope)
		if status != http.StatusOK || !envelope.OK {
			return nil, fmt.Errorf("sendMessage status %d: %s", status, strings.TrimSpace(string(respBody)))
		}
		var msgRes struct {
			MessageID int `json:"message_id"`
		}
		_ = json.Unmarshal(envelope.Result, &msgRes)
		return &botgotd.Message{ID: msgRes.MessageID, Chat: botgotd.Chat{ID: chatID}}, nil
	}

	return nil, fmt.Errorf("bot transport not configured")
}

func editMonitorRichMessage(ctx context.Context, b *botgotd.Bot, target any, msgID int, htmlText string) (*botgotd.Message, error) {
	if b == nil {
		return nil, nil
	}
	chatID := parseMonitorChatID(target)
	if b.API != nil && b.Peers != nil && chatID != 0 {
		if peer := b.Peers.GetInputPeer(chatID); isValidMTProtoPeer(chatID, peer) {
			plainText, entities := botgotd.ParseHTML(htmlText)
			newReq := func(rm tg.InputRichMessageClass) *tg.MessagesEditMessageRequest {
				req := &tg.MessagesEditMessageRequest{
					Peer:     peer,
					ID:       msgID,
					Message:  plainText,
					Entities: entities,
				}
				req.SetRichMessage(rm)
				return req
			}
			if richMsg, ok := buildStripedRichMessage(htmlText); ok {
				if _, err := b.API.MessagesEditMessage(ctx, newReq(richMsg)); err == nil {
					return &botgotd.Message{ID: msgID, Chat: botgotd.Chat{ID: chatID}}, nil
				} else if _, isFlood := parseMonitorRetryAfter(err, time.Hour); isFlood || isMessageNotModifiedError(err) || isMessageNotFoundError(err) {
					return nil, err
				}
			}
			if _, err := b.API.MessagesEditMessage(ctx, newReq(rich.HTML(botgotd.FormatRichHTML(htmlText)))); err == nil {
				return &botgotd.Message{ID: msgID, Chat: botgotd.Chat{ID: chatID}}, nil
			} else if _, isFlood := parseMonitorRetryAfter(err, time.Hour); isFlood || isMessageNotModifiedError(err) || isMessageNotFoundError(err) {
				return nil, err
			}
		}
	}
	return b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
		ChatID:    target,
		MessageID: msgID,
		Text:      htmlText,
		ParseMode: botgotd.ParseModeHTML,
	})
}
