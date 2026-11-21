package assets

import (
	"strconv"

	"assterisk/gateway/internal/botgotd"
)

const (
	BtnStatus   = "💎 Status 💎"
	BtnShop     = "🛒 Shop 🛒"
	BtnGrupList = "🧩 Grup & List 🧩"
	BtnExpert   = "⚡ Expert Extra ⚡"
	BtnReset    = "🗑️ Reset 🗑️"
	BtnAturJeda = "⌛ Atur Jeda ⌛"
	BtnBantuan  = "⚠️ Bantuan ⚠️"
	BtnSettings = "⚙️ Settings ⚙️"
	BtnFeed     = "📰 Feed 📰"
	BtnWebapp   = "🌐 Webapp 🌐"
	BtnHome     = "🌐 Home 🌐"
)

const (
	BtnCustomWatermark = "📌 Custom Watermark 📌"
	BtnAturEmail       = "✉️ Atur Email ✉️"
	BtnSafemode        = "⚠️ Safemode ⚠️"
	BtnTimer           = "⏱️ Timer ⏱️"
	BtnUbahMode        = "👑 Ubah Mode 👑"
	BtnAdmin           = "👤 Admin 👤"
	BtnUbahStatus      = "📊 Ubah Status 📊"
	BtnNotifikasi      = "🔔 Notifikasi 🔔"
)

const (
	BtnCekTopic   = "🧑‍💻 Cek Topic 🧑‍💻"
	BtnTopic      = "🧑‍💻 Topic 🧑‍💻"
	BtnListTopic  = "📌 List Topic 📌"
	BtnSemuaTopic = "📂 Semua Topic 📂"
	BtnHapusTopic = "🗑️ Hapus Topic 🗑️"
	BtnCekList    = "📁 Cek List 📁"
	BtnCekGrup    = "📚 Cek Grup 📚"
	BtnTambahList = "📝 Tambah List 📝"
	BtnTambahGrup = "📝 Tambah Grup 📝"
	BtnSebarKeRC  = "📦 Sebar Ke RC 📦"
)

const (
	BtnTambahkanSemuaGrup = "🔥 Tambahkan Semua Grup Dalam Akun 🔥"
	BtnTambahGrupManual   = "📝 Tambahkan Grup Manual 📝"
)

const (
	BtnHapusSemuaGrup   = "🗑️ Hapus Semua Grup 🗑️"
	BtnHapusSemuaList   = "🗑️ Hapus Semua List 🗑️"
	BtnResetKonfigurasi = "🗑️ Reset Konfigurasi 🗑️"
)

const (
	BtnJedaAman         = "🎁 Jeda Aman 🎁"
	BtnJedaOtomatis     = "💡 Jeda Otomatis 💡"
	BtnJedaPerGrup      = "🔗 Jeda Per Grup 🔗"
	BtnJedaPerSemuaGrup = "🖇️ Jeda Per Semua Grup 🖇️"
	BtnSkipFloodWait    = "⏭️ Skip FloodWait ⏭️"
	BtnMatikan          = "🔕 Matikan 🔕"
	BtnSkipFW30s        = "⏱️ 30 Detik ⏱️"
	BtnSkipFW1m         = "⏱️ 1 Menit ⏱️"
	BtnSkipFW2m         = "⏱️ 2 Menit ⏱️"
	BtnSkipFW3m         = "⏱️ 3 Menit ⏱️"
	BtnSkipFW5m         = "⏱️ 5 Menit ⏱️"
	BtnSkipFW10m        = "⏱️ 10 Menit ⏱️"
	BtnTimerPreset1     = "⏰ 08:00 - 22:00 ⏰"
	BtnTimerPreset2     = "⏰ 07:00 - 23:00 ⏰"
	BtnTimerPreset3     = "⏰ 09:00 - 21:00 ⏰"
	BtnTimerPreset4     = "⏰ 06:00 - 22:00 ⏰"
	BtnTimerNonaktif    = BtnMatikan
)

