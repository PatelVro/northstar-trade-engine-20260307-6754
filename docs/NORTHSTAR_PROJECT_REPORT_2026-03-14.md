# Northstar Project Report

Generated: 2026-03-14
Workspace: `C:\Users\Hill\Documents\nofx`

## Scope

This report is a direct codebase and runtime snapshot of the current project state. It covers:

- Product purpose and architecture
- Implemented trading, dashboard, and backtest capabilities
- Current IBKR paper configuration
- Validation results from local build/test runs
- Latest known runtime behavior from logs and decision records
- Major risks, inconsistencies, and cleanup still needed
- Practical readiness assessment for paper and live use

## Executive Summary

Northstar is a large Go + React automated trading platform with support for multiple brokers/exchanges and a strong current emphasis on IBKR equities. The project now includes:

- A multi-trader backend
- A React monitoring dashboard
- IBKR paper/live execution plumbing
- Local and AI-assisted equity strategies
- News-aware risk overlays
- Replay/backtest tooling with scoring and ranking
- A synthetic demo mode for continuous dashboard activity

The good news is that the platform is real software, not a mockup. The backend test suite passes, the frontend production build passes, IBKR-specific code paths exist, and there are historical decision logs plus backtest outputs showing the system has been exercised.

The hard truth is that it is not yet a trustworthy live-trading system. It is best described today as an ambitious automated research and paper-trading platform with meaningful progress, but still carrying several production blockers:

- account and P&L semantics are inconsistent
- the current paper config contains committed credentials
- rebranding is incomplete
- broker/runtime resilience is not strong enough yet
- latest runtime is down
- the available backtest evidence is too thin to justify confidence in real profitability

## Bottom Line

Current rating:

- Research/backtest platform: good
- Paper-trading platform: usable with supervision
- Unattended live deployment: not recommended yet

If the question is "does this project exist as a serious trading engine?" the answer is yes.
If the question is "is this ready to safely and reliably trade live capital by itself?" the answer is no.

## What The Project Is

At a high level, the repository is an automated trading system with three major layers.

### 1. Backend engine

The Go backend is the execution core. It:

- loads one or more traders from JSON config
- connects to broker/data providers
- builds market context
- requests AI decisions or applies local strategies
- executes orders
- logs decisions and performance
- serves REST and WebSocket APIs

Relevant source areas:

- `main.go`
- `manager/`
- `trader/`
- `broker/`
- `market/`
- `decision/`
- `logger/`
- `api/`

### 2. Dashboard

The React/Vite frontend provides:

- account overview
- positions display
- decision history
- equity charting
- AI learning/performance panels
- monitoring components for execution and strategy compliance

Relevant source areas:

- `web/src/components/`
- `web/src/lib/api.ts`
- `web/src/App.tsx`

### 3. Research and simulation tooling

The repo also includes:

- replay/demo mode
- IBKR historical data download
- multi-profile backtest runs
- scoring and leaderboard generation
- best-profile export

Relevant source areas:

- `cmd/ibkr-backtest/main.go`
- `output/ibkr_backtests/`
- `scripts/`

## Major Implemented Capabilities

### Trading modes and broker integrations

The repo currently advertises and wires support for:

- Binance futures
- Hyperliquid
- Aster
- Alpaca equities
- IBKR equities
- local demo/sim mode

The current serious implementation focus is clearly IBKR equities.

### Equity strategy modes

The current strategy menu includes:

- `ai_only`
- `momentum_fallback`
- `momentum_only`
- `multi_factor`
- `hybrid_ai`

This is meaningful. It gives the project both:

- pure local execution paths that do not depend on an AI API for every decision
- AI-assisted paths that can be filtered by local risk logic

### Risk and execution controls

The codebase now includes a sizable set of controls:

- max gross exposure
- max position percent
- max concurrent positions
- minimum decision confidence
- drawdown throttling
- loss-streak pauses
- volatility brake
- Kelly-style scaling
- market stress gating
- pair correlation limits
- liquidity thresholds
- participation-rate execution assumptions
- news-aware risk shaping

Not all of these are fully validated in production conditions, but they are implemented concepts, not just comments.

### IBKR-specific improvements present in code

IBKR support now includes:

- contract resolution
- live order polling
- order reply confirmation handling
- whole-share normalization
- position parsing from IBKR portfolio payloads
- protective stop-loss and take-profit submission using OCA groups

Evidence:

- `trader/ibkr_trader.go:841`
- `trader/ibkr_trader.go:877`

### News-aware risk layer

The equity engine includes a headline-driven risk layer with configurable thresholds and provider wiring. The code also contains logic for adaptive news credibility and per-symbol learning hooks.

Evidence:

- `trader/auto_trader.go:128`
- `trader/auto_trader.go:545`
- `trader/auto_trader.go:550`

Important caveat:

- The `runtime/news_learning/` directory exists, but it is empty in this snapshot. That means the persistence path is present, but there is no current evidence in this workspace of accumulated news-learning state on disk.

