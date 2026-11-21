package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kenboo14/GoBot/keyboard"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

func (h *Handler) ShowShop(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.renderShopCatalog(ctx, b, chatID, chatID, 0)
}

func (h *Handler) renderShopCatalog(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, editMsgID int) {
	pkgs := loadShopPackages()
	cfg := loadPGConfig()

	var sb strings.Builder
	sb.WriteString("<h1>🛒 Toko Paket Lisensi GotdBot</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>Paket</th><th>Harga</th><th>Manfaat</th></tr>\n")

	var rows [][]botgotd.InlineKeyboardButton
	for _, p := range pkgs {
		sb.WriteString(fmt.Sprintf("  <tr><td><b>%s</b></td><td><code>%s</code></td><td>%s</td></tr>\n",
			html.EscapeString(p.Name),
			html.EscapeString(formatRupiah(p.PriceIDR)),
			html.EscapeString(p.Description),
		))
		if cfg.isConfigured() {
			rows = append(rows, []botgotd.InlineKeyboardButton{
				{Text: fmt.Sprintf("QRIS • %s (%s)", p.Name, formatRupiah(p.PriceIDR)), CallbackData: "shop:buy:" + p.ID},
			})
		}
	}
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n")

	if cfg.isConfigured() {
		sb.WriteString("<p>Pembayaran QRIS otomatis berlaku untuk semua Dompet Digital &amp; Mobile Banking Indonesia. Paket aktif seketika setelah pembayaran masuk.</p>")
	} else {
		sb.WriteString("<p>Untuk pemesanan paket atau upgrade lisensi, silakan hubungi admin GotdBot.</p>")
	}

	if activeOrd := h.shop.getActiveForBuyer(buyerID, time.Now()); activeOrd != nil && cfg.isConfigured() {
		rows = append(rows, []botgotd.InlineKeyboardButton{
			{Text: fmt.Sprintf("📄 Lihat Tagihan Aktif (%s)", formatRupiah(activeOrd.TotalAmount)), CallbackData: "shop:check:" + activeOrd.InvoiceID},
		})
	}

	var markup *botgotd.InlineKeyboardMarkup
	if len(rows) > 0 {
		markup = &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
	}

	if editMsgID > 0 {
		if _, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   editMsgID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: markup,
		}); err == nil {
			return
		}
	}

	params := &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      sb.String(),
		ParseMode: "HTML",
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, _ = b.SendMessage(ctx, params)
}

func (h *Handler) ShopCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	var chatID int64
	var messageID int
	if cb.Message != nil && cb.Message.Message != nil {
		chatID = cb.Message.Message.Chat.ID
		messageID = cb.Message.Message.ID
	}
	if chatID == 0 {
		chatID = cb.From.ID
	}
	buyerID := cb.From.ID

	switch {
	case cb.Data == "shop:menu":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.renderShopCatalog(ctx, b, chatID, buyerID, messageID)
	case strings.HasPrefix(cb.Data, "shop:buy:"):
		pkgID := strings.TrimPrefix(cb.Data, "shop:buy:")
		h.handleShopBuy(ctx, b, chatID, buyerID, cb.ID, pkgID)
	case strings.HasPrefix(cb.Data, "shop:check:"):
		invID := strings.TrimPrefix(cb.Data, "shop:check:")
		h.handleShopCheckStatus(ctx, b, chatID, buyerID, cb.ID, invID)
	case strings.HasPrefix(cb.Data, "shop:cancel:"):
		invID := strings.TrimPrefix(cb.Data, "shop:cancel:")
		h.handleShopCancel(ctx, b, chatID, buyerID, messageID, cb.ID, invID)
	default:
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	}
}

