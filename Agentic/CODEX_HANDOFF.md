# CODEX_HANDOFF.md — GotdBot Architecture & Lifecycle Guide

## 1. Overview & Context

Dokumen ini mencatat keputusan arsitektur, refaktorisasi lifecycle MTProto GotdBot, arsitektur command handler/worker, serta hasil audit dan verifikasi final.

### Riwayat Insiden & Evaluasi Latar Belakang
Pada periode yang sama ketika mekanisme idle reaper dan eviction masih aktif, terjadi peringatan keamanan Telegram, reset sejumlah sesi/token, dan pembekuan bot (*bot frozen*). Pola reconnect berulang merupakan salah satu hal yang perlu dievaluasi, tetapi belum ada bukti yang memastikan bahwa mekanisme tersebut merupakan penyebab langsung insiden.

Evaluasi arsitektur menghasilkan keputusan strategis untuk beralih ke model **Persistent MTProto 24/7** dan membatasi Reaper hanya untuk pembersihan resource sementara.

---

## 2. Arsitektur MTProto, Command Handler, & Worker

### A. Persistent MTProto Client 24/7
- **Satu Akun, Satu Lifecycle**: Setiap akun Telegram terdaftar memiliki satu instance `telegram.Client` yang berjalan terus-menerus selama proses aplikasi aktif dan otorisasi sesi masih valid.
- **Konsistensi Identitas Akun & Generation Token**: Identitas sesi diikat pada `chatID` dan divalidasi dengan token generasi unik (`accountGens[chatID] = genID`). Tidak ada aktivasi ganda untuk sesi atau identitas yang sama.
- **Tanpa Idle Disconnect**: Tidak ada pemutusan koneksi berdasarkan waktu idle atau ketiadaan command.
- **Mekanisme Reconnect vs Fatal Error**:
  - *Gangguan Jaringan Sementara*: Mengandalkan auto-reconnect bawaan pustaka `gotd/td` yang menangani socket reconnect tanpa merusak session authorization.
  - *Otorisasi Dicabut/Tidak Valid*: Jika `client.Run` mengembalikan fatal error (misal `AUTH_KEY_UNREGISTERED` atau session revoked oleh user), lifecycle akun dihentikan secara terencana dan slot dilepaskan.
  - *Akun Dinonaktifkan Pemilik*: Hentikan lifecycle secara eksplisit melalui context cancellation.
  - *Shutdown Aplikasi*: Seluruh client dan worker dihentikan secara graceful melalui root context.

### B. Command Handler & On-Demand Worker
1. **Passive Command Handlers**:
   - Handler command tetap terdaftar di dispatcher tanpa membuat worker khusus yang menganggur.
   - Saat tidak ada command yang cocok, tidak ada pekerjaan command yang dijalankan.
   - Biaya registrasi handler dan routing dispatcher tetap ada di memori, tetapi relatif kecil dibandingkan membuat goroutine permanen.
2. **Eksekusi Sinkron Command Sederhana**:
   - Command sederhana seperti `!help` dan `!p` dieksekusi langsung di callback dispatcher.
   - **Deadline 5 Detik pada `!p`**: Context dengan deadline timeout 5 detik (`pingCtx`) diteruskan ke operasi `client.Ping(pingCtx)` **dan** pengiriman respons `sender.To(peerID).Text(pingCtx, ...)`. Ini merupakan pembatalan berbasis context batas waktu 5 detik (bukan jaminan hard real-time sistem operasi).
3. **On-Demand Worker untuk Pekerjaan Berat (`!gcast`)**:
   - Broadcast dijalankan melalui worker goroutine on-demand terpisah dari callback dispatcher.
   - Konkurensi broadcast dibatasi oleh semaphore `broadcastSem` (maksimal 5 pekerjaan paralel) untuk mencegah lonjakan CPU dan RAM.
   - Rilis semaphore dijamin idempotent (tepat 1 kali) menggunakan `sync.Once` di blok `defer`.
4. **Hierarki Konteks yang Terisolasi**:
   - `appCtx` (`s.root`): Konteks utama aplikasi.
   - `accountCtx` (`s.accountCtxs[chatID]`): Konteks lifecycle akun, diturunkan dari `appCtx`.
   - `jobCtx`: Konteks broadcast akun, diturunkan dari `accountCtx`.
   - **Aturan Pembatalan**:
     - Pembatalan job (`!gcast cancel`) hanya membatalkan `jobCtx` tanpa mengganggu koneksi akun atau command lain.
     - Penghentian akun (`accountCtx` canceled) secara otomatis membatalkan seluruh job broadcast akun tersebut.
     - Shutdown aplikasi (`appCtx` canceled) membatalkan seluruh lifecycle sistem.
