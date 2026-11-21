package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"strconv"
	"strings"
)

type ownerHelpPageSpec struct {
	TabLabel string
	Title    string
	Rows     []monitorRichRow
	Quote    string
}

var ownerHelpPages = []ownerHelpPageSpec{
	{
		TabLabel: "🔑 Token",
		Title:    "🔑 Owner Help · Lisensi &amp; Token",
		Rows: []monitorRichRow{
			{Label: "📖 /token", Value: "Panduan singkat &amp; format durasi perintah token"},
			{Label: "🆓 /token -free", Value: "Buat / lihat shared voucher Free massal (default 7h)"},
			{Label: "💎 /token pro &lt;durasi&gt;", Value: "Terbitkan voucher personal paket <code>Pro</code>"},
			{Label: "👑 /token pro++ &lt;durasi&gt;", Value: "Terbitkan voucher personal <code>Pro++</code> (Custom WM)"},
			{Label: "♾️ /token vip &lt;durasi&gt;", Value: "Terbitkan voucher personal <code>VIP</code> Unlimited"},
			{Label: "⬆️ /token upgrade &lt;uid&gt; &lt;tier&gt; &lt;durasi&gt;", Value: "Naikkan tier akun aktif user secara langsung"},
			{Label: "⏳ /token extend &lt;uid&gt; &lt;durasi&gt;", Value: "Perpanjang masa aktif lisensi aktif user"},
			{Label: "🔍 /token info &lt;uid|token&gt;", Value: "Cek detail status lisensi, slot &amp; masa aktif"},
			{Label: "🚫 /token revoke &lt;uid|token&gt;", Value: "Cabut lisensi / voucher dan putuskan sesi"},
		},
		Quote: "ℹ️ Format durasi: <b>h</b> (Hari), <b>j</b> (Jam), <b>m</b> (Menit). Contoh: <code>/token pro++ 30h</code> · Halaman 1/6.",
	},
	{
		TabLabel: "⚙️ Worker",
		Title:    "⚙️ Owner Help · Multi-Worker &amp; LB",
		Rows: []monitorRichRow{
			{Label: "📊 /worker status", Value: "Lihat pool aktif (<code>ubot-1</code>, <code>ubot-2</code>, <code>ubot-hell</code>), beban user &amp; RAM"},
			{Label: "🔀 /worker move &lt;uid&gt; &lt;worker_id&gt;", Value: "Pindahkan sesi user ke worker spesifik tanpa login ulang"},
			{Label: "🔥 /worker move &lt;uid&gt; &lt;hell|normal&gt;", Value: "Pindahkan user ke pool <code>hell</code> atau <code>normal</code> least-loaded"},
			{Label: "🚧 /worker drain &lt;worker_id&gt;", Value: "Keluarkan worker dari rotasi &amp; migrasikan seluruh usernya"},
			{Label: "✅ /worker undrain &lt;worker_id&gt;", Value: "Aktifkan kembali worker agar menerima user baru &amp; failover"},
		},
		Quote: "ℹ️ Auto-HELL memindah user berat (skor &gt;= 70 selama 3x cek) ke <code>ubot-hell</code> dan mengembalikan setelah &gt;= 6 jam jika skor &lt;= 25 · Halaman 2/6.",
	},
	{
		TabLabel: "📊 Server",
		Title:    "📊 Owner Help · Server &amp; Simulasi",
		Rows: []monitorRichRow{
			{Label: "💻 /server atau !server", Value: "Metrik host VPS, RAM/CPU container, Redis broker &amp; tombol Refresh"},
			{Label: "🧪 /simulasi", Value: "Bantuan simulasi beban virtual userbot (tanpa MTProto asli)"},
			{Label: "🚀 /simulasi &lt;jumlah&gt;", Value: "Jalankan beban virtual (contoh: <code>/simulasi 100</code>)"},
			{Label: "📈 /simulasi status", Value: "Cek metrik P50/P95/P99 latency &amp; throughput simulasi"},
			{Label: "🛑 /simulasi off", Value: "Hentikan &amp; bersihkan seluruh virtual userbot simulasi"},
			{Label: "⚡ /ping", Value: "Uji konektivitas &amp; respons worker userbot aktif"},
			{Label: "🧾 !code [bahasa]", Value: "Format ulang teks pesan yang di-reply ke blok kode"},
		},
		Quote: "ℹ️ Gunakan <code>/server</code> untuk memantau VPS live dan <code>/simulasi</code> untuk stress-test aman sebelum rilis · Halaman 3/6.",
	},
	{
		TabLabel: "🧭 Monitor",
		Title:    "🧭 Owner Help · Panel &amp; Topik Monitor",
		Rows: []monitorRichRow{
			{Label: "🎛️ /start atau /status", Value: "Panel utama bot, status userbot &amp; instalasi token"},
			{Label: "📢 /broadcast &lt;pesan&gt;", Value: "Kirim job broadcast langsung via gateway ke worker"},
			{Label: "🔌 /plugin &lt;uid|@user&gt;", Value: "Atur On/Off fitur &amp; sub-fitur khusus user tertentu"},
			{Label: "📖 /help &amp; /help user", Value: "Buka Owner Help ini atau menu asisten user biasa"},
			{Label: "🚀 MONITOR_BOT_LOADER", Value: "Startup gateway/ubot, migrasi worker &amp; failover otomatis"},
			{Label: "🗄️ MONITOR_DATABASE", Value: "Status berkala SQLite WAL, MongoDB backup &amp; Redis L2"},
			{Label: "📊 MONITOR_TOPIC", Value: "Dasbor Telemetry Live &amp; Alert Digest kejadian sesi"},
			{Label: "🎫 MONITOR_TOKEN_*", Value: "Log token dibuat, diklaim, brankas backup &amp; kedaluwarsa"},
			{Label: "🐞 MONITOR_TRACEBACK", Value: "Peringatan error runtime, panic &amp; anomali handler"},
		},
		Quote: "ℹ️ Ketik <code>/help user</code> bila ingin membuka menu bantuan userbot reguler · Halaman 4/6.",
	},
	{
		TabLabel: "🔌 Plugin",
		Title:    "🔌 Owner Help · Kontrol Plugin Global",
		Quote:    "ℹ️ Ketuk nama modul di bawah untuk atur ON/OFF modul utama maupun sub-command secara global. Khusus per-user pakai <code>/plugin &lt;uid|@username&gt;</code> · Halaman 5/6.",
	},
	{
		TabLabel: "💳 PG",
		Title:    "💳 Owner Help · Payment Gateway QRIS",
		Quote:    "ℹ️ Klik tombol Upload QRIS Statis di bawah lalu kirim gambar/foto QRIS merchant Anda langsung ke chat ini tanpa perlu mengatur .env · Halaman 6/6.",
	},
}