func (h *Handler) handleShopBuy(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, cbID, pkgID string) {
	pkg, ok := findShopPackage(pkgID)
	if !ok {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            "Paket tidak ditemukan.",
			ShowAlert:       true,
		})
		return
	}
	cfg := loadPGConfig()
	if !cfg.isConfigured() {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            "Pembayaran otomatis belum aktif. Hubungi admin.",
			ShowAlert:       true,
		})
		return
	}

	now := time.Now()
	if existing := h.shop.getActiveForBuyer(buyerID, now); existing != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            fmt.Sprintf("Anda masih memiliki tagihan aktif sebesar %s.", formatRupiah(existing.TotalAmount)),
			ShowAlert:       true,
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cbID,
		Text:            "⏳ Membuat kode QRIS pembayaran...",
	})

	extID := fmt.Sprintf("shop_%d_%d", buyerID, now.UnixNano())
	res, err := createPGInvoice(ctx, cfg, extID, strconv.FormatInt(buyerID, 10), pkg)
	if err != nil {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf(`<h1>⚠️ Tagihan Gagal Dibuat</h1><blockquote expandable>%s</blockquote><hr/><p>Silakan coba sesaat lagi atau hubungi admin.</p>`, html.EscapeString(err.Error())),
			ParseMode: "HTML",
		})
		return
	}

	ord := &shopPendingOrder{
		InvoiceID:   res.Invoice.ID,
		ExternalID:  res.Invoice.ExternalID,
		ChatID:      chatID,
		UserID:      buyerID,
		PackageID:   pkg.ID,
		TotalAmount: res.Invoice.TotalAmount,
		ExpiresAt:   res.Invoice.ExpiresAt,
	}

	expStr := time.Unix(res.Invoice.ExpiresAt, 0).Local().Format("15:04:05 WIB")
	caption := fmt.Sprintf(`<h1>🧾 Tagihan Pembayaran QRIS</h1>
<table border="1">
  <tr><th>📦 Paket</th><td><b>%s</b></td></tr>
  <tr><th>💰 Total Bayar</th><td><b><code>%s</code></b></td></tr>
  <tr><th>⏳ Batas Waktu</th><td><code>%s</code></td></tr>
  <tr><th>🔖 Kode Tagihan</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable><b>Penting:</b> Scan QRIS menggunakan aplikasi Dompet Digital / Mobile Banking mana saja. Saat QR dinamis dipindai, nominal <b>%s</b> akan terisi otomatis agar paket aktif seketika.</blockquote>
<hr/>
<p>GotdBot • Pembayaran Otomatis</p>`,
		html.EscapeString(pkg.Name),
		html.EscapeString(formatRupiah(res.Invoice.TotalAmount)),
		expStr,
		html.EscapeString(res.Invoice.ID),
		html.EscapeString(formatRupiah(res.Invoice.TotalAmount)),
	)

	markup := &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{{Text: "🔄 Cek Status Pembayaran", CallbackData: "shop:check:" + res.Invoice.ID}},
			{{Text: "❌ Batalkan Tagihan", CallbackData: "shop:cancel:" + res.Invoice.ID}},
		},
	}

	var pngBytes []byte
	if res.QRPNGBase64 != "" {
		pngBytes, _ = base64.StdEncoding.DecodeString(res.QRPNGBase64)
	}
	msgID, err := sendShopQRPhoto(ctx, b, chatID, fmt.Sprintf("qris_%s.png", res.Invoice.ID), pngBytes, caption, markup)
	if err != nil {
		msg, mErr := b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        caption,
			ParseMode:   "HTML",
			ReplyMarkup: markup,
		})
		if mErr == nil && msg != nil {
			msgID = msg.ID
		}
	}
	ord.QRMsgID = msgID
	h.shop.trackPending(ord)
}

func (h *Handler) handleShopCheckStatus(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, cbID, invID string) {
	cfg := loadPGConfig()
	if !cfg.isConfigured() {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Layanan pembayaran belum aktif.", ShowAlert: true})
		return
	}
	inv, err := getPGInvoice(ctx, cfg, invID)
	if err != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Gagal mengecek tagihan: " + err.Error(), ShowAlert: true})
		return
	}
	switch inv.Status {
	case "PAID":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "✅ Pembayaran sudah diterima!", ShowAlert: true})
		h.fulfillInvoiceByData(ctx, b, inv, chatID, buyerID, "")
	case "PENDING":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            fmt.Sprintf("⏳ Menunggu pembayaran %s.", formatRupiah(inv.TotalAmount)),
			ShowAlert:       true,
		})
	default:
		h.shop.removePending(inv.ID)
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            fmt.Sprintf("Status tagihan: %s", inv.Status),
			ShowAlert:       true,
		})
	}
}

func (h *Handler) handleShopCancel(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, msgID int, cbID, invID string) {
	cfg := loadPGConfig()
	if !cfg.isConfigured() {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID})
		return
	}
	if ord := h.shop.getPending(invID); ord != nil && ord.UserID != buyerID && !isOwner(buyerID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Tagihan bukan milik Anda.", ShowAlert: true})
		return
	}
	inv, err := cancelPGInvoice(ctx, cfg, invID)
	if err != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Tidak dapat dibatalkan: " + err.Error(), ShowAlert: true})
		return
	}
	h.shop.removePending(inv.ID)
	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "✅ Tagihan dibatalkan"})
	if msgID > 0 {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
	}
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf(`<h1>🚫 Tagihan Dibatalkan</h1><p>Tagihan <code>%s</code> sebesar <code>%s</code> telah dibatalkan.</p>`, html.EscapeString(inv.ID), html.EscapeString(formatRupiah(inv.TotalAmount))),
		ParseMode: "HTML",
	})
}

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
