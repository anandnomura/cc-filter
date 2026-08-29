@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Initialize-Certificates.ps1" %*
exit /b %ERRORLEVEL%
