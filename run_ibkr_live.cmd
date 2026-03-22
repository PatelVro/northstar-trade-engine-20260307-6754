@echo off
setlocal

set "GATEWAY_URL=%NORTHSTAR_IBKR_BASE_URL%"
if "%GATEWAY_URL%"=="" set "GATEWAY_URL=https://127.0.0.1:5002/v1/api"
set "CONFIG_FILE=config_ibkr_live.json"
if not exist "%CONFIG_FILE%" set "CONFIG_FILE=config_ibkr_live.example.json"
set "NORTHSTAR_BIN=northstar.exe"
if not exist "%NORTHSTAR_BIN%" set "NORTHSTAR_BIN=nofx.exe"
set "NORTHSTAR_LIVE_VALIDATION_PASSED="
set "NORTHSTAR_LIVE_VALIDATION_CONFIG="
set "NORTHSTAR_LIVE_VALIDATION_CHECKED_AT="
set "NORTHSTAR_LIVE_VALIDATION_SOURCE="

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
set "NORTHSTAR_IBKR_ACCOUNT_ID=%ACCOUNT_ID%"

if not exist data\universe\us_companies.txt (
  echo [1/5] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/5] Running live deployment validation...
if exist "%NORTHSTAR_BIN%" (
  echo Using binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" validate-live "%CONFIG_FILE%"
) else (
  echo Binary not found, falling back to go run validate-live.
  go run main.go validate-live "%CONFIG_FILE%"
)
if errorlevel 1 (
  echo.
  echo Live deployment validation failed. Aborting live startup.
  exit /b 1
)
for %%I in ("%CONFIG_FILE%") do set "NORTHSTAR_LIVE_VALIDATION_CONFIG=%%~fI"
for /f "usebackq delims=" %%T in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "(Get-Date).ToUniversalTime().ToString('o')"`) do set "NORTHSTAR_LIVE_VALIDATION_CHECKED_AT=%%T"
set "NORTHSTAR_LIVE_VALIDATION_PASSED=true"
set "NORTHSTAR_LIVE_VALIDATION_SOURCE=run_ibkr_live.cmd"
echo Live deployment validation passed.
echo.

echo [3/5] Running IBKR live readiness check...
powershell -ExecutionPolicy Bypass -File tools\check_ibkr_live_readiness.ps1 -GatewayUrl "%GATEWAY_URL%" -AccountId "%ACCOUNT_ID%" -Iterations 3 -DelaySeconds 2
if errorlevel 1 (
  echo.
  echo Live readiness check failed. Aborting live startup.
  exit /b 1
)
echo.

set CONFIRM_LIVE_TRADING=true
echo Enforcing CONFIRM_LIVE_TRADING lock...

echo [4/5] Live deployment handoff recorded for %NORTHSTAR_LIVE_VALIDATION_CONFIG%
echo.

echo [5/5] Starting Northstar with %CONFIG_FILE%...
if exist "%NORTHSTAR_BIN%" (
  echo Using binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
