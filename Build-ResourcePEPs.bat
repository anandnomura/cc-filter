@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Build-ResourcePEPs.ps1" %*
exit /b %ERRORLEVEL%
