@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0copy-admin-token.ps1"
set "exitcode=%errorlevel%"
if not "%exitcode%"=="0" (
  echo.
  echo Could not copy the Admin Token. Exit code %exitcode%.
  pause
)
exit /b %exitcode%
