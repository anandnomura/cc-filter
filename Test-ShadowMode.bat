@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Test-ShadowMode.ps1" %*
exit /b %ERRORLEVEL%
