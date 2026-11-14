# GotdBot

GotdBot adalah sistem manajemen userbot Telegram yang terdiri dari dua mikroservis Go:
- **Gateway** — antarmuka kontrol panel (Bot API + MTProto) untuk mengelola sesi userbot, token, grup, siaran, dan telemetri.
- **Ubot** — engine MTProto userbot yang menjalankan ribuan sesi Telegram secara konkuren.

Kedua layanan saling berkomunikasi melalui **Redis Streams**. Data disimpan di **SQLite** (primary) dan opsional **MongoDB** (offsite backup).

## Arsitektur

```
Telegram Bot API / MTProto
        ↓
   Gateway (assterisk/gateway)
        ↓  Redis Streams
   Ubot (assterisk/ubot)
        ↓
   SQLite + MongoDB (opsional)
```

## Persyaratan

- Go 1.26+
- Redis 7+
- SQLite (terdapat dalam dependensi Go, tidak perlu server terpisah)
- Docker & Docker Compose (opsional, untuk deployment)

## Instalasi Cepat (Docker)

```bash
# Clone repo
git clone <repo-url>
cd GotdBot

# Salin contoh env dan isi nilai yang diperlukan
cp .env.example .env
# Edit .env: isi BOT_TOKEN, API_ID, API_HASH, SESSION_ENCRYPTION_KEY, REDIS_PASS, dll.

# Jalankan dengan Docker Compose
docker compose up -d
```

## Instalasi Cepat (Native Go)

```bash
# Terminal 1: Gateway
cd gateway
go run ./cmd/gateway

# Terminal 2: Ubot
cd ubot
go run ./cmd/ubot
```

Pastikan Redis berjalan dan `.env` sudah diisi sebelum menjalankan.

## Variabel Lingkungan (.env)

| Variabel | Wajib | Contoh | Keterangan |
|----------|-------|--------|------------|
| `BOT_TOKEN` | Ya | `123456:ABC-DEF` | Token Bot Telegram |
| `API_ID` | Ya (ubot) | `123456` | API ID dari my.telegram.org |
| `API_HASH` | Ya (ubot) | `abcdef1234567890` | API Hash dari my.telegram.org |
| `SESSION_ENCRYPTION_KEY` | Ya (ubot) | base64 32 byte | Kunci enkripsi sesi userbot |
| `REDIS_ADDR` | Ya | `redis:6379` | Alamat Redis |
| `REDIS_PASS` | Ya | `password-aman` | Password Redis |
| `SQLITE_PATH` | Tidak | `data/userbot.db` | Path file SQLite |
| `MONGO_BACKUP_URI` | Tidak | `mongodb+srv://...` | URI MongoDB Atlas (kosongkan jika tidak perlu) |
| `MAX_ACTIVE_SESSIONS` | Tidak | `300` | Maksimal session aktif |
| `GOMEMLIMIT` | Tidak | `700MiB` | Batas memori Go runtime |
| `GOGC` | Tidak | `30` | Target garbage collector |

## Optimasi untuk VPS Terbatas (1GB RAM / 2 CPU)

Jika berjalan di VPS 1GB RAM untuk ~300-400 user aktif:

1. **Turunkan `GOMEMLIMIT`** ke `700MiB` dan **`GOGC`** ke `30` di `.env`.
2. **Matikan MongoDB** jika tidak diperlukan: kosongkan `MONGO_BACKUP_URI`.
3. **Perkecil SQLite connection pool** di `internal/databases/sqlite.go` (ubah `SetMaxOpenConns` ke `5`, `SetMaxIdleConns` ke `2`).
4. **Turunkan `cache_size` SQLite** ke `-8000` (~8MB).
5. **Nonaktifkan `!eval` dan `!sh`** jika tidak digunakan (boros RAM karena interpreter Yaegi).
6. **Jangan menjalankan 1000 session MTProto sekaligus** di memori. Gunakan lazy-load session: hanya muat blob session saat user aktif, buang setelah idle.

Struktur file `.env` untuk VPS 1GB:

```
GOMEMLIMIT=700MiB
GOGC=30
MAX_ACTIVE_SESSIONS=300
SESSION_HEARTBEAT_SECONDS=60
MONGO_BACKUP_URI=
```

## Struktur Proyek

```
GotdBot/
├── gateway/              # Mikroservis Gateway (assterisk/gateway)
│   ├── cmd/gateway/      # Entry point
│   ├── internal/
│   │   ├── app/          # Inisialisasi aplikasi & shutdown
│   │   ├── botgotd/      # Adapter MTProto / Bot API
│   │   ├── broker/       # Redis Streams broker
│   │   ├── config/       # Konfigurasi dari env + YAML
│   │   ├── controllers/  # Handler panel, token, userbot
│   │   ├── databases/    # SQLite + MongoDB access
│   │   ├── middleware/   # Logging middleware
│   │   ├── models/       # Payload data structures
│   │   └── utils/        # Logger, helpers
│   ├── Dockerfile
│   └── go.mod
├── ubot/                 # Mikroservis Ubot (assterisk/ubot)
│   ├── cmd/ubot/         # Entry point
│   ├── internal/
│   │   ├── app/          # Inisialisasi aplikasi & shutdown
│   │   ├── config/       # Konfigurasi
│   │   ├── databases/    # SQLite, Redis, MongoDB
│   │   ├── middleware/   # Logging middleware
│   │   ├── models/       # Payload data structures
│   │   ├── modules/
│   │   │   ├── broadcast/   # Modul siaran pesan
│   │   │   └── userbot/     # Engine userbot, session, commands
│   │   └── utils/        # Logger, backoff, queue, encrypt
│   ├── Dockerfile
│   └── go.mod
├── docker-compose.yaml    # Produksi (Redis + Gateway + Ubot)
├── docker-compose.dev.yaml# Override development
├── .env.example          # Template variabel lingkungan
├── push.ps1              # Build & push Docker image ke GHCR
├── run.ps1               # Jalankan lewat Docker
├── run-dev.ps1           # Jalankan lewat `go run`
└── sync_config.jsonc     # Config SFTP untuk deploy ke VPS
```

## Keamanan

- Jangan commit file `.env` ke git (sudah di `.gitignore`).
- Fitur `!eval` dan `!sh` mengeksekusi kode arbitrer. Nonaktifkan di production jika tidak diperlukan.
- Session blob disimpan di SQLite. Pertimbangkan enkripsi tambahan untuk data sensitif.
- `SESSION_ENCRYPTION_KEY` harus berupa base64 tepat 32 byte.

## Development

```bash
# Lint (jika golangci-lint terinstal)
golangci-lint run ./...

# Tes
cd gateway && go test ./...
cd ubot && go test ./...

# Build
cd gateway && go build -o bin/gateway ./cmd/gateway
cd ubot && go build -o bin/ubot ./cmd/ubot
```

## Lisensi

Tidak ditentukan. Lihat file lisensi di repo jika tersedia.