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
- Live promotion gate: live traders must also pass the local promotion checklist before trading is allowed
- Dedicated execution manager: trader actions now flow through an execution intent layer with bounded duplicate suppression, final-gate enforcement, and stale execution tracking before broker submission
- Config-level limits for daily loss and drawdown
- Runtime stop windows after risk triggers
- Deployment validation command: `northstar validate-live` checks release build identity, working tree cleanliness, live config validity, promotion status, readiness, and risk-limit presence before deployment
- Live launcher enforcement: `run_ibkr_live.cmd` now runs `validate-live` itself and aborts immediately if that validation fails

## Green Test Discipline

Northstar now has one canonical Go verification command for the active trading paths:

```powershell
powershell -ExecutionPolicy Bypass -File tools\run_green_checks.ps1
```

That script runs:

```powershell
go test -count=1 ./...
```

Why this is the enforced path:

- it covers the current Go runtime, broker, execution, reconciliation, startup, deployment, and safety packages in one deterministic pass
- `-count=1` avoids cached test results so local and CI runs reflect the current tree
- it is the same command used by GitHub Actions in `.github/workflows/go-tests.yml`

CI enforcement:

- runs on `pull_request`
- runs on pushes to `main`
- fails the workflow immediately if the Go tree is not green

Operator/developer rule:

- do not treat a branch as deployment-ready unless `tools\run_green_checks.ps1` passes locally
- do not treat `main` as healthy if the `Go Tests` workflow is red

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

3. Pick a tracked config template such as `config_ibkr.example.json`, `config_paper.example.json`, or `config.json.example`. Keep the tracked file safe and provide credentials through environment variables or an ignored local override file.

4. Start backend:

```bash
go run main.go config.json
```

If a stamped `northstar.exe` release binary already exists, the Windows launcher scripts prefer that binary automatically and only fall back to `go run` for local development.

5. Start web dashboard (new terminal):

```bash
cd web
npm run dev
```

Backend default port: `8080`
Frontend default dev port: `3000`

## Local credential setup

Tracked config files in this repo are examples and safe defaults only. Do not commit real API keys, broker account IDs, session cookies, or private keys.

Preferred setup:

1. Export the required environment variables for your mode:

```powershell
$env:NORTHSTAR_DEEPSEEK_API_KEY = "..."
$env:NORTHSTAR_IBKR_ACCOUNT_ID = "DU1234567"
$env:NORTHSTAR_IBKR_SESSION_COOKIE = "x-sess-uuid=..."
```

2. Run Northstar with a tracked config:

```bash
go run main.go config_ibkr.example.json
```

Optional local override file:

- `config.local.json`
- `<base-config>.local.json` such as `config_ibkr.example.local.json` or `config_ibkr.local.json`

These files are gitignored. They use the same JSON shape as the base config, and the local file overrides the tracked file at startup.

Common environment variables:

- `NORTHSTAR_DEEPSEEK_API_KEY`
- `NORTHSTAR_QWEN_API_KEY`
- `NORTHSTAR_CUSTOM_API_URL`
- `NORTHSTAR_CUSTOM_API_KEY`
- `NORTHSTAR_CUSTOM_MODEL_NAME`
- `NORTHSTAR_IBKR_BASE_URL`
- `NORTHSTAR_IBKR_ACCOUNT_ID`
- `NORTHSTAR_IBKR_SESSION_COOKIE`
- `NORTHSTAR_ALPACA_API_KEY`
- `NORTHSTAR_ALPACA_SECRET_KEY`
- `NORTHSTAR_BINANCE_API_KEY`
- `NORTHSTAR_BINANCE_SECRET_KEY`
- `NORTHSTAR_HYPERLIQUID_PRIVATE_KEY`
- `NORTHSTAR_HYPERLIQUID_WALLET_ADDR`
- `NORTHSTAR_ASTER_USER`
- `NORTHSTAR_ASTER_SIGNER`
- `NORTHSTAR_ASTER_PRIVATE_KEY`

## Common run scripts (Windows)

- Alpaca paper: `run_paper.cmd`
- Alpaca live: `run_live.cmd`
- Replay demo (synthetic local data): `run_replay.cmd`
- IBKR paper: `run_ibkr_paper.cmd`
- IBKR shadow (live-like, no orders): `run_ibkr_shadow.cmd`
- IBKR paper with live-parity config: `run_ibkr_paper_live.cmd`
- IBKR live: `run_ibkr_live.cmd`
- IBKR automated backtest matrix: `run_ibkr_backtest.cmd`
- Live dashboard demo (paper synthetic feed): `run_dashboard_demo.cmd`
- Live dashboard demo full startup (backend + frontend): `run_dashboard_demo_full.cmd`

