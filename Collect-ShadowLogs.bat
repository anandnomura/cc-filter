@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Collect-ShadowLogs.ps1" %*
exit /b %ERRORLEVEL%
