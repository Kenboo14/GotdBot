# Prompt: Redesign UI GotdBot — Modern, Clean, Production Grade

## Konteks Proyek

**GotdBot** adalah bot Telegram berbasis `github.com/gotd/td` (native MTProto).  
Lokasi kode: `d:\GotdBot\gateway\applications\controllers\`

File utama yang perlu diubah:
- `panel.go` — semua fungsi `ShowXxx`, `PromptXxx`, `CekList`, `CekGrup`
- `ubot.go` — `handleBroadcastMonitor`, `SendMessage` (panel)
- `userbot.go` — `ShowServerDashboard`, `HelpCallback`, `serverCallback`
- `panel_callback.go` — pesan di dalam callback handler

---

## Tujuan Redesign

Ubah **semua teks respons bot** menjadi tampilan yang:
- **Simple & readable** — tidak ada emoji berlebihan, tidak ada teks panjang bertele-tele
- **Modern & kekinian** — gunakan Rich Message HTML Telegram (bubble card style)
- **Production grade** — konsisten, informatif, tidak ambigu
- **ReplyKeyboard tetap dipertahankan** — hanya teks isi pesan & inline button yang didesain ulang

---

## Aturan Desain

### 1. Teks Respons

**JANGAN gunakan:**
- Emoji ganda di kiri-kanan: `💎 Status 💎` → cukup `💎 Status`
- Kalimat panjang bertele-tele
- Multiple blockquote untuk info sederhana
- Tanda titik-titik `...` sebagai filler

**GUNAKAN:**
- Satu emoji prefix per baris informasi (opsional, secukupnya)
- Kalimat singkat padat
- `<code>` untuk nilai teknis (ID, angka, token)
- `<b>` hanya untuk label field, bukan dekorasi

---

### 2. Struktur Pesan Standar

Gunakan HTML Telegram (ParseMode HTML), bukan rich message tags `<h1>`, `<tg-button-row>`, `<hr/>` — karena tidak semua client support, dan gotd MTProto SendMessage akan parse entity HTML biasa.

**Format standar:**
```
<b>🏠 Judul Halaman</b>
━━━━━━━━━━━━━━━━━━━━

Baris info atau deskripsi singkat.

<b>Field:</b> <code>nilai</code>
<b>Field:</b> nilai teks
```

Gunakan `━━━━━━━━━━━━━━━━━━━━` sebagai divider (bukan `<hr/>` atau `──────────`).

**Footer opsional (hanya untuk dashboard/status):**
```
<i>GotdBot • vX.X</i>
```

---

### 3. Inline Keyboard Design

Tombol inline harus **fungsional, bukan dekoratif**.

**Rules:**
- Maksimal 2 tombol per baris untuk aksi utama
- Tombol aksi destruktif (Hapus) selalu sendiri di baris terpisah
- Tombol navigasi (Kembali) selalu baris terakhir
- Teks tombol: singkat, pakai 1 emoji prefix saja

**Contoh pola tombol:**
```
[ 🔄 Refresh ]  [ ℹ️ Detail ]
[ 🗑 Hapus ]
[ ← Kembali ]
```

---

### 4. Konvensi Pesan per Konteks

#### Main Menu / Welcome
```
<b>👋 Selamat datang, {nama}!</b>

Gunakan menu di bawah untuk navigasi.

<i>GotdBot • Panel Kontrol</i>
```

#### Status / Dashboard
```
<b>📊 Status Userbot</b>
━━━━━━━━━━━━━━━━━━━━
<b>Akun:</b> <code>{phone}</code>
<b>Mode:</b> {mode}
<b>Jeda:</b> <code>{delay}s</code>
<b>Status:</b> {status}

<b>Server:</b>
  CPU: <code>{cpu}%</code>  RAM: <code>{ram}%</code>
  Uptime: <code>{uptime}</code>

<b>Aktivitas Terakhir:</b>
<code>{tindakan_terakhir}</code>
```
Inline button: `[ 🔄 Refresh ]`

#### Daftar List (CekList) — Summary
```
<b>📋 Daftar List</b>
━━━━━━━━━━━━━━━━━━━━
Total: <code>{n}</code> list tersimpan