### Backtesting and ranking

The IBKR backtest command is fairly advanced for a local project. It includes:

- profile grids
- score ranking
- Monte Carlo metrics
- diversification scoring
- ulcer index
- segment stability
- CVaR and tail ratio
- best-profile export

Evidence:

- `cmd/ibkr-backtest/main.go:307`
- `cmd/ibkr-backtest/main.go:589`
- `cmd/ibkr-backtest/main.go:1493`
- `cmd/ibkr-backtest/main.go:1562`

## Current Configuration Snapshot

Primary trader config inspected: `config_ibkr.json`

Configured trader:

- id: `ibkr_paper_trader`
- name: `Hill Patel IBKR Paper`
- broker/data: `ibkr`
- mode: `paper`
- instrument type: `equity`
- strategy mode: `momentum_fallback`
- AI model: `deepseek`
- scan interval: 3 minutes
- candidate batch size: 12

Current risk settings:

- `max_position_pct = 0.03`
- `max_gross_exposure = 0.10`
- `max_concurrent_positions = 1`
- `risk_per_trade_pct = 0.0025`
- `max_daily_loss_pct = 0.01`
- `min_decision_confidence = 80`

Evidence:

- `config_ibkr.json:17`
- `config_ibkr.json:21`
- `config_ibkr.json:22`
- `config_ibkr.json:25`

Universe data:

- default broad equity universe file count: 7275 symbols
- trusted tradable core list count: 138 symbols

That means the project currently supports both:

- a broad discovery universe
- a narrower trusted trading universe

## Validation Performed For This Report

### Backend tests

Command run:

```powershell
go test ./...
```

Result:

- Passed

Notable note:

- the Go test traversal includes a strange path under `walmart-dashboard/node_modules/...`, which is a repo hygiene smell even though tests still pass

### Frontend build

Command run:

```powershell
npm run build
```

Result:

- Passed

Observations:

- production bundle builds successfully
- output JS bundle is large at about 793 kB minified
- Vite warns about large chunks

Interpretation:

- the dashboard is functional and buildable
- it would benefit from code splitting and bundle cleanup

## Runtime Snapshot On 2026-03-14

At report time:

- `http://127.0.0.1:8080` was not reachable
- there was no active `runtime/ibkr_paper.pid`
- direct broker API probe to `https://127.0.0.1:5002/v1/api/...` returned no active response

This means the system is not currently running right now.

### Latest known paper-trading activity

The latest available paper-trading decision artifact is:

- `decision_logs/ibkr_paper_trader/decision_20260312_091359_cycle372.json`

Latest known state from that file:

- logged at `2026-03-12 09:13:59 -04:00`
- 2 positions open at that moment: `MU` long and `SYK` short
- decision actions were `hold`, `hold`, `wait`

The latest error tail from `runtime/ibkr_paper.err.log` shows the IBKR endpoint became unreachable shortly after:

- `runtime/ibkr_paper.err.log:25991`
- `runtime/ibkr_paper.err.log:25992`

Those lines show connection refusal to:

- `https://127.0.0.1:5002/v1/api/iserver/account/orders`

So the last visible paper session ended in a broker connectivity failure state, not a clean, fully observed shutdown.

## Historical Runtime Evidence

This workspace contains substantial historical runtime traces:

- 2720 decision log files under `decision_logs/ibkr_paper_trader`
- IBKR paper runtime logs in `runtime/ibkr_paper.err.log` and `runtime/ibkr_paper.out.log`
- 4 backtest runs under `output/ibkr_backtests`

That matters because it proves this project has been run repeatedly and is not just configured on paper.

## Backtest Snapshot

Latest inspected backtest run:

- `output/ibkr_backtests/run_20260309_034249`

Leaderboard summary:

1. `multi_factor_s0p35_p0p08`
   - return: `+0.0576%`
   - sharpe: `1.9257`
   - max drawdown: `0.0868%`
   - total trades: `1`

2. `momentum_only_s1p25_p0p10`
   - return: `-0.0544%`
   - sharpe: `-3.2444`
   - max drawdown: `0.1002%`
   - total trades: `2`

Interpretation:

- the backtest framework itself works
- the evidence for edge is currently weak
- the best visible run is based on a very small sample size
- the current backtest results are not strong enough to support any claim that the system is consistently profitable

This is one of the most important conclusions in the whole report.

## Branding And English-Only Cleanup Status

### English-only UI status

The dashboard is currently hard-set to English:

- `web/src/contexts/LanguageContext.tsx:12`
- `web/src/contexts/LanguageContext.tsx:14`
- `web/src/i18n/translations.ts:1`

This is good. The user-facing translation mode has effectively been collapsed to English-only.

### Incomplete cleanup still present

Despite the English-only UI cleanup, the project is not fully rebranded. There are still many `AegisTrade` leftovers in:

