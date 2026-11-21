package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

const maxBackupFileBytes = 2 << 20

type limitedByteBuffer struct {
	buf   bytes.Buffer
	limit int64
}

func (l *limitedByteBuffer) Write(p []byte) (int, error) {
	if int64(l.buf.Len())+int64(len(p)) > l.limit {
		return 0, fmt.Errorf("file size exceeds limit (%d bytes)", l.limit)
	}
	return l.buf.Write(p)
}

func sendBackupDocument(ctx context.Context, b *botgotd.Bot, chatID int64, filename string, content []byte, captionHTML string) error {
	if b == nil {
		return fmt.Errorf("bot instance nil")
	}
	if b.API != nil && b.Peers != nil {
		if peer := b.Peers.GetInputPeer(chatID); peer != nil {
			inputFile, err := uploader.NewUploader(b.API).FromBytes(ctx, filename, content)
			if err == nil {
				plain, entities := botgotd.ParseHTML(captionHTML)
				media := &tg.InputMediaUploadedDocument{
					File:     inputFile,
					MimeType: "text/plain",
					Attributes: []tg.DocumentAttributeClass{
						&tg.DocumentAttributeFilename{FileName: filename},
					},
				}
				var rid [8]byte
				_, _ = io.ReadFull(bytes.NewReader(strconv.AppendInt(nil, time.Now().UnixNano(), 10)), rid[:])
				_, sendErr := b.API.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
					Peer:     peer,
					Media:    media,
					Message:  plain,
					Entities: entities,
					RandomID: time.Now().UnixNano(),
				})
				if sendErr == nil {
					return nil
				}
			}
		}
	}

	if b.ServerURL == "" && b.Token == "" {
		return fmt.Errorf("no transport available to send document")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if captionHTML != "" {
		_ = writer.WriteField("caption", captionHTML)
		_ = writer.WriteField("parse_mode", "HTML")
	}
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return fmt.Errorf("create multipart document field: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return fmt.Errorf("write multipart document bytes: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendDocument", botAPIBaseURL(b), b.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return fmt.Errorf("build sendDocument request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post sendDocument: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("sendDocument status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBytes)))
	}
	return nil
}

func isValidBackupDocumentMeta(doc *botgotd.Document) bool {
	if doc == nil {
		return false
	}
	if doc.FileSize > maxBackupFileBytes {
		return false
	}
	fname := strings.ToLower(strings.TrimSpace(doc.FileName))
	mime := strings.ToLower(strings.TrimSpace(doc.MimeType))
	if fname != "" && !strings.HasSuffix(fname, ".txt") {
		return false
	}
	if mime != "" && !strings.HasPrefix(mime, "text/plain") {
		return false
	}
	return true
}

func downloadBackupDocumentBytes(ctx context.Context, b *botgotd.Bot, message *botgotd.Message) ([]byte, error) {
	if message == nil || message.Document == nil {
		return nil, fmt.Errorf("pesan tidak berisi dokumen backup")
	}
	if !isValidBackupDocumentMeta(message.Document) {
		return nil, fmt.Errorf("format atau ukuran dokumen tidak didukung (hanya .txt maks 2MB)")
	}
	if b == nil {
		return nil, fmt.Errorf("bot transport tidak tersedia")
	}

	if b.API != nil && message.ID > 0 {
		if msgRes, err := b.API.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: message.ID}}); err == nil && msgRes != nil {
			var list []tg.MessageClass
			switch v := msgRes.(type) {
			case *tg.MessagesMessages:
				list = v.Messages
			case *tg.MessagesMessagesSlice:
				list = v.Messages
			case *tg.MessagesChannelMessages:
				list = v.Messages
			}
			for _, item := range list {
				if m, ok := item.(*tg.Message); ok {
					if md, ok := m.Media.(*tg.MessageMediaDocument); ok {
						if doc, ok := md.Document.(*tg.Document); ok {
							if doc.Size > maxBackupFileBytes {
								return nil, fmt.Errorf("ukuran dokumen melebihi 2MB")
							}
							var out limitedByteBuffer
							out.limit = maxBackupFileBytes
							loc := &tg.InputDocumentFileLocation{
								ID:            doc.ID,
								AccessHash:    doc.AccessHash,
								FileReference: doc.FileReference,
							}
							if _, dErr := downloader.NewDownloader().Download(b.API, loc).Stream(ctx, &out); dErr != nil {
								return nil, fmt.Errorf("unduh dokumen via mtproto: %w", dErr)
							}
							return out.buf.Bytes(), nil
						}
					}
				}
			}
		}
	}

	if b.ServerURL == "" && b.Token == "" {
		return nil, fmt.Errorf("file backup tidak dapat diunduh")
	}

	base := botAPIBaseURL(b)
	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", base, b.Token, url.QueryEscape(message.Document.FileID))
	client := &http.Client{Timeout: 20 * time.Second}
	gfReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getFileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build getFile request: %w", err)
	}
	gfResp, err := client.Do(gfReq)
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
		return nil, fmt.Errorf("gagal mendapatkan file_path dokumen backup")
	}

	fileURL := fmt.Sprintf("%s/file/bot%s/%s", base, b.Token, strings.TrimLeft(gfPayload.Result.FilePath, "/"))
	fReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download file request: %w", err)
	}
	fResp, err := client.Do(fReq)
	if err != nil {
		return nil, fmt.Errorf("download backup document: %w", err)
	}
	defer fResp.Body.Close()
	if fResp.StatusCode < 200 || fResp.StatusCode >= 300 {
		return nil, fmt.Errorf("download document status %d", fResp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(fResp.Body, maxBackupFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read backup document body: %w", err)
	}
	if int64(len(data)) > maxBackupFileBytes {
		return nil, fmt.Errorf("ukuran dokumen melebihi 2MB")
	}
	return data, nil
}

func (h *Handler) handleRestoreBackupDocument(ctx context.Context, b *botgotd.Bot, chatID, userID int64, message *botgotd.Message) {
	data, err := downloadBackupDocumentBytes(ctx, b, message)
	if err != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Gagal Membaca File Backup</h1><blockquote expandable>Pastikan file yang diunggah berformat <code>.txt</code> (UTF-8) dan berukuran maksimal 2MB.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return
	}
	if !utf8.Valid(data) {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Format File Tidak Valid</h1><blockquote expandable>Isi berkas backup harus berupa teks UTF-8 yang valid.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return
	}
	h.processRestoreBackupCode(ctx, b, chatID, userID, string(data))
}
