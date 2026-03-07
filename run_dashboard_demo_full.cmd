@echo off
setlocal

echo =========================================================
echo   Live Dashboard Demo Full Startup
echo   ^(Backend + Frontend in separate windows^)
echo =========================================================
echo.

if not exist config.demo.json (
  echo [1/3] Creating config.demo.json from template...
  copy /Y config.demo.example.json config.demo.json >nul
) else (
  echo [1/3] Using existing config.demo.json
)

echo [2/3] Starting backend window...
start "Northstar Demo Backend" cmd /k "cd /d \"%~dp0\" && go run main.go config.demo.json"

echo [3/3] Starting frontend window...
start "Northstar Demo Frontend" cmd /k "cd /d \"%~dp0web\" && npm run dev"

echo.
echo Opening dashboard in browser...
timeout /t 3 >nul
start http://localhost:3000

echo.
echo Demo started. Close both spawned windows to stop.
