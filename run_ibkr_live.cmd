@echo off
setlocal

echo =========================================================
echo   AegisTrade Canadian Equities (Interactive Brokers LIVE)
echo =========================================================
echo.
echo WARNING: THIS WILL EXECUTE REAL TRADES ON INTERACTIVE BROKERS
echo Make sure you have the IBKR Client Portal API Gateway running 
echo locally on port 5000: https://127.0.0.1:5002
echo.

if not exist data\universe\us_companies.txt (
  echo [1/3] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/3] Running IBKR live readiness check...
powershell -ExecutionPolicy Bypass -File tools\check_ibkr_live_readiness.ps1 -GatewayUrl https://127.0.0.1:5002/v1/api -AccountId DUP200062 -Iterations 3 -DelaySeconds 2
if errorlevel 1 (
  echo.
  echo Live readiness check failed. Aborting live startup.
  exit /b 1
)
echo.

set CONFIRM_LIVE_TRADING=true
echo Enforcing CONFIRM_LIVE_TRADING lock...

echo [3/3] Starting AegisTrade with config_ibkr_live.json...
go run main.go config_ibkr_live.json
