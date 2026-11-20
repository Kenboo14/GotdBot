package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
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
		Quote: "ℹ️ Format durasi: <b>h</b> (Hari), <b>j</b> (Jam), <b>m</b> (Menit). Contoh: <code>/token pro++ 30h</code> · Halaman 1/5.",
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
		Quote: "ℹ️ Auto-HELL memindah user berat (skor &gt;= 70 selama 3x cek) ke <code>ubot-hell</code> dan mengembalikan setelah &gt;= 6 jam jika skor &lt;= 25 · Halaman 2/5.",
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
		Quote: "ℹ️ Gunakan <code>/server</code> untuk memantau VPS live dan <code>/simulasi</code> untuk stress-test aman sebelum rilis · Halaman 3/5.",
	},
	{
		TabLabel: "🧭 Monitor",
		Title:    "🧭 Owner Help · Panel &amp; Topik Monitor",
		Rows: []monitorRichRow{
			{Label: "🎛️ /start atau /status", Value: "Panel utama bot, status userbot &amp; instalasi token"},
			{Label: "📢 /broadcast &lt;pesan&gt;", Value: "Kirim job broadcast langsung via gateway ke worker"},
			{Label: "📖 /help &amp; /help user", Value: "Buka Owner Help ini atau menu asisten user biasa"},
			{Label: "🚀 MONITOR_BOT_LOADER", Value: "Startup gateway/ubot, migrasi worker &amp; failover otomatis"},
			{Label: "🗄️ MONITOR_DATABASE", Value: "Status berkala SQLite WAL, MongoDB backup &amp; Redis L2"},
			{Label: "📊 MONITOR_TOPIC", Value: "Dasbor Telemetry Live &amp; Alert Digest kejadian sesi"},
			{Label: "🎫 MONITOR_TOKEN_*", Value: "Log token dibuat, diklaim, brankas backup &amp; kedaluwarsa"},
			{Label: "🐞 MONITOR_TRACEBACK", Value: "Peringatan error runtime, panic &amp; anomali handler"},
		},
		Quote: "ℹ️ Ketik <code>/help user</code> bila ingin membuka menu bantuan userbot reguler · Halaman 4/5.",
	},
	{
		TabLabel: "🔌 Plugin",
		Title:    "🔌 Owner Help · Kontrol Plugin Userbot",
		Quote:    "ℹ️ Ketuk tombol inline di bawah untuk menghidupkan (🟢) atau mematikan (🔴) plugin secara real-time di seluruh worker · Halaman 5/5.",
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
	default:
		return 1
	}
}

func renderOwnerHelpPage(page int) (string, *botgotd.InlineKeyboardMarkup) {
	return (*Handler)(nil).renderOwnerHelp(context.Background(), page)
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
	keyboard := make([][]botgotd.InlineKeyboardButton, 0, 9)

	if page == total {
		states := h.getPluginStates(ctx)
		rows = make([]monitorRichRow, 0, len(ownerPluginSpecs))
		btnRow := make([]botgotd.InlineKeyboardButton, 0, 3)
		for _, sp := range ownerPluginSpecs {
			enabled := states[sp.Key]
			icon := "🟢"
			statusText := "🟢 ON"
			if !enabled {
				icon = "🔴"
				statusText = "🔴 OFF"
			}
			rows = append(rows, monitorRichRow{
				Label: statusText + " · " + sp.Title,
				Value: "<code>" + html.EscapeString(sp.Commands) + "</code>",
			})
			btnRow = append(btnRow, botgotd.InlineKeyboardButton{
				Text:         icon + " " + sp.Title,
				CallbackData: "owner_help:plug:" + sp.Key,
			})
			if len(btnRow) == 3 {
				keyboard = append(keyboard, btnRow)
				btnRow = make([]botgotd.InlineKeyboardButton, 0, 3)
			}
		}
		if len(btnRow) > 0 {
			keyboard = append(keyboard, btnRow)
		}
		keyboard = append(keyboard, []botgotd.InlineKeyboardButton{
			{Text: "✅ Aktifkan Semua", CallbackData: "owner_help:plug_all:1"},
			{Text: "⛔ Matikan Semua", CallbackData: "owner_help:plug_all:0"},
		})
	}

	text := renderMonitorRichCard(spec.Title, rows, spec.Quote, false)

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
	return text, &botgotd.InlineKeyboardMarkup{InlineKeyboard: keyboard}
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
	toast := ""
	switch {
	case strings.HasPrefix(data, "owner_help:page:"):
		if parsed, err := strconv.Atoi(strings.TrimPrefix(data, "owner_help:page:")); err == nil {
			page = parsed
		}
	case strings.HasPrefix(data, "owner_help:plug:"):
		page = len(ownerHelpPages)
		modKey := strings.TrimPrefix(data, "owner_help:plug:")
		if sp, next, ok := h.togglePluginState(ctx, modKey); ok {
			if next {
				toast = fmt.Sprintf("🟢 Plugin %s diaktifkan", sp.Title)
			} else {
				toast = fmt.Sprintf("🔴 Plugin %s dinonaktifkan", sp.Title)
			}
		}
	case strings.HasPrefix(data, "owner_help:plug_all:"):
		page = len(ownerHelpPages)
		enable := strings.TrimPrefix(data, "owner_help:plug_all:") != "0"
		h.setAllPluginStates(ctx, enable)
		if enable {
			toast = "✅ Seluruh plugin userbot diaktifkan"
		} else {
			toast = "⛔ Seluruh plugin userbot dinonaktifkan"
		}
	}

	_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            toast,
	})

	if cb.Message == nil || cb.Message.Message == nil {
		return
	}
	msg := cb.Message.Message
	text, markup := h.renderOwnerHelp(ctx, page)
	_, editErr := editMonitorRichMessageWithMarkup(ctx, b, msg.Chat.ID, msg.ID, text, markup)
	if editErr != nil && !isMessageNotModifiedErr(editErr) {
		if _, sendErr := sendMonitorRichMessageWithMarkup(ctx, b, msg.Chat.ID, msg.MessageThreadID, text, markup); sendErr == nil {
			_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{
				ChatID:    msg.Chat.ID,
				MessageID: msg.ID,
			})
		}
	}
}
