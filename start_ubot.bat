@echo off
pushd "%~dp0ubot"
if not exist ubot-bin.exe (echo Binary belum ada. Jalankan build.bat dahulu.& popd & exit /b 1)
ubot-bin.exe
set result=%errorlevel%
popd
exit /b %result%
