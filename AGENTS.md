# GotdBot Agent Guidelines & Architectural Rules

File ini adalah instruksi proyek permanen bagi seluruh AI Assistant yang bekerja di repositori **GotdBot**. Wajib dibaca sebelum melakukan analisis, refactoring, atau penambahan fitur.

---

## 1. Skill Arsitektur Utama
Detail lengkap arsitektur, flow diagram, dan dokumentasi mendalam tersimpan di:
👉 [`.agents/skills/gotdbot-architecture/SKILL.md`](.agents/skills/gotdbot-architecture/SKILL.md)

---

## 2. Prinsip & Aturan Mutlak Pengembangan

1. **Dual-Layer Messaging: MTProto (Userbot) vs Bot API (LogBot)**
   - Di Telegram Private Chat, ID pesan pada sisi Bot API (`sendMessage` result) **BERBEDA** dengan ID pesan pada sisi Userbot MTProto (`msg.ID`).
   - Sistem perutean balasan log **WAJIB** menyimpan dan mencari berdasarkan **MTProto Message ID** (menggunakan penangkapan `lastLogBotMsgID` di `interceptLogMessage` atau query `resolveLogMTProtoMsgID`), bukan Bot API ID.

2. **Keberadaan Database Rute (SQLite & MongoDB)**
   - **DILARANG** menghapus penyimpanan rute di SQLite/MongoDB dengan asumsi "cukup ambil dari tombol Buka Chat".
   - Alasan teknis: Saat pengguna men-swipe reply di Telegram, Telegram **TIDAK MENGIRIMKAN** data tombol pesan yang di-reply, dan Telegram MTProto **WAJIB** memiliki `AccessHash` untuk membalas ke pengguna lain (`InputPeerUser`).

3. **Format URL Deep Link**
   - Tombol "Buka Chat" untuk Private Message (PM) menggunakan: `tg://openmessage?user_id=%d&message_id=%d`.
   - Tombol untuk Grup/Supergroup menggunakan: `https://t.me/c/%d/%d`.
   - **DILARANG** memasang `tg://user?id=` pada URL inline button karena Telegram Bot API menolaknya (`BUTTON_URL_INVALID`). Gunakan format HTML `<a href="tg://user?id=...">` di teks pesan untuk link nama pengirim.

4. **Multi-Database Roles**
   - **SQLite** (`/app/data/userbot.db`): Cache rute lokal instan (< 1ms), penyimpanan sesi aktif terenkripsi, status log lokal.
   - **MongoDB Cloud**: Backup permanen dan Disaster Recovery saat startup (`Restore`). Menyimpan sesi, catatan, sudo, status log, dan riwayat rute.
   - **Redis**: Komunikasi IPC broker (`ubot` ↔ `gateway`) dan hot-cache inline catatan.

5. **Docker & Deployment**
   - Service `ubot` dan `gateway` di `docker-compose.yaml` memiliki blok `build: { context: ... }`.
   - Perubahan kode Go di VPS **HARUS** di-build ulang tanpa cache:
     ```bash
     git pull origin master && docker compose build --no-cache ubot && docker compose up -d ubot
     ```

6. **Verifikasi Kompilasi Sebelum Selesai**
   - Sebelum menyelesaikan turn, pastikan `cd ubot; go build -v ./...` dan unit test database `cd ubot; go test -v ./applications/databases/...` selalu lulus (exit code 0).

