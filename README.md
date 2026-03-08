# AI Trading Engine (Go + React)

This repository runs automated trading agents with a Go backend and a React dashboard.
It supports multiple brokers/exchanges and both paper-style and live execution modes.

## What it does

- Runs one or more traders in parallel from a JSON config
- Supports AI-driven decisioning (`deepseek`, `qwen`, or custom OpenAI-compatible API)
- Supports crypto and equity workflows
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

Quick run (Windows):

```bash
run_ibkr_backtest.cmd
```

Direct command:

```bash
go run ./cmd/ibkr-backtest -account-id DUP200062
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