## Live-Like Validation Modes

Northstar now has three different non-live validation lanes for equities. They are not interchangeable:

- `demo`
  - synthetic prices and synthetic fills
  - useful for runtime smoke tests only
  - not suitable for judging live-like execution quality
- `shadow`
  - real-time broker-backed market data
  - same decision, risk, supervisor, selector, allocator, and execution-intent path
  - no real broker order is sent
  - best choice when you want live-like behavior with zero order-placement risk
- `paper`
  - real-time broker-backed market data
  - same strategy/execution path
  - broker orders go to the paper account only
  - best choice when you want to test real broker paper-order flow without risking capital

If you want paper validation to stay as close to live as possible, keep a live config and a shadow/paper config in parity and let only these fields differ:

- trader `id`
- trader `name`
- trader `mode`
- live-promotion-only fields
- `api_server_port`

Use the built-in parity checker before startup:

```powershell
powershell -ExecutionPolicy Bypass -File tools\check_mode_parity.ps1 `
  -BaselineConfig config_ibkr_live.json `
  -CandidateConfig config_ibkr_shadow.json
```

Tracked live-like templates:

- `config_ibkr_shadow.example.json`
- `config_ibkr_paper_live.example.json`
- `config_ibkr_shadow.openai.json`

`run_ibkr_shadow.cmd` now picks the shadow config in this order:

1. `config_ibkr_shadow.json`
2. `config_ibkr_shadow.openai.json` when `NORTHSTAR_CUSTOM_API_KEY` or `OPENAI_API_KEY` is available
3. `config_ibkr_shadow.example.json`

If the launcher selects `config_ibkr_shadow.openai.json`, it will also map `OPENAI_API_KEY` into `NORTHSTAR_CUSTOM_API_KEY` automatically and set conservative defaults for `NORTHSTAR_CUSTOM_API_URL` and `NORTHSTAR_CUSTOM_MODEL_NAME` when they are not already set. Because that config intentionally swaps the model provider and uses the simulated broker, the launcher prints a parity warning and skips the strict live-baseline drift check instead of pretending it is exact live parity.

The IBKR live-like launchers can also auto-resolve the local paper account ID and the current `x-sess-uuid` session cookie from the authenticated local gateway, so you do not need to export those values manually when IBeam is already healthy.

If a launcher now stops before startup, use the gateway-state probe to see whether the problem is:

- gateway unreachable
- gateway reachable but not authenticated
- account-state endpoints unavailable
- market-data history unavailable

```powershell
powershell -ExecutionPolicy Bypass -File tools\check_ibkr_gateway_state.ps1
```

IBKR portfolio readiness now performs a bounded warm-up before checking account-scoped `/portfolio/{accountId}/*` endpoints. This matters because the gateway can report `authenticated=true` while `summary` / `positions` still flap through transient `401` or `503` responses until the portfolio session is primed. Northstar now warms `portfolio/accounts` plus the portfolio account listings, retries those account-scoped checks conservatively, and fails fast on hung account endpoints instead of letting the runtime stall for an extended period.

During active runtime, Northstar also reuses one short-lived canonical broker account snapshot for repeated balance/position reads within the same decision window, then invalidates it immediately after execution submission, broker degradation, or broker reconciliation. This keeps the runtime from hammering fragile IBKR portfolio endpoints multiple times per cycle while still failing closed once the snapshot goes stale.

Northstar now also enforces a hard broker-truth runtime gate before trading is allowed for active IBKR-backed modes. That gate is mode-aware:

- broker-managed paper/live modes require fresh verified broker account truth, open-order truth, and position truth
- shadow mode with simulated execution still requires live market-data truth when it is using IBKR data
- demo/replay modes do not require the live broker-truth gate

When Northstar cannot prove the required truth for the current mode, trading stays blocked instead of assuming the last known local state is safe. `/api/status` now includes a `broker_truth` section that shows which component is blocking:

- `account_verified`
- `orders_verified`
- `positions_verified`
- `market_data_verified`

Operator checks before trusting a broker-backed paper/live session:

1. Confirm `broker_truth.trading_blocked=false`
2. Confirm `broker_truth.verified=true`
3. Confirm `order_reconciliation` and `position_reconciliation` remain fresh and healthy

## Operator Runtime Truth

