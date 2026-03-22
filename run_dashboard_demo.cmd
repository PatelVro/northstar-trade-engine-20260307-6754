@echo off
setlocal
set "NORTHSTAR_BIN=northstar.exe"
if not exist "%NORTHSTAR_BIN%" set "NORTHSTAR_BIN=nofx.exe"

echo =========================================================
echo   Live Dashboard Demo ^(Paper Synthetic Feed^)
echo =========================================================
echo.

if not exist config.demo.json (
  echo [1/2] Creating config.demo.json from template...
  copy /Y config.demo.example.json config.demo.json >nul
) else (
  echo [1/2] Using existing config.demo.json
)

echo.
echo [2/2] Starting engine in demo paper mode...
echo Dashboard: http://localhost:3000
echo API:       http://localhost:8080
echo.
echo Note: Start frontend in another terminal:
echo       cd web ^&^& npm run dev

if exist "%NORTHSTAR_BIN%" (
  echo Using binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" config.demo.json
) else (
  echo Binary not found, falling back to go run.
  go run main.go config.demo.json
)
