package controllers

import (
	"context"
	"fmt"
	"html"

	"assterisk/gateway/internal/botgotd"
)

type helpSubCommandSpec struct {
	Key         string
	Label       string
	Usage       string
	Description string
	PluginOn    bool
}

type helpCategorySpec struct {
	Key         string
	Title       string
	Emoji       string
	Description string
	Commands    []helpSubCommandSpec
}

const helpInitialPromptText = "<b>Berikut adalah fitur asisten</b> <i>Go Userbot</i>"

var cachedHelpInitialMarkup = &botgotd.InlineKeyboardMarkup{
	InlineKeyboard: [][]botgotd.InlineKeyboardButton{
		{{Text: "Load", CallbackData: "assist_help:home"}},
	},
}

var helpCatalog = []helpCategorySpec{
	{
		Key:         "admin",
		Title:       "Admin",
		Emoji:       "🛡️",
		Description: "Perintah moderasi dan manajemen anggota maupun pesan pada grup.",
		Commands: []helpSubCommandSpec{
			{Key: "ban", Label: "Ban", Usage: "!ban [reply|user_id]", Description: "Memblokir pengguna dari grup.", PluginOn: false},
			{Key: "del", Label: "Del", Usage: "!del (reply)", Description: "Menghapus pesan yang dibalas di chat.", PluginOn: false},
			{Key: "demote", Label: "Demote", Usage: "!demote [reply|user_id]", Description: "Menurunkan hak admin anggota grup.", PluginOn: false},
			{Key: "dh", Label: "Dh", Usage: "!dh [jumlah]", Description: "Menghapus riwayat pesan milik sendiri.", PluginOn: false},
			{Key: "fullpromote", Label: "Fullpromote", Usage: "!fullpromote [reply|user_id]", Description: "Mengangkat admin dengan seluruh hak akses.", PluginOn: false},
			{Key: "kick", Label: "Kick", Usage: "!kick [reply|user_id]", Description: "Mengeluarkan pengguna dari grup.", PluginOn: false},
			{Key: "mute", Label: "Mute", Usage: "!mute [reply|user_id]", Description: "Membisukan pengguna agar tidak dapat mengirim pesan.", PluginOn: false},
			{Key: "pin", Label: "Pin", Usage: "!pin (reply)", Description: "Menyematkan pesan penting di grup atau chat.", PluginOn: false},
			{Key: "promote", Label: "Promote", Usage: "!promote [reply|user_id]", Description: "Mengangkat pengguna menjadi admin grup.", PluginOn: false},
			{Key: "purge", Label: "Purge", Usage: "!purge (reply)", Description: "Menghapus banyak pesan sekaligus mulai dari pesan yang dibalas.", PluginOn: false},
			{Key: "unban", Label: "Unban", Usage: "!unban [reply|user_id]", Description: "Membuka blokir pengguna dari grup.", PluginOn: false},
			{Key: "unmute", Label: "Unmute", Usage: "!unmute [reply|user_id]", Description: "Mengembalikan izin kirim pesan pengguna.", PluginOn: false},
			{Key: "unpin", Label: "Unpin", Usage: "!unpin [reply]", Description: "Melepas sematan pesan di grup.", PluginOn: false},
			{Key: "zombies", Label: "Zombies", Usage: "!zombies", Description: "Membersihkan akun terhapus (deleted account) dari grup.", PluginOn: false},
		},
	},
	{
		Key:         "notes",
		Title:       "Notes",
		Emoji:       "📝",
		Description: "Simpan, ambil, ekspor, dan impor catatan cepat atau template inline ber-tombol.",
		Commands: []helpSubCommandSpec{
			{Key: "save", Label: "Save", Usage: "!save <nama> [-cb|-pop] (reply)", Description: "Menyimpan teks/media pesan yang dibalas ke cloud notes.", PluginOn: true},
			{Key: "get", Label: "Get", Usage: "!get <nama> atau #<nama>", Description: "Mengambil dan mengirim catatan tersimpan (mendukung tombol inline).", PluginOn: true},
			{Key: "cat", Label: "Cat", Usage: "!cat <nama> [-nofm]", Description: "Menampilkan catatan atau format mentahnya dengan opsi -nofm.", PluginOn: true},
			{Key: "lsnote", Label: "Lsnote", Usage: "!lsnote atau !notes", Description: "Menampilkan daftar seluruh nama catatan tersimpan.", PluginOn: true},
			{Key: "delnote", Label: "Delnote", Usage: "!delnote <nama|nomor>", Description: "Menghapus satu catatan tersimpan.", PluginOn: true},
			{Key: "clearnote", Label: "Clearnote", Usage: "!clearnote", Description: "Menghapus seluruh catatan milik akun secara permanen.", PluginOn: true},
			{Key: "nofm", Label: "Nofm", Usage: "!nofm [-md] (reply)", Description: "Mengonversi pesan berformat menjadi teks mentah HTML/Markdown.", PluginOn: true},
			{Key: "exportnote", Label: "Exportnote", Usage: "!exportnote", Description: "Mengekspor seluruh catatan menjadi file backup gotdbot-notes.json.", PluginOn: true},
			{Key: "importnote", Label: "Importnote", Usage: "!importnote (reply file .json)", Description: "Mengimpor catatan dari file backup JSON.", PluginOn: true},
		},
	},
	{
		Key:         "broadcast",
		Title:       "Broadcast",
		Emoji:       "📢",
		Description: "Pengiriman siaran pesan massal ke grup, private chat, maupun channel target.",
		Commands: []helpSubCommandSpec{
			{Key: "cancel", Label: "Cancel", Usage: "!cancel atau !gcast cancel", Description: "Menghentikan proses broadcast aktif pada akun.", PluginOn: true},
			{Key: "cancelall", Label: "Cancelall", Usage: "!cancelall", Description: "Menghentikan seluruh antrean broadcast berjalan.", PluginOn: false},
			{Key: "fgcast", Label: "Fgcast", Usage: "!fgcast (reply)", Description: "Forward broadcast pesan ke target grup.", PluginOn: false},
			{Key: "fucast", Label: "Fucast", Usage: "!fucast (reply)", Description: "Forward broadcast pesan ke target private chat.", PluginOn: false},
			{Key: "gcast", Label: "Gcast", Usage: "!gcast -gc|-pc|-ch [-bl] <pesan|reply>", Description: "Mengirim broadcast teks/reply ke grup, PM, atau channel (mendukung blacklist -bl).", PluginOn: true},
			{Key: "memcast", Label: "Memcast", Usage: "!memcast <pesan>", Description: "Broadcast ke daftar anggota target.", PluginOn: false},
			{Key: "spam", Label: "Spam", Usage: "!spam <jumlah> <pesan>", Description: "Mengirim pesan berulang di chat saat ini.", PluginOn: false},
			{Key: "spamcast", Label: "Spamcast", Usage: "!spamcast <jumlah> <pesan>", Description: "Broadcast pesan berulang ke target chat.", PluginOn: false},
			{Key: "ucast", Label: "Ucast", Usage: "!ucast <pesan|reply>", Description: "Mengirim broadcast langsung ke target private chat pengguna.", PluginOn: false},
		},
	},
	{
		Key:         "blacklist",
		Title:       "Blacklist",
		Emoji:       "🚫",
		Description: "Kelola daftar pengecualian target agar tidak ikut menerima siaran broadcast.",
		Commands: []helpSubCommandSpec{
			{Key: "addbl", Label: "Addbl", Usage: "!addbl [-pv|-gc|-ch]", Description: "Menambahkan chat saat ini ke blacklist broadcast (pakai -pv sambil reply di grup untuk target user).", PluginOn: true},
			{Key: "rmbl", Label: "Rmbl", Usage: "!rmbl [-pv|-gc|-ch]", Description: "Menghapus chat/user dari daftar blacklist broadcast.", PluginOn: true},
			{Key: "rmallbl", Label: "Rmallbl", Usage: "!rmallbl", Description: "Menghapus seluruh daftar blacklist broadcast akun.", PluginOn: true},
			{Key: "lsbl", Label: "Lsbl", Usage: "!lsbl", Description: "Menampilkan seluruh target yang masuk daftar blacklist.", PluginOn: true},
		},
	},
	{
		Key:         "extra",
		Title:       "Extra",
		Emoji:       "🧰",
		Description: "Utilitas tambahan userbot: prefix, format kode, logger & filter log, AI, dan alat bantu lain.",
		Commands: []helpSubCommandSpec{
			{Key: "ai", Label: "Ai", Usage: "!ai <pertanyaan>", Description: "Asisten tanya-jawab AI langsung di chat.", PluginOn: false},
			{Key: "azan", Label: "Azan", Usage: "!azan <kota>", Description: "Menampilkan jadwal waktu sholat berdasarkan kota.", PluginOn: false},
			{Key: "code", Label: "Code", Usage: "!code [bahasa] (reply)", Description: "Memformat potongan kode dari pesan yang dibalas dengan syntax highlighting.", PluginOn: true},
			{Key: "eval", Label: "Eval", Usage: "!eval <ekspresi>", Description: "Eksekusi evaluasi runtime (saat ini dibatasi untuk efisiensi RAM).", PluginOn: true},
			{Key: "font", Label: "Font", Usage: "!font <gaya> <teks>", Description: "Mengubah gaya huruf teks Unicode.", PluginOn: false},
			{Key: "fontlist", Label: "Fontlist", Usage: "!fontlist", Description: "Menampilkan daftar gaya font yang tersedia.", PluginOn: false},
			{Key: "invite", Label: "Invite", Usage: "!invite <username|user_id>", Description: "Mengundang pengguna ke dalam grup atau channel.", PluginOn: false},
			{Key: "join", Label: "Join", Usage: "!join <link|username>", Description: "Bergabung ke grup atau channel target.", PluginOn: false},
			{Key: "kickme", Label: "Kickme", Usage: "!kickme", Description: "Keluar dari grup saat ini secara cepat.", PluginOn: false},
			{Key: "logger", Label: "Logger", Usage: "!log on|off, !statuslog, !addblock, !rmblock, !lsblock, !rmallblock", Description: "Mengatur Log Bot PM/Mention (anti-bot, anti-tagall, list blocker log).", PluginOn: true},
			{Key: "mirror", Label: "Mirror", Usage: "!mirror (reply)", Description: "Membalikkan teks/gambar pesan yang dibalas.", PluginOn: false},
			{Key: "quotly", Label: "Quotly", Usage: "!quotly (reply)", Description: "Membuat stiker kutipan (quote sticker) dari pesan.", PluginOn: false},
			{Key: "setprefix", Label: "Setprefix", Usage: "!setp <prefix|none>", Description: "Mengubah atau menonaktifkan simbol awalan perintah userbot.", PluginOn: true},
			{Key: "sh", Label: "Sh", Usage: "!sh <perintah>", Description: "Menjalankan perintah shell server (saat ini dinonaktifkan untuk hemat RAM).", PluginOn: true},
			{Key: "telegraph", Label: "Telegraph", Usage: "!telegraph (reply)", Description: "Mengunggah teks/media menjadi tautan Telegraph.", PluginOn: false},
		},
	},
	{
		Key:         "globals",
		Title:       "Globals",
		Emoji:       "🌐",
		Description: "Moderasi global lintas seluruh grup yang dikelola oleh akun userbot.",
		Commands: []helpSubCommandSpec{
			{Key: "gban", Label: "Gban", Usage: "!gban [reply|user_id]", Description: "Memblokir pengguna secara global di semua grup admin.", PluginOn: false},
			{Key: "gkick", Label: "Gkick", Usage: "!gkick [reply|user_id]", Description: "Mengeluarkan pengguna secara global dari semua grup admin.", PluginOn: false},
			{Key: "gmute", Label: "Gmute", Usage: "!gmute [reply|user_id]", Description: "Membisukan pengguna secara global di semua grup admin.", PluginOn: false},
			{Key: "ungban", Label: "Ungban", Usage: "!ungban [reply|user_id]", Description: "Mencabut global ban pengguna.", PluginOn: false},
			{Key: "ungmute", Label: "Ungmute", Usage: "!ungmute [reply|user_id]", Description: "Mencabut global mute pengguna.", PluginOn: false},
			{Key: "lsgban", Label: "Lsgban", Usage: "!lsgban", Description: "Melihat daftar pengguna dalam global ban.", PluginOn: false},
			{Key: "lsgmute", Label: "Lsgmute", Usage: "!lsgmute", Description: "Melihat daftar pengguna dalam global mute.", PluginOn: false},
		},
	},
	{
		Key:         "info",
		Title:       "Info",
		Emoji:       "ℹ️",
		Description: "Informasi status userbot, masa aktif, server VPS, dan latency MTProto.",
		Commands: []helpSubCommandSpec{
			{Key: "alive", Label: "Alive", Usage: "!alive", Description: "Menampilkan status GO UserBot, UserID, masa aktif (UTC+7), uptime server, dan tombol Ping!.", PluginOn: true},
			{Key: "id", Label: "Id", Usage: "!id [reply]", Description: "Mengambil ID chat, topik, maupun pengguna yang dibalas.", PluginOn: false},
			{Key: "info", Label: "Info", Usage: "!info [reply|username]", Description: "Menampilkan profil singkat pengguna atau chat.", PluginOn: false},
			{Key: "limit", Label: "Limit", Usage: "!limit", Description: "Mengecek status batasan spam akun melalui @SpamBot.", PluginOn: false},
			{Key: "ping", Label: "Ping", Usage: "!ping atau !p", Description: "Mengukur kecepatan respons latency koneksi MTProto userbot.", PluginOn: true},
			{Key: "server", Label: "Server", Usage: "!server", Description: "Menampilkan metrik CPU, RAM, swap, uptime, dan sesi pool sistem.", PluginOn: true},
			{Key: "sg", Label: "Sg", Usage: "!sg [reply|user_id]", Description: "Melihat riwayat nama dan username pengguna.", PluginOn: false},
		},
	},
	{
		Key:         "pmpermit",
		Title:       "PM Permit",
		Emoji:       "🔐",
		Description: "Penjaga pesan masuk private chat otomatis sebelum pengirim disetujui.",
		Commands: []helpSubCommandSpec{
			{Key: "approve", Label: "Approve", Usage: "!approve [reply|user_id]", Description: "Menyetujui pengguna untuk mengirim pesan PM.", PluginOn: false},
			{Key: "approved", Label: "Approved", Usage: "!approved", Description: "Menampilkan daftar pengguna yang sudah disetujui di PM.", PluginOn: false},
			{Key: "disapprove", Label: "Disapprove", Usage: "!disapprove [reply|user_id]", Description: "Mencabut persetujuan PM pengguna.", PluginOn: false},
			{Key: "disapproveall", Label: "Disapproveall", Usage: "!disapproveall", Description: "Mencabut seluruh persetujuan PM tersimpan.", PluginOn: false},
			{Key: "pmpermit", Label: "Pmpermit", Usage: "!pmpermit on|off", Description: "Mengaktifkan atau menonaktifkan guard PM Permit.", PluginOn: false},
			{Key: "setpmtext", Label: "Setpmtext", Usage: "!setpmtext <teks|reply>", Description: "Mengatur teks peringatan otomatis PM Permit.", PluginOn: false},
		},
	},
	{
		Key:         "profile",
		Title:       "Profile",
		Emoji:       "👤",
		Description: "Pengaturan informasi profil akun Telegram userbot.",
		Commands: []helpSubCommandSpec{
			{Key: "setbio", Label: "Setbio", Usage: "!setbio <teks>", Description: "Mengubah teks bio profil akun Telegram.", PluginOn: false},
			{Key: "setname", Label: "Setname", Usage: "!setname <nama>", Description: "Mengubah nama tampilan akun Telegram.", PluginOn: false},
			{Key: "setusername", Label: "Setusername", Usage: "!setusername <username>", Description: "Mengubah username publik akun Telegram.", PluginOn: false},
		},
	},
	{
		Key:         "sticker",
		Title:       "Sticker",
		Emoji:       "🎨",
		Description: "Pembuatan dan pemeriksaan informasi stiker Telegram.",
		Commands: []helpSubCommandSpec{
			{Key: "kang", Label: "Kang", Usage: "!kang [emoji] (reply)", Description: "Menyimpan gambar/stiker yang dibalas ke pack stiker pribadi.", PluginOn: false},
			{Key: "strinfo", Label: "Strinfo", Usage: "!strinfo (reply stiker)", Description: "Menampilkan detail pack, emoji, dan ID stiker yang dibalas.", PluginOn: false},
		},
	},
	{
		Key:         "sudo",
		Title:       "Sudo",
		Emoji:       "🔑",
		Description: "Mengelola daftar pengguna terpercaya yang diizinkan menjalankan perintah userbot.",
		Commands: []helpSubCommandSpec{
			{Key: "addsudo", Label: "Addsudo", Usage: "!addsudo <user_id|@username> atau reply", Description: "Memberikan akses sudo kepada pengguna (mendukung deteksi otomatis dari reply).", PluginOn: true},
			{Key: "clearsudo", Label: "Clearsudo", Usage: "!clearsudo", Description: "Menghapus seluruh akses pengguna sudo.", PluginOn: false},
			{Key: "rmsudo", Label: "Rmsudo", Usage: "!rmsudo <user_id|@username> atau reply", Description: "Mencabut akses sudo dari pengguna.", PluginOn: true},
			{Key: "lsudo", Label: "Lsudo", Usage: "!lsudo", Description: "Menampilkan daftar pengguna yang memiliki akses sudo.", PluginOn: false},
		},
	},
	{
		Key:         "vctools",
		Title:       "Vctools",
		Emoji:       "🎙️",
		Description: "Kontrol Voice Chat / Live Stream grup atau channel.",
		Commands: []helpSubCommandSpec{
			{Key: "joinvc", Label: "Joinvc", Usage: "!joinvc", Description: "Bergabung ke obrolan suara grup aktif.", PluginOn: false},
			{Key: "leavevc", Label: "Leavevc", Usage: "!leavevc", Description: "Keluar dari obrolan suara aktif.", PluginOn: false},
			{Key: "play", Label: "Play", Usage: "!play <judul|link>", Description: "Memutar audio di obrolan suara.", PluginOn: false},
			{Key: "setvc", Label: "Setvc", Usage: "!setvc <judul>", Description: "Mengubah judul obrolan suara grup.", PluginOn: false},
			{Key: "startvc", Label: "Startvc", Usage: "!startvc [judul]", Description: "Memulai obrolan suara baru di grup.", PluginOn: false},
			{Key: "stopvc", Label: "Stopvc", Usage: "!stopvc", Description: "Menghentikan obrolan suara grup.", PluginOn: false},
			{Key: "video", Label: "Video", Usage: "!video <judul|link>", Description: "Memutar video stream di obrolan suara.", PluginOn: false},
			{Key: "ytmusic", Label: "Ytmusic", Usage: "!ytmusic <judul|link>", Description: "Mencari dan memutar musik dari YouTube.", PluginOn: false},
			{Key: "ytvideo", Label: "Ytvideo", Usage: "!ytvideo <judul|link>", Description: "Mencari dan memutar video dari YouTube.", PluginOn: false},
		},
	},
}