`/api/status` now carries a canonical `runtime_condition` summary so operators do not need to infer the real state by stitching together gate, incident, and broker fields manually.

Runtime condition states:

- `healthy`
  - runtime is running normally and the current cycle is tradable
- `degraded`
  - runtime is up, but entries or cycle quality are reduced by restrictions or degraded dependencies
- `blocked`
  - runtime is up, but current conditions block trading
- `halted`
  - a hard safety control such as the kill switch or a hard halt is active
- `awaiting_reconciliation`
  - trading is blocked or entry-restricted pending broker/order/position truth cleanup
- `market_closed`
  - startup may still be healthy, but the current cycle is not tradable because the market is closed or otherwise expected to be non-tradable

Severity semantics:

- `info`
  - expected non-tradable state such as market closed
- `warning`
  - degraded or restricted state that needs operator awareness
- `critical`
  - hard stop or unresolved safety-critical state

Decision logs now also persist the current runtime condition and latest known account snapshot so blocked cycles are easier to interpret after the fact.

## Restart Recovery

Northstar now persists the minimum critical runtime state needed for safer restarts at:

- `output/state/<trader_id>/runtime_state.json`

The checkpoint currently includes:

- execution-manager history and duplicate-suppression state
- local order lifecycle state when the active trader exposes the lifecycle store
- local position snapshots used by position reconciliation
- pending protective-order requirements for broker-managed positions
- shadow-mode portfolio state and recent hypothetical decisions
- the latest known account summary used for operator status

Restart behavior is safety-first:

- missing checkpoint: startup proceeds normally
- corrupt or unreadable checkpoint: trading stays blocked until the operator fixes or removes it
- restored broker-managed state with unresolved orders/positions: startup shows `restart_recovery` pending and trading stays blocked until broker reconciliation/position bootstrap clears the uncertainty
- restored shadow state: shadow portfolio continuity is preserved without broker reconciliation

What this guarantees:

- duplicate suppression survives restart
- in-flight local order awareness is not silently discarded
- operator status exposes whether state was restored, whether it is partial, and whether reconciliation is still pending

What it does not guarantee:

- it is not a full durable OMS
- it does not reconstruct broker truth without the broker
- if the process dies between an order mutation and the next checkpoint write, the very latest state change can still be lost

Operator checks after restart:

1. Inspect `/api/status` and confirm `restart_recovery.trading_blocked=false`
2. If `restart_recovery.pending_reconciliation=true`, wait for broker bootstrap / position reconciliation to clear it before trusting entries
3. Confirm `order_reconciliation` and `position_reconciliation` are healthy before resuming normal supervision

## Restart / Interruption Validation Harness

Northstar now has one focused restart-safety validation harness for the highest-value interruption scenarios:

```powershell
powershell -ExecutionPolicy Bypass -File tools\run_restart_validation.ps1
```

This harness is intentionally narrow. It validates the real safety assumptions already built into the runtime, including:

- controlled restart with a clean durable checkpoint
- interruption-style restart with no checkpoint until broker truth is re-established
- restored submitted/in-flight state that must stay blocked until reconciliation clears it
- unresolved execution truth on restart remaining hard-blocked
- reconciliation-inferred execution truth on restart remaining reduce-only / entries-restricted
- corrupt checkpoint failure staying fail-closed and journaled

What it does not prove:

- actual live-broker outage replay against IBKR
- perfect crash durability for writes that never reached disk
- full end-to-end OMS recovery beyond the current checkpoint + reconciliation model

Developer/operator rule:

1. Run `tools\run_green_checks.ps1` for broad repo health.
2. Run `tools\run_restart_validation.ps1` after touching restart, reconciliation, execution-lifecycle, or broker-truth gating logic.
3. Treat any harness failure as a safety regression until the runtime behavior is explained and fixed.

Because the harness lives in the Go test suite, it also runs under the normal `go test ./...` path and the repo's `Go Tests` CI workflow.

## Safety-Critical Event Journal

Northstar now keeps one canonical append-only JSONL journal for the highest-value runtime truth transitions at:

- `output/audit/journal/<trader_id>/events.jsonl`

This journal is intentionally narrow. It is not a giant all-events logging system. It records the events that matter most for forensic reconstruction and restart-era safety work:

