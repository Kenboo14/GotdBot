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
        $value = $Matches[2].Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        [Environment]::SetEnvironmentVariable($Matches[1].Trim(), $value, 'Process')
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

# Optimization settings for low-spec servers (RAM & CPU tuning)
$env:GOMEMLIMIT = if ($env:GOMEMLIMIT) { $env:GOMEMLIMIT } else { '750MiB' }
$env:GOGC = if ($env:GOGC) { $env:GOGC } else { '50' }
$env:IDLE_TIMEOUT_MINUTES = if ($env:IDLE_TIMEOUT_MINUTES) { $env:IDLE_TIMEOUT_MINUTES } else { '30' }
$env:MAX_ACTIVE_SESSIONS = if ($env:MAX_ACTIVE_SESSIONS) { $env:MAX_ACTIVE_SESSIONS } else { '100' }

Write-Host "Starting gateway and ubot in separate PowerShell windows (GOMEMLIMIT=$($env:GOMEMLIMIT), GOGC=$($env:GOGC), IDLE=$($env:IDLE_TIMEOUT_MINUTES)m, MAX_SESSIONS=$($env:MAX_ACTIVE_SESSIONS))..."
Start-Process powershell -ArgumentList '-NoExit', '-Command', "`$env:GOMEMLIMIT='$($env:GOMEMLIMIT)'; `$env:GOGC='$($env:GOGC)'; Set-Location '$PWD\gateway'; go run ./main"
Start-Process powershell -ArgumentList '-NoExit', '-Command', "`$env:GOMEMLIMIT='$($env:GOMEMLIMIT)'; `$env:GOGC='$($env:GOGC)'; `$env:IDLE_TIMEOUT_MINUTES='$($env:IDLE_TIMEOUT_MINUTES)'; `$env:MAX_ACTIVE_SESSIONS='$($env:MAX_ACTIVE_SESSIONS)'; Set-Location '$PWD\ubot'; go run ./main"

Write-Host 'Development services started. Tutup jendela gateway/ubot untuk menghentikan aplikasinya.'
