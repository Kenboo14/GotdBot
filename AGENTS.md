# AGENTS.md - GotdBot

Petunjuk untuk agent AI yang bekerja pada repository GotdBot.

## Ringkasan Proyek

GotdBot adalah sistem userbot Telegram dengan dua mikroservis Go:
- **Gateway** (`assterisk/gateway`) — kontrol panel, Bot API, MTProto, manajemen token & sesi.
- **Ubot** (`assterisk/ubot`) — engine MTProto userbot, session management, command execution.

Komunikasi antar layanan via **Redis Streams**. Database primary **SQLite**, backup opsional **MongoDB**.

## Struktur Direktori Penting

```
gateway/internal/
  app/        # App lifecycle, shutdown
  botgotd/    # MTProto / Bot API adapter (bot.go = 1001 baris, perlu dipecah)
  broker/     # Redis Streams
  config/     # Config loader (configs.yaml + .env)
  controllers/# Panel, token, userbot, session handlers
  databases/  # SQLite + MongoDB access
  utils/      # Logger, helpers

ubot/internal/
  app/        # App lifecycle, shutdown
  config/     # Config loader
  databases/  # SQLite, Redis, MongoDB
  modules/
    userbot/  # Session auth, commands (eval, shell, broadcast, dll.), storage
    broadcast/# Siaran pesan
  utils/      # Logger, backoff, queue, encrypt
```

## Konvensi Kode

- **Go version**: 1.26.0 (lihat `go.mod` di masing-masing modul).
- **Module path**: `assterisk/gateway` dan `assterisk/ubot`.
- **Logging**: `log/slog` via `internal/utils/logger.go` (JSON default, Text jika `LOG_FORMAT=text`).
- **Config**: `config.Load("configs.yaml")` dengan override dari environment variables (`.env` dimuat otomatis di gateway).
- **Database**: SQLite via `modernc.org/sqlite`, WAL mode, connection pool kecil di VPS terbatas.
- **Error handling**: jangan biarkan error menyebar tanpa penanganan; gunakan `fmt.Errorf("...: %w", err)` untuk wrap.
- **Tidak ada komentar dalam kode** kecuali diminta (peraturan kode proyek).

## Perintah Pengembangan

| Tujuan | Perintah |
|--------|----------|
| Build Gateway | `cd gateway && go build -o bin/gateway ./cmd/gateway` |
| Build Ubot | `cd ubot && go build -o bin/ubot ./cmd/ubot` |
| Tes Gateway | `cd gateway && go test ./...` |
| Tes Ubot | `cd ubot && go test ./...` |
| Vet | `cd gateway && go vet ./...` (dan ubot) |
| Run Dev | `./run-dev.ps1` (PowerShell) |
| Run Docker | `./run.ps1` atau `docker compose up -d` |

## Optimasi RAM (Fokus Utama)

Target: VPS 1GB RAM / 2 CPU, ~300-400 user aktif.

1. **`.env`**: `GOMEMLIMIT=700MiB`, `GOGC=30`, `MAX_ACTIVE_SESSIONS=300`, `SESSION_HEARTBEAT_SECONDS=60`, `MONGO_BACKUP_URI=` (kosongkan jika tidak perlu).
2. **`internal/databases/sqlite.go`** (kedua modul): ubah `SetMaxOpenConns(5)`, `SetMaxIdleConns(2)`, `cache_size -8000`.
3. **`ubot/internal/modules/userbot/command_eval.go`** dan **`command_shell.go`**: nonaktifkan atau batasi `!eval` / `!sh` (Yaegi interpreter sangat boros RAM).
4. **Session management**: jangan load semua session ke RAM saat startup. Gunakan lazy-load + eviction setelah idle.
5. **Gateway shutdown**: `gateway/internal/app/shutdown.go` belum menutup Redis/DB — tambahkan saat shutdown.

## Catatan Arsitektur (SKILL.md)

Dokumen arsitektur lengkap ada di `.agents/skills/gotdbot-architecture/SKILL.md`. Prinsip utama:
- Optimize, Efisien, Easy Using, Microservice Purity.
- Batas file: maksimal 400 baris, 10-15 fungsi per file.
- Three-layer separation: Handler → Service → Store.
- Interface-driven design.

## Peringatan Keamanan

- `gateway/internal/app/app.go:60-64` memiliki `API_ID` dan `API_HASH` hardcoded sebagai fallback. Hapus sebelum production.
- `command_eval.go` mengekspos `*mongo.Database`, `*redis.Client`, dan `*telegram.Client` ke kode Go arbitrer via Yaegi. Batasi hanya untuk sudo user atau nonaktifkan.
- Session blob disimpan di SQLite tanpa enkripsi at-rest.

## File yang Tidak Ada (Perlu Dibuat)

- `README.md` — dokumentasi pengguna (sudah dibuat oleh agent).
- `AGENTS.md` — petunjuk agent (file ini).
- `.golangci.yml` — konfigurasi lint (belum ada).
- Tidak ada `Makefile`.

## Branch & Git

- Branch default: `master` atau `main`.
- CI/CD: `.github/workflows/docker-publish.yml` build & push ke `ghcr.io/kenboo14/`.
- Jangan commit `.env`, file `.db`, binary, atau log.