- `go.mod:1`
- `main.go:6`
- `main.go:7`
- `main.go:8`
- `main.go:9`
- `.env.example`
- `docker-compose.yml`
- `pm2.config.js`
- `PM2_DEPLOYMENT.md`
- `DOCKER_DEPLOY.en.md`
- `DOCKER_DEPLOY.md`
- multiple bounty and legacy docs

Conclusion:

- the UI is mostly rebranded
- the codebase and deployment/docs surface are not fully rebranded
- the project is not yet cleanly "all yours" from a naming standpoint

## Data And Accounting Integrity Issues

This is one of the biggest problem areas.

### 1. P&L and equity semantics are inconsistent

The latest decision record shows:

- `total_balance = 977079.6875`
- `total_unrealized_profit = 877079.6875`
- initial balance in config = `100000`

That results in a displayed P&L of about `+877%`, which is clearly not a meaningful strategy-performance number in this context.

The API layer itself contains comments acknowledging field-semantic mismatch:

- `api/server.go:494`
- `api/server.go:496`

This means the system is currently mixing together:

- broker account equity
- strategy initial balance
- fields whose names do not match what they actually store

Impact:

- dashboard P&L is misleading
- strategy self-evaluation can be distorted
- risk scaling based on this data can become unsafe

### 2. Decision cycle numbering is inconsistent

The latest decision file is named `cycle372`, but the embedded prompt says `Cycle #386`.

Impact:

- auditability is weaker than it should be
- replay/debugging becomes harder
- operator trust in logs is reduced

## Security And Operational Hygiene Risks

### 1. A real-looking API key is committed in config

`config_ibkr.json:26` contains a DeepSeek key in plaintext.

This is a direct security problem. Even if the key is already rotated or test-only, committed secrets in working configs are unacceptable for a serious deployment workflow.

### 2. Broker/account identifiers are in repo-local config

`config_ibkr.json:12` includes the paper account id.

This is less severe than a secret, but still part of poor credential hygiene when combined with committed keys and cookies elsewhere in the repo.

### 3. Dirty working tree

The repository is not clean. Core files currently show modified or untracked status, including:

- `trader/auto_trader.go`
- `trader/ibkr_trader.go`
- `market/ibkr_provider.go`
- `broker/ibkr_client.go`
- `cmd/ibkr-backtest/main.go`
- several test files and docs

Impact:

- unclear release state
- harder reproducibility
- harder rollback or trustworthy deployment

## Code Quality Observations

### Strengths

- There is real breadth here: broker adapters, dashboard, telemetry, backtesting, replay, and risk layers all exist
- IBKR support is materially better than a placeholder adapter
- The test/build baseline is healthy enough to keep iterating quickly
- There is enough structure to turn this into a serious product with further hardening

### Weaknesses

There are still signs of rushed or machine-generated contamination in core code. For example:

- `trader/auto_trader.go:1056`
- `trader/auto_trader.go:1894`

These include nonsensical strings inside logs and error messages.

Impact:

- lower maintainability
- operator confusion
- weaker trust in error handling and telemetry quality

## Readiness Assessment

### What is credible today

- Running local paper strategies against IBKR equities
- Viewing historical decisions and equity charts
- Running replay/backtest experiments
- Comparing strategy profiles
- Operating a dashboard in local/demo mode
- Continuing feature development on a solid enough technical base

### What is not yet credible today

- claiming consistent alpha
- claiming robust autonomous profitability
- unattended live deployment with serious capital
- trusting displayed P&L without fixing the accounting model
- assuming the current backtest outputs are statistically meaningful

## Suggested Priority Order

If the goal is to turn this into something you can realistically trust, the next priorities should be:

1. Fix accounting and P&L semantics end to end.
2. Remove committed secrets and move all credentials to environment or ignored local files.
3. Finish the rebrand and remove legacy `AegisTrade` references.
4. Clean the dirty tree and establish a stable release branch or tagged baseline.
5. Add startup readiness checks for broker, data, news, and API health before market open.
6. Improve broker resilience around disconnect/reconnect and order reconciliation.
7. Expand backtests into longer, broader, more statistically useful studies.
8. Add session reporting so every paper day produces a clear summary automatically.

## Overall Verdict

Northstar is already a serious codebase with real automation, real IBKR integration, a real dashboard, and a meaningful research loop. It is much more than a toy.

At the same time, it still behaves like a fast-moving advanced prototype rather than a finished trading product. The biggest problem is not missing features. The biggest problem is trust:

- can you trust the accounting
- can you trust the runtime state
- can you trust the logs
- can you trust the deployment hygiene

Right now, the answer is "partly, but not enough for live money."

That is a strong place to build from, but it is not the finish line.

## Appendix: Key Files Worth Reading First

If someone needs to understand the project quickly, these are the best entry points:

- `README.md`
- `main.go`
- `api/server.go`
- `trader/auto_trader.go`
- `trader/ibkr_trader.go`
- `cmd/ibkr-backtest/main.go`
- `config_ibkr.json`
- `web/src/App.tsx`
- `web/src/components/CompetitionPage.tsx`
- `docs/UPGRADE_BACKLOG_100.md`
