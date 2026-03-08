@echo off
setlocal

echo =========================================================
echo   Northstar Canadian Equities (Interactive Brokers API) 
echo =========================================================
echo.
echo Make sure you have the IBKR Client Portal API Gateway running 
echo locally on port 5000: https://127.0.0.1:5002
echo.

if not exist data\universe\us_companies.txt (
  echo [1/2] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/2] Running Northstar with config_ibkr.json...
echo.

go run main.go config_ibkr.json
