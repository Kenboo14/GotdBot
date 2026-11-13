package assets

import (
	"fmt"
	"html"
)

// ─── Bot Response Text Templates ─────────────────────────────────────────────
// All text uses HTML parse mode.

// ─── Main Menu & Generic ──────────────────────────────────────────────────────

// TxtMainMenu returns the welcome / main control panel message.
func TxtMainMenu(userID int64, userName string) string {
	if userName == "" {
		userName = "User"
	}
	return fmt.Sprintf(
		`<h1>🤖 GotdBot Control Panel</h1>`+
			`<blockquote expandable>👋 <b>Halo, <a href="tg://user?id=%d">%s</a>!</b><br/>`+
			`<i>Sesi userbot Anda sedang berjalan dan terhubung dengan aman via MTProto. Seluruh data disinkronkan secara real-time.</i></blockquote>`+
			`<hr/><p>Silakan gunakan menu navigasi keyboard di bawah untuk mengontrol bot:</p>`,
		userID, html.EscapeString(userName),
	)
}

// TxtStartGreeting returns the initial /start greeting with choice table.
func TxtStartGreeting(userName string) string {
	if userName == "" {
		userName = "User"
	}
	return fmt.Sprintf(`<h1>👋 Selamat Datang, %s!</h1>
<blockquote expandable><b>GotdBot • Userbot Manager</b>
Sistem otomatisasi siaran promosi dan manajemen akun userbot Telegram MTProto berkecepatan tinggi.</blockquote>
<table border="1">
  <tr><th>Opsi Pemasangan</th><th>Keterangan</th></tr>
  <tr><td>🔑 Input Token</td><td>Klaim Voucher Master atau Restore Token</td></tr>
  <tr><td>🚀 Buat Userbot</td><td>Sambungkan ulang lisensi aktif Anda</td></tr>
  <tr><td>⚠️ Bantuan</td><td>Panduan lengkap penggunaan bot</td></tr>
  <tr><td>🎯 Control</td><td>Menu utama kontrol panel userbot</td></tr>
</table>
<hr/>
<p>Silakan pilih opsi pada tombol keyboard di bawah:</p>`, html.EscapeString(userName))
}

const TxtPemasanganSedangBerjalan = `<h1>⏳ Pemasangan Sedang Berjalan</h1>
<blockquote expandable>Proses pemasangan sesi userbot Anda saat ini sedang berlangsung.</blockquote>
<hr/>
<p>Silakan lanjutkan proses atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

// TxtPilihanTidakDikenali is shown when user sends unexpected text.
const TxtPilihanTidakDikenali = `<h1>⚠️ Pilihan Tidak Dikenali</h1>` +
	`<blockquote expandable>Silakan gunakan salah satu tombol keyboard di bawah untuk memilih opsi yang tersedia.</blockquote>` +
	`<p>Pilih tombol yang tersedia.</p>`

// TxtBantuan is the help / guide text.
const TxtBantuan = `<h1>⚠️ Panduan &amp; Bantuan Penggunaan</h1>
<table border="1">
  <tr><th>🔘 Tombol</th><th>📘 Fungsi</th></tr>
  <tr><td>💎 Status</td><td>Melihat ringkasan lengkap data dan konfigurasi</td></tr>
  <tr><td>🧩 Grup &amp; List</td><td>Mengatur materi teks promosi &amp; target grup</td></tr>
  <tr><td>⌛ Atur Jeda</td><td>Mengubah interval jeda pengiriman antar grup</td></tr>
  <tr><td>⚠️ Safemode</td><td>Melindungi akun dari risiko limitasi Telegram</td></tr>
