package botgotd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message/rich"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
)

type registeredHandler struct {
	hType   HandlerType
	pattern string
	mType   MatchType
	handler HandlerFunc
}

type registeredMatchFunc struct {
	match   func(u *Update) bool
	handler HandlerFunc
}

type Bot struct {
	Client     *telegram.Client
	API        *tg.Client
	Sender     *message.Sender
	Peers      *PeerStorage
	Token      string
	ServerURL  string
	Dispatcher tg.UpdateDispatcher

	mu         sync.RWMutex
	handlers   []registeredHandler
	matchFuncs []registeredMatchFunc
	httpClient *http.Client
}

func (b *Bot) apiURL(endpoint string) string {
	base := b.ServerURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	return fmt.Sprintf("%s/bot%s/%s", strings.TrimRight(base, "/"), b.Token, endpoint)
}

func NewBot(client *telegram.Client, token string, peersStorage *PeerStorage) *Bot {
	var api *tg.Client
	var sender *message.Sender
	if client != nil {
		api = tg.NewClient(client)
		sender = message.NewSender(api)
	}
	transport := &http.Transport{
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &Bot{
		Client:     client,
		API:        api,
		Sender:     sender,
		Peers:      peersStorage,
		Token:      token,
		Dispatcher: tg.NewUpdateDispatcher(),
		httpClient: &http.Client{
			Timeout:   35 * time.Second,
			Transport: transport,
		},
	}
}

func NewTestBot(serverURL string, token string) *Bot {
	b := NewBot(nil, token, nil)
	b.ServerURL = serverURL
	return b
}

func (b *Bot) RegisterHandler(hType HandlerType, pattern string, mType MatchType, handler HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, registeredHandler{
		hType:   hType,
		pattern: pattern,
		mType:   mType,
		handler: handler,
	})
}

func (b *Bot) RegisterHandlerMatchFunc(match func(u *Update) bool, handler HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.matchFuncs = append(b.matchFuncs, registeredMatchFunc{
		match:   match,
		handler: handler,
	})
}

func (b *Bot) DispatchUpdate(ctx context.Context, u *Update) {
	if u == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 1. Try matchFuncs first
	for _, mf := range b.matchFuncs {
		if mf.match(u) {
			go mf.handler(ctx, b, u)
			return
		}
	}

	// 2. Try callback query
	if u.CallbackQuery != nil {
		data := u.CallbackQuery.Data
		for _, rh := range b.handlers {
			if rh.hType != HandlerTypeCallbackQueryData {
				continue
			}
			matched := false
			if rh.mType == MatchTypeExact && data == rh.pattern {
				matched = true
			} else if rh.mType == MatchTypePrefix && strings.HasPrefix(data, rh.pattern) {
				matched = true
			}
			if matched {
				go rh.handler(ctx, b, u)
				return
			}
		}
	}

	// 2b. Try inline query
	if u.InlineQuery != nil {
		for _, rh := range b.handlers {
			if rh.hType != HandlerTypeInlineQuery {
				continue
			}
			go rh.handler(ctx, b, u)
			return
		}
	}

	// 3. Try message text
	if u.Message != nil && u.Message.Text != "" {
		text := u.Message.Text
		for _, rh := range b.handlers {
			if rh.hType != HandlerTypeMessageText {
				continue
			}
			matched := false
			if rh.pattern == "" {
				matched = true
			} else if rh.mType == MatchTypeExact && text == rh.pattern {
				matched = true
			} else if rh.mType == MatchTypePrefix && strings.HasPrefix(text, rh.pattern) {
				matched = true
			}
			if matched {
				go rh.handler(ctx, b, u)
				return
			}
		}
	}
}

func parseChatID(val any) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	default:
		return 0
	}
}

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func (b *Bot) parseHTMLToEntities(rawHTML string) (string, []tg.MessageEntityClass) {
	text := cleanHTML(rawHTML)
	if !strings.Contains(text, "<") || !strings.Contains(text, ">") {
		return text, nil
	}
	var builder entity.Builder
	err := styling.Perform(&builder, html.String(nil, text))
	if err != nil {
		return text, nil
	}
	plain, entities := builder.Complete()
	return plain, entities
}

