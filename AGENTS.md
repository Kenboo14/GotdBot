# AGENTS.md - GotdBot

Petunjuk untuk agent AI yang bekerja pada repository GotdBot.

## Ringkasan Proyek

GotdBot adalah sistem userbot Telegram dengan dua mikroservis Go:
- **Gateway** (`assterisk/gateway`) — kontrol panel, Bot API, MTProto, manajemen token & sesi.
- **Ubot** (`assterisk/ubot`) — engine MTProto userbot, session management, command execution.

Komunikasi antar layanan via **Redis Streams**. Database primary **SQLite**, backup opsional **MongoDB**.

## Peran Komponen & Pilar Fitur

### 1. Tiga Komponen Utama
- **Bot (Main / Panel Bot — `gateway`)**: Kontrol panel UI (`/start`, login sesi, token/lisensi, pengaturan panel Jaseb) serta penyedia menu/tombol inline untuk userbot.
- **Userbot (`ubot`)**: Akun Telegram pribadi milik user yang berjalan via MTProto (`gotd/td`) untuk mengeksekusi siaran Jaseb dan command asisten ber-prefix (`!`).
- **LogBot (`ubot`)**: Bot khusus laporan progres/notifikasi Jaseb sekaligus penerus & *reply router* log pesan masuk (`!log`) ke owner.

### 2. Dua Pilar Fitur
- **Fitur Jaseb (Panel Otomatis)**: Pengaturan list teks/media, grup/topik, jeda, timer, dan mode dilakukan melalui **Bot**, dieksekusi terjadwal oleh **Userbot** (`panel_broadcast.go`), dan laporannya dikirim melalui **LogBot**.
- **Fitur Personal / Asisten Userbot (Command `!`)**: Dijalankan langsung oleh user/sudo di chat via **Userbot** (`session_updates.go`) seperti notes (`!save`, `!get`, `#shortcut`, `!cat`, `!notes`), `!ping`/`!p`, `!server`, `!help` (inline via **Bot**), `!gcast`/`!broadcast`, blacklist (`!addbl`/`!rmbl`/`!lsbl`), `!addsudo`, dan `!log`.

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

## Efisiensi Token & Aturan Multi-Agent (Strict / On-Demand)

Untuk menekan biaya token secara maksimal:

1. **Default — Chat Biasa (Tanpa `/create`)**:
   - **DILARANG** memanggil subagent (`auditor`, `developer`, `qa`).
   - **Tanya jawab / diskusi / cek cepat**: Jawab **langsung** dari memori konteks percakapan dan ringkasan `AGENTS.md` tanpa membaca ulang file.
   - **Edit kode biasa / perbaikan terarah**: Baca **hanya** file dan rentang baris (`StartLine`–`EndLine`) yang dituju, lalu edit langsung dan verifikasi secukupnya.
2. **Mode Multi-Agent — HANYA saat `/create <deskripsi>` (atau diminta eksplisit)**:
   - Baca & ikuti skill `.agents/skills/create/SKILL.md` (`auditor` → `developer` → `qa`).
   - Orchestrator **wajib** menyertakan path file absolut dan rentang baris spesifik pada prompt subagent agar subagent tidak memindai ulang seluruh project.
   - Prioritaskan `send_message` ke sesi subagent yang sudah aktif bila masih dalam konteks pekerjaan yang sama.

## Status Optimasi Terkini (Baseline Memori)

Perubahan optimasi & hardening yang sudah diterapkan (tidak perlu diaudit/diterapkan ulang):
- **`ubot` `!eval` & `!sh`**: Sudah dinonaktifkan pada `command_eval.go` dan `command_shell.go` untuk menghemat RAM.
- **Redis Stream PEL Recovery & `XAck`**: Kedua modul (`gateway/internal/broker/redis.go`, `ubot/internal/databases/redis.go`) sudah memakai pagination cursor `lastPendingID`, 16-worker bounded pool, dan 3x retry `XAck`.
- **`PeerStorage` (`gateway/internal/botgotd/peers.go`)**: Sudah memiliki fast-path in-memory deduplication (`RLock`) dan bounded SQLite write (`writeSem` kapasitas 4).
- **`DataManager.SaveUserConfig` (`gateway/internal/databases/manager.go`)**: Sudah memakai deep copy `cloneJasebConfig`, `mongoSaveSem` (kapasitas 8), dan per-owner coalescing (`pendingMongoCfg` / `inFlightMongo`).
- **Userbot Lifecycle & Heartbeat (`ubot/internal/modules/userbot/`)**:
  - `session_commands.go`: Perbaikan deadlock `s.mu` lewat `findActiveClientLocked` + context-aware wait.
  - `userbot_service.go`: Staggered startup (`Restore()`), bounded `syncPeersOnReady` (`peerSyncSem` kapasitas 4), dan exponential backoff + jitter + `tgerr.AsFloodWait` saat reconnect.
  - `session_heartbeat.go` & `panel_broadcast.go`: Delta filter + interval 10 menit pada `syncSessionStates`, lock splitting `s.mu` vs `s.sudoMu` di `reapStaleResources()`, eviction `panelMsgCache` (TTL 5m), dan `pruneSyncMap` untuk `userAccessHashes`, `channelAccessHashes`, serta `albumCache`.

## Optimasi RAM (Fokus Utama)

Target: VPS 1GB RAM / 2 CPU, ~300-400 user aktif.

1. **`.env`**: `GOMEMLIMIT=700MiB`, `GOGC=30`, `MAX_ACTIVE_SESSIONS=300`, `SESSION_HEARTBEAT_SECONDS=60`, `MONGO_BACKUP_URI=` (kosongkan jika tidak perlu).
2. **`internal/databases/sqlite.go`** (kedua modul): ubah `SetMaxOpenConns(5)`, `SetMaxIdleConns(2)`, `cache_size -8000`.
3. **`ubot/internal/modules/userbot/command_eval.go`** dan **`command_shell.go`**: sudah dinonaktifkan (pertahankan tetap nonaktif di production).
4. **Session management**: gunakan lazy-load/admission limit + eviction cache idle (`reapStaleResources`).
5. **Gateway shutdown**: `gateway/internal/app/shutdown.go` perlu dipastikan menutup Redis/DB saat shutdown.

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