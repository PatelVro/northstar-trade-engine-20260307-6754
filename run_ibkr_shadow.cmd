@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "GATEWAY_URL=%NORTHSTAR_IBKR_BASE_URL%"
if "%GATEWAY_URL%"=="" set "GATEWAY_URL=https://127.0.0.1:5002/v1/api"
set "LIVE_CONFIG=config_ibkr_live.json"
if not exist "%LIVE_CONFIG%" set "LIVE_CONFIG=config_ibkr_live.example.json"
set "CONFIG_FILE=config_ibkr_shadow.json"
set "CONFIG_SOURCE=local"
if not exist "%CONFIG_FILE%" (
  if exist "config_ibkr_shadow.openai.json" (
    if not "%NORTHSTAR_CUSTOM_API_KEY%"=="" (
      set "CONFIG_FILE=config_ibkr_shadow.openai.json"
      set "CONFIG_SOURCE=openai"
    ) else if not "%OPENAI_API_KEY%"=="" (
      set "CONFIG_FILE=config_ibkr_shadow.openai.json"
      set "CONFIG_SOURCE=openai"
    ) else if not "%NORTHSTAR_DEEPSEEK_API_KEY%"=="" (
      set "CONFIG_FILE=config_ibkr_shadow.example.json"
      set "CONFIG_SOURCE=deepseek_example"
    ) else (
      set "CONFIG_FILE=config_ibkr_shadow.openai.json"
      set "CONFIG_SOURCE=openai_missing_key"
    )
  ) else (
    set "CONFIG_FILE=config_ibkr_shadow.example.json"
    set "CONFIG_SOURCE=deepseek_example"
  )
)
set "NORTHSTAR_BIN=northstar.exe"
if not exist "%NORTHSTAR_BIN%" set "NORTHSTAR_BIN=nofx.exe"

set "ACCOUNT_ID=%NORTHSTAR_IBKR_ACCOUNT_ID%"
if not "%~1"=="" (
  set "ACCOUNT_ID=%~1"
)

echo =========================================================
echo   Northstar IBKR Shadow ^(Live-Like, No Real Orders^)
echo =========================================================
echo.
echo This mode uses real-time IBKR market data and the normal strategy
echo pipeline, but records hypothetical trades only.
echo.
echo Selected config: %CONFIG_FILE%
if /I "%CONFIG_SOURCE%"=="openai" echo Using OpenAI-compatible custom model shadow config.
if /I "%CONFIG_SOURCE%"=="deepseek_example" echo Using DeepSeek shadow example config.
if /I "%CONFIG_SOURCE%"=="openai_missing_key" echo Using OpenAI-compatible shadow config, but a custom API key must be available in env.
echo.

if not exist data\universe\us_companies.txt (
  echo [1/4] Building US company universe...
  powershell -ExecutionPolicy Bypass -File tools\gen_us_equity_universe.ps1
  echo.
)

echo [2/5] Resolving IBKR runtime context...
for /f "usebackq tokens=1,* delims==" %%A in (`powershell -ExecutionPolicy Bypass -File tools\resolve_ibkr_runtime_context.ps1 -GatewayUrl "%GATEWAY_URL%"`) do (
  if /I "%%A"=="ACCOUNT_ID" set "ACCOUNT_ID=%%B"
  if /I "%%A"=="SESSION_COOKIE" set "NORTHSTAR_IBKR_SESSION_COOKIE=%%B"
)
if "%ACCOUNT_ID%"=="" (
  echo [ERROR] Unable to resolve IBKR account ID from the local gateway.
  exit /b 1
)
set "NORTHSTAR_IBKR_ACCOUNT_ID=%ACCOUNT_ID%"
echo Resolved paper account: %ACCOUNT_ID%
echo.

if /I "%CONFIG_FILE%"=="config_ibkr_shadow.openai.json" (
  if "!NORTHSTAR_CUSTOM_API_KEY!"=="" (
    if not "!OPENAI_API_KEY!"=="" (
      set "NORTHSTAR_CUSTOM_API_KEY=!OPENAI_API_KEY!"
    )
  )
  if "!NORTHSTAR_CUSTOM_API_URL!"=="" set "NORTHSTAR_CUSTOM_API_URL=https://api.openai.com/v1"
  if "!NORTHSTAR_CUSTOM_MODEL_NAME!"=="" set "NORTHSTAR_CUSTOM_MODEL_NAME=gpt-4o"
  if "!NORTHSTAR_CUSTOM_API_KEY!"=="" (
    echo [ERROR] config_ibkr_shadow.openai.json requires NORTHSTAR_CUSTOM_API_KEY or OPENAI_API_KEY.
    exit /b 1
  )
)

echo [3/5] Checking live/parity config drift...
if /I "%CONFIG_FILE%"=="config_ibkr_shadow.openai.json" (
  echo [PARITY WARN] OpenAI-compatible shadow config is not strict live parity. Skipping baseline drift check against %LIVE_CONFIG%.
) else (
  powershell -ExecutionPolicy Bypass -File tools\check_mode_parity.ps1 -BaselineConfig "%LIVE_CONFIG%" -CandidateConfig "%CONFIG_FILE%"
  if errorlevel 1 (
    echo.
    echo Config parity check failed. Aborting shadow startup.
    exit /b 1
  )
)
echo.

echo [4/5] Running IBKR readiness check...
powershell -ExecutionPolicy Bypass -File tools\check_ibkr_live_readiness.ps1 -GatewayUrl "%GATEWAY_URL%" -AccountId "%ACCOUNT_ID%" -Iterations 3 -DelaySeconds 2
if errorlevel 1 (
  echo.
  echo IBKR readiness check failed. Aborting shadow startup.
  exit /b 1
)
echo.

echo [5/5] Starting Northstar with %CONFIG_FILE%...
if exist "%NORTHSTAR_BIN%" (
  echo Using binary: %NORTHSTAR_BIN%
  "%NORTHSTAR_BIN%" "%CONFIG_FILE%"
) else (
  echo Binary not found, falling back to go run.
  go run main.go "%CONFIG_FILE%"
)
