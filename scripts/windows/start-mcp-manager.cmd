@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-mcp-manager.ps1"
set "exitcode=%errorlevel%"
if not "%exitcode%"=="0" (
  echo.
  echo MCP Manager launcher exited with code %exitcode%.
  pause
)
exit /b %exitcode%