const (
	BtnModeNormal  = "👑 Normal 👑"
	BtnModeFast    = "⚡ Fast ⚡"
	BtnModeSilent  = "🔕 Silent 🔕"
	BtnModeForward = "📨 Forward 📨"
)

const (
	BtnUpgradeToken = "🔑 Tambah Token 🔑"
	BtnGantiToken   = "🔄 Ganti Token 🔄"
	BtnAmbilToken   = "🔒 Ambil Token 🔒"
	BtnPindahAkses  = "🔀 Pindah Akses 🔀"
	BtnBackupData   = "📎 Backup Data 📎"
	BtnRestoreData  = "♻️ Restore Data ♻️"
)

const (
	BtnInputToken     = "🔑 Input Token 🔑"
	BtnBuatUserbot    = "🚀 Buat Userbot 🚀"
	BtnControl        = "🎯 Control 🎯"
	BtnControlAlt     = "🎮 Control 🎮"
	BtnBatal          = "⬅️ Batal ⬅️"
	BtnKembali        = "◀️ Kembali ◀️"
	BtnBagikanKontak  = "📱 Bagikan Kontak 📱"
	BtnHapusWatermark = "🗑️ Hapus Watermark 🗑️"
)

var (
	MainMenuMarkup          *botgotd.ReplyKeyboardMarkup
	StatusMenuMarkup        *botgotd.ReplyKeyboardMarkup
	SettingsMenuMarkup      *botgotd.ReplyKeyboardMarkup
	GrupListMenuMarkup      *botgotd.ReplyKeyboardMarkup
	TambahGrupOptionsMarkup *botgotd.ReplyKeyboardMarkup
	CancelOnlyMarkup       *botgotd.ReplyKeyboardMarkup
	AturJedaMarkup         *botgotd.ReplyKeyboardMarkup
	JedaPerGrupMarkup      *botgotd.ReplyKeyboardMarkup
	JedaPerSemuaGrupMarkup *botgotd.ReplyKeyboardMarkup
	SkipFloodWaitMarkup    *botgotd.ReplyKeyboardMarkup
	UbahModeMarkup         *botgotd.ReplyKeyboardMarkup
	TimerMarkup            *botgotd.ReplyKeyboardMarkup
	ResetMarkup            *botgotd.ReplyKeyboardMarkup
	HapusListMarkup        *botgotd.ReplyKeyboardMarkup
	HapusGrupMarkup        *botgotd.ReplyKeyboardMarkup
	CancelMarkup           *botgotd.ReplyKeyboardMarkup
	CreateChoiceMarkup     *botgotd.ReplyKeyboardMarkup
	InstallPhoneMarkup     *botgotd.ReplyKeyboardMarkup
	WatermarkPromptMarkup  *botgotd.ReplyKeyboardMarkup
	RemoveKeyboard         *botgotd.ReplyKeyboardRemove
)

