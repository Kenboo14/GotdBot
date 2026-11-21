package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"encoding/base64"
	"html"
	"strings"
)

const (
	ownerPGUploadState   = "waiting_owner_pg_qris"
	maxOwnerPGImageBytes = 2 << 20
)

func (h *Handler) appendOwnerPGHelp(ctx context.Context) ([]monitorRichRow, [][]ownerRichButton) {
	cfg := loadPGConfig()
	st := fetchPGMerchantStatus(ctx, cfg)

	serverStatus := "🔴 Belum Terhubung"
	if st.Connected {
		serverStatus = "🟢 Terhubung"
	} else if !cfg.isConfigured() || cfg.AdminKey == "" {
		serverStatus = "⚠️ PG_BASE_URL / PG_ADMIN_KEY Belum Diatur"
	}

	qrisStatus := "⚠️ Belum Ada QRIS (Klik Upload di Bawah)"
	if st.Configured {
		label := st.MerchantName
		if label == "" {
			label = st.MerchantID
		}
		if st.MerchantCity != "" {
			label += " (" + st.MerchantCity + ")"
		}
		qrisStatus = "🟢 Aktif · <code>" + html.EscapeString(label) + "</code>"
	}

	rows := []monitorRichRow{
		{Label: "🌐 Server PG", Value: serverStatus},
		{Label: "🏪 QRIS Merchant", Value: qrisStatus},
		{Label: "📤 Upload QRIS", Value: "Klik tombol di bawah lalu kirim foto QRIS atau teks <code>000201...</code>"},
		{Label: "🧾 /shop status &lt;id&gt;", Value: "Cek detail &amp; status tagihan QRIS pembeli"},
		{Label: "✅ /shop confirm &lt;id&gt;", Value: "Konfirmasi lunas manual &amp; kirim paket otomatis"},
	}

	btnRows := [][]ownerRichButton{
		{{Text: "📤 Upload / Ganti QRIS Statis", Data: "owner_pg:upload", Style: "primary"}},
		{{Text: "🔄 Refresh Status PG", Data: "owner_pg:refresh"}},
	}
	return rows, btnRows
}

func (h *Handler) OwnerPGCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	if !isOwner(cb.From.ID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⛔ Menu PG QRIS ini khusus Owner / Developer.",
			ShowAlert:       true,
		})
		return
	}

	var chatID int64
	var msgID, threadID int
	if cb.Message != nil && cb.Message.Message != nil {
		chatID = cb.Message.Message.Chat.ID
		msgID = cb.Message.Message.ID
		threadID = cb.Message.Message.MessageThreadID
	}
	if chatID == 0 {
		chatID = cb.From.ID
	}

	switch strings.TrimSpace(cb.Data) {
	case "owner_pg:upload":
		h.setPanelState(chatID, ownerPGUploadState)
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "📤 Silakan kirim gambar QRIS statis ke chat ini.",
		})
		promptCard := renderMonitorRichCard("📤 Upload QRIS Statis Merchant", []monitorRichRow{
			{Label: "🖼️ Kirim Foto / File", Value: "Upload gambar PNG/JPG QRIS statis dari merchant apa pun"},
			{Label: "📝 Atau Kirim Teks", Value: "Tempel payload QRIS mentah diawali <code>000201...</code>"},
		}, "ℹ️ Bot otomatis membaca QRIS dan menyimpannya terenkripsi di server PG tanpa perlu edit .env.", false)
		markup := &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "❌ Batal Upload", CallbackData: "owner_pg:cancel"}},
			},
		}
		_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, promptCard, markup)
	case "owner_pg:cancel":
		h.setPanelState(chatID, "")
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "↩️ Upload QRIS dibatalkan.",
		})
		if msgID > 0 {
			text, markup := h.renderOwnerHelp(ctx, 6)
			_, _ = editMonitorRichMessageWithMarkup(ctx, b, chatID, msgID, text, markup)
		}
	case "owner_pg:refresh":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "✅ Status PG diperbarui.",
		})
		if msgID > 0 {
			text, markup := h.renderOwnerHelp(ctx, 6)
			_, _ = editMonitorRichMessageWithMarkup(ctx, b, chatID, msgID, text, markup)
		}
	default:
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	}
}