{loop: • List #{i} — Msg ID <code>{msgID}</code>}

<i>Klik Browse untuk melihat isi tiap list.</i>
```

#### Detail List — satu item
```
<b>📄 Detail List #{i}</b>
━━━━━━━━━━━━━━━━━━━━
Cache ID: <code>{msgID}</code>
```
Inline button:
```
[ 🗑 Hapus List ]
[ ← Kembali ke List ]
```

#### Daftar Grup (CekGrup) — Summary
```
<b>📚 Daftar Grup Target</b>
━━━━━━━━━━━━━━━━━━━━
Total: <code>{n}</code> grup

{loop: {i}. {title} — <code>{chat_id}</code>}
```

#### Detail Grup — satu item
```
<b>📌 Detail Grup #{i}</b>
━━━━━━━━━━━━━━━━━━━━
<b>Nama:</b> {title}
<b>Target:</b> <code>{target}</code>
<b>Chat ID:</b> <code>{chat_id}</code>
```
Inline button:
```
[ 🗑 Hapus Grup ]
[ ← Kembali ke Grup ]
```

#### Broadcast Monitor (Progress)
```
<b>📡 Broadcast Berjalan</b>
━━━━━━━━━━━━━━━━━━━━
✅ Berhasil: <code>{success}</code>
❌ Gagal:    <code>{failed}</code>
⏳ Sisa:     <code>{remaining}</code>
⏱ Jeda:     <code>{delay}s</code>

Status: <code>{status_text}</code>
```

#### Broadcast Selesai
```
<b>✅ Broadcast Selesai</b>
━━━━━━━━━━━━━━━━━━━━
Berhasil: <code>{success}</code> | Gagal: <code>{failed}</code>
Durasi: <code>{duration}</code>
```

#### Help / Bantuan
```
<b>❓ Bantuan — {section}</b>
━━━━━━━━━━━━━━━━━━━━
{deskripsi singkat section}

<b>Perintah:</b>
• <code>/start</code> — {desc}
• <code>/status</code> — {desc}
• ...
```
Inline button navigasi section: `[ Admin ] [ Notes ] [ Ping ] ...`

#### Prompt Input (waiting state)
```
<b>✏️ {judul aksi}</b>

{instruksi singkat 1-2 kalimat}

<i>Ketik /cancel untuk membatalkan.</i>
```

#### Konfirmasi / Error / Sukses
```
✅ <b>Berhasil!</b> {keterangan singkat}
```
```
❌ <b>Gagal.</b> {alasan singkat}
```
```
⚠️ <b>Peringatan:</b> {keterangan}
```

---

### 5. ReplyKeyboard — Tetap Dipertahankan (JANGAN DIRUBAH)

> [!IMPORTANT]
> **Seluruh teks dan layout ReplyKeyboard button WAJIB tetap dipertahankan persis seperti aslinya.**
> Jangan mengubah teks emoji ganda pada tombol keyboard (contoh: `"💎 Status 💎"`, `"🛒 Shop 🛒"`, `"⬅️ Batal ⬅️"` dsb. tetap utuh), agar alur kebiasaan navigasi user tidak berubah.
> Yang didesain ulang secara visual hanyalah: **isi bubble pesan (teks respons), format tabel/struktur data, dan inline keyboard button.**

---

## Yang TIDAK Boleh Diubah

- **Teks dan susunan tombol `ReplyKeyboard`** (semua variabel keyboard di `panel.go` tetap sama persis)
- Callback data string: `"panel:list:delete:{id}"`, `"assist_help:{section}"`, dll.
- Redis Stream logic dan struktur payload
- Fungsi Go (signature, parameter)
- Logika state management (`setPanelState`, `waiting_xxx`)
- HTTP fallback dan error handling

---

## Yang HARUS Diubah

1. Semua string literal teks respons di `panel.go`, `ubot.go`, `userbot.go`, `users.go`, `panel_callback.go`
2. Desain format visual: judul tebal, divider `━━━━━━━━━━━━━━━━━━━━`, baris tabel / key-value rapi menggunakan `<code>`
3. Format teks inline keyboard button (di `ubot.go`, `panel.go`, dan `panel_callback.go`)
4. Fungsi `ShowMainMenu`, `ShowStatus`, `ShowGrupList`, `CekList`, `CekGrup`, `PromptTambahList`, `PromptTambahGrup`, `PromptTambahGrupManual`, `handleBroadcastMonitor`, `ShowServerDashboard`, `HelpCallback`, `broadcastMonitorText`, `broadcastDetailText`

---

## Deliverable

Setelah selesai:
1. Semua file diubah sesuai aturan di atas
2. Jalankan `go build ./...` di `gateway/` — harus zero error
3. Jalankan `go test ./...` di `gateway/` — harus pass
4. Commit dengan message: `ui: redesign bot responses — clean, modern, production grade`

---

## Catatan Teknis

- Gunakan `ParseMode: "HTML"` untuk semua pesan teks
- Karakter `&` di teks HTML harus di-escape: `&amp;`
- Gunakan `html.EscapeString()` untuk semua nilai dinamis (nama user, judul grup, dll.)
- Divider: gunakan `━━━━━━━━━━━━━━━━━━━━` (10 karakter `━`)
- Jangan gunakan `<h1>`, `<tg-button-row>`, `<hr/>` — gunakan HTML biasa

