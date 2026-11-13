package assets

import "assterisk/gateway/applications/botgotd"

// ─── Reply Keyboard Button Label Constants ───────────────────────────────────

// Main Menu
const (
	BtnStatus    = "💎 Status 💎"
	BtnShop      = "🛒 Shop 🛒"
	BtnGrupList  = "🧩 Grup & List 🧩"
	BtnExpert    = "⚡ Expert Extra ⚡"
	BtnReset     = "🗑️ Reset 🗑️"
	BtnAturJeda  = "⌛ Atur Jeda ⌛"
	BtnBantuan   = "⚠️ Bantuan ⚠️"
	BtnSettings  = "⚙️ Settings ⚙️"
	BtnFeed      = "📰 Feed 📰"
	BtnWebapp    = "🌐 Webapp 🌐"
	BtnHome      = "🌐 Home 🌐"
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
	BtnCekTopic    = "🧑‍💻 Cek Topic 🧑‍💻"
	BtnTopic       = "🧑‍💻 Topic 🧑‍💻"
	BtnListTopic   = "📌 List Topic"
	BtnCekList     = "📁 Cek List 📁"
	BtnCekGrup     = "📚 Cek Grup 📚"
	BtnTambahList  = "📝 Tambah List 📝"
	BtnTambahGrup  = "📝 Tambah Grup 📝"
	BtnSebarKeRC   = "📦 Sebar Ke RC 📦"
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

// Atur Jeda
const (
	BtnJeda3  = "⏱️ 3 Detik"
	BtnJeda5  = "⏱️ 5 Detik"
	BtnJeda10 = "⏱️ 10 Detik"
	BtnJeda15 = "⏱️ 15 Detik"
	BtnJeda30 = "⏱️ 30 Detik"
	BtnJeda60 = "⏱️ 60 Detik"
)

// Ubah Mode
const (
	BtnModeNormal = "👑 Normal"
	BtnModeFast   = "⚡ Fast"
	BtnModeSilent = "🔕 Silent"
)

// Timer
const (
	BtnTimerNonaktif = "⏱️ Nonaktifkan Timer"
	BtnTimer1Jam     = "⏱️ Tiap 1 Jam"
	BtnTimer2Jam     = "⏱️ Tiap 2 Jam"
	BtnTimer3Jam     = "⏱️ Tiap 3 Jam"
)

// Install / Wizard
const (
	BtnInputToken  = "🔑 Input Token 🔑"
	BtnBuatUserbot = "🚀 Buat Userbot 🚀"
	BtnControl     = "🎯 Control 🎯"
	BtnBatal       = "⬅️ Batal ⬅️"
	BtnBagikanKontak = "📱 Bagikan Kontak"
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
	AturJedaMarkup *botgotd.ReplyKeyboardMarkup

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
			{{Text: BtnJeda3}, {Text: BtnJeda5}, {Text: BtnJeda10}},
			{{Text: BtnJeda15}, {Text: BtnJeda30}, {Text: BtnJeda60}},
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
			{{Text: BtnTimerNonaktif}, {Text: BtnTimer1Jam}},
			{{Text: BtnTimer2Jam}, {Text: BtnTimer3Jam}},
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

	RemoveKeyboard = &botgotd.ReplyKeyboardRemove{RemoveKeyboard: true}
}