func (h *Handler) handleOwnerPGQRISUpload(ctx context.Context, b *botgotd.Bot, chatID, senderID int64, text string, message *botgotd.Message) {
	threadID := 0
	if message != nil {
		threadID = message.MessageThreadID
		if message.From != nil && message.From.ID != 0 {
			senderID = message.From.ID
		}
	}
	if !isOwner(senderID) {
		return
	}

	cfg := loadPGConfig()
	if !cfg.isConfigured() || cfg.AdminKey == "" {
		h.setPanelState(chatID, "")
		errCard := renderMonitorRichCard("⚠️ Server PG Belum Siap", []monitorRichRow{
			{Label: "Status", Value: "Variabel <code>PG_BASE_URL</code> / <code>PG_ADMIN_KEY</code> belum diatur"},
		}, "ℹ️ Atur variabel tersebut pada env gateway terlebih dahulu.", false)
		_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, errCard)
		return
	}

	var staticQRIS, b64Img string
	trimmed := strings.TrimSpace(text)
	if message != nil && (len(message.Photo) > 0 || message.Document != nil) {
		imgBytes, err := downloadOwnerPGImageBytes(ctx, b, message)
		if err != nil {
			h.replyOwnerPGUploadError(ctx, b, chatID, threadID, err.Error())
			return
		}
		b64Img = base64.StdEncoding.EncodeToString(imgBytes)
	} else if strings.HasPrefix(trimmed, "000201") {
		staticQRIS = trimmed
	} else {
		h.replyOwnerPGUploadError(ctx, b, chatID, threadID, "Kirimkan foto/gambar QRIS atau teks QRIS valid yang diawali 000201...")
		return
	}

	name, city, err := upsertPGMerchantQRIS(ctx, cfg, staticQRIS, b64Img)
	if err != nil {
		h.replyOwnerPGUploadError(ctx, b, chatID, threadID, err.Error())
		return
	}

	h.setPanelState(chatID, "")
	if name == "" {
		name = cfg.MerchantID
	}
	if city == "" {
		city = "-"
	}
	okCard := renderMonitorRichCard("✅ QRIS Statis Berhasil Dipasang", []monitorRichRow{
		{Label: "🏪 Nama Merchant", Value: "<code>" + html.EscapeString(name) + "</code>"},
		{Label: "📍 Kota", Value: "<code>" + html.EscapeString(city) + "</code>"},
		{Label: "🆔 Merchant ID", Value: "<code>" + html.EscapeString(cfg.MerchantID) + "</code>"},
		{Label: "🛒 Menu Shop", Value: "🟢 Siap menerima pembayaran QRIS otomatis"},
	}, "ℹ️ Menu <b>🛒 Shop 🛒</b> sekarang langsung memakai QRIS statis ini tanpa perlu edit .env.", false)
	_, _ = sendMonitorRichMessage(ctx, b, chatID, threadID, okCard)
}

func (h *Handler) replyOwnerPGUploadError(ctx context.Context, b *botgotd.Bot, chatID int64, threadID int, detail string) {
	card := renderMonitorRichCard("⚠️ Gagal Membaca QRIS", []monitorRichRow{
		{Label: "Detail", Value: html.EscapeString(detail)},
	}, "ℹ️ Pastikan gambar QRIS jelas dan tidak terpotong, lalu kirim ulang gambar atau tekan Batal.", false)
	markup := &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{{Text: "❌ Batal Upload", CallbackData: "owner_pg:cancel"}},
		},
	}
	_, _ = sendMonitorRichMessageWithMarkup(ctx, b, chatID, threadID, card, markup)
}
