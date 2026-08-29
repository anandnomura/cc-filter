@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Demo-Bap.ps1" %*
exit /b %errorlevel%
