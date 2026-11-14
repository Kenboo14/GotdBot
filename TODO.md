# TODO — Optimasi RAM & CPU GotdBot (VPS 1GB / 2 CPU)

Target: ~300-400 user aktif (lazy-load session), tidak mencoba menjalankan 1000 session MTProto sekaligus di RAM 1GB.

## Prioritas 1 — Segera dilakukan (dampak RAM terbesar)

### 1. `.env` / `docker-compose.yaml` — DONE
- [x] `GOMEMLIMIT=700MiB` (dari 1100MiB)
- [x] `GOGC=30` (dari 50)
- [x] `MAX_ACTIVE_SESSIONS=300` (dari 550)
- [x] `SESSION_HEARTBEAT_SECONDS=60` (dari 30)
- [x] `MONGO_BACKUP_URI=` dikosongkan

### 2. `ubot/internal/databases/sqlite.go` — DONE
- [x] `SetMaxOpenConns(5)` (dari 50)
- [x] `SetMaxIdleConns(2)` (dari 25)
- [x] `SetConnMaxLifetime(30 * time.Minute)` (dari 1 jam)
- [x] `PRAGMA cache_size = -8000;` (dari -64000)

### 3. `gateway/internal/databases/sqlite.go` — DONE
- [x] `SetMaxOpenConns(5)` (dari 50)
- [x] `SetMaxIdleConns(2)` (dari 25)
- [x] `SetConnMaxLifetime(30 * time.Minute)` (dari 1 jam)
- [x] `PRAGMA cache_size = -8000;` (dari -64000)

## Prioritas 2 — Keamanan + RAM

### 4. `ubot/internal/modules/userbot/command_eval.go` — DONE
- [x] `!eval` dinonaktifkan (return error "Fitur !eval dinonaktifkan untuk menghemat RAM.")
- [x] Impor `message` dan `styling` yang tidak digunakan dihapus

### 5. `ubot/internal/modules/userbot/command_shell.go` — DONE
- [x] `!sh` dinonaktifkan (return error "Fitur !sh dinonaktifkan untuk menghemat RAM.")

### 6. `gateway/internal/app/app.go` — DONE
- [x] Hardcoded `API_ID=25033414` dan `API_HASH="c0b6f8537df9fa559e65a14a8e51a100"` dihapus
- [x] Sekarang fatal jika `API_ID` atau `API_HASH` kosong

## Prioritas 3 — Session Management (kritis untuk skala 1000 user)

### 7. `ubot/internal/modules/userbot/userbot_service.go` — DONE
- [x] `Restore()` tidak lagi memanggil `s.activate()` untuk semua session saat startup
- [x] Hanya memuat metadata (prefix, owner) ke memori, status diatur ke `StateStopped`
- [x] Session aktif dihidupkan on-demand via `HandleCommand` (sudah ada logika wake-up)
- [x] `time.Sleep(30ms)` per session di startup dihapus (anti-CPU-starvation tidak diperlukan lagi)

## Prioritas 4 — Shutdown bersih

### 8. `gateway/internal/app/shutdown.go` — DONE
- [x] Ditambahkan parameter `redis *broker.Redis` dan `dbManager *databases.DataManager`
- [x] Menutup Redis client saat shutdown
- [x] Menutup DataManager (SQLite + MongoDB) saat shutdown

### 9. `gateway/internal/app/app.go` — DONE
- [x] `WaitShutdown(ctx)` diubah menjadi `WaitShutdown(ctx, redis, dbManager)`

### 10. `ubot/internal/app/shutdown.go` — DONE
- [x] Ditambahkan parameter `redisClient *databases.Redis`
- [x] Menutup Redis client saat shutdown

### 11. `ubot/internal/app/app.go` — DONE
- [x] `WaitShutdown(ctx, sqliteClient, rawMongoClient)` diubah menjadi `WaitShutdown(ctx, sqliteClient, rawMongoClient, redisClient)`

### 12. `gateway/internal/databases/manager.go` — DONE
- [x] Ditambahkan method `Close()` pada `DataManager` untuk menutup SQLite + MongoDB

### 13. `gateway/internal/databases/sqlite.go` — DONE
- [x] Ditambahkan method `Close()` pada `SQLiteClient`

## Prioritas 5 — Verifikasi

### 14. Build & Test — DONE
- [x] `cd gateway && go build ./...` — LULUS
- [x] `cd ubot && go build ./...` — LULUS
- [x] `cd gateway && go vet ./...` — LULUS
- [x] `cd ubot && go vet ./...` — LULUS
- [x] `cd gateway && go test ./...` — LULUS
- [x] `cd ubot && go test ./...` — LULUS

## Catatan
- Semua file sudah diubah dan divalidasi melalui `go build`, `go vet`, dan `go test`.
- 1000 user di 1GB RAM hanya mungkin dengan model lazy-load session; tidak semua session bisa aktif sekaligus di memori.
- `!eval` dan `!sh` dinonaktifkan untuk menghemat RAM (Yaegi interpreter sangat boros).