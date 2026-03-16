@echo off
setlocal

set "CONFIG_FILE=config_live.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_live.example.json"
set "NORTHSTAR_BIN=northstar.exe"

echo =========================================================
echo   Northstar LIVE Trading Mode (Alpaca) 
echo =========================================================
echo.

echo [WARNING] YOU ARE ABOUT TO ENTER LIVE TRADING. REAL MONEY WILL BE USED.
echo Setting safety override: CONFIRM_LIVE_TRADING=true
set CONFIRM_LIVE_TRADING=true
echo.

echo Running Northstar with %CONFIG_FILE%...
echo.

if exist "%NORTHSTAR_BIN%" (
  echo Using release binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Release binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
