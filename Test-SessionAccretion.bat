@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Test-SessionAccretion.ps1" %*
exit /b %ERRORLEVEL%