func convertReplyMarkup(markup any) tg.ReplyMarkupClass {
	if markup == nil {
		return nil
	}
	switch m := markup.(type) {
	case tg.ReplyMarkupClass:
		return m
	case *InlineKeyboardMarkup:
		if m == nil {
			return nil
		}
		var rows []tg.KeyboardInlineButtonRow
		for _, row := range m.InlineKeyboard {
			var buttons []tg.KeyboardInlineButton
			for _, btn := range row {
				var btnType tg.InlineButtonTypeClass
				if btn.CallbackData != "" {
					btnType = &tg.InlineButtonTypeCallback{Data: []byte(btn.CallbackData)}
				} else if btn.URL != "" {
					btnType = &tg.InlineButtonTypeURL{URL: btn.URL}
				} else if btn.SwitchInlineQueryCurrentChat != "" {
					btnType = &tg.InlineButtonTypeSwitchInline{
						SamePeer: true,
						Query:    btn.SwitchInlineQueryCurrentChat,
					}
				}
				if btnType != nil {
					buttons = append(buttons, tg.KeyboardInlineButton{
						Text: btn.Text,
						Type: btnType,
					})
				}
			}
			if len(buttons) > 0 {
				rows = append(rows, tg.KeyboardInlineButtonRow{Buttons: buttons})
			}
		}
		return &tg.ReplyInlineMarkup{Rows: rows}
	case *ReplyKeyboardMarkup:
		if m == nil {
			return nil
		}
		var rows []tg.KeyboardButtonRow
		for _, row := range m.Keyboard {
			var buttons []tg.KeyboardButton
			for _, btn := range row {
				var btnType tg.ButtonTypeClass
				if btn.RequestContact {
					btnType = &tg.ButtonTypeRequestPhone{}
				} else {
					btnType = &tg.ButtonTypeDefault{}
				}
				buttons = append(buttons, tg.KeyboardButton{
					Text: btn.Text,
					Type: btnType,
				})
			}
			if len(buttons) > 0 {
				rows = append(rows, tg.KeyboardButtonRow{Buttons: buttons})
			}
		}
		return &tg.ReplyKeyboardMarkup{
			Resize:    m.ResizeKeyboard,
			SingleUse: m.OneTimeKeyboard,
			Rows:      rows,
		}
	case *ReplyKeyboardRemove:
		if m == nil {
			return nil
		}
		return &tg.ReplyKeyboardHide{}
	}
	return nil
}

func extractMsgIDFromUpdates(updates tg.UpdatesClass) int {
	if updates == nil {
		return 0
	}
	switch u := updates.(type) {
	case *tg.Updates:
		for _, upd := range u.Updates {
			switch m := upd.(type) {
			case *tg.UpdateNewMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					return msg.ID
				}
			case *tg.UpdateNewChannelMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					return msg.ID
				}
			}
		}
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.UpdateShortMessage:
		return u.ID
	case *tg.UpdatesCombined:
		for _, upd := range u.Updates {
			switch m := upd.(type) {
			case *tg.UpdateNewMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					return msg.ID
				}
			case *tg.UpdateNewChannelMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					return msg.ID
				}
			}
		}
	}
	return 0
}

