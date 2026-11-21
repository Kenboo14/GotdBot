package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
)

const (
	ownerPGPriceStatePrefix = "owner_pg_price:"
	ownerPGDiscStatePrefix  = "owner_pg_disc:"
)

func (h *Handler) handleOwnerPGPanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state, text string, message *botgotd.Message) bool {
	senderID := userID
	if message != nil && message.From != nil && message.From.ID != 0 {
		senderID = message.From.ID
	}
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
	}

	switch {
	case state == ownerPGUploadState:
		if isOwnerPGCancelInput(text) {
			h.setPanelState(chatID, "")
			h.replyOwnerPGActionCancelled(ctx, b, chatID, threadID)
			return true
		}
		h.handleOwnerPGQRISUpload(ctx, b, chatID, senderID, text, message)
		return true
	case strings.HasPrefix(state, ownerPGPriceStatePrefix):
		target := strings.TrimPrefix(state, ownerPGPriceStatePrefix)
		h.handleOwnerPGPriceInput(ctx, b, chatID, threadID, senderID, target, text)
		return true
	case strings.HasPrefix(state, ownerPGDiscStatePrefix):
		target := strings.TrimPrefix(state, ownerPGDiscStatePrefix)
		h.handleOwnerPGDiscountInput(ctx, b, chatID, threadID, senderID, target, text)
		return true
	default:
		return false
	}
}

func isOwnerPGCancelInput(text string) bool {
	t := strings.TrimSpace(text)
	return t == assets.BtnBatal || strings.EqualFold(t, "batal") || t == "/cancel"
}

func (h *Handler) replyOwnerPGActionCancelled(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int) {
	card := renderMonitorRichCard("↩️ Pengaturan Shop Dibatalkan", []monitorRichRow{
		{Label: "Status", Value: "Tidak ada perubahan yang disimpan"},
	}, "ℹ️ Buka kembali <code>/help</code> → tab <b>💳 PG</b> untuk mengelola harga, diskon, atau QRIS.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, card)
}

func ownerPGItemLabel(target string) string {
	switch target {
	case "pro":
		return "💎 Pro"
	case "proplus":
		return "👑 Pro++"
	case "all":
		return "🔥 Semua Paket (Pro & Pro++)"
	default:
		return target
	}
}

func parseOwnerPGRupiah(raw string) (int64, error) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.ReplaceAll(clean, "rp", "")
	clean = strings.ReplaceAll(clean, ".", "")
	clean = strings.ReplaceAll(clean, ",", "")
	clean = strings.ReplaceAll(clean, " ", "")
	if clean == "" {
		return 0, fmt.Errorf("nominal harga tidak boleh kosong")
	}
	val, err := strconv.ParseInt(clean, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("format angka harga tidak valid (contoh: 45000)")
	}
	if val < shopMinPriceIDR || val > shopMaxPriceIDR {
		return 0, fmt.Errorf("harga harus antara %s sampai %s", formatRupiah(shopMinPriceIDR), formatRupiah(shopMaxPriceIDR))
	}
	return val, nil
}

func parseOwnerPGDiscountPct(raw string) (int, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(raw, "%", ""))
	if clean == "" {
		return 0, fmt.Errorf("persentase diskon tidak boleh kosong")
	}
	val, err := strconv.Atoi(clean)
	if err != nil {
		return 0, fmt.Errorf("masukkan angka persentase bulat 0 sampai 99")
	}
	if val < 0 || val > 99 {
		return 0, fmt.Errorf("persentase diskon harus antara 0%% sampai 99%%")
	}
	return val, nil
}

func (h *Handler) sendOwnerPGPricePrompt(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, target string) {
	pkg, ok := h.resolveShopPackage(target, 1)
	if !ok {
		return
	}
	h.setPanelState(chatID, ownerPGPriceStatePrefix+target)
	card := renderMonitorRichCard("💰 Atur Harga · "+pkg.Name, []monitorRichRow{
		{Label: "📦 Item", Value: "<b>" + html.EscapeString(pkg.Name) + "</b> (30 Hari / Qty)"},
		{Label: "💵 Harga Saat Ini", Value: "<code>" + html.EscapeString(formatRupiah(pkg.UnitPriceIDR)) + "</code>"},
		{Label: "✏️ Kirim Nominal Baru", Value: "Ketik angka Rupiah, contoh: <code>50000</code> atau <code>50.000</code>"},
	}, "ℹ️ Perubahan harga langsung berlaku untuk menu <b>🛒 Shop 🛒</b>.", false)
	markup := &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{{Text: "❌ Batal", CallbackData: "owner_pg:cancel_prompt"}},
		},
	}
	_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, card, markup)
}

