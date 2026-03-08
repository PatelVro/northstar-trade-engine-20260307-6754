@echo off
setlocal

echo =========================================================
echo   Northstar Automated IBKR Backtest Matrix
echo =========================================================
echo.

set "GATEWAY_URL=https://127.0.0.1:5002/v1/api"
set "ACCOUNT_ID=DUP200062"

if not "%~1"=="" (
  set "ACCOUNT_ID=%~1"
)

echo Gateway: %GATEWAY_URL%
echo Account: %ACCOUNT_ID%
echo.
echo Running automated strategy matrix on IBKR historical data...
echo.

go run ./cmd/ibkr-backtest ^
  -gateway-url "%GATEWAY_URL%" ^
  -account-id "%ACCOUNT_ID%" ^
  -symbols-file "data/universe/us_canada_tradable_core.txt" ^
  -max-symbols 20 ^
  -bar-interval 1h ^
  -bar-limit 1000 ^
  -max-cycles 240 ^
  -replay-warmup-bars 120 ^
  -candidate-batch-size 20 ^
  -profiles "momentum_only:1.10:0.05,momentum_only:1.25:0.10,momentum_only:1.40:0.10,momentum_fallback:1.25:0.10"

echo.
echo Done. Check output\ibkr_backtests\run_* for leaderboard and per-profile artifacts.
