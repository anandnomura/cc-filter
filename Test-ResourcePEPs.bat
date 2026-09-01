@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Test-ResourcePEPs.ps1" %*
exit /b %ERRORLEVEL%
