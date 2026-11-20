package assets

import (
	"strconv"

	"assterisk/gateway/internal/botgotd"
)

// ─── Reply Keyboard Button Label Constants ───────────────────────────────────

// Main Menu
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

// Status Menu
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

// Grup & List Menu
const (
	BtnCekTopic   = "🧑‍💻 Cek Topic 🧑‍💻"
	BtnTopic      = "🧑‍💻 Topic 🧑‍💻"
	BtnListTopic  = "📌 List Topic"
	BtnSemuaTopic = "📂 Semua Topic"
	BtnHapusTopic = "🗑️ Hapus Topic"
	BtnCekList    = "📁 Cek List 📁"
	BtnCekGrup    = "📚 Cek Grup 📚"
	BtnTambahList = "📝 Tambah List 📝"
	BtnTambahGrup = "📝 Tambah Grup 📝"
	BtnSebarKeRC  = "📦 Sebar Ke RC 📦"
)

// Tambah Grup Options
const (
	BtnTambahkanSemuaGrup = "🔥 Tambahkan Semua Grup Dalam Akun 🔥"
	BtnTambahGrupManual   = "📝 Tambahkan Grup Manual 📝"
)

// Reset Menu
const (
	BtnHapusSemuaGrup   = "🗑️ Hapus Semua Grup 🗑️"
	BtnHapusSemuaList   = "🗑️ Hapus Semua List 🗑️"
	BtnResetKonfigurasi = "🗑️ Reset Konfigurasi 🗑️"
)

const (
	BtnJedaAman         = "🎁 Jeda Aman"
	BtnJedaOtomatis     = "💡 Jeda Otomatis"
	BtnJedaPerGrup      = "🔗 Jeda Per Grup 🔗"
	BtnJedaPerSemuaGrup = "🖇️ Jeda Per Semua Grup 🖇️"
	BtnSkipFloodWait    = "⏭️ Skip FloodWait"
	BtnMatikan          = "🔕 Matikan"
	BtnSkipFW30s        = "⏱️ 30 Detik"
	BtnSkipFW1m         = "⏱️ 1 Menit"
	BtnSkipFW2m         = "⏱️ 2 Menit"
	BtnSkipFW3m         = "⏱️ 3 Menit"
	BtnSkipFW5m         = "⏱️ 5 Menit"
	BtnSkipFW10m        = "⏱️ 10 Menit"
	BtnTimerPreset1     = "⏰ 08:00 - 22:00"
	BtnTimerPreset2     = "⏰ 07:00 - 23:00"
	BtnTimerPreset3     = "⏰ 09:00 - 21:00"
	BtnTimerPreset4     = "⏰ 06:00 - 22:00"
	BtnTimerNonaktif    = BtnMatikan
)

// Ubah Mode
const (
	BtnModeNormal = "👑 Normal"
	BtnModeFast   = "⚡ Fast"
	BtnModeSilent = "🔕 Silent"
)

// Install / Wizard
const (
	BtnInputToken     = "🔑 Input Token 🔑"
	BtnBuatUserbot    = "🚀 Buat Userbot 🚀"
	BtnControl        = "🎯 Control 🎯"
	BtnControlAlt     = "🎮 Control 🎮"
	BtnBatal          = "⬅️ Batal ⬅️"
	BtnBagikanKontak  = "📱 Bagikan Kontak"
	BtnHapusWatermark = "🗑️ Hapus Watermark 🗑️"
)

// ─── Pre-built Reply Keyboard Markups ────────────────────────────────────────

var (
	// MainMenuMarkup is the main bot control panel keyboard.
	MainMenuMarkup *botgotd.ReplyKeyboardMarkup

	// StatusMenuMarkup is the Status sub-menu keyboard.
	StatusMenuMarkup *botgotd.ReplyKeyboardMarkup

	// GrupListMenuMarkup is the Grup & List sub-menu keyboard.
	GrupListMenuMarkup *botgotd.ReplyKeyboardMarkup

	// TambahGrupOptionsMarkup lets the user choose between auto-import and manual group add.
	TambahGrupOptionsMarkup *botgotd.ReplyKeyboardMarkup

	// CancelOnlyMarkup shows a single Batal button.
	CancelOnlyMarkup *botgotd.ReplyKeyboardMarkup

	// AturJedaMarkup shows delay option buttons.
	AturJedaMarkup        *botgotd.ReplyKeyboardMarkup
	JedaPerGrupMarkup     *botgotd.ReplyKeyboardMarkup
	JedaPerSemuaGrupMarkup *botgotd.ReplyKeyboardMarkup
	SkipFloodWaitMarkup   *botgotd.ReplyKeyboardMarkup

	// UbahModeMarkup shows broadcasting mode options.
	UbahModeMarkup *botgotd.ReplyKeyboardMarkup

	// TimerMarkup shows timer schedule options.
	TimerMarkup *botgotd.ReplyKeyboardMarkup

	// ResetMarkup shows reset options.
	ResetMarkup *botgotd.ReplyKeyboardMarkup

	// HapusListMarkup shows the hapus-all-list confirm button.
	HapusListMarkup *botgotd.ReplyKeyboardMarkup

	// HapusGrupMarkup shows the hapus-all-grup confirm button.
	HapusGrupMarkup *botgotd.ReplyKeyboardMarkup

	// CancelMarkup is an alias for CancelOnlyMarkup (backward-compat).
	CancelMarkup *botgotd.ReplyKeyboardMarkup

	// CreateChoiceMarkup is the initial wizard keyboard (before userbot is linked).
	CreateChoiceMarkup *botgotd.ReplyKeyboardMarkup

	// InstallPhoneMarkup asks the user to share their contact.
	InstallPhoneMarkup *botgotd.ReplyKeyboardMarkup

	WatermarkPromptMarkup *botgotd.ReplyKeyboardMarkup

	// RemoveKeyboard removes the reply keyboard.
	RemoveKeyboard *botgotd.ReplyKeyboardRemove
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
	CancelMarkup = CancelOnlyMarkup // alias

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
			{{Text: BtnModeNormal}, {Text: BtnModeFast}, {Text: BtnModeSilent}},
			{{Text: BtnBatal}},
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
