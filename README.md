# GotdBot — Instalasi, Setup, dan Menjalankan

Gateway Telegram Bot API dan worker userbot berbasis `github.com/gotd/td`.
Gateway menangani `/create`, inline help, callback, dan Redis. Ubot menangani
sesi akun Telegram, `!help`, `!ping`, `!p`, `!code`, `!eval`, dan `!sh`.

Setiap pengguna memiliki satu sesi userbot. Sesi disimpan sebagai dokumen
terpisah di MongoDB dan dienkripsi AES-256-GCM.

## Prasyarat

Development Windows membutuhkan Docker Desktop, WSL2, Go 1.25+, Git, dan bot
Telegram dari [@BotFather](https://t.me/BotFather). API ID dan API hash diambil
dari <https://my.telegram.org/apps>.

| Lingkungan | Kebutuhan |
|---|---|
| Windows development | Windows 10/11, Docker Desktop + WSL2, Go 1.25.5+, Git |
| Linux development | Linux, Go 1.25.5+, Docker Engine/Compose, Git |
| VPS production | Ubuntu 22.04/24.04, minimal 2 CPU/2 GB RAM, Docker Engine/Compose |

Semua lingkungan membutuhkan bot Telegram dari [@BotFather](https://t.me/BotFather)
dan API ID/API hash dari <https://my.telegram.org/apps>.

> Gunakan hanya satu instance Gateway untuk satu `BOT_TOKEN`; dua instance akan
> menyebabkan `409 Conflict` saat polling Telegram.

### Apakah perlu `venv`?

Tidak. GotdBot adalah aplikasi Go, bukan aplikasi Python. Pada production VPS,
Gateway, Ubot, MongoDB, dan Redis berjalan di container Docker sehingga dependency
tidak bercampur dengan program lain di host. Go dependency juga dikelola oleh
`go.mod` masing-masing module (`gateway` dan `ubot`), bukan instalasi global.

Python virtual environment hanya diperlukan untuk tool Python terpisah yang tidak
termasuk dalam runtime GotdBot.

## Konfigurasi `.env`

Salin `.env.example`:

```powershell
Copy-Item .env.example .env
```

Isi `.env`:

```dotenv
API_ID=12345678
API_HASH=isi_api_hash
BOT_TOKEN=token_dari_botfather
MONGO_URI=mongodb+srv://USER:PASSWORD@cluster.mongodb.net/?retryWrites=true&w=majority
MONGO_DB=gotdbot
REDIS_PASS=ganti_password_redis
SESSION_ENCRYPTION_KEY=base64_32_byte_key
```

Buat key sesi sekali saja:

```powershell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))
```

Untuk MongoDB lokal Compose, gunakan `MONGO_URI=mongodb://mongo:27017`. Untuk
Atlas, tambahkan IP komputer/VPS pada **Network Access**. Jangan commit `.env`
atau membagikan URI database. Jangan mengganti `SESSION_ENCRYPTION_KEY` setelah
sesi dibuat karena sesi lama tidak dapat didekripsi.

## Development di Terminal VS Code

Mode ini menjalankan Gateway dan Ubot langsung dengan `go run`; Docker hanya
menjalankan dependency seperti Redis dan MongoDB lokal bila dipakai.

### Perbedaan mode Dev dan Production

| Mode | Gateway/Ubot | MongoDB/Redis | Penggunaan |
|---|---|---|---|
| Development | Berjalan langsung dari source dengan `go run` | Berjalan di Docker | Pengembangan fitur dan debugging |
| Production | Berjalan sebagai container Docker | Berjalan sebagai container Docker | VPS/deployment |

Pada mode development, perubahan kode cukup dilakukan dengan restart proses Go.
Pada mode production, perubahan kode membutuhkan rebuild image dengan
`docker compose up -d --build`.

### Memberi nama project Docker Compose

Jika ingin nama container dan network lebih mudah dikenali, gunakan opsi `-p`.
Gunakan nama project yang sama untuk perintah berikutnya:

```powershell
# Production
docker compose -p gotdbot up -d --build --remove-orphans
docker compose -p gotdbot ps
docker compose -p gotdbot logs -f gateway
docker compose -p gotdbot down
```

Untuk dependency mode development:

```powershell
docker compose -p gotdbot-dev -f docker-compose.yaml -f docker-compose.dev.yaml up -d mongo redis
docker compose -p gotdbot-dev ps
docker compose -p gotdbot-dev down --remove-orphans
```

Opsi `-p` hanya mengatur nama project Compose; image, source code, dan volume
tidak berubah. Jika memakai `run.ps1` atau `run-dev.ps1`, script menggunakan
nama project default berdasarkan nama folder repository.

```powershell
powershell -ExecutionPolicy Bypass -File .\run-dev.ps1
```

Script membuka jendela PowerShell Gateway dan Ubot. Jangan menjalankannya dua
kali karena dapat membuat dua polling Telegram.

Periksa status:

```powershell
docker ps
Get-Process go -ErrorAction SilentlyContinue
Test-NetConnection localhost -Port 6379
```

Jika `MONGO_URI` memakai `mongodb+srv`, sesi disimpan ke Atlas. MongoDB lokal
yang masih terlihat di `docker ps` boleh dibiarkan, tetapi tidak dipakai untuk
sesi.

Setelah mengubah kode, restart proses Go saja:

```powershell
Stop-Process -Name go -Force
powershell -ExecutionPolicy Bypass -File .\run-dev.ps1
```

### Linux development

```bash
sudo apt update
sudo apt install -y git golang docker.io docker-compose-plugin
sudo usermod -aG docker "$USER"
newgrp docker
cp .env.example .env
nano .env
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml up -d mongo redis

# Terminal 1
cd gateway && go run ./main

# Terminal 2
cd ubot && go run ./main
```

Untuk menghentikan dependency development:

```bash
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml stop mongo redis
```

Untuk menghentikan sekaligus menghapus seluruh container dan network yang dibuat
oleh mode development, tanpa menghapus volume/data database:

```bash
docker compose -f docker-compose.yaml -f docker-compose.dev.yaml down --remove-orphans
```

Perintah ini aman dijalankan sebelum berpindah dari mode development ke
production. Jangan menambahkan `--volumes` jika sesi userbot dan data MongoDB
lokal masih ingin dipertahankan.

### Development di VPS Linux

`run-dev.ps1` hanya untuk Windows. Di VPS Ubuntu, jalankan dependency dan
aplikasi secara manual pada terminal terpisah.

Terminal 1 — MongoDB dan Redis:

```bash
cd ~/GotdBot
docker compose -p gotdbot-dev \
  -f docker-compose.yaml \
  -f docker-compose.dev.yaml \
  up -d mongo redis
```

Terminal 2 — Gateway:

```bash
cd ~/GotdBot/gateway
go run ./main
```

Terminal 3 — Ubot:

```bash
cd ~/GotdBot/ubot
go run ./main
```

Cek dependency development:

```bash
docker compose -p gotdbot-dev \
  -f docker-compose.yaml \
  -f docker-compose.dev.yaml \
  ps
```

Hentikan dependency development:

```bash
docker compose -p gotdbot-dev \
  -f docker-compose.yaml \
  -f docker-compose.dev.yaml \
  down --remove-orphans
```

Mode ini cocok untuk debugging sementara. Untuk aplikasi VPS yang berjalan
24/7, gunakan mode production Docker dengan project name `gotdbot`.

## Production di VPS

Ikuti langkah berikut setelah masuk ke VPS melalui SSH.

### 1. Periksa kebutuhan

Jalankan perintah satu per satu:

```bash
docker --version
docker compose version
git --version
curl --version
```

Jika semuanya menampilkan versi, lewati langkah instalasi dan lanjut ke langkah
deploy. Jika ada `command not found`, pasang komponen yang belum tersedia.

### 2. Instal Docker jika belum tersedia

Jalankan blok ini hanya jika `docker --version` gagal:

```bash
sudo apt update
sudo apt install -y ca-certificates curl git
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"
```

Keluar dari SSH lalu masuk kembali, atau jalankan:

```bash
newgrp docker
```

### 3. Instal Docker Compose jika belum tersedia

Jalankan ini hanya jika `docker compose version` gagal:

```bash
sudo apt update
sudo apt install -y docker-compose-plugin
```

Verifikasi ulang:

```bash
docker --version
docker compose version
```

### 4. Clone dan konfigurasi GotdBot

```bash
git clone https://github.com/Kenboo14/GotdBot.git
cd GotdBot
cp .env.example .env
nano .env
```

Isi `API_ID`, `API_HASH`, `BOT_TOKEN`, `MONGO_URI`, `REDIS_PASS`, dan
`SESSION_ENCRYPTION_KEY`, lalu simpan file `.env`.

### 5. Build dan jalankan production

```bash
docker compose -p gotdbot up -d --build --remove-orphans
docker compose -p gotdbot ps
```

Pastikan port keluar HTTPS (443) dan DNS VPS berfungsi. MongoDB Atlas harus
memasukkan IP VPS pada **Network Access**. Jika menggunakan MongoDB lokal dari
Compose, biarkan `MONGO_URI=mongodb://mongo:27017`; volume Docker menyimpan data
meskipun container direcreate.

Jalankan setelah reboot VPS:

```bash
cd GotdBot
docker compose -p gotdbot up -d
docker compose -p gotdbot ps
```

Pada Windows, alternatifnya:

```powershell
powershell -ExecutionPolicy Bypass -File .\run.ps1 -Detached
```

`run.ps1` membangun image dan menjalankan seluruh service di background.
Untuk mode foreground, jalankan `powershell -ExecutionPolicy Bypass -File .\run.ps1`.

Log dan update:

```bash
docker compose -p gotdbot logs -f gateway
docker compose -p gotdbot logs -f ubot
git pull
docker compose -p gotdbot up -d --build --remove-orphans
```

Restart/stop:

```bash
docker compose -p gotdbot restart gateway ubot
docker compose -p gotdbot stop gateway ubot
docker compose -p gotdbot start gateway ubot
docker compose -p gotdbot down
```

Jangan gunakan `docker compose -p gotdbot down --volumes` kecuali memang ingin menghapus
data lokal secara permanen.

## Memasang userbot

1. Kirim `/create` kepada bot Gateway.
2. Tekan **Kirim Kontak** atau masukkan nomor internasional manual.
3. Kirim OTP Telegram.
4. Kirim password 2FA jika diminta.

Bot hanya menerima contact milik pengguna yang memasang sesi. Setelah contact
dikirim, keyboard berubah menjadi tombol **Cancel** saja. Jika sesi sudah aktif,
`/create` ditolak. User A dan User B disimpan sebagai dokumen MongoDB berbeda.

## Command userbot

Default prefix adalah `!`:

```text
!help
!ping
!p
!server
!code
!eval fmt.Println(m.Chat.ID)
!sh pwd
!setp .
!gcast -gc -bl Halo grup
!gcast cancel
!addbl -gc
!rmbl -gc
!rmallbl
!lsbl
!addsudo 123456789
!rmsudo 123456789
```

`!setp none` menghapus prefix sehingga command menjadi `help`, `p`, dan `eval`.
Prefix disimpan di MongoDB dan dipulihkan setelah restart.

Eval menggunakan Yaegi, bukan Python atau JavaScript:

```text
!eval fmt.Println(m.Text)
!eval fmt.Println(m.Chat.ID)
!eval m.Reply("Halo")
```

Variabel Eval: `u`, `c`, `m`, `replit`, `db`, dan `r`.

Broadcast mendukung target `-gc` (grup), `-pc` (private), `-ch` (channel), dan
`-bl` untuk mengecualikan target yang ada di blacklist. Perintah blacklist
berlaku per akun userbot dan disimpan di MongoDB. Hanya pemilik userbot yang
dapat menambah/menghapus sudo; pengguna sudo dapat menjalankan command yang
diizinkan, termasuk broadcast.

## Troubleshooting

Docker tidak terdeteksi:

```powershell
docker version
docker compose version
wsl --status
```

Script PowerShell diblokir:

```powershell
powershell -ExecutionPolicy Bypass -File .\run-dev.ps1
```

Jika bot tidak merespons atau muncul `409 Conflict`, pastikan hanya satu
Gateway melakukan polling untuk `BOT_TOKEN` tersebut.

Jika Atlas gagal tersambung, periksa username/password, IP allowlist, URI
`mongodb+srv`, dan `MONGO_DB`. Sesi lokal tidak otomatis berpindah ke Atlas;
gunakan migrasi atau login ulang.

Periksa Redis:

```bash
docker compose -p gotdbot ps redis
docker compose -p gotdbot logs --tail=100 redis
```

Jika ingin melihat seluruh status service:

```bash
docker compose -p gotdbot ps
docker compose -p gotdbot logs --tail=100 gateway ubot
```

Jika source berubah tetapi container memakai image lama, rebuild:

```bash
docker compose -p gotdbot up -d --build --remove-orphans
```

## Keamanan

- Jangan commit `.env`, token bot, API hash, password database, atau session key.
- Rotasi `BOT_TOKEN` melalui BotFather jika pernah bocor.
- Gunakan password MongoDB dan Redis yang kuat.
- Eval dapat menjalankan kode Go dengan hak proses Ubot; gunakan hanya untuk
  pengguna yang dipercaya.
