# AI Trading Engine (Go + React)

This repository runs automated trading agents with a Go backend and a React dashboard.
It supports multiple brokers/exchanges and both paper-style and live execution modes.

## What it does

- Runs one or more traders in parallel from a JSON config
- Supports AI-driven decisioning (`deepseek`, `qwen`, or custom OpenAI-compatible API)
- Supports crypto and equity workflows
- Supports fully local equity decisioning (`multi_factor`) and AI+factor ensemble filtering (`hybrid_ai`)
- Exposes REST + WebSocket APIs for monitoring
- Provides a web dashboard for account, positions, decisions, and performance

## Supported integrations

- Binance futures
- Hyperliquid
- Aster
- Alpaca (equities)
- Interactive Brokers Client Portal API (equities)

## Safety controls

- Live mode hard gate: `CONFIRM_LIVE_TRADING=true` is required for live traders
- Config-level limits for daily loss and drawdown
- Runtime stop windows after risk triggers

## Requirements

- Go 1.25+
- Node.js 18+
- npm
- Optional: Python 3 (for synthetic replay data generator)
- Optional: Docker (for IBKR gateway stack)

## Quick start

1. Install backend dependencies:

```bash
go mod download
```

2. Install frontend dependencies:

```bash
cd web
npm install
cd ..
```

3. Create your runtime config from `config.json.example` and fill your own keys/secrets.

4. Start backend:

```bash
go run main.go config.json
```

5. Start web dashboard (new terminal):

```bash
cd web
npm run dev
```

Backend default port: `8080`
Frontend default dev port: `3000`

## Common run scripts (Windows)

- Alpaca paper: `run_paper.cmd`
- Alpaca live: `run_live.cmd`
- Replay demo (synthetic local data): `run_replay.cmd`
- IBKR paper: `run_ibkr_paper.cmd`
- IBKR live: `run_ibkr_live.cmd`
- IBKR automated backtest matrix: `run_ibkr_backtest.cmd`
- Live dashboard demo (paper synthetic feed): `run_dashboard_demo.cmd`
- Live dashboard demo full startup (backend + frontend): `run_dashboard_demo_full.cmd`

## Equity strategy modes

For equity traders (`exchange=ibkr` or `exchange=alpaca`), `strategy_mode` supports:

- `ai_only`: pure AI decision flow
- `momentum_fallback`: AI first, local momentum opens if AI stays passive
- `momentum_only`: fully local momentum strategy (no AI API usage)
- `multi_factor`: fully local multi-factor strategy (trend, momentum, RSI, MACD, volume, ATR regime, cross-sectional strength, macro regime)
- `hybrid_ai`: AI decisions filtered and risk-shaped by local multi-factor logic before execution

Reference template: `ibkr_autonomous_template.json`

Key automation/risk fields:

- `max_gross_exposure`
- `max_position_pct`
- `max_concurrent_positions`
- `symbol_cooldown_cycles`
- `max_pair_correlation`
- `min_liquidity_usd`
- `min_decision_confidence`
- `execution_commission_bps`
- `execution_slippage_bps`
- `execution_impact_bps`
- `max_participation_rate`
- `drawdown_throttle_start`
- `drawdown_throttle_min_scale`
- `max_portfolio_heat_pct`
- `max_net_exposure_pct`
- `loss_streak_pause_threshold`
- `loss_streak_pause_cycles`
- `performance_risk_lookback`
- `volatility_brake_target_pct`
- `volatility_brake_lookback`
- `volatility_brake_min_scale`
- `kelly_fraction_cap`
- `kelly_lookback`
- `kelly_min_trades`
- `market_stress_entry_block`
- `market_stress_risk_min_scale`
- `use_news_risk`
- `enable_news_in_replay`
- `news_provider`
- `news_lookback_minutes`
- `news_refresh_seconds`
- `news_market_impact_thresh`
- `news_symbol_impact_thresh`
- `news_hard_block_thresh`
- `news_max_risk_reduction`
- adaptive symbol edge memory (learns from realized closes)
- `risk_per_trade_pct`
- `min_factor_score`
- `profit_lock_threshold_pct`
- `trailing_stop_atr_mult`
- `max_holding_cycles`
- `allow_short`
- `use_macro_filters`
- `regime_risk_scaling`
- `dynamic_position_sizing`
- `benchmark_symbols`

## Live dashboard demo mode