5. **Generation-Safe Job Cleanup**:
   - State broadcast disimpan dalam bentuk pointer struct `broadcastJob{id, cancel}`.
   - Saat worker selesai, penghapusan dari map memverifikasi identitas pointer job (`if s.broadcasts[ownerID] == job`). Hal ini mencegah job A yang selesai terlambat menghapus state job B yang baru dimulai oleh owner yang sama.

---

## 3. Koreksi & Verifikasi Klaim Efisiensi Resource

> [!IMPORTANT] Klarifikasi Metrik Resource
> Klaim efisiensi harus dipisahkan secara tegas antara estimasi internal, klaim upstream, dan data benchmark riil.

| Angka / Sumber | Status Pengukuran | Keterangan |
| :--- | :--- | :--- |
| **1,5–2,5 MB / client** | Estimasi internal lama, **belum terukur** | Perkiraan awal proyek, belum diverifikasi secara empiris di server produksi. |
| **~150 KB / idle client** | Klaim upstream `gotd/td`, **bukan benchmark GotdBot** | Angka iklan pustaka gotd murni untuk koneksi TCP kosong, belum mencakup alokasi state aplikasi GotdBot. |
| **RSS / heap per client di VPS** | **Belum diketahui** | Memerlukan benchmark bertahap di lingkungan produksi VPS target. |

- **Catatan Teknis Profiling**: Endpoint pprof (`/debug/pprof/heap`) hanya mencatat alokasi heap runtime Go (`HeapAlloc`, `HeapInuse`), **bukan OS RSS**. Pengukuran OS RSS riil harus diambil dari sistem operasi (misalnya `/proc/<pid>/status` atau metrik cgroup Docker pada host Linux).

---

## 4. Kebijakan Concurrency, Lifecycle, & Reaper

### A. Transisi Atomik `connecting` ke `active`
- Status aktivasi dilacak menggunakan `s.connecting[chatID]` (uint64 genID) dan `s.active[chatID]`.
- Perpindahan status dari `connecting` ke `active` dilakukan di bawah lock `s.mu` yang sama:
  ```go
  s.mu.Lock()
  if s.connecting[chatID] == genID {
      delete(s.connecting, chatID)
  }
  if s.accountGens[chatID] == genID {
      s.active[chatID] = client
      // set owners ...
  }
  s.mu.Unlock()
  ```
  Ini menjamin tidak ada celah di mana akun tidak terhitung (under-counting) atau terhitung ganda (over-counting) dalam evaluasi admission limit.

### B. Admission Limit Policy (MAX_ACTIVE_SESSIONS)
- Mengubah fungsi `MAX_ACTIVE_SESSIONS` dari mekanisme *eviction* menjadi **Admission Limit**.
- Pendaftaran akun baru ditolak (`[PoolCap]`) jika `len(s.active) + len(s.connecting) >= maxActiveSessions`.
- Sesi aktif yang sedang berjalan **tidak pernah dikorbankan/diputus** demi memberikan slot bagi sesi baru.

### C. Generation-Safe Lifecycle Cleanup (Final Micro-Fix)
- Di dalam `defer` aktivasi client, pembersihan setiap map diverifikasi langsung terhadap token kepemilikan generasi dan pointer instance:
  ```go
  s.mu.Lock()
  // Ownership check: hanya bersihkan connecting jika token generasi masih cocok
  if s.connecting[chatID] == genID {
      delete(s.connecting, chatID)
  }
  // Ownership check: hanya bersihkan active jika client pointer masih cocok
  if s.active[chatID] == client {
      delete(s.active, chatID)
      delete(s.lastActive, chatID)
  }
  // Ownership check: hanya bersihkan cancel, context, dan gen jika generasi masih milik lifecycle ini
  if s.accountGens[chatID] == genID {
      delete(s.activeCancels, chatID)
      delete(s.accountCtxs, chatID)
      delete(s.accountGens, chatID)
  }
  if client != nil {
      delete(s.clientOwners, client)
      delete(s.senderCache, client)
  }
  s.mu.Unlock()
  ```
- **Jaminan Keamanan**: Setiap map diverifikasi secara independen terhadap objek/token miliknya (`genID` atau `client`). Lifecycle lama dipastikan tidak dapat menghapus context, cancel function, connecting marker, atau pointer client milik lifecycle baru.

