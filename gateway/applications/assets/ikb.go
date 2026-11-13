package assets

import (
	"fmt"

	"assterisk/gateway/applications/botgotd"
)

// ─── Inline Keyboard Button Label Constants ───────────────────────────────────

// Control Remote (ctrl:*) inline button labels
const (
	IkbRefreshStatus       = "🔄 Refresh Status"
	IkbStatus              = "💎 Status"
	IkbGrupList            = "📂 Grup & List"
	IkbAturJeda            = "⏱️ Atur Jeda"
	IkbListAkun            = "👥 List Akun"
	IkbKembali             = "🔙 Kembali"
	IkbKembaliGrupList     = "🔙 Kembali ke Grup & List"
	IkbTutupPanel          = "❌ Tutup Panel"
	IkbCekList             = "📁 Cek List"
	IkbCekGrup             = "📚 Cek Grup"
	IkbTambahList          = "📝 Tambah List"
	IkbTambahGrup          = "📝 Tambah Grup"
	IkbImportSemuaGrup     = "🔥 Import Semua Grup"
	IkbTambahGrupManual    = "📝 Tambah Grup Manual"
	IkbHapusList           = "🗑️ Hapus Semua List"
	IkbHapusGrup           = "🗑️ Hapus Semua Grup"
	IkbKonfirmasiHapusList = "⚠️ Ya, Hapus Semua List"
	IkbKonfirmasiHapusGrup = "⚠️ Ya, Hapus Semua Grup"
	IkbBatal               = "↩️ Batal"
	IkbPrev                = "⬅️ Prev"
	IkbNext                = "➡️ Next"

	// Jeda values (seconds) — displayed as button labels
	IkbJeda3  = "⚡ 3 detik"
	IkbJeda5  = "⚡ 5 detik"
	IkbJeda10 = "⏱️ 10 detik"
	IkbJeda15 = "⏱️ 15 detik"
	IkbJeda30 = "⏳ 30 detik"
	IkbJeda60 = "⏳ 60 detik"

	// Status panel (non-remote)
	IkbRefreshStatusPanel = "🔄 Refresh"
)

// btn is a shorthand to build a single inline button with callback data.
func btn(text, data string) botgotd.InlineKeyboardButton {
	return botgotd.InlineKeyboardButton{Text: text, CallbackData: data}
}

// btnURL builds a single inline button with a URL.
func btnURL(text, url string) botgotd.InlineKeyboardButton {
	return botgotd.InlineKeyboardButton{Text: text, URL: url}
}

// ─── Control Remote Inline Keyboard Builders ─────────────────────────────────

// CtrlAccountListIKB returns the inline keyboard for ctrl:list (single-use; built on demand).
// Each row = one account button. Close button at the bottom.
func CtrlAccountListIKB(accounts []struct {
	ID   int64
	Name string
}) *botgotd.InlineKeyboardMarkup {
	rows := make([][]botgotd.InlineKeyboardButton, 0, len(accounts)+1)
	for _, a := range accounts {
		rows = append(rows, []botgotd.InlineKeyboardButton{
			btn(a.Name, fmt.Sprintf("ctrl:select:%d", a.ID)),
		})
	}
	rows = append(rows, []botgotd.InlineKeyboardButton{
		btn(IkbTutupPanel, "ctrl:close"),
	})
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// CtrlAdminMenuIKB returns the main admin menu inline keyboard for a given targetID.
func CtrlAdminMenuIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbStatus, "ctrl:status:"+t), btn(IkbGrupList, "ctrl:gruplist:"+t)},
			{btn(IkbAturJeda, "ctrl:jeda:"+t), btn(IkbListAkun, "ctrl:list")},
			{btn(IkbTutupPanel, "ctrl:close")},
		},
	}
}

// CtrlStatusIKB returns the status view inline keyboard.
func CtrlStatusIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbRefreshStatus, "ctrl:status_refresh:"+t)},
			{btn(IkbKembali, "ctrl:menu:"+t), btn(IkbTutupPanel, "ctrl:close")},
		},
	}
}

// CtrlGrupListIKB returns the Grup & List submenu inline keyboard.
func CtrlGrupListIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbCekList, "ctrl:cek_list:"+t), btn(IkbCekGrup, "ctrl:cek_grup:"+t)},
			{btn(IkbTambahList, "ctrl:tambah_list:"+t), btn(IkbTambahGrup, "ctrl:tambah_grup:"+t)},
			{btn(IkbHapusList, "ctrl:hapus_list_confirm:"+t), btn(IkbHapusGrup, "ctrl:hapus_grup_confirm:"+t)},
			{btn(IkbKembali, "ctrl:menu:"+t), btn(IkbTutupPanel, "ctrl:close")},
		},
	}
}