</table>
<hr/>
<p>GotdBot • Multi-Configuration VPS Control Panel</p>`

// ─── Grup & List ──────────────────────────────────────────────────────────────

// TxtGrupList returns the Grup & List menu heading.
const TxtGrupList = `<h1>📥 Grup &amp; List</h1>` +
	`<blockquote expandable>💡 <b>Petunjuk Singkat:</b>` +
	`<br/>1. <b>List :</b> <i>Materi promosi (teks, media single, media grup) yang akan disiarkan.</i>` +
	`<br/>2. <b>Grup :</b> <i>Daftar grup atau supergroup tujuan siaran.</i>` +
	`<br/>3. <b>Topic :</b> <i>Thread forum grup Telegram untuk penargetan topik khusus.</i></blockquote>` +
	`<p>Atur target grup dan materi siaran Anda lewat tombol keyboard navigasi di bawah.</p>`

// TxtCekListKosong is shown when no promotional lists are saved.
const TxtCekListKosong = `<h1>📋 Ringkasan List Promosi</h1>` +
	`<blockquote expandable>Belum ada list materi promosi tersimpan.</blockquote>` +
	`<hr/><p>Gunakan tombol <b>📝 Tambah List 📝</b> untuk menambahkan materi baru.</p>`

// TxtCekGrupKosong is shown when no broadcast groups are saved.
const TxtCekGrupKosong = `<h1>📚 Ringkasan Grup Target</h1>` +
	`<blockquote expandable>Belum ada grup target siaran tersimpan.</blockquote>` +
	`<hr/><p>Gunakan tombol <b>📝 Tambah Grup 📝</b> untuk menambahkan grup target.</p>`

// TxtPromptTambahList is the prompt shown when waiting for the user to send broadcast material.
const TxtPromptTambahList = `<h1>📝 Tambah List Promosi</h1>` +
	`<blockquote expandable>📌 <b>Format materi promosi yang didukung:</b>` +
	`<br/>1. <b>Teks :</b> Pesan teks biasa atau berformat HTML / Markdown.` +
	`<br/>2. <b>Media Single :</b> Foto, video, dokumen, audio, atau animasi / GIF (+ caption).` +
	`<br/>3. <b>Media Grup :</b> Album kumpulan foto atau video (+ caption).</blockquote>` +
	`<hr/><p>Kirimkan materi promosi sekarang atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

