package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

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
	case cb.Data == "shop:noop":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	case strings.HasPrefix(cb.Data, "shop:pkg:"):
		rest := strings.TrimPrefix(cb.Data, "shop:pkg:")
		h.handleShopSelectQty(ctx, b, chatID, messageID, cb.ID, rest)
	case strings.HasPrefix(cb.Data, "shop:buy:"):
		pkgSpec := strings.TrimPrefix(cb.Data, "shop:buy:")
		h.handleShopBuy(ctx, b, chatID, buyerID, cb.ID, pkgSpec)
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

func (h *Handler) handleShopSelectQty(ctx context.Context, b *botgotd.Bot, chatID int64, msgID int, cbID, rest string) {
	baseID, qty, ok := parseShopPackageSpec(rest)
	if !ok {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            "Tipe paket tidak ditemukan.",
			ShowAlert:       true,
		})
		return
	}
	if qty < 1 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            "Minimal pembelian Qty adalah 1.",
		})
		return
	}
	if qty > shopMaxQty {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            fmt.Sprintf("Maksimal pembelian Qty adalah %d.", shopMaxQty),
		})
		return
	}
	pkg, ok := h.resolveShopPackage(baseID, qty)
	if !ok {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Paket tidak valid."})
		return
	}
	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID})
	h.renderShopPackageQty(ctx, b, chatID, msgID, pkg)
}

func (h *Handler) handleShopBuy(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, cbID, pkgSpec string) {
	pkg, ok := h.resolveShopPackage(pkgSpec, 0)
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
	if allowed, retrySec := h.shop.checkOrderRateLimit(buyerID, now); !allowed {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cbID,
			Text:            fmt.Sprintf("⏳ Batas pembuatan tagihan tercapai (maks %dx / 5 menit). Tunggu %s lagi.", shopRateLimitMax, formatShopRateWaitText(retrySec)),
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
	h.shop.recordOrderCreation(buyerID, now)

	ord := &shopPendingOrder{
		InvoiceID:   res.Invoice.ID,
		ExternalID:  res.Invoice.ExternalID,
		ChatID:      chatID,
		UserID:      buyerID,
		PackageID:   pkg.ID,
		Qty:         pkg.Qty,
		TotalAmount: res.Invoice.TotalAmount,
		ExpiresAt:   res.Invoice.ExpiresAt,
	}

	expStr := time.Unix(res.Invoice.ExpiresAt, 0).Local().Format("15:04:05 WIB")
	discRow := ""
	if pkg.DiscountPct > 0 {
		discRow = fmt.Sprintf("  <tr><th>🏷️ Diskon</th><td>%d%% (-%s)</td></tr>\n", pkg.DiscountPct, html.EscapeString(formatRupiah(pkg.DiscountIDR)))
	}
	caption := fmt.Sprintf(`<h1>🧾 Tagihan Pembayaran QRIS</h1>
<table border="1">
  <tr><th>📦 Paket</th><td><b>%s</b></td></tr>
  <tr><th>🔢 Jumlah</th><td><b>%dx</b> (%d Hari)</td></tr>
%s  <tr><th>💰 Total Bayar</th><td><b><code>%s</code></b></td></tr>
  <tr><th>⏳ Batas Waktu</th><td><code>%s</code></td></tr>
  <tr><th>🔖 Kode Tagihan</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable><b>Penting:</b> Scan QRIS menggunakan aplikasi Dompet Digital / Mobile Banking mana saja. Saat QR dinamis dipindai, nominal <b>%s</b> akan terisi otomatis agar paket aktif seketika.</blockquote>
<hr/>
<p>GotdBot • Pembayaran Otomatis</p>`,
		html.EscapeString(pkg.Name),
		pkg.Qty,
		pkg.DurationDays,
		discRow,
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
	ord := h.shop.getPending(invID)
	if ord != nil && ord.UserID != buyerID && !isOwner(buyerID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Tagihan bukan milik Anda.", ShowAlert: true})
		return
	}
	inv, err := cancelPGInvoice(ctx, cfg, invID)
	if err != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Tidak dapat dibatalkan: " + err.Error(), ShowAlert: true})
		return
	}
	removed := h.shop.removePending(inv.ID)
	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "✅ Tagihan dibatalkan"})

	var delIDs []int
	if msgID > 0 {
		delIDs = append(delIDs, msgID)
	}
	if removed != nil && removed.QRMsgID > 0 && removed.QRMsgID != msgID {
		delIDs = append(delIDs, removed.QRMsgID)
	}
	if len(delIDs) > 0 {
		h.deleteBotMessages(ctx, b, chatID, delIDs)
	}
}