- execution lifecycle transitions observed through the local order lifecycle store
- reconciliation runs that repair, infer, or leave broker-managed execution outcomes unresolved
- explicit broker-truth state transitions such as verified, restricted, blocked, and recovered
- trading-gate transitions that materially change whether trading, entries, or exits are allowed
- kill-switch activation / clear / cancel-open-orders outcomes
- restart-recovery restore / pending / failure / reconciled markers
- protection-state transitions such as pending creation, submission pending, clear, and protection confirmed

Important truth model:

- broker-confirmed lifecycle truth stays distinct from reconciliation-inferred truth
- inferred execution outcomes are preserved as inferred in the journal with authority/confidence fields
- unresolved broker-missing outcomes remain explicit and safety-relevant
- reconciliation events now promote the primary inferred/unresolved issue into explicit journal authority/confidence/order-id fields
- the journal is append-only and survives restart because each event is appended and synced to disk

Operator checks:

1. Inspect `/api/status.event_journal` for the active journal path, event count, last event type, and any last write/read error
2. Inspect `output/audit/journal/<trader_id>/events.jsonl` when you need the exact ordered sequence of execution, reconciliation, kill-switch, or restart transitions
3. If you see unresolved reconciliation outcomes in the journal, keep trading blocked until broker reconciliation clears them

Current limitation:

- the journal is append-only and durable, but it is not yet a full event-sourced runtime; it complements the restart checkpoint and audit records instead of replacing them

If IBKR starts returning `Chart data unavailable` or similar history-endpoint failures during a shadow or paper session, Northstar now treats that as a bounded market-data availability block instead of a generic runtime crash. Shadow cycles stay in the safe blocked path, a `market_data_validation_failed` incident is opened with runbook guidance, and the incident clears automatically once fresh history requests succeed again.

Northstar now also runs a small liquid-symbol preflight before the full equity decision pipeline loads IBKR market data. If liquid probes like `AAPL`, `MSFT`, `NVDA`, `SPY`, or `QQQ` are delayed or unusable across the board, the runtime records one clear feed-delay state in `/api/status` and opens a `market_data_validation_failed` incident instead of spamming per-symbol failures for the whole candidate batch. This makes delayed or non-real-time IBKR sessions explicit and fail-safe.

The local `ibeam_gateway` Docker health check is also auth-aware now. It only reports `healthy` when `/iserver/auth/status` says both `authenticated=true` and `connected=true`, so `docker ps` is no longer a misleading proxy for “Gateway process is up but the IBKR session is actually usable.”

Recommended startup paths:

- safest live-like validation: `run_ibkr_shadow.cmd`
- broker-paper execution validation: `run_ibkr_paper_live.cmd`

## Live promotion checklist

Live mode now has two separate gates:

- readiness: can the system run safely right now?
- promotion: has this trader/config been explicitly approved for live use?

Live trading stays blocked unless both pass.

Recommended local-only promotion fields for live traders:

- `live_promotion_approved`: explicit operator approval; keep this in an ignored local override unless you have a deliberate release process
- `promotion_source_trader_id`: optional paper trader ID whose session reports should be used as evidence
- `min_paper_session_reports`: minimum recent parseable paper session reports required for promotion
- `require_backtest_summary`: optionally require a local `study_summary.json`
- `require_release_build_for_live`: require stamped release build metadata instead of `go run` / dev builds
- `promotion_max_evidence_age_days`: max age for paper/backtest evidence

Evidence sources checked locally:

- paper session reports under `output/session_reports/<trader_id>/`
- backtest study summaries under `output/**/study_summary.json`

Passing promotion does not claim profitability. It only means:

- live config sanity passed
- explicit live approval is present
- startup readiness passed
- broker runtime is healthy
- bounded local paper evidence exists
- required build/backtest gates passed

Example local live override:

```json
{
  "traders": [
    {
      "id": "ibkr_live_trader",
      "live_promotion_approved": true,
      "promotion_source_trader_id": "ibkr_paper_trader",
      "min_paper_session_reports": 3,
      "require_backtest_summary": true,
      "require_release_build_for_live": true,
      "promotion_max_evidence_age_days": 30
    }
  ]
}
```

Operator status for live traders now includes a `promotion` section in `GET /api/status`.

## Deployment validation

Before promoting a config to live deployment, run:

```bash
northstar validate-live -config config_ibkr_live.json
```

You can also pass the config path positionally:

```bash
northstar validate-live config_ibkr_live.json
```

This command fail-closes if any enabled live trader is not deployment-ready. It verifies:

- release build identity is present
- git working tree is clean
- config parses successfully and contains enabled live traders
- live-trader risk limits are defined
- trader startup readiness passes
- trader promotion approval passes

