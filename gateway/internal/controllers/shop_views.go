package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strings"
	"time"
)

func (h *Handler) ShowShop(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.renderShopCatalog(ctx, b, chatID, chatID, 0)
}

func formatShopCatalogPriceCell(p shopPackage) string {
	if p.DiscountPct > 0 {
		return fmt.Sprintf("<s>%s</s> <b>%s</b> <code>(-%d%%)</code>",
			html.EscapeString(formatRupiah(p.UnitPriceIDR)),
			html.EscapeString(formatRupiah(p.PriceIDR)),
			p.DiscountPct,
		)
	}
	return fmt.Sprintf("<code>%s</code>", html.EscapeString(formatRupiah(p.PriceIDR)))
}

func formatShopRateWaitText(retrySec int64) string {
	min := retrySec / 60
	sec := retrySec % 60
	if min > 0 && sec > 0 {
		return fmt.Sprintf("%d menit %d detik", min, sec)
	}
	if min > 0 {
		return fmt.Sprintf("%d menit", min)
	}
	return fmt.Sprintf("%d detik", sec)
}

func (h *Handler) renderShopCatalog(ctx context.Context, b *botgotd.Bot, chatID, buyerID int64, editMsgID int) {
	pkgs := h.listShopPackages()
	cfg := loadPGConfig()

	var sb strings.Builder
	sb.WriteString("<h1>🛒 Toko Paket Lisensi GotdBot</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>Tipe (30 Hari / Qty)</th><th>Harga</th><th>Manfaat</th></tr>\n")

	typeRow := make([]botgotd.InlineKeyboardButton, 0, len(pkgs))
	for _, p := range pkgs {
		sb.WriteString(fmt.Sprintf("  <tr><td><b>%s</b></td><td>%s</td><td>%s</td></tr>\n",
			html.EscapeString(p.Name),
			formatShopCatalogPriceCell(p),
			html.EscapeString(p.Description),
		))
		if cfg.isConfigured() {
			btnText := fmt.Sprintf("%s • %s", p.Name, formatRupiah(p.PriceIDR))
			if p.DiscountPct > 0 {
				btnText = fmt.Sprintf("%s • %s (-%d%%)", p.Name, formatRupiah(p.PriceIDR), p.DiscountPct)
			}
			typeRow = append(typeRow, botgotd.InlineKeyboardButton{
				Text:         btnText,
				CallbackData: fmt.Sprintf("shop:pkg:%s:1", p.BaseID),
			})
		}
	}
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n")

	if cfg.isConfigured() {
		sb.WriteString("<p>Pilih tipe <b>Pro</b> atau <b>Pro++</b> di bawah untuk menentukan jumlah <b>Qty</b> (kelipatan 30 hari). Pembayaran QRIS otomatis terverifikasi seketika.</p>")
	} else {
		sb.WriteString("<p>Untuk pemesanan paket atau upgrade lisensi, silakan hubungi admin GotdBot.</p>")
	}

	var rows [][]botgotd.InlineKeyboardButton
	if len(typeRow) > 0 {
		rows = append(rows, typeRow)
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
	h.sendOrEditShopMessage(ctx, b, chatID, editMsgID, sb.String(), markup)
}

func (h *Handler) sendOrEditShopMessage(ctx context.Context, b *botgotd.Bot, chatID int64, editMsgID int, text string, markup *botgotd.InlineKeyboardMarkup) {
	if editMsgID > 0 {
		if _, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   editMsgID,
			Text:        text,
			ParseMode:   "HTML",
			ReplyMarkup: markup,
		}); err == nil {
			return
		}
	}
	params := &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "HTML",
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	_, _ = b.SendMessage(ctx, params)
}

func (h *Handler) renderShopPackageQty(ctx context.Context, b *botgotd.Bot, chatID int64, editMsgID int, pkg shopPackage) {
	var sb strings.Builder
	sb.WriteString("<h1>🛒 Pilih Jumlah (Qty) Paket</h1>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString(fmt.Sprintf("  <tr><th>📦 Tipe Paket</th><td><b>%s</b></td></tr>\n", html.EscapeString(pkg.Name)))
	sb.WriteString(fmt.Sprintf("  <tr><th>🔢 Jumlah (Qty)</th><td><b>%dx</b> (<code>%d Hari</code>)</td></tr>\n", pkg.Qty, pkg.DurationDays))
	sb.WriteString(fmt.Sprintf("  <tr><th>💵 Harga Satuan</th><td><code>%s</code> / 30 Hari</td></tr>\n", html.EscapeString(formatRupiah(pkg.UnitPriceIDR))))
	if pkg.DiscountPct > 0 {
		sb.WriteString(fmt.Sprintf("  <tr><th>🏷️ Diskon Promo</th><td><b>%d%%</b> (<code>-%s</code>)</td></tr>\n", pkg.DiscountPct, html.EscapeString(formatRupiah(pkg.DiscountIDR))))
	}
	sb.WriteString(fmt.Sprintf("  <tr><th>💰 Total Bayar</th><td><b><code>%s</code></b></td></tr>\n", html.EscapeString(formatRupiah(pkg.PriceIDR))))
	sb.WriteString("</table>\n<hr/>\n")
	sb.WriteString("<p>Atur Qty lalu ketuk <b>Buat Tagihan QRIS</b> untuk memproses pembayaran.</p>")

	prevData := fmt.Sprintf("shop:pkg:%s:%d", pkg.BaseID, pkg.Qty-1)
	nextData := fmt.Sprintf("shop:pkg:%s:%d", pkg.BaseID, pkg.Qty+1)

	presets := []int{1, 2, 3, 6, 12}
	presetRow := make([]botgotd.InlineKeyboardButton, 0, len(presets))
	for _, q := range presets {
		label := fmt.Sprintf("%dx", q)
		data := fmt.Sprintf("shop:pkg:%s:%d", pkg.BaseID, q)
		if q == pkg.Qty {
			label = fmt.Sprintf("• %dx •", q)
			data = "shop:noop"
		}
		presetRow = append(presetRow, botgotd.InlineKeyboardButton{
			Text:         label,
			CallbackData: data,
		})
	}

	rows := [][]botgotd.InlineKeyboardButton{
		{
			{Text: "➖", CallbackData: prevData},
			{Text: fmt.Sprintf("📦 Qty: %dx (%d Hari)", pkg.Qty, pkg.DurationDays), CallbackData: "shop:noop"},
			{Text: "➕", CallbackData: nextData},
		},
		presetRow,
		{{Text: fmt.Sprintf("💳 Buat Tagihan QRIS • %s", formatRupiah(pkg.PriceIDR)), CallbackData: fmt.Sprintf("shop:buy:%s:%d", pkg.BaseID, pkg.Qty)}},
		{{Text: "⬅️ Kembali", CallbackData: "shop:menu"}},
	}

	h.sendOrEditShopMessage(ctx, b, chatID, editMsgID, sb.String(), &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows})
}
