@echo off
setlocal

echo =========================================================
echo   Northstar LIVE Trading Mode (Alpaca) 
echo =========================================================
echo.

echo [WARNING] YOU ARE ABOUT TO ENTER LIVE TRADING. REAL MONEY WILL BE USED.
echo Setting safety override: CONFIRM_LIVE_TRADING=true
set CONFIRM_LIVE_TRADING=true
echo.

echo Running Northstar with config_live.json...
echo.

go run main.go config_live.json
