@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Analyze-ShadowLogs.ps1" %*
exit /b %ERRORLEVEL%