var (
	cachedHelpHomeMarkup *botgotd.InlineKeyboardMarkup
	cachedHelpBackMarkup *botgotd.InlineKeyboardMarkup
	cachedHelpPages      map[string]helpPageSnapshot
)

type helpPageSnapshot struct {
	text   string
	markup *botgotd.InlineKeyboardMarkup
}

func init() {
	cachedHelpHomeMarkup = buildHelpHomeKeyboard()
	cachedHelpBackMarkup = &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{{{Text: "Back", CallbackData: "assist_help:home"}}},
	}
	cachedHelpPages = buildHelpPageCache()
}

func buildButtonGridWithBack(buttons []botgotd.InlineKeyboardButton, backData string) *botgotd.InlineKeyboardMarkup {
	rows := make([][]botgotd.InlineKeyboardButton, 0, (len(buttons)+2)/3+1)
	for i := range buttons {
		if i%3 == 0 {
			rows = append(rows, []botgotd.InlineKeyboardButton{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], buttons[i])
	}
	if backData != "" {
		rows = append(rows, []botgotd.InlineKeyboardButton{{Text: "Back", CallbackData: backData}})
	}
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildHelpHomeKeyboard() *botgotd.InlineKeyboardMarkup {
	buttons := make([]botgotd.InlineKeyboardButton, 0, len(helpCatalog))
	for _, cat := range helpCatalog {
		buttons = append(buttons, botgotd.InlineKeyboardButton{
			Text:         cat.Title,
			CallbackData: "assist_help:cat:" + cat.Key,
		})
	}
	return buildButtonGridWithBack(buttons, "")
}

func buildHelpCategoryPage(cat helpCategorySpec) helpPageSnapshot {
	subButtons := make([]botgotd.InlineKeyboardButton, 0, len(cat.Commands))
	activeCount := 0
	for _, cmd := range cat.Commands {
		if cmd.PluginOn {
			activeCount++
		}
		subButtons = append(subButtons, botgotd.InlineKeyboardButton{
			Text:         cmd.Label,
			CallbackData: fmt.Sprintf("assist_help:cmd:%s:%s", cat.Key, cmd.Key),
		})
	}

	pluginSummary := "Load Plugin : Off"
	if activeCount == len(cat.Commands) && activeCount > 0 {
		pluginSummary = "Load Plugin : On"
	} else if activeCount > 0 {
		pluginSummary = fmt.Sprintf("Load Plugin : %d/%d On", activeCount, len(cat.Commands))
	}

	text := fmt.Sprintf(
		`<h1>%s Menu %s</h1>
<blockquote expandable>%s</blockquote>
<table border="1">
  <tr><th>Informasi</th><th>Nilai</th></tr>
  <tr><td>📦 Modul</td><td><code>%s</code></td></tr>
  <tr><td>🔌 Status</td><td><code>%s</code></td></tr>
  <tr><td>📑 Sub Command</td><td><code>%d perintah</code></td></tr>
</table>
<hr/>
<p>Pilih salah satu tombol di bawah untuk melihat detail perintah:</p>`,
		cat.Emoji,
		html.EscapeString(cat.Title),
		html.EscapeString(cat.Description),
		html.EscapeString(cat.Title),
		html.EscapeString(pluginSummary),
		len(cat.Commands),
	)

	return helpPageSnapshot{
		text:   text,
		markup: buildButtonGridWithBack(subButtons, "assist_help:home"),
	}
}

func buildHelpCommandDetailPage(cat helpCategorySpec, cmd helpSubCommandSpec) helpPageSnapshot {
	pluginStatus := "Load Plugin : Off"
	if cmd.PluginOn {
		pluginStatus = "Load Plugin : On"
	}
	text := fmt.Sprintf(
		`<h1>%s %s • %s</h1>
<blockquote expandable>%s</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Keterangan</th></tr>
  <tr><td>📂 Kategori</td><td><code>%s</code></td></tr>
  <tr><td>⌨️ Penggunaan</td><td><code>%s</code></td></tr>
  <tr><td>🔌 Status</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>Tekan <b>Back</b> untuk kembali ke sub menu <b>%s</b>.</p>`,
		cat.Emoji,
		html.EscapeString(cat.Title),
		html.EscapeString(cmd.Label),
		html.EscapeString(cmd.Description),
		html.EscapeString(cat.Title),
		html.EscapeString(cmd.Usage),
		html.EscapeString(pluginStatus),
		html.EscapeString(cat.Title),
	)

	return helpPageSnapshot{
		text: text,
		markup: &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "Back", CallbackData: "assist_help:cat:" + cat.Key}},
			},
		},
	}
}

