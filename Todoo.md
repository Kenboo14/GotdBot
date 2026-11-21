# Product Requirements Document (PRD): Penyempurnaan Blacklist, Manajemen Grup, Sistem Tokenisasi & Copywriting GotdBot

- **Status**: Draft PRD (Siap Implementasi)
- **Tanggal**: 2026-11-21
- **Cakupan Layanan**: `gateway` (`assterisk/gateway`) & `ubot` (`assterisk/ubot`)
- **Standar Kode (`AGENTS.md`)**:
  - Maksimal `400` baris per file Go baru atau yang dimodifikasi.
  - Tanpa komentar kode Go (`//` atau `/* */`) baru.
  - Error wrapping menggunakan `fmt.Errorf("...: %w", err)`.
  - Dilarang menyentuh `.env` produksi atau file database `*.db`.

---

## 1. Ringkasan Eksekutif & Tujuan

Dokumen ini merangkum seluruh kebutuhan pengembangan lanjutan untuk meningkatkan akurasi identifikasi chat Telegram, fleksibilitas input pengguna/panel, keamanan dan portabilitas sistem token serta backup data, serta kebersihan bahasa respon bot yang dilihat oleh customer:

1. **Epic 1 — Blacklist Broadcast (`!addbl`, `!lsbl`, `!rmbl`)**: Standardisasi format ID supergrup (`-100xxxx`), tampilan terkelompok `GROUP` / `PRIVATE` / `CHANNEL`, pemotongan nama/judul maksimal `10` karakter, dukungan `!addbl` jarak jauh (*remote*), serta `!rmbl` berdasarkan nomor urut kategori tanpa harus masuk ke chat target.
2. **Epic 2 — Tambah Grup Manual, Auto-Join & Normalisasi ID (`Cek Grup` / `Display Grup`)**: Dukungan input `-100xxx`, `-xxx`, `@username`, link publik (`t.me/xxx`), maupun link invite private (`t.me/+xxx` / `t.me/joinchat/xxx`) lengkap dengan auto-join userbot dan standarisasi tampilan ID supergrup `-100xxx`.
3. **Epic 3 — Sistem Tokenisasi Baru (`GOTD:M`, `GOTD:S`, `GOTD:B`)**: Token Fernet double-Base64 (~134 karakter / payload 73 byte) untuk seluruh tier (`GOTD:M` Master, `GOTD:S` Token Kepemilikan/Sesi) serta kode backup portabel `GOTD:B<base64_json>` (dengan fallback file `.txt` jika `> 4000` karakter), tetap mempertahankan kompatibilitas penuh terhadap token lama (`GOTD:`, `GOTD_SHA:`, `GOTD_BKP:`).
4. **Epic 4 — Pembersihan Copywriting Customer-Facing**: Menghilangkan seluruh istilah teknis internal (`enkripsi`, `SHA`, `SHA-256`, `Fernet`, `Base64`, `GOTD_SHA`) dari pesan yang dibaca customer; menampilkan hasil **berhasil** atau **gagal** secara ringkas dan bersih.

---

## 2. Epic 1 — Blacklist Broadcast (`!addbl`, `!lsbl`, `!rmbl`)

