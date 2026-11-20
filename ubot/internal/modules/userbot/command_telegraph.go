package userbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

const telegraphMaxImageBytes int64 = 32 * 1024 * 1024

var (
	telegraphPublishMu sync.Mutex
	telegraphImgHTTP   = &http.Client{Timeout: 45 * time.Second}
)

type limitedBufferWriter struct {
	buf bytes.Buffer
	max int64
}

func (w *limitedBufferWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len())+int64(len(p)) > w.max {
		return 0, fmt.Errorf("foto untuk Telegraph melebihi batas 32 MB")
	}
	return w.buf.Write(p)
}

func largestPhotoThumb(sizes []tg.PhotoSizeClass) (string, int64) {
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
			var maxProgressive int64
			for _, n := range sz.Sizes {
				if int64(n) > maxProgressive {
					maxProgressive = int64(n)
				}
			}
			if maxProgressive >= bestSize {
				bestSize = maxProgressive
				bestType = sz.Type
			}
		}
	}
	if bestType == "" {
		bestType = "y"
	}
	return bestType, bestSize
}

func extractTelegraphImageSource(source *tg.Message) (tg.InputFileLocationClass, int64, bool) {
	if source == nil || source.Media == nil {
		return nil, 0, false
	}
	switch med := source.Media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := med.Photo.(*tg.Photo)
		if !ok || photo == nil || photo.ID == 0 {
			return nil, 0, false
		}
		thumbType, size := largestPhotoThumb(photo.Sizes)
		return photo.AsInputPhotoFileLocation(thumbType), size, true
	case *tg.MessageMediaDocument:
		doc, ok := med.Document.(*tg.Document)
		if !ok || doc == nil || doc.ID == 0 {
			return nil, 0, false
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(doc.MimeType)), "image/") {
			return doc.AsInputDocumentFileLocation(""), doc.Size, true
		}
	}
	return nil, 0, false
}

