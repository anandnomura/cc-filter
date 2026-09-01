@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Test-AgentGrant.ps1" %*
exit /b %ERRORLEVEL%
