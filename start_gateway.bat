@echo off
pushd "%~dp0gateway"
if not exist gateway-bin.exe (echo Binary belum ada. Jalankan build.bat dahulu.& popd & exit /b 1)
gateway-bin.exe
set result=%errorlevel%
popd
exit /b %result%