Typical deployment flow:

1. Build a stamped release binary.
2. Ensure the repo working tree is clean.
3. Run `northstar validate-live ...`.
4. Only deploy/start live trading if the command exits with code `0`.

The actual Windows live launcher now enforces that flow. `run_ibkr_live.cmd`:

1. resolves the live config and account context
2. runs `northstar validate-live <config>`
3. aborts immediately on any non-zero validation exit code
4. records a short-lived validation handoff for that exact config
5. only then runs IBKR readiness and starts the live process

Northstar live startup also fails closed inside the binary when enabled live traders are present but that validation handoff is missing, stale, or for a different config path. This means a direct `northstar.exe config_ibkr_live.json` start will no longer quietly bypass the stricter deployment validator.

Launch-time failures now block live startup for conditions already checked by `validate-live`, including:

- dirty working tree
- non-release or dirty build identity
- invalid live config
- missing enabled live traders
- failed readiness or promotion checks
- stale or mismatched live-validation handoff

What to do when live validation fails:

1. read the `validate-live` failure lines printed by the launcher
2. fix the blocking condition
3. rerun `run_ibkr_live.cmd`

Shadow, paper, demo, and replay launchers do not use this live-only handoff requirement.

## Trading universe truth

For equity traders, the runtime entry universe is now explicit and canonical:

- `default_coins` plus any merged `default_coins_file` symbols define the configured equity entry universe
- `trusted_symbols_file` is treated as an additional allowlist filter, not a hidden universe expander
- Northstar no longer pulls the equity entry universe from the merged dynamic pool at runtime

That means a config like:

- `default_coins = ["AAPL", "MSFT", "NVDA"]`
- `default_coins_file = "data/universe/us_companies.txt"`

produces one explicit merged equity universe before trading starts, and that resolved universe is what the runtime rotates through for new entry candidates.

Operator visibility:

- startup logs print the resolved equity universe source and counts
- `/api/status` now includes a `universe` section with:
  - selection mode
  - configured/effective counts
  - trusted-symbol filter details
  - last candidate window and market-data load order
- Northstar writes the full resolved universe manifest to:
  - `output/universe/<trader_id>/active_universe.json`
- paper/session reports now record the active universe source and counts used during the session

Safety note:

- equity configs must now resolve to at least one non-`USDT` symbol through `default_coins` or `default_coins_file`
- if the explicit equity universe is empty, startup fails instead of quietly falling back to a hidden broader pool

## Execution management

Northstar now uses a dedicated execution-management layer between approved decisions and broker submission:

```text
strategy -> pre-trade risk -> supervisor/final gate -> execution manager -> broker -> lifecycle/reconciliation
```

What this adds:

- explicit execution intents for trader actions
- bounded duplicate suppression so equivalent in-flight or very recent intents are not blindly resubmitted
- conservative final-gate checks at submit time
- honest immediate execution statuses such as `blocked`, `duplicate_suppressed`, `submitted`, `acknowledged`, `partially_filled`, `filled`, `rejected`, and `stale`
- bounded execution summaries in `GET /api/status` and paper session reports

Important scope notes:

- this is not a full OMS/EMS or smart-routing system
- ambiguous broker-submit outcomes are not auto-retried
- broker truth for later fills and terminal states still comes from order lifecycle tracking and reconciliation

Northstar no longer treats IBKR submit acknowledgement or disappearance from the open-orders list as fill truth. For IBKR:

- `submitted` means Northstar transmitted the intent locally, but broker lifecycle truth is still incomplete
- `acknowledged` means broker/open-order lifecycle has confirmed the order is working, but there is still no fill truth yet
- `partially_filled` and `filled` only come from broker-confirmed lifecycle or later reconciliation evidence

When reconciliation has to repair a broker-missing order from position evidence, Northstar now keeps that authority explicit instead of flattening it into ordinary broker-confirmed truth:

- `broker_confirmed` means the lifecycle state came directly from broker-visible order truth
- `reconciliation_inferred` means the lifecycle outcome was inferred later from position evidence and still requires operator awareness
- `unresolved` means the broker-managed order disappeared and Northstar still does not have enough evidence to normalize the outcome safely

Operator-facing impact:

- `/api/status.order_reconciliation` now exposes current confirmed, inferred, pending, and unresolved counts
- `/api/status.order_reconciliation` and `/api/status.broker_truth` now expose the primary current execution-truth issue, including authority, confidence, and whether review is required
- `/api/status.broker_truth` now shows when broker truth is verified but confidence is degraded by inferred outcomes
- unresolved broker-missing outcomes open critical incidents/alerts and keep trading blocked through the broker-truth gate
- inferred outcomes open warning incidents/alerts, restrict new entries until reconciliation is clean again, and remain visible in session reports, decision logs, and order audit records

