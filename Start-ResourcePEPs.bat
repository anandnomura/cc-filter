@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-ResourcePEPs.ps1" %*
exit /b %ERRORLEVEL%