func (b *Bot) SendMessage(ctx context.Context, params *SendMessageParams) (*Message, error) {
	if params == nil {
		return nil, fmt.Errorf("params cannot be nil")
	}
	chatID := parseChatID(params.ChatID)
	if chatID == 0 {
		return nil, fmt.Errorf("invalid chatID")
	}

	if b.API != nil && b.Peers != nil {
		peer := b.Peers.GetInputPeer(chatID)
		if peer != nil {
			plainText, entities := b.parseHTMLToEntities(params.Text)
			markup := convertReplyMarkup(params.ReplyMarkup)
			req := &tg.MessagesSendMessageRequest{
				Peer:        peer,
				Message:     plainText,
				Entities:    entities,
				ReplyMarkup: markup,
				RandomID:    randomID(),
			}
			if params.ReplyParameters != nil && params.ReplyParameters.MessageID > 0 {
				req.SetReplyTo(&tg.InputReplyToMessage{
					ReplyToMsgID: params.ReplyParameters.MessageID,
				})
			}
			if isRichContent(params.Text) {
				req.SetRichMessage(rich.HTML(FormatRichHTML(params.Text)))
			}
			updates, err := b.API.MessagesSendMessage(ctx, req)
			if err == nil {
				msgID := extractMsgIDFromUpdates(updates)
				return &Message{ID: msgID, Chat: Chat{ID: chatID}}, nil
			}
			log.Printf("[botgotd] MessagesSendMessage MTProto error: %v, falling back to HTTP", err)
		}
	}

	// If text contains Rich Message HTML tags, send via Rich Message endpoint
	if isRichContent(params.Text) {
		msgID, err := b.SendRichMessage(ctx, chatID, FormatRichHTML(params.Text), params.ReplyMarkup)
		if err == nil {
			return &Message{ID: msgID, Chat: Chat{ID: chatID}}, nil
		}
		log.Printf("[botgotd] sendRichMessage fallback: %v", err)
	}

	// Telegram Bot API standard fallback or MTProto
	url := b.apiURL("sendMessage")
	body := map[string]any{
		"chat_id": chatID,
		"text":    cleanHTML(params.Text),
	}
	if params.ParseMode != "" {
		body["parse_mode"] = string(params.ParseMode)
	} else if strings.Contains(params.Text, "<") && strings.Contains(params.Text, ">") {
		body["parse_mode"] = "HTML"
	}
	if params.ReplyMarkup != nil {
		body["reply_markup"] = params.ReplyMarkup
	}
	if params.ReplyParameters != nil {
		body["reply_parameters"] = params.ReplyParameters
	}

	return b.postHTTPMessage(ctx, url, body)
}