// TxtPromptTambahGrupManual is the prompt shown when waiting for manual group input.
const TxtPromptTambahGrupManual = `<h1>📝 Tambah Grup Manual</h1>` +
	`<blockquote expandable>📌 <b>Format target yang didukung:</b>` +
	`<br/>1. <b>Username grup:</b> (contoh: <code>@grupchat</code>)` +
	`<br/>2. <b>Chat ID grup:</b> (contoh: <code>-1001234567890</code>)` +
	`<br/>3. <b>Tautan grup:</b> (contoh: <code>t.me/namagrup</code>)` +
	`<br/><br/><i>Jika lebih dari satu grup, pisahkan masing-masing target dengan baris baru.</i></blockquote>` +
	`<hr/><p>Kirimkan target grup sekarang atau klik tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

// ─── Remote Control (ctrl:*) ──────────────────────────────────────────────────

// TxtControlAccountList returns the account selector heading.
func TxtControlAccountList(count int) string {
	return fmt.Sprintf(
		`<h1>🖥️ Panel Kontrol Akun</h1>`+
			`<hr/><b>🧑 Anda memiliki %d akses akun</b>`+
			`<br/><i>📋 Silakan pilih akun userbot yang ingin Anda kontrol di bawah:</i>`+
			`<br/><blockquote expandable>💡 Tips: Untuk mengubah teks / grup secara optimal, pastikan userbot dalam posisi Status Nonaktif.</blockquote>`,
		count,
	)
}

// TxtControlAccountListEmpty is shown when the user has 0 accessible accounts.
func TxtControlAccountListEmpty(userID int64) string {
	return fmt.Sprintf(
		`<h1>🖥️ Panel Kontrol Akun</h1>`+
			`<hr/><b>👤 Anda memiliki 0 akses</b><br/>`+
			`Anda belum memiliki akses untuk mengontrol akun userbot manapun.<br/><br/>`+
			`<blockquote>User ID Anda: <code>%d</code></blockquote><br/>`+
			`Pastikan Anda telah memasang userbot sendiri via <b>/start</b>, atau minta pemilik userbot lain menambahkan User ID Anda sebagai <b>ADMIN</b> di menu Pengaturan Admin.`,
		userID,
	)
}

// TxtControlAdminMenu returns the remote admin menu heading.
func TxtControlAdminMenu(targetName string, targetID int64, adminName string) string {
	return fmt.Sprintf(
		`<h1>🎛️ Menu Admin Remote</h1>`+
			`<blockquote expandable>👑 <b>Admin:</b> %s`+
			`<br/>🎯 <b>Target:</b> %s (<code>%d</code>)</blockquote>`+
			`<p>Pilih aksi yang ingin Anda lakukan pada akun userbot di atas:</p>`,
		html.EscapeString(adminName),
		html.EscapeString(targetName),
		targetID,
	)
}

// TxtControlGrupList returns the remote group and list menu body.
func TxtControlGrupList(targetName string, targetID int64, lCount, gCount int) string {
	return fmt.Sprintf(
		`<h1>🔮 Grup &amp; List (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<br/><b>📚 Total List Promosi:</b> <code>%d materi</code>`+
			`<br/><b>🌐 Total Target Grup:</b> <code>%d grup</code>`+
			`<br/><blockquote expandable>💡 Gunakan tombol di bawah untuk melihat, menambah, atau mereset list &amp; grup akun ini secara remote.</blockquote>`,
		html.EscapeString(targetName), targetID, lCount, gCount,
	)
}

// TxtControlTambahList returns the remote tambah-list prompt (EditMessageText version).
func TxtControlTambahList(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>📝 Tambah List Promosi (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>🚀 <b>Format materi promosi yang didukung:</b>`+
			`<br/>1. Teks: Pesan teks biasa / berformat HTML.`+
			`<br/>2. Media Single: Foto, video, dokumen, audio, animasi (+ caption).`+
			`<br/>3. Media Grup: Album kumpulan foto atau video (+ caption).</blockquote>`+
			`<p>Kirimkan materi promosi sekarang via pesan chat. Materi akan disimpan langsung ke sesi akun target.</p>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlTambahGrupOptions returns the remote tambah-grup options prompt.
func TxtControlTambahGrupOptions(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>📝 Tambah Grup Target (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<p>Pilih metode penambahan grup:</p>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlTambahGrupManual returns the remote manual group add prompt.
func TxtControlTambahGrupManual(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>📝 Tambah Grup Manual (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>📌 <b>Format target yang didukung:</b>`+
			`<br/>1. Username grup: (contoh: <code>@grupchat</code>)`+
			`<br/>2. Chat ID grup: (contoh: <code>-1001234567890</code>)`+
			`<br/>3. Tautan grup: (contoh: <code>t.me/namagrup</code>)`+
			`<br/><br/><i>Jika lebih dari satu grup, pisahkan tiap grup dengan baris baru.</i></blockquote>`+
			`<p>Kirimkan target grup sekarang via pesan chat, atau tekan tombol batal di bawah:</p>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlImportGroupsStarted is shown after triggering fetch_all.
func TxtControlImportGroupsStarted(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>🔄 Import Grup Dimulai</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>⏳ Proses import semua grup dari akun target sedang berjalan di background.`+
			`<br/>Silakan cek kembali daftar grup dalam beberapa saat.</blockquote>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlListSentSuccess is shown after remote tambah-list completes.
func TxtControlListSentSuccess(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>✅ List Promosi Berhasil Dikirim (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>Materi promosi telah dikirimkan ke worker userbot untuk disimpan ke database target.</blockquote>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlMediaSaving is shown while remote media is being processed.
func TxtControlMediaSaving(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>⏳ Media Sedang Disimpan ke Cache (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>Media materi promosi sedang diproses oleh sesi userbot target.</blockquote>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlGrupSentSuccess is shown after remote tambah-grup completes.
func TxtControlGrupSentSuccess(targetName string, targetID int64, count int) string {
	return fmt.Sprintf(
		`<h1>✅ Grup Berhasil Ditambahkan (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>%d grup telah dikirimkan ke worker userbot untuk disimpan ke database target.</blockquote>`,
		html.EscapeString(targetName), targetID, count,
	)
}

// TxtControlHapusListConfirm asks confirmation before wiping all lists.
func TxtControlHapusListConfirm(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>⚠️ Konfirmasi Hapus Semua List</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>🗑️ Seluruh materi list promosi akun target akan <b>dihapus permanen</b>. Tindakan ini tidak dapat diurungkan.</blockquote>`+
			`<p>Tekan tombol konfirmasi di bawah untuk melanjutkan:</p>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlHapusListDone is shown after lists are wiped.
func TxtControlHapusListDone(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>🗑️ Semua List Dihapus</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>Seluruh materi list promosi akun target telah berhasil dihapus.</blockquote>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlHapusGrupConfirm asks confirmation before wiping all groups.
func TxtControlHapusGrupConfirm(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>⚠️ Konfirmasi Hapus Semua Grup</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>🗑️ Seluruh target grup akun target akan <b>dihapus permanen</b>. Tindakan ini tidak dapat diurungkan.</blockquote>`+
			`<p>Tekan tombol konfirmasi di bawah untuk melanjutkan:</p>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlHapusGrupDone is shown after groups are wiped.
func TxtControlHapusGrupDone(targetName string, targetID int64) string {
	return fmt.Sprintf(
		`<h1>🗑️ Semua Grup Dihapus</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>Seluruh target grup akun target telah berhasil dihapus.</blockquote>`,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlJeda returns the jeda picker text.
func TxtControlJeda(targetName string, targetID int64, currentDelay int) string {
	return fmt.Sprintf(
		`<h1>⏱️ Atur Jeda Siaran (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			`<blockquote expandable>⏱️ <b>Jeda saat ini:</b> <code>%d detik</code></blockquote>`+
			`<p>Pilih durasi jeda antar pesan siaran:</p>`,
		html.EscapeString(targetName), targetID, currentDelay,
	)
}

// TxtControlCekList returns the Cek List body for a given page.
// items: slice of "N. <text>" lines already formatted.
func TxtControlCekList(targetName string, targetID int64, page, totalPages, totalItems int, items []string) string {
	var body string
	if len(items) == 0 {
		body = `<blockquote expandable>Belum ada materi list tersimpan.</blockquote>`
	} else {
		body = `<blockquote expandable>`
		for _, it := range items {
			body += it + "\n"
		}
		body += fmt.Sprintf(`<i>Halaman %d / %d — Total %d materi</i></blockquote>`, page+1, totalPages, totalItems)
	}
	return fmt.Sprintf(
		`<h1>📋 Cek List Promosi (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			body,
		html.EscapeString(targetName), targetID,
	)
}

// TxtControlCekGrup returns the Cek Grup body for a given page.
func TxtControlCekGrup(targetName string, targetID int64, page, totalPages, totalItems int, items []string) string {
	var body string
	if len(items) == 0 {
		body = `<blockquote expandable>Belum ada grup target tersimpan.</blockquote>`
	} else {
		body = `<blockquote expandable>`
		for _, it := range items {
			body += it + "\n"
		}
		body += fmt.Sprintf(`<i>Halaman %d / %d — Total %d grup</i></blockquote>`, page+1, totalPages, totalItems)
	}
	return fmt.Sprintf(
		`<h1>📚 Cek Grup Target (Remote)</h1>`+
			`<hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)`+
			body,
		html.EscapeString(targetName), targetID,
	)
}

// ─── Install / Wizard Texts ───────────────────────────────────────────────────

// TxtInputToken is the prompt shown when user is asked to enter their license token.
const TxtInputToken = `<h1>🔑 Input Token Lisensi</h1>` +
	`<blockquote expandable><b>Hak Kepemilikan &amp; Lisensi Database</b>` +
	`<br/>Token lisensi berfungsi sebagai voucher aktivasi awal atau kunci pemulihan (brankas) agar seluruh data (list promosi, target grup, notes, prefix) otomatis terhubung kembali.</blockquote>` +
	`<table border="1">` +
	`<tr><th>Jenis Token</th><th>Format</th></tr>` +
	`<tr><td>Voucher Master</td><td><code>GOTD:&lt;32_huruf&gt;</code></td></tr>` +
	`<tr><td>Token Backup</td><td><code>GOTD_SHA:&lt;sha256&gt;</code></td></tr>` +
	`</table>` +
	`<hr/><p>Kirimkan token Anda sekarang, atau tekan tombol <b>⬅️ Batal ⬅️</b> di bawah.</p>`

// TxtAksesInstalDitolak is shown when the user has no active license.
const TxtAksesInstalDitolak = `<h1>⚠️ Akses Pemasangan Ditolak</h1>` +
	`<blockquote expandable>Akun Telegram Anda belum memiliki lisensi yang aktif, atau masa aktif token Anda sebelumnya telah kedaluwarsa.</blockquote>` +
	`<hr/><p>Silakan gunakan tombol <b>🔑 Input Token 🔑</b> untuk memasukkan voucher dari Owner.</p>`

// TxtTokenSHANotFound is shown when a SHA token isn't in the database.
const TxtTokenSHANotFound = `<h1>❌ Token Tidak Ditemukan</h1>` +
	`<blockquote expandable>Token Backup yang Anda masukkan tidak terdaftar dalam database sistem.</blockquote>` +
	`<hr/><p>Pastikan token yang Anda masukkan benar atau hubungi Owner untuk verifikasi.</p>`

// TxtTokenDetected returns the "token recognized" message shown after a valid token is submitted.
func TxtTokenDetected(tier, sisaDur string) string {
	return fmt.Sprintf(
		`<h1>✅ Token Terdeteksi: %s</h1>`+
			`<blockquote expandable>Hak kepemilikan database &amp; seluruh data Anda (List, Grup, Notes, Prefix) otomatis terhubung kembali.</blockquote>`+
			`<table border="1">`+
			`<tr><th>Properti</th><th>Keterangan</th></tr>`+
			`<tr><td>🎖️ Tier Lisensi</td><td><code>%s</code></td></tr>`+
			`<tr><td>⏳ Masa Aktif</td><td><code>Sisa %s</code></td></tr>`+
			`</table>`+
			`<hr/><p>📱 Silakan tekan tombol <b>📱 Bagikan Kontak</b> di bawah untuk menghubungkan akun Telegram Anda:</p>`,
		tier, tier, sisaDur,
	)
}

// ─── Reset Menu & Cancellation Notices ────────────────────────────────────────

const TxtResetSemuaGrupSukses = `<h1>🗑️ Seluruh Grup Berhasil Dihapus</h1>` +
	`<blockquote expandable>Semua daftar grup target broadcast telah dibersihkan secara permanen dari database.</blockquote>` +
	`<hr/><p>GotdBot • Reset Data Manager</p>`

const TxtResetSemuaListSukses = `<h1>🗑️ Seluruh List Berhasil Dihapus</h1>` +
	`<blockquote expandable>Semua materi list promosi telah dibersihkan secara permanen dari database.</blockquote>` +
	`<hr/><p>GotdBot • Reset Data Manager</p>`

const TxtResetKonfigurasiSukses = `<h1>🔄 Konfigurasi Berhasil Direset</h1>` +
	`<blockquote expandable>Seluruh parameter pengaturan bot telah dipulihkan ke setelan default pabrik.</blockquote>` +
	`<hr/><p>GotdBot • Reset Data Manager</p>`

const TxtTindakanDibatalkan = `<p>↩️ <b>Tindakan dibatalkan.</b></p>`
