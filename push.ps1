Write-Host "🔨 Building Gateway..." -ForegroundColor Cyan
docker build --platform linux/amd64 -t ghcr.io/kenboo14/gotdbot-gateway:latest ./gateway

Write-Host "🔨 Building Ubot..." -ForegroundColor Cyan
docker build --platform linux/amd64 -t ghcr.io/kenboo14/gotdbot-ubot:latest ./ubot

Write-Host "🚀 Pushing Gateway..." -ForegroundColor Green
docker push ghcr.io/kenboo14/gotdbot-gateway:latest

Write-Host "🚀 Pushing Ubot..." -ForegroundColor Green
docker push ghcr.io/kenboo14/gotdbot-ubot:latest

Write-Host "✅ Selesai! Tinggal jalankan 'docker compose pull && docker compose up -d' di VPS." -ForegroundColor Yellow