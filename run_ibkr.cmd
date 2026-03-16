@echo off
setlocal

set "CONFIG_FILE=config_ibkr.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_ibkr.example.json"
set "NORTHSTAR_BIN=northstar.exe"

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

echo [2/2] Running Northstar with %CONFIG_FILE%...
echo.

if exist "%NORTHSTAR_BIN%" (
  echo Using release binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Release binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