func resolveOwnerHelpPageArg(arg string) int {
	a := strings.ToLower(strings.TrimSpace(arg))
	if n, err := strconv.Atoi(a); err == nil && n >= 1 && n <= len(ownerHelpPages) {
		return n
	}
	switch {
	case strings.Contains(a, "tok") || strings.Contains(a, "lic"):
		return 1
	case strings.Contains(a, "work") || strings.Contains(a, "lb") || strings.Contains(a, "hell"):
		return 2
	case strings.Contains(a, "serv") || strings.Contains(a, "sim") || strings.Contains(a, "vps"):
		return 3
	case strings.Contains(a, "mon") || strings.Contains(a, "top") || strings.Contains(a, "panel"):
		return 4
	case strings.Contains(a, "plug") || strings.Contains(a, "fitur") || strings.Contains(a, "mod"):
		return 5
	case strings.Contains(a, "pg") || strings.Contains(a, "qris") || strings.Contains(a, "shop") || strings.Contains(a, "pay"):
		return 6
	default:
		return 1
	}
}

func renderOwnerHelpPage(page int) (string, *botgotd.InlineKeyboardMarkup) {
	return (*Handler)(nil).renderOwnerHelp(context.Background(), page)
}

func buildOwnerHelpNavRows(page int) [][]botgotd.InlineKeyboardButton {
	total := len(ownerHelpPages)
	keyboard := make([][]botgotd.InlineKeyboardButton, 0, 5)
	tabRow := make([]botgotd.InlineKeyboardButton, 0, 3)
	for i, item := range ownerHelpPages {
		label := item.TabLabel
		if i+1 == page {
			label = "• " + label
		}
		tabRow = append(tabRow, botgotd.InlineKeyboardButton{
			Text:         label,
			CallbackData: fmt.Sprintf("owner_help:page:%d", i+1),
		})
		if len(tabRow) == 3 {
			keyboard = append(keyboard, tabRow)
			tabRow = make([]botgotd.InlineKeyboardButton, 0, 3)
		}
	}
	if len(tabRow) > 0 {
		keyboard = append(keyboard, tabRow)
	}

	navRow := make([]botgotd.InlineKeyboardButton, 0, 3)
	if page > 1 {
		navRow = append(navRow, botgotd.InlineKeyboardButton{
			Text:         "⬅️ Prev",
			CallbackData: fmt.Sprintf("owner_help:page:%d", page-1),
		})
	}
	navRow = append(navRow, botgotd.InlineKeyboardButton{
		Text:         fmt.Sprintf("📄 %d / %d", page, total),
		CallbackData: "owner_help:noop",
	})
	if page < total {
		navRow = append(navRow, botgotd.InlineKeyboardButton{
			Text:         "Next ➡️",
			CallbackData: fmt.Sprintf("owner_help:page:%d", page+1),
		})
	}

	keyboard = append(keyboard, navRow, []botgotd.InlineKeyboardButton{
		{Text: "🗑️ Tutup", CallbackData: "owner_help:close"},
	})
	return keyboard
}

