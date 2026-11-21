package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type pgMerchantStatus struct {
	Connected    bool
	Configured   bool
	MerchantID   string
	MerchantName string
	MerchantCity string
	ErrMsg       string
}

func fetchPGMerchantStatus(ctx context.Context, cfg pgClientConfig) pgMerchantStatus {
	if !cfg.isConfigured() || cfg.AdminKey == "" {
		return pgMerchantStatus{ErrMsg: "PG_BASE_URL / PG_ADMIN_KEY belum diisi"}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	endpoint := fmt.Sprintf("%s/v1/admin/merchants/%s", cfg.BaseURL, url.PathEscape(cfg.MerchantID))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return pgMerchantStatus{ErrMsg: err.Error()}
	}
	req.Header.Set("X-Admin-Key", cfg.AdminKey)

	resp, err := shopHTTPClient.Do(req)
	if err != nil {
		return pgMerchantStatus{ErrMsg: err.Error()}
	}
	defer resp.Body.Close()

	var payload struct {
		OK         bool `json:"ok"`
		Configured bool `json:"configured"`
		Merchant   *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"merchant"`
		MerchantName string `json:"merchant_name"`
		MerchantCity string `json:"merchant_city"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&payload); err != nil {
		return pgMerchantStatus{ErrMsg: "respons server PG tidak valid"}
	}
	if resp.StatusCode != http.StatusOK || !payload.OK {
		return pgMerchantStatus{ErrMsg: payload.Error}
	}
	name := payload.MerchantName
	if name == "" && payload.Merchant != nil {
		name = payload.Merchant.Name
	}
	mid := cfg.MerchantID
	if payload.Merchant != nil && payload.Merchant.ID != "" {
		mid = payload.Merchant.ID
	}
	return pgMerchantStatus{
		Connected:    true,
		Configured:   payload.Configured,
		MerchantID:   mid,
		MerchantName: name,
		MerchantCity: payload.MerchantCity,
	}
}

