@echo off
setlocal

set "CONFIG_FILE=config_ibkr.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_ibkr.example.json"
set "NORTHSTAR_BIN=northstar.exe"
if not exist "%NORTHSTAR_BIN%" set "NORTHSTAR_BIN=nofx.exe"

echo =========================================================
echo   Northstar Canadian Equities (Interactive Brokers Paper)
echo =========================================================
echo.
echo Make sure you have the IBKR Client Portal API Gateway running 
echo locally on port 5000: https://127.0.0.1:5002
echo.
echo Note: this uses the standard tracked paper config.
echo If you want live-like strategy and sizing parity, use run_ibkr_paper_live.cmd
echo or run_ibkr_shadow.cmd instead.
echo.

if not exist data\universe\us_companies.txt (
  echo [1/2] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/2] Starting Northstar in Paper Trading mode...
echo.

echo Using config: %CONFIG_FILE%
if exist "%NORTHSTAR_BIN%" (
  echo Using binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