func buildTelegraphContentNodes(bodyText, imageURL string) []map[string]any {
	nodes := make([]map[string]any, 0, 8)
	for _, rawLine := range strings.Split(strings.ReplaceAll(bodyText, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		nodes = append(nodes, map[string]any{
			"tag":      "p",
			"children": []any{line},
		})
	}
	if imageURL != "" {
		nodes = append(nodes, map[string]any{
			"tag": "figure",
			"children": []any{
				map[string]any{
					"tag":   "img",
					"attrs": map[string]any{"src": imageURL},
				},
			},
		})
	}
	return nodes
}

func uploadImageToImgBB(ctx context.Context, apiKey string, imgBytes []byte) (string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("API key ImgBB belum dikonfigurasi")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "telegraph-image.jpg")
	if err != nil {
		return "", fmt.Errorf("buat form imgbb: %w", err)
	}
	if _, err := part.Write(imgBytes); err != nil {
		return "", fmt.Errorf("tulis data foto imgbb: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("tutup form imgbb: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, "https://api.imgbb.com/1/upload?key="+apiKey, &body)
	if err != nil {
		return "", fmt.Errorf("siapkan request imgbb: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := telegraphImgHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("koneksi ImgBB gagal: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("baca respons ImgBB: %w", err)
	}

	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
		Error any `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)

	if resp.StatusCode >= 400 || !parsed.Success {
		reason := "permintaan ditolak"
		switch errVal := parsed.Error.(type) {
		case map[string]any:
			if msg, ok := errVal["message"].(string); ok && msg != "" {
				reason = msg
			} else if code, exists := errVal["code"]; exists {
				reason = fmt.Sprintf("%v", code)
			}
		case string:
			if errVal != "" {
				reason = errVal
			}
		}
		if strings.Contains(strings.ToLower(reason), "invalid api v1 key") {
			reason = "API key ImgBB ditolak. Buat key yang valid lalu perbarui IMGBB_API_KEY di .env VPS."
		}
		reason = strings.ReplaceAll(reason, apiKey, "[redacted]")
		if len([]rune(reason)) > 120 {
			reason = string([]rune(reason)[:120])
		}
		return "", fmt.Errorf("ImgBB menolak upload (HTTP %d: %s)", resp.StatusCode, reason)
	}

	if !strings.HasPrefix(parsed.Data.URL, "https://") {
		return "", fmt.Errorf("ImgBB tidak mengembalikan direct URL HTTPS")
	}
	return parsed.Data.URL, nil
}

func createTelegraphPage(ctx context.Context, title, author string, nodes []map[string]any) (string, error) {
	telegraphPublishMu.Lock()
	defer telegraphPublishMu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	postJSON := func(endpoint string, payload map[string]any) ([]byte, error) {
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("request telegraph: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := toolHTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("kirim telegraph: %w", err)
		}
		defer resp.Body.Close()
		return io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	}

	accRaw, err := postJSON("https://api.telegra.ph/createAccount", map[string]any{
		"short_name":  "GotdBot",
		"author_name": author,
	})
	if err != nil {
		return "", err
	}
	var accParsed struct {
		OK     bool `json:"ok"`
		Result struct {
			AccessToken string `json:"access_token"`
		} `json:"result"`
	}
	if json.Unmarshal(accRaw, &accParsed) != nil || !accParsed.OK || accParsed.Result.AccessToken == "" {
		return "", fmt.Errorf("gagal membuat akun Telegraph sementara")
	}

	pageRaw, err := postJSON("https://api.telegra.ph/createPage", map[string]any{
		"access_token": accParsed.Result.AccessToken,
		"title":        title,
		"author_name":  author,
		"content":      nodes,
	})
	if err != nil {
		return "", err
	}
	var pageParsed struct {
		OK     bool `json:"ok"`
		Result struct {
			Path string `json:"path"`
			URL  string `json:"url"`
		} `json:"result"`
	}
	if json.Unmarshal(pageRaw, &pageParsed) != nil || !pageParsed.OK {
		return "", fmt.Errorf("Telegraph tidak mengembalikan path halaman")
	}
	if pageParsed.Result.Path != "" {
		return "https://telegra.ph/" + strings.TrimPrefix(pageParsed.Result.Path, "/"), nil
	}
	if pageParsed.Result.URL != "" {
		return pageParsed.Result.URL, nil
	}
	return "", fmt.Errorf("Telegraph tidak mengembalikan path halaman")
}

func truncateRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max])
}

func (s *Service) telegraphAuthorName(ctx context.Context, client *gobot.Client) string {
	if client != nil {
		if self, err := client.Self(ctx); err == nil && self != nil && strings.TrimSpace(self.FirstName) != "" {
			return strings.TrimSpace(self.FirstName)
		}
	}
	return "GotdBot UserBot"
}

func (s *Service) sendTelegraphSuccess(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, pageURL string, statusMsgID, threadID int) error {
	escapedURL := html.EscapeString(pageURL)
	card := ubotInlineCardPayload{
		Title: "Telegra.ph Link",
		HTML:  fmt.Sprintf("<b>Ini</b> <a href=\"%s\"><i>Telegra.ph Link</i></a> <b>Anda!</b>", escapedURL),
		Buttons: [][]ubotInlineCardButton{
			{{Text: "Link!", URL: pageURL}},
		},
	}
	if err := s.trySendInlineCard(ctx, client, target, card, threadID); err == nil {
		deleteUserStatusMessage(ctx, client, target, statusMsgID)
		return nil
	}
	fallbackText := "Ini Telegra.ph Link Anda: " + pageURL
	if statusMsgID > 0 {
		if err := editUserText(ctx, client, target, statusMsgID, fallbackText); err == nil {
			return nil
		}
	}
	return sendUserText(ctx, client, target, fallbackText, threadID)
}

func (s *Service) telegraphCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, args string, threadID int) error {
	source := msg
	if msg != nil && msg.ReplyTo != nil {
		if reply, err := s.replyMessage(ctx, client, msg, target); err == nil && reply != nil {
			source = reply
		}
	}

	loc, mediaSize, isMedia := extractTelegraphImageSource(source)
	if !isMedia {
		body := strings.TrimSpace(args)
		if body == "" && source != nil {
			body = strings.TrimSpace(source.Message)
		}
		if body == "" {
			return sendUserText(ctx, client, target, "Kirim foto dengan caption atau pesan teks, lalu balas dengan !telegraph.", threadID)
		}
		nodes := buildTelegraphContentNodes(body, "")
		pageURL, err := createTelegraphPage(ctx, "Telegraph GotdBot", s.telegraphAuthorName(ctx, client), nodes)
		if err != nil {
			return sendUserText(ctx, client, target, "Telegraph gagal: "+err.Error(), threadID)
		}
		return s.sendTelegraphSuccess(ctx, client, target, pageURL, 0, threadID)
	}

	apiKey := strings.TrimSpace(os.Getenv("IMGBB_API_KEY"))
	if apiKey == "" {
		return sendUserText(ctx, client, target, "Telegraph belum dikonfigurasi: isi IMGBB_API_KEY di file .env proyek lalu restart.", threadID)
	}
	if mediaSize > telegraphMaxImageBytes {
		return sendUserText(ctx, client, target, "Foto untuk Telegraph maksimal 32 MB.", threadID)
	}

	statusMsgID, _ := sendUserStatusMessage(ctx, client, target, "Mengunggah foto dan membuat halaman Telegraph…", threadID)
	finishErr := func(out string) error {
		if statusMsgID > 0 {
			if err := editUserText(ctx, client, target, statusMsgID, out); err == nil {
				return nil
			}
		}
		return sendUserText(ctx, client, target, out, threadID)
	}

	dlCtx, dlCancel := context.WithTimeout(ctx, 45*time.Second)
	defer dlCancel()
	writer := &limitedBufferWriter{max: telegraphMaxImageBytes}
	if _, err := downloader.NewDownloader().Download(client.API(), loc).Stream(dlCtx, writer); err != nil {
		return finishErr("Telegraph gagal mengunduh foto: " + truncateRunes(err.Error(), 160))
	}
	if writer.buf.Len() == 0 {
		return finishErr("Telegraph gagal: file foto kosong.")
	}

	imageURL, err := uploadImageToImgBB(ctx, apiKey, writer.buf.Bytes())
	if err != nil {
		return finishErr("Telegraph gagal: " + truncateRunes(err.Error(), 160))
	}

	caption := ""
	if source != nil {
		caption = strings.TrimSpace(source.Message)
	}
	title := strings.TrimSpace(args)
	if title == "" {
		title = caption
	}
	if title == "" {
		title = "Telegraph GotdBot"
	}
	title = truncateRunes(title, 120)

	nodes := buildTelegraphContentNodes(caption, imageURL)
	pageURL, err := createTelegraphPage(ctx, title, s.telegraphAuthorName(ctx, client), nodes)
	if err != nil {
		return finishErr("Telegraph gagal membuat halaman: " + truncateRunes(err.Error(), 160))
	}
	return s.sendTelegraphSuccess(ctx, client, target, pageURL, statusMsgID, threadID)
}
