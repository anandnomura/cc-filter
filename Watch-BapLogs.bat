@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Watch-BapLogs.ps1" %*
exit /b %errorlevel%
