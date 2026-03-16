@echo off
setlocal

set "GATEWAY_URL=%NORTHSTAR_IBKR_BASE_URL%"
if "%GATEWAY_URL%"=="" set "GATEWAY_URL=https://127.0.0.1:5002/v1/api"
set "CONFIG_FILE=config_ibkr_live.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_ibkr_live.example.json"
set "NORTHSTAR_BIN=northstar.exe"

set "ACCOUNT_ID=%NORTHSTAR_IBKR_ACCOUNT_ID%"
if not "%~1"=="" (
  set "ACCOUNT_ID=%~1"
)

echo =========================================================
echo   Northstar Canadian Equities (Interactive Brokers LIVE)
echo =========================================================
echo.
echo WARNING: THIS WILL EXECUTE REAL TRADES ON INTERACTIVE BROKERS
echo Make sure you have the IBKR Client Portal API Gateway running 
echo locally on port 5000: https://127.0.0.1:5002
echo.

if "%ACCOUNT_ID%"=="" (
  echo [ERROR] Set NORTHSTAR_IBKR_ACCOUNT_ID or pass the IBKR account ID as the first argument.
  exit /b 1
)

if not exist data\universe\us_companies.txt (
  echo [1/3] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/3] Running IBKR live readiness check...
powershell -ExecutionPolicy Bypass -File tools\check_ibkr_live_readiness.ps1 -GatewayUrl "%GATEWAY_URL%" -AccountId "%ACCOUNT_ID%" -Iterations 3 -DelaySeconds 2
if errorlevel 1 (
  echo.
  echo Live readiness check failed. Aborting live startup.
  exit /b 1
)
echo.

set CONFIRM_LIVE_TRADING=true
echo Enforcing CONFIRM_LIVE_TRADING lock...

echo [3/3] Starting Northstar with %CONFIG_FILE%...
if exist "%NORTHSTAR_BIN%" (
  echo Using release binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Release binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
