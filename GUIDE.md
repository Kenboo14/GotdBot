# 🚀 Panduan Optimasi GotdBot: Skalabilitas Tinggi untuk Spek Rendah (Ratusan hingga Ribuan User)

Dokumen ini berisi panduan teknis, rekomendasi arsitektur, dan daftar rencana tindakan (*Action Plan / TODO*) untuk mengoptimalkan performa RAM dan CPU pada GotdBot agar mampu berjalan stabil di server spesifikasi rendah (misalnya VPS 1 vCPU / 1GB–2GB RAM).

---

## 📋 Daftar Rencana Aksi (TODO List)

### 🟢 Tahap 1: Quick Wins (Efisiensi Cepat & Nol Risiko)
- [ ] **1.1. Pembuatan Index MongoDB**
  - Buat compound index pada collection utama untuk mencegah *full collection scan*:
    - `userbot_sudo`: `{ owner_id: 1, user_id: 1 }` (Unik)
    - `userbot_broadcast_blacklist`: `{ owner_id: 1, type: 1, chat_id: 1 }` (Unik)
    - `userbot_sessions`: `{ account_id: 1 }`, `{ owner_id: 1 }`
- [ ] **1.2. Caching Owner ID (Eliminasi `client.Self(ctx)`)**
  - Simpan `ownerID` / `accountID` secara lokal di struct memory sesi saat akun di-restore/diaktifkan.
  - Hindari network round-trip MTProto `client.Self(ctx)` berulang pada setiap eksekusi command (`!gcast`, `!sudo`, `!addbl`, dll.).
- [ ] **1.3. In-Memory Cache untuk Pengecekan Sudo**
  - Implementasikan in-memory map/sync.Map (atau cache ber-TTL) untuk daftar user sudo per-owner.
  - Menghilangkan query MongoDB setiap kali ada pesan masuk yang diproses oleh userbot.
- [ ] **1.4. Pembersihan Memory Leak pada Inline Cache (`inlineBots`)**
  - Hapus pointer `*telegram.Client` yang tersimpan di `inlineBots` saat client offline/dihapus, atau gunakan identifier tunggal.
- [ ] **1.5. Tuning Runtime Environment Go**
  - Pasang environment variable pada container/systemd service:
    ```bash
    GOMEMLIMIT=750MiB  # Sesuaikan dengan 70-80% kapasitas RAM server
    GOGC=50            # Garbage collector lebih agresif membersihkan heap
    ```

---

### 🟡 Tahap 2: Optimasi Beban Kerja & Throttling (CPU & Network)
- [ ] **2.1. Concurrency Limiter untuk Operasi Berat (Worker Pool)**
  - Batasi jumlah broadcast (`!gcast`) simultan yang berjalan di satu instance (misal: maksimal 5–10 job aktif bersamaan menggunakan worker pool / channel semaphore).
- [ ] **2.2. Optimasi Pengambilan Dialog pada Broadcast**
  - Batasi penarikan dialog MTProto (`GetDialogs`) agar tidak menarik data berlebihan sekaligus ke memory.
  - Buat jeda mikro (*micro-pacing*) saat iterasi fetch dialog untuk mencegah lonjakan CPU 100%.
- [ ] **2.3. Pengamanan Eksekusi Evaluator (`!eval` dan `!sh`)**
  - Terapkan limit alokasi memori dan pembatasan timeout ketat (10–15 detik) untuk proses eval Yaegi dan shell command agar tidak menyedot CPU thread server.

---

### 🔴 Tahap 3: Manajemen Koneksi Skala Besar (Ratusan - Ribuan Sesi)
- [ ] **3.1. Idle Connection Reaper (On-Demand Session)**
  - Masalah utama ribuan sesi: 1 userbot = ~1.5–4 MB RAM koneksi TCP/TLS persisten. 1.000 user = ~2GB–4GB RAM.
  - Buat mekanisme pemutus koneksi jika userbot tidak memiliki aktivitas listening/command selama waktu tertentu (misal: 30 menit).
  - Sambungkan kembali secara dinamis (*lazy connect*) ketika ada instruksi dari Gateway/Redis.
- [ ] **3.2. Penyesuaian Update Handler Dispatcher**
  - Hanya aktifkan update dispatcher jika akun memang dikonfigurasi untuk merespons pesan teks/perintah secara aktif. Matikan update handler jika akun hanya digunakan sebagai worker kirim.

---

## 🛠️ Panduan Penerapan Teknis

### 1. Inisialisasi Index MongoDB
Tambahkan inisialisasi index otomatis pada file [`ubot/applications/modules/userbot/userbot_module.go`](file:///c:/Users/ubotr/OneDrive/Desktop/Gotd/GotdBot/ubot/applications/modules/userbot/userbot_module.go) atau saat database pertama kali terhubung:

```go
func ensureIndexes(ctx context.Context, db *mongo.Database) error {
    // Index userbot_sudo
    _, err := db.Collection("userbot_sudo").Indexes().CreateOne(ctx, mongo.IndexModel{
        Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "user_id", Value: 1}},
        Options: options.Index().SetUnique(true),
    })
    if err != nil {
        return err
    }

    // Index userbot_broadcast_blacklist
    _, err = db.Collection("userbot_broadcast_blacklist").Indexes().CreateOne(ctx, mongo.IndexModel{
        Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "type", Value: 1}, {Key: "chat_id", Value: 1}},
        Options: options.Index().SetUnique(true),
    })
    if err != nil {
        return err
    }

    // Index userbot_sessions
    _, err = db.Collection("userbot_sessions").Indexes().CreateOne(ctx, mongo.IndexModel{
        Keys: bson.D{{Key: "account_id", Value: 1}},
    })
    return err
}
```

### 2. Cache Sudo & Owner ID di Struct Service
Ubah struct `Service` di [`ubot/applications/modules/userbot/userbot_service.go`](file:///c:/Users/ubotr/OneDrive/Desktop/Gotd/GotdBot/ubot/applications/modules/userbot/userbot_service.go):

```go
type Service struct {
    // ... field lainnya ...
    clientOwners map[*telegram.Client]int64       // Cache Owner ID per client
    sudoCache    map[string]bool                  // Cache "ownerID:userID" -> bool
    sudoMu       sync.RWMutex
}
```

Implementasikan pembacaan cepat sebelum query ke Mongo:
```go
func (s *Service) isSudoCached(ctx context.Context, ownerID, userID int64) bool {
    key := fmt.Sprintf("%d:%d", ownerID, userID)
    s.sudoMu.RLock()
    allowed, found := s.sudoCache[key]
    s.sudoMu.RUnlock()
    if found {
        return allowed
    }

    // Fallback ke Mongo jika belum ada di cache
    allowed = s.isSudo(ctx, ownerID, userID)
    s.sudoMu.Lock()
    s.sudoCache[key] = allowed
    s.sudoMu.Unlock()
    return allowed
}
```

---

## 📊 Estimasi Penghematan Resource

| Komponen | Sebelum Optimasi | Sesudah Optimasi | Efisiensi |
| :--- | :--- | :--- | :--- |
| **Pengecekan Pesan Masuk** | Full-scan MongoDB per pesan | In-memory cache hit (~0ms) | **Hemat CPU ~80%** |
| **Pemanggilan Command** | Network call `Self(ctx)` MTProto | Ambil dari memori lokal | **Latensi -100ms per perintah** |
| **Kapasitas 1.000 User** | Butuh ~3.5GB–4GB RAM | Butuh ~400MB–800MB RAM (dengan idle reaper) | **Hemat RAM ~75%** |