If you want a graph that updates all day without broker credentials or AI token usage, use demo mode.

- Template config: `config.demo.example.json`
- Local runtime config (auto-created): `config.demo.json`
- Status badge in UI: `LIVE DEMO (PAPER)`

Demo mode behavior:

- no live broker API calls
- no AI model API calls
- synthetic paper equity updates every cycle
- decision log and equity history continue to build for charts

Quick launch options:

- Backend only: `run_dashboard_demo.cmd`
- Backend + frontend windows + auto-open browser: `run_dashboard_demo_full.cmd`

## Automated IBKR backtesting

Use the built-in backtest command to:

- download historical bars from IBKR
- run multiple strategy profiles automatically
- generate ranked results (`leaderboard.json` and `leaderboard.csv`)
- score profiles by risk-adjusted quality (return, drawdown, sharpe, sortino, profit factor, win rate, trade activity, fee penalty)
- add robustness scoring from first-half vs second-half return consistency
- add Monte Carlo bootstrap robustness (`mc_p05_return_pct`, `mc_p50_return_pct`, `mc_positive_rate_pct`) from closed trades
- include trade diversification metrics (`traded_symbols`, `trade_hhi`, `diversification_score`) in ranking
- include stability metrics (`ulcer_index_pct`, `segment_stability_score`) in ranking
- include tail-risk metrics (`calmar_ratio`, `cvar95_pct`, `tail_ratio`, `return_per_fee`) in ranking
- enforce minimum-trade threshold in ranking to reduce overfit winners
- compare local and AI-assisted strategy modes (`multi_factor`, `momentum_only`, `momentum_fallback`, `hybrid_ai`)
- include realistic execution assumptions (commission + slippage) in simulated fills

Quick run (Windows):

```bash
run_ibkr_backtest.cmd
```

Direct command:

```bash
go run ./cmd/ibkr-backtest -account-id DUP200062
```

Auto parameter grid search:

```bash
go run ./cmd/ibkr-backtest -account-id DUP200062 -auto-grid -strategy-grid multi_factor,momentum_only,momentum_fallback -score-grid 0.30,0.35,0.45,1.25 -position-grid 0.06,0.08,0.10
```

Auto-grid with best-profile output:

```bash
go run ./cmd/ibkr-backtest -account-id DUP200062 -auto-grid -write-best-profile best_profile.json
```

90-cycle local multi-factor sweep example:

```bash
go run ./cmd/ibkr-backtest -account-id DUP200062 -profiles multi_factor:0.35:0.08,multi_factor:0.45:0.10,momentum_only:1.25:0.10 -max-cycles 90
```

Artifacts are written under:

- `output/ibkr_backtests/run_YYYYMMDD_HHMMSS/leaderboard.json`
- `output/ibkr_backtests/run_YYYYMMDD_HHMMSS/leaderboard.csv`
- `output/ibkr_backtests/run_YYYYMMDD_HHMMSS/profiles/<profile>/...`

## Build and test

Backend:

```bash
go build ./...
go test ./...
```

Frontend:

```bash
cd web
npm run build
```

## API endpoints

Health and realtime:

- `GET /health`
- `GET /ws`

Core REST:

- `GET /api/traders`
- `GET /api/competition`
- `GET /api/status?trader_id=...`
- `GET /api/account?trader_id=...`
- `GET /api/positions?trader_id=...`
- `GET /api/decisions?trader_id=...`
- `GET /api/decisions/latest?trader_id=...`
- `GET /api/statistics?trader_id=...`
- `GET /api/equity-history?trader_id=...`
- `GET /api/performance?trader_id=...`
- `GET /api/candles?trader_id=...&symbol=...`

## Project layout

```text
api/          HTTP + WebSocket server
broker/       Broker-specific client helpers
config/       Config structs, defaults, validation
decision/     AI decision engine
logger/       Decision and performance logging
manager/      Multi-trader lifecycle manager
market/       Market data providers
mcp/          AI model client wrappers
pool/         Symbol pool utilities
trader/       Exchange/broker trader implementations
tools/        Utility scripts
scripts/      Runtime helpers (start/stop/firewall)
web/          React dashboard
```

## Security notes

- Do not commit real API keys, passwords, cookies, or account IDs.
- Keep local credential files in `.gitignore`.
- Use paper/sim mode before any live deployment.

## License

MIT