// CtrlCekListIKB returns the pagination keyboard for Cek List.
func CtrlCekListIKB(targetID int64, page, totalPages int) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	nav := make([]botgotd.InlineKeyboardButton, 0, 2)
	if page > 0 {
		nav = append(nav, btn(IkbPrev, fmt.Sprintf("ctrl:cek_list:%s:%d", t, page-1)))
	}
	if page < totalPages-1 {
		nav = append(nav, btn(IkbNext, fmt.Sprintf("ctrl:cek_list:%s:%d", t, page+1)))
	}
	rows := [][]botgotd.InlineKeyboardButton{}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, []botgotd.InlineKeyboardButton{
		btn(IkbKembaliGrupList, "ctrl:gruplist:"+t),
		btn(IkbTutupPanel, "ctrl:close"),
	})
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// CtrlCekGrupIKB returns the pagination keyboard for Cek Grup.
func CtrlCekGrupIKB(targetID int64, page, totalPages int) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	nav := make([]botgotd.InlineKeyboardButton, 0, 2)
	if page > 0 {
		nav = append(nav, btn(IkbPrev, fmt.Sprintf("ctrl:cek_grup:%s:%d", t, page-1)))
	}
	if page < totalPages-1 {
		nav = append(nav, btn(IkbNext, fmt.Sprintf("ctrl:cek_grup:%s:%d", t, page+1)))
	}
	rows := [][]botgotd.InlineKeyboardButton{}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, []botgotd.InlineKeyboardButton{
		btn(IkbKembaliGrupList, "ctrl:gruplist:"+t),
		btn(IkbTutupPanel, "ctrl:close"),
	})
	return &botgotd.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// CtrlTambahListIKB returns the inline keyboard shown while awaiting list input.
func CtrlTambahListIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbBatal, "ctrl:cancel:"+t)},
		},
	}
}

// CtrlTambahGrupOptionsIKB returns the choice keyboard for Tambah Grup (remote).
func CtrlTambahGrupOptionsIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbImportSemuaGrup, "ctrl:fetch_all:"+t)},
			{btn(IkbTambahGrupManual, "ctrl:tambah_grup_manual:"+t)},
			{btn(IkbBatal, "ctrl:cancel:"+t)},
		},
	}
}

// CtrlTambahGrupManualIKB returns the inline keyboard shown while awaiting group input.
func CtrlTambahGrupManualIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbBatal, "ctrl:cancel:"+t)},
		},
	}
}

// CtrlJedaIKB returns the jeda (delay) picker inline keyboard.
func CtrlJedaIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{
				btn(IkbJeda3, "ctrl:set_jeda:"+t+":3"),
				btn(IkbJeda5, "ctrl:set_jeda:"+t+":5"),
				btn(IkbJeda10, "ctrl:set_jeda:"+t+":10"),
			},
			{
				btn(IkbJeda15, "ctrl:set_jeda:"+t+":15"),
				btn(IkbJeda30, "ctrl:set_jeda:"+t+":30"),
				btn(IkbJeda60, "ctrl:set_jeda:"+t+":60"),
			},
			{btn(IkbKembali, "ctrl:menu:"+t), btn(IkbTutupPanel, "ctrl:close")},
		},
	}
}

// CtrlHapusListConfirmIKB returns the confirmation keyboard for hapus semua list.
func CtrlHapusListConfirmIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbKonfirmasiHapusList, "ctrl:hapus_list_do:"+t)},
			{btn(IkbBatal, "ctrl:gruplist:"+t)},
		},
	}
}

// CtrlHapusGrupConfirmIKB returns the confirmation keyboard for hapus semua grup.
func CtrlHapusGrupConfirmIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbKonfirmasiHapusGrup, "ctrl:hapus_grup_do:"+t)},
			{btn(IkbBatal, "ctrl:gruplist:"+t)},
		},
	}
}

// CtrlDoneIKB is a simple "back to group-list menu" keyboard after an action completes.
func CtrlDoneIKB(targetID int64) *botgotd.InlineKeyboardMarkup {
	t := fmt.Sprintf("%d", targetID)
	return &botgotd.InlineKeyboardMarkup{
		InlineKeyboard: [][]botgotd.InlineKeyboardButton{
			{btn(IkbKembaliGrupList, "ctrl:gruplist:"+t), btn(IkbTutupPanel, "ctrl:close")},
		},
	}
}

// ─── Status Panel (non-remote) ────────────────────────────────────────────────

// StatusPanelIKB returns the refresh button for the user's own status page.
var StatusPanelIKB = &botgotd.InlineKeyboardMarkup{
	InlineKeyboard: [][]botgotd.InlineKeyboardButton{
		{{Text: IkbRefreshStatusPanel, CallbackData: "panel:status:refresh"}},
	},
}