func (h *Handler) renderOwnerHelp(ctx context.Context, page int) (string, *botgotd.InlineKeyboardMarkup) {
	total := len(ownerHelpPages)
	if page < 1 {
		page = 1
	}
	if page > total {
		page = total
	}
	spec := ownerHelpPages[page-1]
	rows := spec.Rows
	richButtons := ""

	if page == 5 {
		var btnRows [][]ownerRichButton
		rows, btnRows = h.appendOwnerPluginGrid(ctx, 0, make([]monitorRichRow, 0, len(ownerPluginSpecs)))
		richButtons = renderOwnerRichButtonRows(btnRows)
	} else if page == 6 {
		var btnRows [][]ownerRichButton
		rows, btnRows = h.appendOwnerPGHelp(ctx)
		richButtons = renderOwnerRichButtonRows(btnRows)
	}

	text := renderMonitorRichCard(spec.Title, rows, spec.Quote, false)
	if richButtons != "" {
		text += "\n" + richButtons
	}
	return text, &botgotd.InlineKeyboardMarkup{InlineKeyboard: buildOwnerHelpNavRows(page)}
}

func (h *Handler) SendOwnerHelp(ctx context.Context, b *botgotd.Bot, update *botgotd.Update, page int) {
	if update == nil || update.Message == nil {
		return
	}
	globalDevMonitor.ObserveIncomingChat(update.Message.Chat)
	text, markup := h.renderOwnerHelp(ctx, page)
	_, _ = sendMonitorRichMessageWithMarkup(ctx, b, update.Message.Chat.ID, update.Message.MessageThreadID, text, markup)
}

func (h *Handler) OwnerHelpCallback(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	cb := update.CallbackQuery
	if !isOwner(cb.From.ID) {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "⛔ Menu bantuan ini khusus Owner / Developer.",
			ShowAlert:       true,
		})
		return
	}

	data := strings.TrimSpace(cb.Data)
	if data == "owner_help:noop" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Halaman aktif",
		})
		return
	}

	if data == "owner_help:close" {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Menu bantuan ditutup",
		})
		if cb.Message != nil && cb.Message.Message != nil {
			_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{
				ChatID:    cb.Message.Message.Chat.ID,
				MessageID: cb.Message.Message.ID,
			})
		}
		return
	}

	page := 1
	if strings.HasPrefix(data, "owner_help:page:") {
		if parsed, err := strconv.Atoi(strings.TrimPrefix(data, "owner_help:page:")); err == nil {
			page = parsed
		}
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
	})

	if cb.Message == nil || cb.Message.Message == nil {
		return
	}
	msg := cb.Message.Message
	text, markup := h.renderOwnerHelp(ctx, page)
	_, _ = editMonitorRichMessageWithMarkup(ctx, b, msg.Chat.ID, msg.ID, text, markup)
}
