@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Test-SessionCapabilities.ps1" %*
exit /b %ERRORLEVEL%
