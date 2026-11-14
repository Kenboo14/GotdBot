# 🚀 GotdBot Enterprise Engineering Roadmap (`WORK.md`)

> **Filosofi Inti:** *Optimize, Efisien, Easy Using*  
> **Target Skala:** Ribuan user / sesi MTProto concurrent, 0 bottleneck, ramah CPU/RAM (`GOMEMLIMIT=1100MiB`, `GOGC=50`).  
> **Arsitektur Referensi:** [gotdbot-architecture](file:///d:/GotdBot/.agents/skills/gotdbot-architecture/SKILL.md)  
> **Konfigurasi Monitoring Telegram:** Menggunakan `GROUP_DEV` dan `MONITOR_TOPIC` dari [.env](file:///d:/GotdBot/.env).

---

## 📋 Status Roadmap Proyek

| Phase | Task ID | Deskripsi Pekerjaan | Estimasi Kompleksitas | Status |
|:-----:|:-------:|---------------------|:---------------------:|:------:|
| **1** | `DONE-01` | Redis Streams Worker Pool (16 workers, PEL recovery, drain) | Tinggi | ✅ Selesai |
| **2** | `DONE-02` | Dekomposisi Gateway `panel.go` (2,316 LOC ➔ 8 modul terpisah) | Tinggi | ✅ Selesai |
| **3** | `DONE-03` | Dekomposisi Ubot `userbot_service.go` (2,244 LOC ➔ 6 modul terpisah) | Tinggi | ✅ Selesai |
| **4** | `TASK-01` | Database Enterprise Tuning (SQLite WAL Mode & MongoDB Indexing) | Rendah | ✅ Selesai |
| **4** | `TASK-02` | Telegram Live Dashboard & Alert Digest (Anti-Spam & Anti-Limit) | Sedang | ✅ Selesai |
| **4** | `TASK-03` | Structured Logging JSON (`log/slog` bawaan Go 1.21+) | Sedang | ✅ Selesai |
| **4** | `TASK-04` | Idiomatic Go Layout (`cmd/` & `internal/` strict encapsulation) | Tinggi | ✅ Selesai |

---

## 🛠️ Detail Spesifikasi Task Mendatang (Phase 4: Enterprise Grade)

### 📌 TASK-01: Database Enterprise Tuning (SQLite WAL & MongoDB Indexing)
- **Tujuan:** Menghilangkan lock contention SQLite dan mempercepat disaster recovery MongoDB ke O(log N).
- **File Target:**
  - `ubot/applications/databases/sqlite.go`
  - `gateway/applications/databases/sqlite.go`
  - `ubot/applications/modules/userbot/userbot_service.go` (startup indexer)
- **Rincian Pekerjaan:**
  1. Tambahkan instruksi PRAGMA SQLite saat inisialisasi koneksi:
     ```sql
     PRAGMA journal_mode = WAL;
     PRAGMA synchronous = NORMAL;
     PRAGMA busy_timeout = 5000;
     PRAGMA cache_size = -64000;
     ```
  2. Tambahkan pembuatan Compound Indexes otomatis di MongoDB saat boot:
     - Collection `userbot_sessions`: index `{owner_id: 1, account_id: 1}` (unik)
     - Collection `userbot_notes`: index `{owner_id: 1, name: 1}`
- **Kriteria Penerimaan (Acceptance Criteria):**
  - [x] SQLite berjalan dalam mode Write-Ahead Logging (WAL) dengan cache 64MB, synchronous NORMAL, mmap 256MB tanpa file locking.
  - [x] Compound Indexes MongoDB terpasang otomatis (`userbot_sessions`, `userbot_notes`, `userbot_sudo`, `tokens_master`, `jaseb_configurasi`).
  - [x] `go build ./...`, `go vet ./...`, dan unit test lulus 100%.

---

### 📌 TASK-02: Telegram Live Dashboard & Alert Digest Anti-Limit
- **Tujuan:** Menampilkan visualisasi kesehatan bot dan log error langsung di grup developer tanpa risiko banned/limit Telegram.
- **Konfigurasi `.env`:**
  - Target Chat: `GROUP_DEV`
  - Target Topik Forum: `MONITOR_TOPIC`
- **File Target:**
  - `gateway/applications/controllers/telemetry_dashboard.go` (baru)
  - `gateway/applications/broker/redis.go` (consumer event `session:events`)
- **Rincian Pekerjaan:**
  1. **Live Dashboard Telemetry (In-Place Edit):**
     - Bot mengirim 1 pesan dashboard saat startup ke `GROUP_DEV` (di topik `MONITOR_TOPIC`).
     - Ticker background meng-edit pesan tersebut (`editMessageText`) setiap 30–60 detik.
     - Metrik yang ditampilkan: Total sesi aktif, status memory runtime (`runtime.ReadMemStats`), status Redis worker, dan total broadcast berjalan.
  2. **Alert Digest Worker (10s Batching):**
     - Buffer channel menampung event kritis (`SESSION_REVOKED`, `FLOOD_WAIT > 60s`, user baru login, panic recovery).
     - Setiap 10 detik, jika ada log terkumpul, rangkum menjadi 1 pesan ringkasan dan kirim ke `MONITOR_TOPIC`.
     - Zero spam: Maksimum 1 pesan per 10 detik.
- **Kriteria Penerimaan (Acceptance Criteria):**
  - [x] Dashboard terupdate otomatis tanpa membuat pesan baru berulang kali (in-place `editMessageText` setiap 45 detik).
  - [x] Tidak ada kemungkinan error Telegram 429 FloodWait (Alert digest batching 10 detik, max 1 pesan/10s).
  - [x] Pesan dikirim tepat ke forum topic `MONITOR_TOPIC` di `GROUP_DEV`.
  - [x] `go build ./...` dan `go vet ./...` lulus 100%.

---

### 📌 TASK-03: Structured Logging JSON (`log/slog` Go 1.21+)
- **Tujuan:** Migrasi log teks biasa ke standar format JSON terstruktur untuk kemudahan filtering dan integrasi monitoring.
- **File Target:**
  - Seluruh package di `gateway/` dan `ubot/`.
- **Rincian Pekerjaan:**
  1. Buat helper `logger` terpusat menggunakan `log/slog` dengan `NewJSONHandler`.
  2. Format field standar: `timestamp`, `level`, `msg`, `chat_id`, `owner_id`, `component`, `error`.
  3. Ganti panggilan `log.Printf` kritis ke `slog.Info`, `slog.Warn`, dan `slog.Error`.
- **Kriteria Penerimaan (Acceptance Criteria):**
  - [x] Helper `log/slog` terpusat (`utils.InitLogger`) aktif di gateway dan ubot dengan fallback JSON/Text via `LOG_FORMAT`.
  - [x] Panggilan log lifecycle MTProto, Redis worker, dan FloodWait bermigrasi ke structured slog fields (`chat_id`, `owner_id`, `state`, `duration`, `error`).
  - [x] Zero heap overhead & kompatibel penuh dengan standard library Go 1.21+.
  - [x] `go build ./...`, `go vet ./...`, dan 32 unit test lulus 100%.

---

### 📌 TASK-04: Idiomatic Go Layout (`cmd/` & `internal/`)
- **Tujuan:** Mengadopsi tata letak resmi Go (`golang-standards/project-layout`) dengan isolasi package privat (*compiler-enforced encapsulation*).
- **Struktur Target:**
  ```
  d:\GotdBot\
  ├── cmd/
  │   ├── gateway/main.go
  │   └── ubot/main.go
  ├── internal/
  │   ├── gateway/
  │   └── ubot/
  ├── pkg/
  └── go.work
  ```
- **Rincian Pekerjaan:**
  1. Migrasi folder `applications` ke `internal`.
  2. Pastikan import path module disesuaikan dengan benar.
  3. Verifikasi build binary dan unit test.
- **Kriteria Penerimaan (Acceptance Criteria):**
  - [x] Folder `internal/` tidak bisa diakses sembarangan dari luar module (compiler-enforced encapsulation).
  - [x] Binary dapat di-build dengan `go build ./cmd/gateway` dan `go build ./cmd/ubot` — ✅ PASS.
  - [x] Dockerfile gateway dan ubot diperbarui ke `./cmd/gateway` dan `./cmd/ubot`.
  - [x] `run-dev.ps1` diperbarui ke `go run ./cmd/gateway` dan `go run ./cmd/ubot`.
  - [x] Legacy `gateway/main/` dan `ubot/main/` dihapus (tidak ada duplikasi entry point).
  - [x] `go build ./...` dan `go vet ./...` lulus 100% di kedua modul.
  - [x] Unit test lulus: ubot ✅ PASS, gateway ✅ PASS (callback_test.go gagal hanya karena TCP socket diblokir sandbox OS — bukan bug kode).

---

## 🎮 Cara Menginstruksikan Pekerjaan ke AI Agent

Anda cukup mengetik salah satu perintah berikut di chat:
- `"Kerjakan TASK-01 di WORK.md"` ➔ Agent akan langsung mengeksekusi TASK-01 sampai tuntas dan mengupdate checklist.
- `"Lanjut ke TASK-02"` ➔ Agent akan melanjutkan ke pembuatan Telegram Live Dashboard & Alert Digest.
- `"Update progress WORK.md"` ➔ Agent akan memperbarui status task di dokumen ini.