### D. Single Central Maintenance Loop (Reaper) & sudoCache Final Micro-Fix
Reaper (`reapStaleResources`) berjalan sebagai **satu maintenance loop terpusat** untuk seluruh aplikasi setiap 2 menit:
1. **Login OTP Timeout**: Sesi pembuatan akun (`/create`) yang tidak diselesaikan user lebih dari 5 menit dibatalkan (`cancel()`) dan dihapus dari `s.logins`.
2. **Pembedaan Hasil Negatif vs Error Database Operasional pada `sudoCache`**:
   - **Record Ditemukan**: Cache `allowed = true` dengan TTL 30 menit.
   - **Record Tidak Ditemukan (`mongo.ErrNoDocuments`)**: Cache `allowed = false` dengan TTL 30 menit.
   - **Error Database Operasional (Timeout/Network/Connection Drop)**: **FAIL-CLOSED** tanpa mencache keputusan negatif. Error dicatat di log dan tidak mencemari cache selama 30 menit.
   - **Anti-Stale-Write Protection**: Saat thread selesai melakukan query MongoDB, penyimpanan cache memeriksa apakah ada thread lain yang telah memperbarui cache lebih dulu:
     ```go
     cur, exists := s.sudoCache[cacheKey]
     if !exists || time.Now().After(cur.expiresAt) {
         s.sudoCache[cacheKey] = sudoCacheEntry{
             allowed:   true,
             expiresAt: time.Now().Add(30 * time.Minute),
         }
     } else {
         return cur.allowed
     }
     ```
   - **Strict Expiration**: Lookup membaca cache dan langsung menganggap cache miss serta menghapus entri jika `time.Now().After(entry.expiresAt)`.
   - Reaper membersihkan sisa entri expired setiap 2 menit dan melakukan pemangkasan bertahap (maks 20%) jika ukuran melebihi 2.000 entri untuk mencegah *cache miss storm*.
3. **Proteksi Mutlak Client & Job**: Reaper **DILARANG KERAS** memanggil `cancel()` pada `activeCancels` atau menghapus entri dari `s.active` dan `s.broadcasts`.

---

## 5. Komponen Diagnostics: Safe Profiling (`pprof`)

- **File**: `ubot/applications/app/pprof.go`
- **Listener**: `127.0.0.1:6060` (Private `http.ServeMux`, bukan `http.DefaultServeMux`).
- **Akses Diagnostik dari Lingkungan Docker**:
  - Listener bind ke loopback container (`127.0.0.1`).
  - *Metode 1 (Praktis)*: Jalankan `curl 127.0.0.1:6060/debug/pprof/heap -o heap.pprof` dari dalam container, lalu salin keluar menggunakan `docker cp`.
  - *Metode 2 (Remote/Tunnel)*: Publish port container hanya ke `127.0.0.1:6060` host VPS pada file Docker Compose / run flag, kemudian akses melalui SSH tunnel (`ssh -L 6060:127.0.0.1:6060 user@vps`). Jangan membuka port pprof ke interface publik.

---

## 6. Hasil Verifikasi, Audit, & Pengujian Aktual

### A. Pengujian Unit Package Userbot (`go test -count=1`)
Lokasi: `ubot/applications/modules/userbot/lifecycle_test.go`
Perintah: `Set-Location -Path "ubot\applications\modules\userbot"; go test -count=1 -v -run "Test" .`
Hasil: **PASS (100% dari 10 unit test, fresh execution tanpa cache)**:
1. `TestReapStaleResources_KeepsActiveSessionsPersistent`
2. `TestDoubleActivationGuard`
3. `TestAdmissionLimit_NoEviction`
4. `TestConcurrentActivation_AtomicSingleWinner`
5. `TestReapStaleResources_ConcurrentWithSudoCache`
6. `TestBroadcastGenerationSafeCleanup`
7. `TestAccountCancellation_PropagatesToBroadcast`
8. `TestLifecycleOldGenerationCleanup_DoesNotDeleteNewGeneration` *(Baru)*
9. `TestSudoCache_ErrorHandlingAndStaleWrite` *(Baru)*
10. `TestPingContextDeadlineExceeded` *(Baru)*

### B. Pengujian Modul Penuh (`go test -count=1 ./...`)
Lokasi: Root modul `ubot`
Perintah: `go test -count=1 ./...`
Hasil: **PASS (Zero test failures across all packages, 0.272s)**.

