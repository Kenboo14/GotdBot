[CmdletBinding()]
param(
    [switch]$Detached
)

$ErrorActionPreference = 'Stop'

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'Docker Desktop belum tersedia. Instal, jalankan Docker Desktop, lalu buka ulang terminal VS Code.'
}

if (-not (Test-Path '.env')) {
    Copy-Item '.env.example' '.env'
    throw 'File .env dibuat. Isi BOT_TOKEN dari @BotFather, lalu jalankan kembali .\run.ps1.'
}

$token = (Get-Content '.env' | Where-Object { $_ -match '^BOT_TOKEN=' } | Select-Object -First 1) -replace '^BOT_TOKEN=', ''
if ([string]::IsNullOrWhiteSpace($token)) {
    throw 'BOT_TOKEN pada .env masih kosong.'
}

if ($Detached) {
    docker compose up --build --remove-orphans --detach
    docker compose ps
} else {
    docker compose up --build --remove-orphans
}
