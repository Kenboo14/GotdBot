[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go belum tersedia di PATH. Instal Go atau buka ulang terminal VS Code.'
}

if (-not (Test-Path '.env')) {
    throw '.env belum ada. Buat dari .env.example lalu isi kredensial Telegram.'
}

# Load simple KEY=VALUE entries from .env into this PowerShell process.
Get-Content '.env' | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#') -and $line -match '^([^=]+)=(.*)$') {
        [Environment]::SetEnvironmentVariable($Matches[1].Trim(), $Matches[2].Trim(), 'Process')
    }
}

$env:MONGO_URI = if ($env:MONGO_URI) { $env:MONGO_URI } else { 'mongodb://localhost:27017' }
$env:REDIS_ADDR = if ($env:REDIS_ADDR) { $env:REDIS_ADDR } else { 'localhost:6379' }

Write-Host 'Starting MongoDB and Redis development dependencies...'
$dockerPath = (Get-Command docker -ErrorAction SilentlyContinue).Source
if (-not $dockerPath) {
    $knownDocker = 'C:\Users\ubotr\AppData\Local\Programs\DockerDesktop\resources\bin\docker.exe'
    if (Test-Path $knownDocker) {
        $dockerPath = $knownDocker
    }
}
if ($dockerPath) {
    $dependencies = @('redis')
    if (-not $env:MONGO_URI.StartsWith('mongodb+srv://', [System.StringComparison]::OrdinalIgnoreCase)) {
        $dependencies += 'mongo'
    }
    & $dockerPath compose -f docker-compose.yaml -f docker-compose.dev.yaml up -d $dependencies
} else {
    Write-Warning 'Docker tidak ditemukan. Pastikan MongoDB dan Redis lokal sudah berjalan.'
}

Write-Host 'Starting gateway and ubot in separate PowerShell windows...'
Start-Process powershell -ArgumentList '-NoExit', '-Command', "Set-Location '$PWD\gateway'; go run ./main"
Start-Process powershell -ArgumentList '-NoExit', '-Command', "Set-Location '$PWD\ubot'; go run ./main"

Write-Host 'Development services started. Tutup jendela gateway/ubot untuk menghentikan aplikasinya.'