func buildHelpPageCache() map[string]helpPageSnapshot {
	pages := make(map[string]helpPageSnapshot, 128)
	pages["assist_help:home"] = helpPageSnapshot{
		text: fmt.Sprintf(
			`<h1>🤖 Asisten GO UserBot</h1>
<blockquote expandable>Pilih kategori utama di bawah untuk melihat seluruh sub perintah beserta status modul <b>GO UserBot</b>.</blockquote>
<hr/>
<p>Total tersedia <code>%d kategori</code> fitur userbot:</p>`,
			len(helpCatalog),
		),
		markup: cachedHelpHomeMarkup,
	}

	for _, cat := range helpCatalog {
		catSnap := buildHelpCategoryPage(cat)
		pages["assist_help:cat:"+cat.Key] = catSnap
		for _, cmd := range cat.Commands {
			pages[fmt.Sprintf("assist_help:cmd:%s:%s", cat.Key, cmd.Key)] = buildHelpCommandDetailPage(cat, cmd)
		}
	}

	// Alias kompatibilitas untuk shortcut callback lama
	pages["assist_help:notes"] = pages["assist_help:cat:notes"]
	pages["assist_help:ping"] = pages["assist_help:cmd:info:ping"]
	pages["assist_help:code"] = pages["assist_help:cmd:extra:code"]
	pages["assist_help:server"] = pages["assist_help:cmd:info:server"]
	return pages
}

func helpInitialKeyboard() *botgotd.InlineKeyboardMarkup {
	return cachedHelpInitialMarkup
}

func helpHomeKeyboard() *botgotd.InlineKeyboardMarkup {
	return cachedHelpHomeMarkup
}

func (h *Handler) sendHelpInitialPrompt(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil {
		return
	}
	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:          update.Message.Chat.ID,
		MessageThreadID: update.Message.MessageThreadID,
		Text:            helpInitialPromptText,
		ParseMode:       "HTML",
		ReplyMarkup:     helpInitialKeyboard(),
	})
}