func (b *Bot) SendRichMessage(ctx context.Context, chatID int64, htmlContent string, markup any) (int, error) {
	url := b.apiURL("sendRichMessage")
	body := map[string]any{
		"chat_id": chatID,
		"rich_message": map[string]any{
			"html": FormatRichHTML(htmlContent),
		},
	}
	if markup != nil {
		body["reply_markup"] = markup
	}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	var resp *http.Response
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = b.httpClient.Do(req)
		if err == nil {
			break
		}
		if attempt == 2 || ctx.Err() != nil {
			return 0, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("sendRichMessage status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(respBody, &res)
	return res.Result.MessageID, nil
}

func (b *Bot) CopyMessage(ctx context.Context, params *CopyMessageParams) (*MessageId, error) {
	url := b.apiURL("copyMessage")
	body := map[string]any{
		"chat_id":      params.ChatID,
		"from_chat_id": params.FromChatID,
		"message_id":   params.MessageID,
	}
	if params.Caption != "" {
		body["caption"] = params.Caption
	}
	if params.ParseMode != "" {
		body["parse_mode"] = string(params.ParseMode)
	}
	if params.ReplyMarkup != nil {
		body["reply_markup"] = params.ReplyMarkup
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copyMessage status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool      `json:"ok"`
		Result MessageId `json:"result"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, err
	}
	return &res.Result, nil
}

func (b *Bot) CopyMessages(ctx context.Context, params *CopyMessagesParams) ([]MessageId, error) {
	url := b.apiURL("copyMessages")
	body := map[string]any{
		"chat_id":      params.ChatID,
		"from_chat_id": params.FromChatID,
		"message_ids":  params.MessageIDs,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copyMessages status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool        `json:"ok"`
		Result []MessageId `json:"result"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, err
	}
	return res.Result, nil
}

func (b *Bot) EditMessageText(ctx context.Context, params *EditMessageTextParams) (*Message, error) {
	if params == nil {
		return nil, fmt.Errorf("params cannot be nil")
	}

	if b.API != nil {
		plainText, entities := b.parseHTMLToEntities(params.Text)
		markup := convertReplyMarkup(params.ReplyMarkup)

		// 1. Inline Message edit via MTProto
		if params.RawInlineMessageID != nil {
			req := &tg.MessagesEditInlineBotMessageRequest{
				ID:          params.RawInlineMessageID,
				Message:     plainText,
				Entities:    entities,
				ReplyMarkup: markup,
			}
			if isRichContent(params.Text) {
				req.SetRichMessage(rich.HTML(FormatRichHTML(params.Text)))
			}
			_, err := b.API.MessagesEditInlineBotMessage(ctx, req)
			if err == nil {
				return &Message{}, nil
			}
			log.Printf("[botgotd] MessagesEditInlineBotMessage error: %v", err)
			return nil, err
		}

		// 2. Chat Message edit via MTProto
		chatID := parseChatID(params.ChatID)
		if chatID != 0 && params.MessageID != 0 && b.Peers != nil {
			peer := b.Peers.GetInputPeer(chatID)
			if peer != nil {
				req := &tg.MessagesEditMessageRequest{
					Peer:        peer,
					ID:          params.MessageID,
					Message:     plainText,
					Entities:    entities,
					ReplyMarkup: markup,
				}
				if isRichContent(params.Text) {
					req.SetRichMessage(rich.HTML(FormatRichHTML(params.Text)))
				}
				_, err := b.API.MessagesEditMessage(ctx, req)
				if err == nil {
					return &Message{ID: params.MessageID, Chat: Chat{ID: chatID}}, nil
				}
				log.Printf("[botgotd] MessagesEditMessage error: %v", err)
				return nil, err
			}
		}
	}

	url := b.apiURL("editMessageText")
	body := map[string]any{
		"text": cleanHTML(params.Text),
	}
	if params.InlineMessageID != "" {
		body["inline_message_id"] = params.InlineMessageID
	} else {
		body["chat_id"] = params.ChatID
		body["message_id"] = params.MessageID
	}
	if params.ParseMode != "" {
		body["parse_mode"] = string(params.ParseMode)
	} else if strings.Contains(params.Text, "<") && strings.Contains(params.Text, ">") {
		body["parse_mode"] = "HTML"
	}
	if params.ReplyMarkup != nil {
		body["reply_markup"] = params.ReplyMarkup
	}
	return b.postHTTPMessage(ctx, url, body)
}

func (b *Bot) EditMessageReplyMarkup(ctx context.Context, params *EditMessageReplyMarkupParams) (*Message, error) {
	if params == nil {
		return nil, fmt.Errorf("params cannot be nil")
	}

	if b.API != nil {
		markup := convertReplyMarkup(params.ReplyMarkup)
		if params.RawInlineMessageID != nil {
			_, err := b.API.MessagesEditInlineBotMessage(ctx, &tg.MessagesEditInlineBotMessageRequest{
				ID:          params.RawInlineMessageID,
				ReplyMarkup: markup,
			})
			if err == nil {
				return &Message{}, nil
			}
		}
		chatID := parseChatID(params.ChatID)
		if chatID != 0 && params.MessageID != 0 && b.Peers != nil {
			peer := b.Peers.GetInputPeer(chatID)
			if peer != nil {
				_, err := b.API.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
					Peer:        peer,
					ID:          params.MessageID,
					ReplyMarkup: markup,
				})
				if err == nil {
					return &Message{ID: params.MessageID, Chat: Chat{ID: chatID}}, nil
				}
			}
		}
	}

	url := b.apiURL("editMessageReplyMarkup")
	body := map[string]any{}
	if params.InlineMessageID != "" {
		body["inline_message_id"] = params.InlineMessageID
	} else {
		body["chat_id"] = params.ChatID
		body["message_id"] = params.MessageID
	}
	if params.ReplyMarkup != nil {
		body["reply_markup"] = params.ReplyMarkup
	}
	return b.postHTTPMessage(ctx, url, body)
}

