@echo off
setlocal
where go >nul 2>nul || (echo Go belum tersedia. Tutup lalu buka ulang terminal VS Code.& exit /b 1)

pushd "%~dp0gateway"
go build -o gateway-bin.exe ./main
if errorlevel 1 (popd & exit /b 1)
popd

pushd "%~dp0ubot"
go build -o ubot-bin.exe ./main
if errorlevel 1 (popd & exit /b 1)
popd

echo Build Windows selesai.
