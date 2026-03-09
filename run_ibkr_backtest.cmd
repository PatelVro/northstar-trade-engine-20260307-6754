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
  -auto-grid ^
  -strategy-grid "multi_factor,momentum_only,momentum_fallback" ^
  -score-grid "0.30,0.35,0.45,0.55,1.25" ^
  -position-grid "0.06,0.08,0.10,0.12" ^
  -commission-bps 0.35 ^
  -slippage-bps 0.75 ^
  -execution-impact-bps 12 ^
  -max-participation-rate 0.15 ^
  -max-portfolio-heat-pct 0.035 ^
  -max-net-exposure-pct 0.65 ^
  -loss-streak-pause-threshold 3 ^
  -loss-streak-pause-cycles 5 ^
  -performance-risk-lookback 20 ^
  -volatility-brake-target-pct 0.008 ^
  -volatility-brake-lookback 40 ^
  -volatility-brake-min-scale 0.45 ^
  -kelly-fraction-cap 0.33 ^
  -kelly-lookback 30 ^
  -kelly-min-trades 10 ^
  -market-stress-entry-block 0.82 ^
  -market-stress-risk-min-scale 0.35 ^
  -use-news-risk false ^
  -enable-news-in-replay false ^
  -news-provider "rss" ^
  -news-lookback-minutes 240 ^
  -news-refresh-seconds 120 ^
  -news-market-impact-thresh 0.65 ^
  -news-symbol-impact-thresh 0.70 ^
  -news-hard-block-thresh 0.85 ^
  -news-max-risk-reduction 0.55 ^
  -min-trades-for-score 4 ^
  -min-traded-symbols 2 ^
  -mc-sims 300 ^
  -write-best-profile "best_profile.json"

echo.
echo Done. Check output\ibkr_backtests\run_* for leaderboard, per-profile artifacts, and best_profile.json.
