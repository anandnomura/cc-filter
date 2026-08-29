@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0View-AuditTrail.ps1" %*
exit /b %errorlevel%