func (b *Bot) DeleteMessage(ctx context.Context, params *DeleteMessageParams) (bool, error) {
	if params == nil {
		return false, fmt.Errorf("params cannot be nil")
	}
	return b.DeleteMessages(ctx, &DeleteMessagesParams{
		ChatID:     params.ChatID,
		MessageIDs: []int{params.MessageID},
	})
}

func (b *Bot) DeleteMessages(ctx context.Context, params *DeleteMessagesParams) (bool, error) {
	if params == nil || len(params.MessageIDs) == 0 {
		return false, fmt.Errorf("params or message_ids cannot be empty")
	}

	if b.API != nil && b.Peers != nil {
		chatID := parseChatID(params.ChatID)
		peer := b.Peers.GetInputPeer(chatID)
		if ch, ok := peer.(*tg.InputPeerChannel); ok {
			_, err := b.API.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
				ID:      params.MessageIDs,
			})
			if err == nil {
				return true, nil
			}
		} else {
			_, err := b.API.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
				ID: params.MessageIDs,
			})
			if err == nil {
				return true, nil
			}
		}
	}

	url := b.apiURL("deleteMessages")
	body := map[string]any{
		"chat_id":     params.ChatID,
		"message_ids": params.MessageIDs,
	}
	return b.postHTTPBool(ctx, url, body)
}

func (b *Bot) AnswerCallbackQuery(ctx context.Context, params *AnswerCallbackQueryParams) (bool, error) {
	if params == nil {
		return false, fmt.Errorf("params cannot be nil")
	}

	if b.API != nil {
		qID, err := strconv.ParseInt(params.CallbackQueryID, 10, 64)
		if err == nil {
			req := &tg.MessagesSetBotCallbackAnswerRequest{
				QueryID: qID,
				Alert:   params.ShowAlert,
			}
			if params.Text != "" {
				req.SetMessage(params.Text)
			}
			if params.URL != "" {
				req.SetURL(params.URL)
			}
			_, rpcErr := b.API.MessagesSetBotCallbackAnswer(ctx, req)
			if rpcErr == nil {
				return true, nil
			}
			log.Printf("[botgotd] MessagesSetBotCallbackAnswer error: %v", rpcErr)
			return false, rpcErr
		}
	}

	url := b.apiURL("answerCallbackQuery")
	body := map[string]any{
		"callback_query_id": params.CallbackQueryID,
	}
	if params.Text != "" {
		body["text"] = params.Text
	}
	if params.ShowAlert {
		body["show_alert"] = true
	}
	if params.URL != "" {
		body["url"] = params.URL
	}
	return b.postHTTPBool(ctx, url, body)
}

func (b *Bot) AnswerInlineQuery(ctx context.Context, params *AnswerInlineQueryParams) (bool, error) {
	if params == nil {
		return false, fmt.Errorf("params cannot be nil")
	}
	url := b.apiURL("answerInlineQuery")
	var formattedResults []any
	for _, res := range params.Results {
		if art, ok := res.(*InlineQueryResultArticle); ok {
			resMap := map[string]any{
				"type":                  "article",
				"id":                    art.ID,
				"title":                 art.Title,
				"input_message_content": art.InputMessageContent,
			}
			if art.Description != "" {
				resMap["description"] = art.Description
			}
			if art.ReplyMarkup != nil {
				resMap["reply_markup"] = art.ReplyMarkup
			}
			formattedResults = append(formattedResults, resMap)
		} else {
			formattedResults = append(formattedResults, res)
		}
	}
	body := map[string]any{
		"inline_query_id": params.InlineQueryID,
		"results":         formattedResults,
		"cache_time":      params.CacheTime,
		"is_personal":     params.IsPersonal,
	}
	return b.postHTTPBool(ctx, url, body)
}

func (b *Bot) postHTTPMessage(ctx context.Context, url string, body map[string]any) (*Message, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = b.httpClient.Do(req)
		if err == nil {
			break
		}
		if attempt == 2 || ctx.Err() != nil {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram API status %d: %s", resp.StatusCode, string(respBody))
	}
	var res struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, err
	}
	return &res.Result, nil
}

