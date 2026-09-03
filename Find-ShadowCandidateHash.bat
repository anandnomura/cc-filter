@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File %~dp0Find-ShadowCandidateHash.ps1 %*
exit /b %ERRORLEVEL%