Protective orders are now also truth-driven:

- Northstar does not claim protection is active just because an entry submit succeeded
- if an entry is not yet filled, protection stays in a durable `pending` state
- once lifecycle or reconciliation confirms fill quantity, Northstar submits only the protective quantity that is actually confirmed
- after restart, unresolved pending protection is restored and trading stays blocked until reconciliation clears the uncertainty

Operator checks when broker-managed order truth is degraded:

1. Inspect `/api/status.order_reconciliation` for `current_inferred_orders` and `current_unresolved_orders`
2. Inspect `/api/status.broker_truth` for `entries_restricted`, `primary_issue_authority`, `primary_issue_confidence`, and the primary issue reason
3. Review `output/audit/orders/<trader_id>/` to see whether the lifecycle truth was broker-confirmed or reconciliation-inferred
4. If `current_unresolved_orders > 0`, do not resume normal entries until reconciliation clears the ambiguity
5. If only inferred outcomes remain, keep trading in the restricted state and confirm the broker lifecycle catches up before treating the result as fully broker-confirmed

## Equity strategy modes

For equity traders (`exchange=ibkr` or `exchange=alpaca`), `strategy_mode` supports:

- `ai_only`: pure AI decision flow
- `momentum_fallback`: AI first, local momentum opens if AI stays passive
- `momentum_only`: fully local momentum strategy (no AI API usage)
- `multi_factor`: fully local multi-factor strategy (trend, momentum, RSI, MACD, volume, ATR regime, cross-sectional strength, macro regime)
- `hybrid_ai`: AI decisions filtered and risk-shaped by local multi-factor logic before execution

Reference template: `ibkr_autonomous_template.json`

Northstar now uses one canonical runtime decision architecture for equities:

1. load canonical market data first (`market.Get` now carries features, regime, and selector output)
2. let the configured `strategy_mode` generate candidate actions
3. run the canonical selector/allocator dispatch layer before execution

That means the research pipeline is no longer only a shadow/backtest overlay for equities. It is now the universal entry gate and sizing layer across runtime equity modes. `strategy_mode` still chooses how ideas are generated; the canonical pipeline now consistently decides whether those entry ideas are allowed and how large they may be.

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
- automatic news credibility learning per symbol (persistent memory under `runtime/news_learning/`)
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
go run ./cmd/ibkr-backtest
```

Auto parameter grid search:

```bash
go run ./cmd/ibkr-backtest -auto-grid -strategy-grid multi_factor,momentum_only,momentum_fallback -score-grid 0.30,0.35,0.45,1.25 -position-grid 0.06,0.08,0.10
```

Auto-grid with best-profile output:

```bash
go run ./cmd/ibkr-backtest -auto-grid -write-best-profile best_profile.json
```

90-cycle local multi-factor sweep example:

```bash
go run ./cmd/ibkr-backtest -profiles multi_factor:0.35:0.08,multi_factor:0.45:0.10,momentum_only:1.25:0.10 -max-cycles 90
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

## Release baseline

For paper/live operator trust, treat release builds as explicit artifacts rather than whatever happens to be in the working tree.

Recommended baseline:

1. Confirm the workspace state you intend to deploy:

```bash
git status --short
```

2. Run backend validation:

```bash
go test ./...
```

3. If you are deploying the dashboard too, build the frontend:

```bash
cd web
npm run build
cd ..
```

4. Build a stamped binary:

```powershell
powershell -ExecutionPolicy Bypass -File tools/build_release.ps1 -Version v0.1.0 -OutFile northstar.exe
```

Equivalent manual build:

```bash
go build -trimpath -ldflags="-X northstar/buildinfo.Version=v0.1.0 -X northstar/buildinfo.Commit=<git-commit> -X northstar/buildinfo.BuildTime=<utc-rfc3339> -X northstar/buildinfo.Channel=release -X northstar/buildinfo.Dirty=clean" -o northstar
```

5. Verify the binary identity before launch:

```bash
./northstar --version
```

6. After startup, confirm the running instance identity:

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/api/status?trader_id=<trader_id>
```

Both endpoints now include build metadata so operators can verify `version`, `commit`, `build_time`, `channel`, and `dirty` state.

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