func (b *Bot) postHTTPBool(ctx context.Context, url string, body map[string]any) (bool, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return false, err
	}
	var resp *http.Response
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return false, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = b.httpClient.Do(req)
		if err == nil {
			break
		}
		if attempt == 2 || ctx.Err() != nil {
			return false, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("telegram API status %d: %s", resp.StatusCode, string(respBody))
	}
	return true, nil
}

var (
	reImg          = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["'][^>]*>`)
	reVid          = regexp.MustCompile(`<video[^>]+src=["']([^"']+)["'][^>]*>.*?</video>`)
	reBlockquote   = regexp.MustCompile(`(?s)(<blockquote[^>]*>)(.*?)(</blockquote>)`)
	reLineBreak    = regexp.MustCompile(`(?i)(<br\s*/?>)\s*\n`)
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
)

// FormatRichHTML ensures newlines inside blockquotes are converted to <br/>
// so Telegram rich messages do not collapse them into a single line.
func FormatRichHTML(htmlText string) string {
	return reBlockquote.ReplaceAllStringFunc(htmlText, func(match string) string {
		parts := reBlockquote.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		openTag := parts[1]
		content := parts[2]
		closeTag := parts[3]

		content = strings.ReplaceAll(content, "\r\n", "\n")
		content = reLineBreak.ReplaceAllString(content, "$1")
		content = strings.ReplaceAll(content, "\n", "<br/>")

		return openTag + content + closeTag
	})
}

func isRichContent(text string) bool {
	// Konten Rich sejati membutuhkan tabel, tombol rich khusus (tg-button-row), atau media tersemat.
	// Pesan menu/petunjuk blockquote biasa (tanpa tabel/button row) dikirim via pesan native Telegram
	// agar baris baru \n di dalam blockquote tidak di-collapse oleh client.
	return strings.Contains(text, "<tg-button-row>") ||
		strings.Contains(text, "<table") ||
		strings.Contains(text, "<img") ||
		strings.Contains(text, "<video")
}

func cleanHTML(text string) string {
	s := strings.ReplaceAll(text, "\r\n", "\n")
	s = strings.ReplaceAll(s, "<br/>\n", "\n")
	s = strings.ReplaceAll(s, "<br />\n", "\n")
	s = strings.ReplaceAll(s, "<br>\n", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<h1>", "<b>")
	s = strings.ReplaceAll(s, "</h1>", "</b>\n")
	s = strings.ReplaceAll(s, "<h2>", "<b>")
	s = strings.ReplaceAll(s, "</h2>", "</b>\n")
	if idx := strings.Index(s, "<tg-button-row>"); idx != -1 {
		if endIdx := strings.Index(s, "</tg-button-row>"); endIdx != -1 {
			s = s[:idx] + s[endIdx+len("</tg-button-row>"):]
		}
	}
	s = strings.ReplaceAll(s, "<tr><th>", "• <b>")
	s = strings.ReplaceAll(s, "</th><th>", " | ")
	s = strings.ReplaceAll(s, "</th></tr>", "</b>\n")
	s = strings.ReplaceAll(s, "<tr><td>", "• ")
	s = strings.ReplaceAll(s, "</td><td>", ": ")
	s = strings.ReplaceAll(s, "</td></tr>", "\n")
	tableTags := []string{
		"<table border=\"1\">", "<table>", "</table>",
		"<tr>", "</tr>", "<th>", "</th>", "<td>", "</td>",
		"<thead>", "</thead>", "<tbody>", "</tbody>",
	}
	for _, tag := range tableTags {
		s = strings.ReplaceAll(s, tag, "")
	}
	s = reImg.ReplaceAllString(s, `<a href="$1">&#8204;</a>`)
	s = reVid.ReplaceAllString(s, `<a href="$1">&#8204;</a>`)
	// Hilangkan divider panjang horizontal line yang tidak perlu
	s = strings.ReplaceAll(s, "<hr/>", "")
	s = strings.ReplaceAll(s, "<hr />", "")
	s = strings.ReplaceAll(s, "<hr>", "")
	// Batasi baris kosong berlebih agar teks tidak renggang berlebihan
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
