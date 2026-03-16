@echo off
setlocal

set "CONFIG_FILE=config_replay.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_replay.example.json"
set "NORTHSTAR_BIN=northstar.exe"

echo =========================================================
echo   Northstar Synthetic Replay Demo (Zero API Cost)
echo =========================================================
echo.

echo [1/3] Generating Synthetic Market Data (AAPL, MSFT, NVDA)...
python tools\gen_synthetic_data.py

echo.
echo [2/3] Checking Configuration...
if not exist data\csv\AAPL.csv (
    echo [ERROR] Synthetic data generation failed.
    exit /b 1
) else (
    echo [OK] Synthetic data found.
)

echo.
echo [3/3] Running Northstar in Replay Mode...
echo (The system will run a simulated trading loop using the local CSV data)
echo (Press Ctrl+C to stop the demo after a few cycles)
echo.

if exist "%NORTHSTAR_BIN%" (
  echo Using release binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Release binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
