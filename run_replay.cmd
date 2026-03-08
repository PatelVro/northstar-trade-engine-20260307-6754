@echo off
setlocal

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

go run main.go config_replay.json