func upsertPGMerchantQRIS(ctx context.Context, cfg pgClientConfig, staticQRIS, qrImageBase64 string) (string, string, error) {
	bodyMap := map[string]any{
		"id":        cfg.MerchantID,
		"is_active": true,
	}
	if staticQRIS != "" {
		bodyMap["static_qris"] = staticQRIS
	}
	if qrImageBase64 != "" {
		bodyMap["qr_image_base64"] = qrImageBase64
	}
	raw, err := json.Marshal(bodyMap)
	if err != nil {
		return "", "", fmt.Errorf("encode upload qris: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.BaseURL+"/v1/admin/merchants", bytes.NewReader(raw))
	if err != nil {
		return "", "", fmt.Errorf("build request upload qris: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", cfg.AdminKey)

	resp, err := shopHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("hubungi server PG: %w", err)
	}
	defer resp.Body.Close()

	var parsed struct {
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
		MerchantName string `json:"merchant_name"`
		MerchantCity string `json:"merchant_city"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&parsed); err != nil {
		return "", "", fmt.Errorf("decode respons server PG: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !parsed.OK {
		if parsed.Error == "" {
			parsed.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", "", fmt.Errorf("%s", parsed.Error)
	}
	return parsed.MerchantName, parsed.MerchantCity, nil
}

func downloadOwnerPGImageBytes(ctx context.Context, b *botgotd.Bot, message *botgotd.Message) ([]byte, error) {
	if b == nil || message == nil {
		return nil, fmt.Errorf("pesan gambar tidak valid")
	}
	if b.API != nil && message.ID > 0 {
		if loc := resolveMTProtoImageLocation(ctx, b, message.Chat.ID, message.ID); loc != nil {
			var out limitedByteBuffer
			out.limit = maxOwnerPGImageBytes
			if _, err := downloader.NewDownloader().Download(b.API, loc).Stream(ctx, &out); err == nil && out.buf.Len() > 0 {
				return out.buf.Bytes(), nil
			}
		}
	}

	fileID := ""
	if message.Document != nil {
		if message.Document.FileSize > maxOwnerPGImageBytes {
			return nil, fmt.Errorf("ukuran file melebihi 2MB")
		}
		fileID = message.Document.FileID
	} else if len(message.Photo) > 0 {
		fileID = message.Photo[len(message.Photo)-1].FileID
	}
	if fileID == "" || (b.ServerURL == "" && b.Token == "") {
		return nil, fmt.Errorf("file gambar tidak dapat diunduh")
	}

	base := botAPIBaseURL(b)
	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", base, b.Token, url.QueryEscape(fileID))
	gfReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getFileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build getFile: %w", err)
	}
	gfResp, err := shopHTTPClient.Do(gfReq)
	if err != nil {
		return nil, fmt.Errorf("call getFile: %w", err)
	}
	defer gfResp.Body.Close()

	var gfPayload struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(gfResp.Body, 16384)).Decode(&gfPayload); err != nil || !gfPayload.OK || gfPayload.Result.FilePath == "" {
		return nil, fmt.Errorf("gagal mengambil file_path gambar QRIS")
	}

	fileURL := fmt.Sprintf("%s/file/bot%s/%s", base, b.Token, strings.TrimLeft(gfPayload.Result.FilePath, "/"))
	fReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download image: %w", err)
	}
	fResp, err := shopHTTPClient.Do(fReq)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer fResp.Body.Close()
	if fResp.StatusCode < 200 || fResp.StatusCode >= 300 {
		return nil, fmt.Errorf("download image status %d", fResp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(fResp.Body, maxOwnerPGImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image body: %w", err)
	}
	if int64(len(data)) > maxOwnerPGImageBytes {
		return nil, fmt.Errorf("ukuran gambar melebihi 2MB")
	}
	return data, nil
}

func resolveMTProtoImageLocation(ctx context.Context, b *botgotd.Bot, chatID int64, msgID int) tg.InputFileLocationClass {
	var list []tg.MessageClass
	if b.Peers != nil && chatID < -1000000000000 {
		if ch, ok := b.Peers.GetInputPeer(chatID).(*tg.InputPeerChannel); ok {
			res, err := b.API.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
				ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}},
			})
			if err == nil && res != nil {
				if cm, ok := res.(*tg.MessagesChannelMessages); ok {
					list = cm.Messages
				}
			}
		}
	}
	if len(list) == 0 {
		res, err := b.API.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}})
		if err == nil && res != nil {
			switch v := res.(type) {
			case *tg.MessagesMessages:
				list = v.Messages
			case *tg.MessagesMessagesSlice:
				list = v.Messages
			case *tg.MessagesChannelMessages:
				list = v.Messages
			}
		}
	}
	for _, item := range list {
		m, ok := item.(*tg.Message)
		if !ok || m.Media == nil {
			continue
		}
		switch med := m.Media.(type) {
		case *tg.MessageMediaPhoto:
			if photo, ok := med.Photo.(*tg.Photo); ok && photo != nil && photo.ID != 0 {
				return photo.AsInputPhotoFileLocation(largestPGPhotoThumb(photo.Sizes))
			}
		case *tg.MessageMediaDocument:
			if doc, ok := med.Document.(*tg.Document); ok && doc != nil && doc.ID != 0 {
				return doc.AsInputDocumentFileLocation("")
			}
		}
	}
	return nil
}

func largestPGPhotoThumb(sizes []tg.PhotoSizeClass) string {
	bestType := ""
	var bestSize int64
	for _, item := range sizes {
		switch sz := item.(type) {
		case *tg.PhotoSize:
			if int64(sz.Size) >= bestSize {
				bestSize = int64(sz.Size)
				bestType = sz.Type
			}
		case *tg.PhotoSizeProgressive:
			var maxP int64
			for _, n := range sz.Sizes {
				if int64(n) > maxP {
					maxP = int64(n)
				}
			}
			if maxP >= bestSize {
				bestSize = maxP
				bestType = sz.Type
			}
		}
	}
	if bestType == "" {
		return "y"
	}
	return bestType
}