### C. Pengujian Race Detector (`go test -race`)
Perintah: `go test -race ./...`
Hasil: **BELUM TERUJI — Terhalang Toolchain CGO/GCC**
- Keterangan Faktual: Lingkungan host Windows lokal tidak memiliki compiler C (`gcc: executable file not found in %PATH%`). Go race detector membutuhkan CGO. Pengujian ini wajib divalidasi pada pipeline CI/CD Linux dengan GCC.

### D. Static Analysis & Kompilasi Biner Modul
- `ubot`: `go vet ./...` & `go build ./...` -> **PASS** (Zero warnings / clean binary build).
- `gateway`: `go vet ./...` & `go build ./...` -> **PASS** (Zero warnings / clean binary build).

---

## 7. Status Verifikasi Simulator

- Direktori `ubot/simulator/` terverifikasi **TERHAPUS SEPENUHNYA** dari filesystem (`Test-Path: False`).
- Pemeriksaan codebase memastikan **TIDAK ADA** sisa dependensi, import, atau referensi produksi yang mengarah ke package simulator.

---

## 8. Ringkasan File yang Diubah / Ditambahkan

| File | Status | Keterangan |
| :--- | :--- | :--- |
| `ubot/applications/modules/userbot/userbot_service.go` | Modified | Persistent MTProto 24/7, Token Generasi Lifecycle `accountGens`, Generation-Safe Defer Cleanup, Ping 5s Context Deadline |
| `ubot/applications/modules/userbot/command_broadcast.go` | Modified | Generation-safe job cleanup, hierarki accountCtx, idempotent semaphore release |
| `ubot/applications/modules/userbot/command_sudo.go` | Modified | Pemisahan `mongo.ErrNoDocuments` vs Operational Error, Anti-Stale-Write Optimistic Check, Strict TTL Expiry |
| `ubot/applications/modules/userbot/lifecycle_test.go` | Added/Updated | 10 Unit tests untuk lifecycle generations, concurrency, sudoCache errors & stale-write, ping deadline |
| `ubot/applications/app/pprof.go` | Added | Internal pprof profiler server pada `127.0.0.1:6060` |
| `ubot/applications/app/app.go` | Modified | Inisialisasi background profiler |
| `ubot/simulator/` | Deleted | Dihapus sepenuhnya sesuai mandat isolasi produksi |
| `CODEX_HANDOFF.md` | Updated | Dokumentasi arsitektur final, perbaikan micro-fix, dan panduan teknis terperinci |

---

## 9. Matriks Kategori Verifikasi & Kesiapan Deployment

| Aspek / Komponen | Status Verifikasi | Keterangan |
| :--- | :--- | :--- |
| Persistent MTProto & No Idle Eviction | **Verified by code inspection & test** | MTProto stabil tanpa disconnect periodic. |
| Double Activation & Admission Control | **Verified by test** | `TestDoubleActivationGuard`, `TestConcurrentActivation_AtomicSingleWinner`, `TestAdmissionLimit_NoEviction`. |
| Lifecycle Generation Token Ownership | **Verified by test** | `TestLifecycleOldGenerationCleanup_DoesNotDeleteNewGeneration`. |
| sudoCache Negatives vs Operational Error | **Verified by test** | `TestSudoCache_ErrorHandlingAndStaleWrite` (ErrNoDocuments di-cache, transient error fail-closed). |
| sudoCache Anti-Stale-Write & Strict Expiry | **Verified by test** | `TestSudoCache_ErrorHandlingAndStaleWrite` (hasil query lambat tidak menimpa data baru). |
| Generation-Safe Broadcast & Account Cancel | **Verified by test** | `TestBroadcastGenerationSafeCleanup` & `TestAccountCancellation_PropagatesToBroadcast`. |
| Ping Context Deadline (5 Detik) | **Verified by test & inspection** | `TestPingContextDeadlineExceeded` & `pingCtx` diteruskan ke `Ping()` dan `sender.To().Text()`. |
| Memory & CPU per Client (2.000 Akun) | **Requires production benchmark** | Belum diketahui di lingkungan production; harus diukur bertahap di VPS target via pprof dan OS RSS. |
| Multi-core Race Safety | **Not yet verified (Need Linux CI)** | Terhalang ketiadaan toolchain GCC lokal Windows; wajib dijalankan via `go test -race ./...` di Linux CI. |
| **Status Kesiapan Deployment** | **READY FOR LINUX CI & STAGING BENCHMARK** | Kode siap dibangun ke image Docker dan diuji di lingkungan staging/Linux dengan compiler GCC. |
