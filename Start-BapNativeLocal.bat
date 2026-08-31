@echo off
setlocal
pushd "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-BapNativeLocal.ps1" %*
set "BAP_EXIT_CODE=%ERRORLEVEL%"
popd
exit /b %BAP_EXIT_CODE%