func (h *Handler) sendOwnerPGDiscountPrompt(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, target string) {
	h.setPanelState(chatID, ownerPGDiscStatePrefix+target)
	pkgs := h.listShopPackages()
	rows := make([]monitorRichRow, 0, len(pkgs)+1)
	for _, p := range pkgs {
		if target == "all" || p.BaseID == target {
			rows = append(rows, monitorRichRow{
				Label: html.EscapeString(p.Name),
				Value: fmt.Sprintf("Diskon aktif: <code>%d%%</code> (Harga akhir: <code>%s</code>)", p.DiscountPct, html.EscapeString(formatRupiah(p.PriceIDR))),
			})
		}
	}
	rows = append(rows, monitorRichRow{
		Label: "✏️ Input Manual (%)",
		Value: "Atau ketik angka persen <code>0</code> - <code>99</code> langsung di chat (contoh: <code>25</code>)",
	})
	card := renderMonitorRichCard("🏷️ Atur Diskon · "+html.EscapeString(ownerPGItemLabel(target)), rows, "ℹ️ Pilih cepat persentase di bawah atau ketik angka diskon (%) manual.", false)
	markup := &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{
				{Text: "0% (Off)", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:0", target)},
				{Text: "10%", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:10", target)},
				{Text: "15%", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:15", target)},
			},
			{
				{Text: "20%", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:20", target)},
				{Text: "25%", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:25", target)},
				{Text: "50%", CallbackData: fmt.Sprintf("owner_pg:setdisc:%s:50", target)},
			},
			{{Text: "❌ Batal", CallbackData: "owner_pg:cancel_prompt"}},
		},
	}
	_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, card, markup)
}

func (h *Handler) handleOwnerPGPriceInput(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, senderID int64, target, text string) {
	if !isOwner(senderID) {
		return
	}
	if isOwnerPGCancelInput(text) {
		h.setPanelState(chatID, "")
		h.replyOwnerPGActionCancelled(ctx, b, chatID, threadID)
		return
	}
	price, err := parseOwnerPGRupiah(text)
	if err != nil {
		errCard := renderMonitorRichCard("⚠️ Nominal Harga Tidak Valid", []monitorRichRow{
			{Label: "Detail", Value: html.EscapeString(err.Error())},
		}, "ℹ️ Kirim angka harga yang valid (contoh: <code>45000</code>) atau ketuk Batal.", false)
		markup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "❌ Batal", CallbackData: "owner_pg:cancel_prompt"}}},
		}
		_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, errCard, markup)
		return
	}
	if err := h.shop.setItemPrice(target, price); err != nil {
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, renderMonitorRichCard("⚠️ Gagal Mengubah Harga", []monitorRichRow{{Label: "Error", Value: html.EscapeString(err.Error())}}, "", false))
		return
	}
	h.setPanelState(chatID, "")
	pkg, _ := h.resolveShopPackage(target, 1)
	okCard := renderMonitorRichCard("✅ Harga Shop Diperbarui", []monitorRichRow{
		{Label: "📦 Paket", Value: html.EscapeString(pkg.Name) + " (30 Hari)"},
		{Label: "💵 Harga Dasar Baru", Value: "<code>" + html.EscapeString(formatRupiah(pkg.UnitPriceIDR)) + "</code>"},
		{Label: "🏷️ Diskon Aktif", Value: fmt.Sprintf("<code>%d%%</code>", pkg.DiscountPct)},
		{Label: "💰 Harga Jual Efektif", Value: "<b><code>" + html.EscapeString(formatRupiah(pkg.PriceIDR)) + "</code></b>"},
	}, "ℹ️ Buka kembali <code>/help</code> → tab <b>💳 PG</b> untuk melihat ringkasan terbaru.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, okCard)
}

func (h *Handler) handleOwnerPGDiscountInput(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, senderID int64, target, text string) {
	if !isOwner(senderID) {
		return
	}
	if isOwnerPGCancelInput(text) {
		h.setPanelState(chatID, "")
		h.replyOwnerPGActionCancelled(ctx, b, chatID, threadID)
		return
	}
	pct, err := parseOwnerPGDiscountPct(text)
	if err != nil {
		errCard := renderMonitorRichCard("⚠️ Persentase Diskon Tidak Valid", []monitorRichRow{
			{Label: "Detail", Value: html.EscapeString(err.Error())},
		}, "ℹ️ Kirim angka <code>0</code> sampai <code>99</code> atau ketuk tombol preset.", false)
		markup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "❌ Batal", CallbackData: "owner_pg:cancel_prompt"}}},
		}
		_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, errCard, markup)
		return
	}
	if err := h.shop.setDiscount(target, pct); err != nil {
		return
	}
	h.setPanelState(chatID, "")
	h.sendOwnerPGDiscountSummary(ctx, b, chatID, threadID, target, pct)
}

func (h *Handler) sendOwnerPGDiscountSummary(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, target string, pct int) {
	pkgs := h.listShopPackages()
	rows := []monitorRichRow{
		{Label: "🏷️ Diskon Diterapkan", Value: fmt.Sprintf("<b>%s</b> → <code>%d%%</code>", html.EscapeString(ownerPGItemLabel(target)), pct)},
	}
	for _, p := range pkgs {
		rows = append(rows, monitorRichRow{
			Label: html.EscapeString(p.Name),
			Value: formatShopCatalogPriceCell(p),
		})
	}
	card := renderMonitorRichCard("✅ Diskon Shop Diperbarui", rows, "ℹ️ Harga diskon sudah aktif otomatis di menu <b>🛒 Shop 🛒</b>.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, card)
}

func (h *Handler) handleOwnerPGQuickSetDiscount(ctx context.Context, b *botgotd.Bot, chatID int64, msgID int, cbID, payload string) {
	parts := strings.Split(payload, ":")
	if len(parts) != 2 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID})
		return
	}
	target := parts[0]
	pct, err := strconv.Atoi(parts[1])
	if err != nil || h.shop.setDiscount(target, pct) != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cbID, Text: "Diskon tidak valid", ShowAlert: true})
		return
	}
	h.setPanelState(chatID, "")
	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cbID,
		Text:            fmt.Sprintf("✅ Diskon %s diatur ke %d%%", ownerPGItemLabel(target), pct),
	})
	if msgID > 0 {
		h.deleteBotMessages(ctx, b, chatID, []int{msgID})
	}
	h.sendOwnerPGDiscountSummary(ctx, b, chatID, 0, target, pct)
}
