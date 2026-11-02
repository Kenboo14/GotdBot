# 🚀 Panduan Optimasi GotdBot: Skalabilitas Tinggi untuk Spek Rendah (Ratusan hingga Ribuan User)

Dokumen ini berisi panduan teknis, rekomendasi arsitektur, dan daftar rencana tindakan (*Action Plan / TODO*) untuk mengoptimalkan performa RAM dan CPU pada GotdBot agar mampu berjalan stabil di server spesifikasi rendah (VPS 1 vCPU / 1GB–2GB RAM).

---

## 📋 Daftar Rencana Aksi (TODO List)

### 🟢 Tahap 1: Quick Wins (Efisiensi Cepat & Nol Risiko)
- [x] **1.1. Pembuatan Index MongoDB**
  - Compound index pada `userbot_sudo`, `userbot_broadcast_blacklist`, `userbot_sessions`
- [x] **1.2. Caching Owner ID (Eliminasi `client.Self(ctx)`)**
  - `clientOwners` map menyimpan ownerID saat client aktif
- [x] **1.3. In-Memory Cache untuk Pengecekan Sudo**
  - `sudoCache map[string]bool` dengan `sync.RWMutex`
- [x] **1.4. Pembersihan Memory Leak pada Inline Cache**
  - `inlineBots`, `clientOwners`, `senderCache` dihapus saat disconnect
- [x] **1.5. Tuning Runtime Environment Go**
  - `GOMEMLIMIT=750MiB`, `GOGC=50` pada `run-dev.ps1`

---

### 🟡 Tahap 2: Optimasi Beban Kerja & Throttling (CPU & Network)
- [x] **2.1. Concurrency Limiter untuk Operasi Berat (Worker Pool)**
  - Semaphore channel `broadcastSem` max 5 broadcast aktif bersamaan
- [x] **2.2. Optimasi Pengambilan Dialog pada Broadcast**
  - Micro-pacing 10ms per 50 dialog mencegah CPU spike
- [x] **2.3. Pengamanan Eksekusi Evaluator (`!eval` dan `!sh`)**
  - Limit buffer 50% lebih kecil, timeout 10 detik

---

### 🔴 Tahap 3: Manajemen Koneksi Skala Besar (Ratusan - Ribuan Sesi)
- [x] **3.1. Idle Connection Reaper (On-Demand Session)**
  - Background reaper setiap 1 menit, timeout configurable via `IDLE_TIMEOUT_MINUTES`
  - On-demand wake up saat ada instruksi dari Gateway/Redis
- [x] **3.2. Hard Cap Connection Pool (`MAX_ACTIVE_SESSIONS`)**
  - Batas maksimal sesi aktif bersamaan, default 100
  - Eviction policy: oldest-idle-first (sesi paling lama idle di-evict)

---

### 🟣 Tahap 4: Object Reuse & Allocation Elimination (Level Mentok)
- [x] **4.1. Cached `message.Sender` per Client (`getSender()`)**
  - Menghilangkan ~15-20 alokasi heap per command
- [x] **4.2. Singleton `sharedHTTPClient` untuk Bot API**
  - Transport pooling menghilangkan TLS handshake berulang
- [x] **4.3. Pre-computed Singleton Keyboard Markup**
  - `cachedHelpHomeMarkup` dan `cachedHelpBackMarkup` sekali buat
- [x] **4.4. Zero-Allocation Fast Bailout & Command Parser**
  - `strings.IndexByte` sub-slice scanning tanpa heap allocation
- [x] **4.5. Async Non-Blocking Callback Answer**
  - `AnswerCallbackQuery` di background goroutine

---

## 🛠️ Konfigurasi Environment Variables

| Variabel | Default | Deskripsi |
| :--- | :--- | :--- |
| `GOMEMLIMIT` | `750MiB` | Batas memori Go runtime. Set ke 70-80% RAM server. |
| `GOGC` | `50` | Agresivitas GC. Semakin kecil = GC lebih sering, heap lebih kecil. |
| `IDLE_TIMEOUT_MINUTES` | `30` | Menit idle sebelum sesi otomatis dimatikan reaper. Turunkan untuk VPS kecil. |
| `MAX_ACTIVE_SESSIONS` | `100` | Jumlah maksimum koneksi MTProto aktif bersamaan. Set rendah untuk VPS 512MB-1GB. |

### Rekomendasi per Spesifikasi Server

| Spek Server | `GOMEMLIMIT` | `GOGC` | `IDLE_TIMEOUT_MINUTES` | `MAX_ACTIVE_SESSIONS` |
| :--- | :--- | :--- | :--- | :--- |
| VPS 512MB RAM | `350MiB` | `30` | `10` | `30` |
| VPS 1GB RAM | `750MiB` | `50` | `20` | `80` |
| VPS 2GB RAM | `1500MiB` | `50` | `30` | `150` |
| VPS 4GB RAM | `3000MiB` | `80` | `60` | `500` |

---

## 📊 Estimasi Penghematan Resource

| Komponen | Sebelum Optimasi | Sesudah Optimasi | Efisiensi |
| :--- | :--- | :--- | :--- |
| **Pengecekan Pesan Masuk** | Full-scan MongoDB per pesan | In-memory cache hit (~0ms) | **Hemat CPU ~95%** |
| **Pemanggilan Command** | Network call `Self(ctx)` MTProto | Ambil dari memori lokal | **Latensi -100ms per perintah** |
| **Wrapper Sender per Command** | `NewSender(NewClient())` alokasi baru | Cached `getSender()` | **0 alokasi per command** |
| **HTTP ke Bot API** | TLS handshake baru tiap panggilan | Singleton pooled client | **-50ms per resolve** |
| **Kapasitas 1.000 User** | Butuh ~3.5GB–4GB RAM | ~350MB–600MB RAM (hard cap) | **Hemat RAM ~85%** |
| **Kapasitas 10.000 User** | Mustahil (~35GB RAM) | ~350MB–600MB RAM (hard cap) | **Hemat RAM ~99%** |
