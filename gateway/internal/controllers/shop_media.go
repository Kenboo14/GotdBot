package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/Kenboo14/GoBot/keyboard"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

func sendShopQRPhoto(ctx context.Context, b *botgotd.Bot, chatID int64, filename string, pngBytes []byte, captionHTML string, markup *botgotd.InlineKeyboardMarkup) (int, error) {
	if b == nil || len(pngBytes) == 0 {
		return 0, fmt.Errorf("qr png kosong")
	}
	if b.API != nil && b.Peers != nil {
		if peer := b.Peers.GetInputPeer(chatID); peer != nil {
			if inputFile, err := uploader.NewUploader(b.API).FromBytes(ctx, filename, pngBytes); err == nil {
				plain, entities := botgotd.ParseHTML(captionHTML)
				req := &tg.MessagesSendMediaRequest{
					Peer:        peer,
					Media:       &tg.InputMediaUploadedPhoto{File: inputFile},
					Message:     plain,
					Entities:    entities,
					RandomID:    time.Now().UnixNano(),
					ReplyMarkup: keyboard.ConvertReplyMarkup(markup),
				}
				if upd, sendErr := b.API.MessagesSendMedia(ctx, req); sendErr == nil {
					return extractSentMessageID(upd), nil
				}
			}
		}
	}
	if b.ServerURL == "" && b.Token == "" {
		return 0, fmt.Errorf("no transport for photo")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	_ = writer.WriteField("caption", captionHTML)
	_ = writer.WriteField("parse_mode", "HTML")
	if markup != nil {
		if mRaw, err := json.Marshal(markup); err == nil {
			_ = writer.WriteField("reply_markup", string(mRaw))
		}
	}
	part, err := writer.CreateFormFile("photo", filename)
	if err != nil {
		return 0, fmt.Errorf("create photo field: %w", err)
	}
	if _, err := part.Write(pngBytes); err != nil {
		return 0, fmt.Errorf("write photo bytes: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("close photo writer: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendPhoto", botAPIBaseURL(b), b.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return 0, fmt.Errorf("build sendPhoto request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("post sendPhoto: %w", err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	_ = json.Unmarshal(respBytes, &apiResp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !apiResp.OK {
		return 0, fmt.Errorf("sendPhoto failed status %d", resp.StatusCode)
	}
	return apiResp.Result.MessageID, nil
}

func extractSentMessageID(upd tg.UpdatesClass) int {
	switch u := upd.(type) {
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.Updates:
		for _, item := range u.Updates {
			switch m := item.(type) {
			case *tg.UpdateMessageID:
				return m.ID
			case *tg.UpdateNewMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					return msg.ID
				}
			}
		}
	}
	return 0
}
