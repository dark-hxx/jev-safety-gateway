@echo off
chcp 65001 >nul
setlocal
set "PS=powershell"
where pwsh >nul 2>nul && set "PS=pwsh"
%PS% -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-local-test.ps1" %*
set "CODE=%ERRORLEVEL%"
endlocal & exit /b %CODE%