func init() {
	MainMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnStatus}, {Text: BtnShop}},
			{{Text: BtnGrupList}, {Text: BtnExpert}},
			{{Text: BtnReset}, {Text: BtnAturJeda}, {Text: BtnBantuan}},
			{{Text: BtnSettings}, {Text: BtnFeed}, {Text: BtnWebapp}},
		},
		ResizeKeyboard: true,
	}

	StatusMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnCustomWatermark}},
			{{Text: BtnAturEmail}, {Text: BtnSafemode}},
			{{Text: BtnTimer}, {Text: BtnUbahMode}, {Text: BtnAdmin}},
			{{Text: BtnUbahStatus}, {Text: BtnNotifikasi}},
			{{Text: BtnHome}},
		},
		ResizeKeyboard: true,
	}

	GrupListMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnCekTopic}, {Text: BtnTopic}, {Text: BtnListTopic}},
			{{Text: BtnCekList}, {Text: BtnCekGrup}},
			{{Text: BtnTambahList}, {Text: BtnTambahGrup}},
			{{Text: BtnSebarKeRC}, {Text: BtnHome}},
		},
		ResizeKeyboard: true,
	}

	TambahGrupOptionsMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnTambahkanSemuaGrup}},
			{{Text: BtnTambahGrupManual}},
			{{Text: BtnHome}},
		},
		ResizeKeyboard: true,
	}

	CancelOnlyMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard:       [][]botgotd.KeyboardButton{{{Text: BtnBatal}}},
		ResizeKeyboard: true,
	}
	CancelMarkup = CancelOnlyMarkup

	AturJedaMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnJedaAman}, {Text: BtnJedaOtomatis}},
			{{Text: BtnJedaPerGrup}, {Text: BtnJedaPerSemuaGrup}},
			{{Text: BtnSkipFloodWait}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	perGrupRows := buildNumberRangeRows(5, 300, 5, 5)
	perGrupRows = append(perGrupRows, []botgotd.KeyboardButton{{Text: BtnMatikan}})
	perGrupRows = append(perGrupRows, []botgotd.KeyboardButton{{Text: BtnBatal}})
	JedaPerGrupMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard:       perGrupRows,
		ResizeKeyboard: true,
	}

	perSemuaRows := buildNumberRangeRows(2, 120, 2, 5)
	perSemuaRows = append(perSemuaRows, []botgotd.KeyboardButton{{Text: "240"}, {Text: "360"}})
	perSemuaRows = append(perSemuaRows, []botgotd.KeyboardButton{{Text: BtnMatikan}})
	perSemuaRows = append(perSemuaRows, []botgotd.KeyboardButton{{Text: BtnBatal}})
	JedaPerSemuaGrupMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard:       perSemuaRows,
		ResizeKeyboard: true,
	}

	SkipFloodWaitMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnSkipFW30s}, {Text: BtnSkipFW1m}, {Text: BtnSkipFW2m}},
			{{Text: BtnSkipFW3m}, {Text: BtnSkipFW5m}, {Text: BtnSkipFW10m}},
			{{Text: BtnMatikan}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	UbahModeMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnModeNormal}, {Text: BtnModeFast}},
			{{Text: BtnModeSilent}, {Text: BtnModeForward}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	SettingsMenuMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnUpgradeToken}, {Text: BtnGantiToken}},
			{{Text: BtnBackupData}, {Text: BtnRestoreData}},
			{{Text: BtnAmbilToken}, {Text: BtnPindahAkses}},
			{{Text: BtnStatus}, {Text: BtnHome}},
		},
		ResizeKeyboard: true,
	}

	TimerMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnTimerPreset1}, {Text: BtnTimerPreset2}},
			{{Text: BtnTimerPreset3}, {Text: BtnTimerPreset4}},
			{{Text: BtnMatikan}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	ResetMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnHapusSemuaGrup}, {Text: BtnHapusSemuaList}},
			{{Text: BtnResetKonfigurasi}},
			{{Text: BtnHome}},
		},
		ResizeKeyboard: true,
	}

	HapusListMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnHapusSemuaList}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	HapusGrupMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnHapusSemuaGrup}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	CreateChoiceMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnInputToken}, {Text: BtnBuatUserbot}},
			{{Text: BtnBantuan}, {Text: BtnControl}},
		},
		ResizeKeyboard: true,
	}

	InstallPhoneMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnBagikanKontak, RequestContact: true}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}

	WatermarkPromptMarkup = &botgotd.ReplyKeyboardMarkup{
		Keyboard: [][]botgotd.KeyboardButton{
			{{Text: BtnHapusWatermark}},
			{{Text: BtnBatal}},
		},
		ResizeKeyboard: true,
	}

	RemoveKeyboard = &botgotd.ReplyKeyboardRemove{RemoveKeyboard: true}
}

func buildNumberRangeRows(start, end, step, cols int) [][]botgotd.KeyboardButton {
	rows := make([][]botgotd.KeyboardButton, 0, ((end-start)/step+1)/cols+1)
	row := make([]botgotd.KeyboardButton, 0, cols)
	for v := start; v <= end; v += step {
		row = append(row, botgotd.KeyboardButton{Text: strconv.Itoa(v)})
		if len(row) == cols {
			rows = append(rows, row)
			row = make([]botgotd.KeyboardButton, 0, cols)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return rows
}