### 2.1 Analisis Akar Masalah Saat Ini
Berdasarkan inspeksi kode pada [command_blacklist.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/command_blacklist.go) dan [sqlite.go](file:///mnt/data/GotdBot/ubot/internal/databases/sqlite.go):
1. Supergrup (`*tg.InputPeerChannel` dengan `Megagroup = true`) disimpan sebagai `kind = "group"` dengan `chatID = value.ChannelID` (ID positif MTProto, mis. `2590747818`). Fungsi `displayChatID("group", id)` mengembalikan `-id` (`-2590747818`) alih-alih `-1000000000000 - id` (`-1002590747818`), sehingga supergrup muncul **tanpa awalan `-100`**.
2. Fungsi `SQLiteClient.ListBlacklist` ([sqlite.go](file:///mnt/data/GotdBot/ubot/internal/databases/sqlite.go)) dan tabel `broadcast_blacklist` belum menyimpan kolom `title` maupun `username`, sehingga `!lsbl` saat membaca dari SQLite menghasilkan nama/judul kosong.
3. `!addbl` hanya mengizinkan argumen target pada mode `-pv`, serta belum mendukung target grup/channel lewat ID, username, atau link dari luar lokasi chat.
4. `!rmbl` belum mendukung penghapusan berdasarkan nomor urut tampilan (`!rmbl -gc <no>`, `!rmbl -pv <no>`, `!rmbl -ch <no>`).

### 2.2 Spesifikasi Fungsional

#### A. Format Output `!lsbl`
Urutan tampilan dikelompokkan menjadi **3 bagian terpisah** dengan penomoran dimulai dari `1` pada setiap bagian (hanya tampilkan bagian yang memiliki entri, atau pesan kosong jika seluruhnya kosong):

```text
List Blacklist:

GROUP
1. -100xxxxxxxxxx | NamaSuperg
2. -xxxxxxxxx | NamaGrupBi

PRIVATE
1. 834738 | @usernamen
2. 3473847 | NamaDepanP

CHANNEL
1. -100xxxxxxxxxx | NamaChanne
```

Aturan format `!lsbl`:
- **Format ID**:
  - Supergrup & Channel: wajib berawalan `-100` (`-1000000000000 - channelID` jika disimpan sebagai positive channelID).
  - Grup biasa (*basic chat*): berawalan `-` (`-chatID`).
  - Private user: ID positif tanpa `-`.
- **Prioritas Label**:
  - `GROUP` dan `CHANNEL`: gunakan **Title** chat (maksimal **10 karakter** rune; potong tanpa merusak UTF-8 multi-byte).
  - `PRIVATE`: **prioritas `@username`**, jika tidak punya username gunakan fallback **Nama (First Name / Full Name)**, maksimal **10 karakter** rune.
- **Pengurutan Deterministik**: Setiap bagian diurutkan konsisten (mis. berdasarkan `created_at` / urutan simpan atau `chat_id` menaik) antara `!lsbl` dan `!rmbl -gc|-pv|-ch <no>` agar nomor urut selalu cocok tepat.

#### B. Perintah `!addbl` (2 Mode)
1. **Mode Langsung di Lokasi Chat**:
   - `!addbl` di dalam grup/supergrup/channel/PM otomatis mem-blacklist chat tempat command dikirim.
   - `!addbl -pv` sambil me-reply pesan user di grup otomatis mem-blacklist pengirim pesan yang dibalas.
2. **Mode Remote (Tanpa Masuk Chat)**:
   - `!addbl <target>` mendukung input:
     - ID Supergrup / Channel (`-100xxxx`)
     - ID Grup Biasa (`-xxxx`)
     - Username grup / channel / user (`@username` atau `username`)
     - Link grup / channel (`https://t.me/username` atau `t.me/username`)
     - User ID (`12345678`)
   - Dukungan flag opsional `-gc`, `-pv`, `-ch` tetap dapat dipakai bila ingin memperjelas tipe, atau tipe dideteksi otomatis dari entitas hasil resolusi Telegram (`PeerUser` → `private`, `PeerChat` → `group`, `PeerChannel` dengan `Broadcast=true` → `channel`, `PeerChannel` dengan `Megagroup=true` → `group`).
   - Resolusi title & `@username` diambil dari entity/dialog cache atau MTProto (`ContactsResolveUsername` / `ChannelsGetChannels` / `MessagesGetChats` / `UsersGetUsers`) lalu disimpan permanen ke SQLite + MongoDB.

#### C. Perintah `!rmbl` (2 Mode)
1. **Mode Langsung di Lokasi Chat / Target Eksplisit**:
   - `!rmbl` di chat target (atau `!rmbl <id|@username>`) menghapus chat tersebut dari blacklist.
2. **Mode Remote Berdasarkan Nomor `!lsbl`**:
   - `!rmbl -gc <nomor>` → menghapus item ke-`<nomor>` pada bagian **GROUP**.
   - `!rmbl -pv <nomor>` → menghapus item ke-`<nomor>` pada bagian **PRIVATE**.
   - `!rmbl -ch <nomor>` → menghapus item ke-`<nomor>` pada bagian **CHANNEL**.
   - Mendukung penghapusan satu nomor (mis. `!rmbl -gc 1`) dan validasi batas nomor (`>= 1` dan `<= len(section)`). Jika nomor tidak ditemukan/tidak valid, balas pesan gagal terstruktur sesuai gaya `command_style.go`.

### 2.3 Perubahan Skema Data & File Terdampak
- **Skema SQLite `broadcast_blacklist`** ([sqlite.go](file:///mnt/data/GotdBot/ubot/internal/databases/sqlite.go)):
  - Tambah migrasi aman (`ALTER TABLE ... ADD COLUMN`) untuk `peer_kind TEXT` (untuk membedakan supergrup vs grup biasa secara eksplisit atau normalisasi ID `-100`), `title TEXT`, dan `username TEXT`.
- **MongoDB `userbot_broadcast_blacklist`**:
  - Simpan field `peer_kind`, `title`, dan `username` bersamaan saat upsert `$set`.
- **Pencocokan Broadcast (`command_broadcast.go`, `panel_broadcast.go`)**:
  - Pastikan pengecekan blacklist saat mengirim broadcast (`gcast` / `ucast` / Jaseb) membaca format ID ternormalisasi / konsisten untuk grup biasa, supergrup, channel, maupun private.
- **File Terdampak**:
  - [command_blacklist.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/command_blacklist.go) (pecah helper bila mendekati batas 400 baris)
  - [sqlite.go](file:///mnt/data/GotdBot/ubot/internal/databases/sqlite.go)
  - [command_broadcast.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/command_broadcast.go)

### 2.4 Kriteria Penerimaan (Acceptance Criteria)
- `!addbl` di supergrup menghasilkan konfirmasi ID berawalan `-100` (bukan `-` saja).
- `!lsbl` menampilkan 3 bagian `GROUP`, `PRIVATE`, `CHANNEL` bernomor urut `1..N`, supergrup/channel memakai `-100xxxx`, grup biasa `-xxxxx`, `PRIVATE` memprioritaskan `@username` lalu Nama, dan seluruh label terpotong rapi maksimal `10` karakter.
- `!rmbl -gc 1`, `!rmbl -pv 2`, dan `!rmbl -ch 1` menghapus entri yang sesuai dari luar lokasi chat, sinkron antara SQLite dan MongoDB.
- `!addbl <grupid|@username|userid|link>` berhasil menambah entri secara remote beserta title/username-nya.

---

## 3. Epic 2 — Tambah Grup Manual, Auto-Join & Normalisasi ID (`Cek Grup` / `Display Grup`)

### 3.1 Analisis Akar Masalah Saat Ini
Berdasarkan inspeksi pada [panel_input.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_input.go), [panel_grup.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_grup.go), dan [session_commands.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/session_commands.go):
1. Pada `waiting_add_group_manual` ([panel_input.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_input.go)), gateway langsung menyimpan teks mentah ke `h.db.AddGroup(ctx, userID, p, p, chatIDVal)` tanpa mem-parsing format link `https://t.me/username`, link private `https://t.me/+hash` / `joinchat/hash`, tidak melakukan resolusi entitas, dan tidak memicu userbot untuk auto-join atau memvalidasi target.
2. Pada `fetchAccountGroups` ([session_commands.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/session_commands.go)), untuk supergrup (`*tg.InputPeerChannel` / `entities.Channels[id]`), `Target` dan `ChatID` disimpan sebagai integer positif MTProto (mis. `2590747818`) alih-alih format Bot API `-1002590747818`. Akibatnya `CekGrup`, `DisplayGroup`, dan `showGroupDetail` di [panel_grup.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_grup.go) menampilkan supergrup tanpa awalan `-100`.

### 3.2 Spesifikasi Fungsional

#### A. Parsing & Resolusi Input `Tambah Grup Manual`
Mendukung input tunggal maupun multi-baris / multi-token (dipisah newline/spasi):
1. **ID Supergrup**: `-100xxxxxxxxxx`
2. **ID Grup Biasa**: `-xxxxxxxxx`
3. **Username Grup/Supergrup**: `@namagrup` atau `namagrup`
4. **Link Publik Telegram**: `https://t.me/namagrup`, `http://t.me/namagrup`, `t.me/namagrup`
5. **Link Invite Private Telegram**: `https://t.me/+invitehash`, `t.me/+invitehash`, `https://t.me/joinchat/invitehash`

#### B. Alur Eksekusi & Auto-Join via Userbot
1. Gateway menerima input `waiting_add_group_manual` (dan input tambah grup manual pada mode remote kontrol `ctrl:` jika ada) lalu meneruskan job resolusi & join (mis. `add_groups_manual`) ke worker `ubot` yang memegang sesi userbot aktif milik user.
2. Di `ubot`:
   - Untuk **link private** (`+hash` / `joinchat/hash`): cek dengan `MessagesCheckChatInvite`. Jika belum bergabung, panggil `MessagesImportChatInvite` agar userbot **auto-join**. Jika `USER_ALREADY_PARTICIPANT`, ambil objek chat yang sudah diikuti.
   - Untuk **`@username` / link publik `t.me/username`**: resolve via `ContactsResolveUsername`. Jika tipe adalah channel/supergrup dan userbot belum menjadi peserta (atau saat cek partisipasi belum join), panggil `ChannelsJoinChannel` agar **auto-join**.
   - Untuk **ID `-100xxx` / `-xxx`**: resolve via cache dialog / `PeerStorage` / `ChannelsGetChannels` / `MessagesGetChats` untuk memvalidasi chat ada dan mengambil `Title` serta `AccessHash` asli.
3. **Normalisasi & Batas Paket**:
   - Terapkan batas `policy.MaxGroups` sesuai paket user (berhenti menambah bila kuota paket penuh).
   - Simpan `ChatID` dengan format kanonik Bot API: **supergrup wajib `-100xxx`**, grup biasa **`-xxx`**.
   - Simpan `Title` asli grup (bukan lagi menyamakan title dengan string input mentah) dan `Target` (`-100xxx` / `-xxx` / `@username`), serta `AccessHash` & `PeerType` (`"channel"` untuk supergrup, `"chat"` untuk grup biasa) untuk eksekusi Jaseb MTProto.
4. **Laporan Ringkas & Error**:
   - Laporkan jumlah grup berhasil ditambahkan vs gagal (mis. link kedaluwarsa `INVITE_HASH_EXPIRED`, bukan grup/user private, atau terkena limit/FloodWait) secara jelas kepada customer.

#### C. Normalisasi ID Supergrup di `fetchAccountGroups`, `Cek Grup`, `Display Grup` & Panel Detail
1. Fungsi normalisasi ID kanonik grup:
   - Jika `PeerType == "channel"` dan `rawID > 0`, format kanoniknya adalah `-1000000000000 - rawID` (mis. `2590747818` → `-1002590747818`).
   - Jika `PeerType == "chat"` dan `rawID > 0`, format kanoniknya adalah `-rawID`.
2. Helper konversi balik di sisi MTProto `ubot` saat membaca `jasebGroupDoc`: jika `ChatID <= -1000000000000`, konversi ke `ChannelID = -(ChatID + 1000000000000)` untuk `InputPeerChannel`; jika `ChatID < 0`, konversi ke `ChatID = -ChatID` untuk `InputPeerChat` — sehingga tampilan selalu `-100xxx`/`-xxx` sementara MTProto pengiriman tetap presisi.
3. Fungsi tampilan di [panel_grup.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_grup.go) (`CekGrup`, `DisplayGroup`, `showGroupDetail`) dan `panel_remote.go` memformat `Target` / `Chat ID` menggunakan format kanonik (`-100xxxx` untuk supergrup, `-xxxxx` untuk grup biasa), termasuk untuk data grup lama yang sudah tersimpan sebelumnya.

### 3.3 File Terdampak
- [panel_input.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_input.go) & [panel_grup.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_grup.go)
- [panel_remote.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_remote.go)
- [session_commands.go](file:///mnt/data/GotdBot/ubot/internal/modules/userbot/session_commands.go) & modul pengiriman Jaseb (`panel_broadcast.go`)

### 3.4 Kriteria Penerimaan (Acceptance Criteria)
- User memasukkan `-100xxx`, `-xxx`, `@username`, `t.me/xxx`, atau `t.me/+invite` di Tambah Grup Manual: userbot auto-join ke link/username yang belum diikuti, mengambil judul grup asli, dan menyimpannya dalam daftar grup target.
- `Cek Grup`, `Display Grup`, `Detail Grup`, dan hasil import otomatis selalu menampilkan supergrup dengan awalan `-100` dan grup biasa dengan `-`.
- Broadcast Jaseb tetap sukses mengirim ke grup dengan ID ternormalisasi `-100xxx` maupun `-xxx`.

---

## 4. Epic 3 — Sistem Tokenisasi Baru (`GOTD:M`, `GOTD:S`, `GOTD:B`)

### 4.1 Spesifikasi Format Token Fernet Double-Base64 (`GOTD:M` & `GOTD:S`)
Berdasarkan spesifikasi target:
- Token menggunakan standar **Fernet (versi `0x80`)** dengan **AES-128-CBC + HMAC-SHA256**:
  - Format biner token Fernet: `Version (1 byte: 0x80) || Timestamp (8 byte big-endian UTC detik) || IV (16 byte acak CSPRNG) || Ciphertext (kelipatan 16 byte PKCS#7) || HMAC-SHA256 (32 byte)`.
  - Hasil Fernet kemudian di-encode **Base64 dua kali (double Base64 / URL-safe Base64)** sehingga menghasilkan string sekitar **134 karakter** (73 byte payload mentah Fernet untuk plaintext 16-31 byte; untuk token ringkas dengan ID terkompresi agar tepat/mendekati 73 byte Fernet → ~132–134 karakter double-Base64).
- **Prefiks Baru**:
  1. **`GOTD:M<double_base64_fernet>`** — **Token Master (Voucher Aktivasi Awal)**
     - Berlaku seragam untuk seluruh paket: **Free**, **Pro**, **Pro++**, dan **VIP**.
     - Isi plaintext terenkripsi memuat metadata ringkas terstruktur (mis. versi, tipe `"M"`, kode nonce/id 16-byte atau ID voucher ringkas agar total token Fernet 73 byte / double-Base64 ~134 karakter).
  2. **`GOTD:S<double_base64_fernet>`** — **Token Kepemilikan / Sesi Aktif**
     - Diterbitkan dan diaktifkan setelah user berhasil memasang / login userbot (disimpan pada `UserLicense.TokenSHA` / indeks unik agar tetap bisa dicari langsung maupun diverifikasi cryptographically).
     - Dipakai saat user terlogout lalu memasang kembali userbot, berpindah slot token, atau **memindahkan kepemilikan token ke akun Telegram lain** (tetap mengikuti aturan pembatasan paket `Free`: token Free tidak bisa dipindah ke akun lain).
- **Manajemen Kunci Rahasia (`TOKEN_FERNET_KEY`)**:
  - Membaca kunci 32-byte Fernet (16-byte signing key + 16-byte encryption key) dari env `TOKEN_FERNET_KEY` (Base64 URL-safe).
  - Jika `TOKEN_FERNET_KEY` tidak disetel, derivasikan secara deterministik menggunakan HMAC/SHA-256 berlabel domain dari `SESSION_ENCRYPTION_KEY` (jika tersedia) atau fallback secret runtime yang konsisten antar restart jika dikonfigurasi di `.env` — wajib memastikan gateway dapat memverifikasi dan men-decode seluruh `GOTD:M` & `GOTD:S` yang pernah diterbitkan.

### 4.2 Spesifikasi Format Kode Backup (`GOTD:B`)
- **Format Utama**: `GOTD:B<base64(json_compact)>` (Base64 JSON stateless & self-contained seperti contoh struktur user, tanpa bergantung mutlak pada cache sementara server).
- **Skema JSON Payload Backup (`GOTD:B`)**:
  Memetakan struktur data GotdBot ke key JSON ringkas agar kompatibel dengan pola contoh user dan mudah diparse/di-restore di semua akun/slot:
  - `"status"` (bool): status aktif/siaran (`Berjalan` = `true`, `Berhenti` = `false`)
  - `"notif"` (bool): notifikasi aktif
  - `"typenotif"` (bool / string): tipe/mode notifikasi bila relevan
  - `"jeda"` (int): jeda antar grup (`Delay`)
  - `"jedagrup"` (int): jeda per grup (`Delay`)
  - `"jedaputar"` (int): jeda putar semua grup (`DelayAllGroups`)
  - `"gc"` (`[]int64`): daftar target Chat ID grup (format `-100xxx` / `-xxx`)
  - `"gcuser"` (`map[string]string`): pemetaan `"<chat_id>": "<title>"` untuk judul grup
  - `"msg"` (`[]int`): daftar cache message ID List utama (`Lists`)
  - `"mode"` (bool / string): mode (`"biasa"` / `"pro"` / dst.)
  - `"wm"` (string): teks watermark
  - `"timer"` (string / array): pengaturan timer aktif
  - `"admin"` (int64 / string): admin darurat / sudo
  - `"topic"` (`map[string][]TopicBackupEntry`): pemetaan topic forum terhubung ke list
  - `"tlist"` (`map[string]TopicListBackupEntry`): daftar `TopicListEntry` (`msg`/posts & `mode` play_mode)
  - `"safemode"` (bool): status safemode
  - `"floodskip"` (int): `SkipFloodWait`
  - `"exp"` (string): timestamp masa aktif saat backup dibuat (informatif, **tidak** meng-override masa aktif lisensi target saat restore!)
  - `"type"` (`"broadcast"`), `"email"` (string), `"extra"` (objek opsional metadata tambahan).
- **Aturan Keamanan & Masa Aktif Saat Restore**:
  - Proses Restore hanya memulihkan data konfigurasi (`Groups`, `Lists`, `Topics`, `TopicLists`, pengaturan broadcast `Delay`, `DelayMode`, `Timer`, `SafeMode`, `SkipFloodWait`, `Watermark` sesuai hak tier penerima) dan **dilarang** mengubah/memperpanjang `ExpiresAt` atau menaikkan `Tier` lisensi akun tujuan!
- **Penanganan Ukuran Besar (`> 4000` karakter)**:
  - Jika kode `GOTD:B...` memiliki panjang `<= 4000` karakter, kirim langsung sebagai pesan teks di bot (dalam `<code>...</code>` atau salinan bersih).
  - Jika panjang kode `GOTD:B...` **melebihi `4000` karakter**, bot otomatis mengirimnya sebagai dokumen file `.txt` (mis. `gotdbot-backup-<userID>.txt`) agar tidak terkena batas `MESSAGE_TOO_LONG` Telegram.
  - Pada menu **♻️ Restore Data**, bot menerima **teks `GOTD:B...`** maupun **file dokumen `.txt`** yang diunggah user berisi `GOTD:B...` (unduh via Bot API/MTProto, validasi prefix & dekode Base64 JSON).

### 4.3 Aturan Kompatibilitas Token Lama (Backward Compatibility)
Untuk memastikan user lama tidak terputus aksesnya:
1. **Generasi Baru**:
   - `/token` (seluruh tier: `-free`, `pro`, `pro++`, `vip`), pembuatan voucher baru, dan alokasi token user baru seluruhnya menerbitkan prefiks **`GOTD:M...`**.
   - Penerbitan/rotasi token kepemilikan setelah pasang userbot (`ambil token` / alert darurat / DB slot) menerbitkan prefiks **`GOTD:S...`**.
   - Backup data (`📎 Backup Data` & alert backup darurat) menerbitkan prefiks **`GOTD:B...`**.
2. **Validasi / Penerimaan Eksisting**:
   - Input `waiting_token`, `waiting_upgrade_token`, `/token info`, `/token revoke` tetap menerima baik prefiks baru (`GOTD:M...`, `GOTD:S...`) maupun prefiks lama (`GOTD:<32_huruf>`, `GOTD_SHA:<hex64>`).
   - Input `waiting_restore_backup` tetap menerima baik `GOTD:B<base64_json>` (teks atau file `.txt`) maupun kode lama `GOTD_BKP:<kode>`.

### 4.4 File Terdampak
- Paket kriptografi/token baru mis. `gateway/internal/controllers/token_fernet.go` & `gateway/internal/controllers/token_backup.go` (menjaga setiap file `<= 400` baris)
- [tokens.go](file:///mnt/data/GotdBot/gateway/internal/controllers/tokens.go)
- [token_upgrade.go](file:///mnt/data/GotdBot/gateway/internal/controllers/token_upgrade.go)
- [token_owner_manage.go](file:///mnt/data/GotdBot/gateway/internal/controllers/token_owner_manage.go)
- [token_slots.go](file:///mnt/data/GotdBot/gateway/internal/controllers/token_slots.go)
- [panel_settings.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_settings.go)
- [panel_media.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_media.go) / `HandleMessage` untuk penerimaan dokumen `.txt` saat `waiting_restore_backup`
- [userbot.go](file:///mnt/data/GotdBot/gateway/internal/controllers/userbot.go), [ubot.go](file:///mnt/data/GotdBot/gateway/internal/controllers/ubot.go), [emergency_notify.go](file:///mnt/data/GotdBot/gateway/internal/controllers/emergency_notify.go)

### 4.5 Kriteria Penerimaan (Acceptance Criteria)
- Token master yang dibuat lewat `/token` (Free, Pro, Pro++, VIP) selalu diawali `GOTD:M`, berbasis Fernet double-Base64 (versi `0x80`, panjang target ~134 karakter / 73 byte payload biner Fernet), dan dapat didekode/divalidasi dengan `TOKEN_FERNET_KEY`.
- Token kepemilikan/sesi yang diterima user setelah pasang userbot selalu diawali `GOTD:S` dan dapat dipakai login ulang setelah logout maupun pindah kepemilikan akun (non-Free).
- Kode backup baru diawali `GOTD:B`, berisi Base64 JSON konfigurasi user, otomatis dikirim sebagai file `.txt` bila `> 4000` karakter, dan dapat di-restore baik dari pesan teks maupun file `.txt` tanpa mengubah `Tier`/`ExpiresAt` akun tujuan.
- Token lama (`GOTD:<32>`, `GOTD_SHA:...`, `GOTD_BKP:...`) tetap dapat diproses tanpa error.

---

## 5. Epic 4 — Pembersihan Copywriting Respon Bot (Customer-Facing)

### 5.1 Aturan Copywriting
Customer tidak perlu mengetahui mekanisme internal hashing, enkripsi, maupun arsitektur tokenisasi backend.
- **Istilah Dilarang di Pesan Customer** (panel user, `/start`, `waiting_token`, `Settings`, `Ambil Token`, `Ganti Token`, `Backup/Restore`, error format token):
  - `SHA`, `SHA-256`, `GOTD_SHA` (ganti penyebutan ke *Token Akses* / *Token Kepemilikan* atau `GOTD:S:...` bila perlu menyebut prefiks kode)
  - `enkripsi`, `terenkripsi`, `encrypt`, `Fernet`, `AES`, `Base64`, `hash`, `checksum`
  - Kalimat penjelasan tentang "token dienkripsi/di-hash setelah login"
- **Gaya Respon yang Diharapkan**:
  - Singkat, jelas, fokus pada status **Berhasil** atau **Gagal** dan tindakan selanjutnya (mis. `✅ Token berhasil diaktifkan.`, `❌ Token tidak valid atau sudah digunakan.`, `✅ Data berhasil dipulihkan.`, `❌ Kode backup tidak valid.`).
  - Notifikasi internal Owner (`MONITOR_TOKEN_*` / `/token`) boleh tetap memuat detail manajemen secukupnya, namun pesan langsung ke customer harus bebas dari jargon kriptografi.

### 5.2 File Terdampak
- [text.go](file:///mnt/data/GotdBot/gateway/internal/assets/text.go) & [ikb.go](file:///mnt/data/GotdBot/gateway/internal/assets/ikb.go)
- [userbot.go](file:///mnt/data/GotdBot/gateway/internal/controllers/userbot.go)
- [panel_settings.go](file:///mnt/data/GotdBot/gateway/internal/controllers/panel_settings.go)
- [token_upgrade.go](file:///mnt/data/GotdBot/gateway/internal/controllers/token_upgrade.go)
- [token_slots.go](file:///mnt/data/GotdBot/gateway/internal/controllers/token_slots.go)
- [emergency_notify.go](file:///mnt/data/GotdBot/gateway/internal/controllers/emergency_notify.go)

### 5.3 Kriteria Penerimaan (Acceptance Criteria)
- Audit teks respon customer di seluruh paket `gateway/internal/assets` dan `gateway/internal/controllers` memastikan tidak ada kata `SHA`, `enkripsi`/`terenkripsi`, `Fernet`, atau `Base64` yang terkirim ke customer.
- Seluruh alur aktivasi token, ambil token, pindah slot, pindah akun, backup, dan restore menampilkan pesan bersih berbasis status berhasil/gagal.

---

## 6. Roadmap Implementasi & Pengujian

### Fase Implementasi Disarankan
1. **Fase 1 (Blacklist & Normalisasi Grup — `ubot` + `gateway`)**:
   - Migrasi kolom `broadcast_blacklist` SQLite & MongoDB (`peer_kind`, `title`, `username`), normalisasi ID `-100xxx` di `displayChatID` & `fetchAccountGroups`, serta format baru `!lsbl` dan mode `!addbl` / `!rmbl -gc|-pv|-ch <no>`.
   - Auto-join + resolusi grup manual (`-100xxx`, `-xxx`, `@username`, `t.me/...`, `t.me/+...`) di `waiting_add_group_manual`, serta tampilan konsisten `-100` pada `CekGrup` / `DisplayGroup` / `showGroupDetail`.
2. **Fase 2 (Sistem Tokenisasi Fernet & Backup `GOTD:B` — `gateway`)**:
   - Implementasi codec Fernet double-Base64 (`GOTD:M`, `GOTD:S`) serta enkoder/dekoder JSON Base64 `GOTD:B` (termasuk fallback `.txt` > 4000 karakter & handler dokumen `.txt` saat restore).
   - Integrasi backward compatibility token lama di DB, lookup, slot switcher, dan `/token` Owner.
3. **Fase 3 (Pembersihan Copywriting & Penyelarasan Tes)**:
   - Pembersihan teks customer-facing di `assets` dan `controllers` agar hanya menampilkan berhasil/gagal tanpa istilah teknis enkripsi/SHA.

### Rencana Pengujian Wajib
- **Unit & Race Test `ubot`** (`go test -p 1 -count=1 -race ./internal/...`):
  - Test normalisasi ID supergrup (`-100xxx`) vs grup (`-xxx`) vs channel (`-100xxx`) pada `!addbl`, `!lsbl`, dan `fetchAccountGroups`.
  - Test pemotongan label maks `10` karakter UTF-8 & prioritas `@username` pada `PRIVATE`.
  - Test `!rmbl -gc <no>`, `!rmbl -pv <no>`, `!rmbl -ch <no>`, termasuk out-of-range index.
  - Test parsing input grup manual (`-100`, `-`, `@user`, `t.me/user`, `t.me/+hash`) dan mock auto-join MTProto.
- **Unit & Race Test `gateway`** (`go test -p 1 -count=1 -race ./internal/...`):
  - Test enkripsi & dekripsi Fernet double-Base64 (`GOTD:M`, `GOTD:S`): versi `0x80`, verifikasi panjang payload & HMAC invalid/tampered.
  - Test generasi & restore `GOTD:B` (inline `< 4000` char, attachment `.txt` `> 4000` char, verifikasi `Tier`/`ExpiresAt` tidak berubah saat restore).
  - Test kompatibilitas mundur terhadap `GOTD:<32_huruf>`, `GOTD_SHA:...`, dan `GOTD_BKP:...`.
  - Test audit copywriting bahwa respon customer tidak mengandung `SHA` / `enkripsi` / `Fernet` / `Base64`.
- **Static & Size Checks**: `go vet ./...`, `go build ./...`, cek jumlah baris semua file baru/terubah `<= 400` baris, dan cek bebas komentar Go baru.
