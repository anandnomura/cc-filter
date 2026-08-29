@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-LocalClaude.ps1" %*
exit /b %ERRORLEVEL%
