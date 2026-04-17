# Upgrade Backlog (100 Items)

This backlog is intentionally execution-focused: each line is a concrete task that improves consistency, accuracy, robustness, or profitability workflow quality.

Legend:
- `[x]` completed in this pass
- `[ ]` pending

## 1) Execution Reliability

1. [x] Parse IBKR live-order responses across array and wrapped-object schemas.
2. [x] Surface IBKR order reject signals as hard errors instead of silent success.
3. [x] Confirm suppressable IBKR warnings explicitly and validate confirm responses.
4. [x] Make IBKR HTTP retries safe for POST bodies by rebuilding request body per attempt.
5. [x] Add idempotency key support for order submit retries.
6. [x] Add client-side order de-duplication window by `(symbol, side, qty, ts bucket)`.
7. [x] Add timeout budget per execution phase (submit, confirm, fill-poll, bracket).
8. [x] Add configurable max order-staleness auto-cancel.
9. [ ] Add partial-fill aware bracket sizing.
10. [x] Add retry policy matrix by endpoint class (quote/order/portfolio/auth).
11. [ ] Add bulk cancellation fallback for stuck order storms.
12. [ ] Add side/quantity normalization utility for all broker adapters.
13. [x] Add exchange-session guard to prevent accidental after-hours entry.
14. [x] Add order throttling token bucket to avoid pacing rejects.
15. [ ] Add per-symbol lock to avoid concurrent conflicting orders.
16. [ ] Add “pending close” lock to prevent reopen before close settlement.
17. [ ] Add adaptive sleep after 429 based on Retry-After header.
18. [ ] Add reconciliation job that compares broker positions vs internal state.
19. [ ] Add retry-safe response body capture for all broker errors.
20. [ ] Add execution simulator parity tests vs live order semantics.

## 2) Data Quality and Universe Hygiene

21. [x] Filter invalid symbols in backtest symbol parser (header/garbage/token safety).
22. [x] Enforce minimum bars per symbol before including in backtests.
23. [ ] Add symbol metadata cache (country, exchange, currency, lot rules).
24. [ ] Add automatic delisting detection and universe pruning.
25. [ ] Add stale-data detection with provider fallback.
26. [ ] Add per-symbol data quality score (missing bars, bad prints, splits).
27. [ ] Add corporate actions adjustment validation checks.
28. [ ] Add duplicate timestamp deduplication in bar ingestion.
29. [ ] Add calendar-aware gap handling (holidays, half-days).
30. [ ] Add premarket/after-hours toggle and separate features.
31. [ ] Add survivorship-bias controls for historical universe.
32. [ ] Add spread-quality checks before allowing entries.
33. [ ] Add quote-to-trade consistency checks for suspicious ticks.
34. [ ] Add outlier-bar clipping rules with audit logging.
35. [ ] Add provider drift monitor (IBKR vs secondary provider).
36. [ ] Add periodic revalidation of conid cache.
37. [ ] Add per-provider latency histogram and SLA tracking.
38. [ ] Add data schema versioning for replay compatibility.
39. [ ] Add cold-start preload of top symbols before open.
40. [ ] Add universe build pipeline with reproducible snapshots.

## 3) Strategy and Signal Engine

41. [ ] Add walk-forward parameter selection by regime.
42. [ ] Add ensemble weighting between factor and AI decision paths.
43. [ ] Add confidence calibration (Platt/isotonic style) for open signals.
44. [ ] Add feature drift detector to disable stale factors.
45. [ ] Add factor importance telemetry per trade.
46. [ ] Add hierarchical model: market -> sector -> symbol residual alpha.
47. [ ] Add dynamic momentum horizon selection by volatility state.
48. [ ] Add event-risk feature flags around macro calendars.
49. [ ] Add cross-sectional ranking with turnover constraints.
50. [ ] Add adaptive entry delay after gap opens.
51. [ ] Add position add/reduce logic (scale in/out) instead of all-or-none.
52. [ ] Add re-entry cooldown by realized edge persistence.
53. [ ] Add minimum holding-time filter to reduce churn.
54. [ ] Add expected value estimate at decision-time (EV in bps).
55. [ ] Add directional disagreement penalty when model and factors conflict.
56. [ ] Add strategy voting quorum with abstain state.
57. [ ] Add false-breakout detector using intraday reversion signals.
58. [ ] Add liquidity-conditional momentum scoring.
59. [ ] Add sector-relative strength overlay for equity entries.
60. [ ] Add strategy fail-safe mode that falls back to conservative baseline.

## 4) News and Alternative Signals

61. [ ] Add event deduplication across syndicated headlines.
62. [ ] Add source reliability priors per publisher/domain.
63. [ ] Add language normalization and entity extraction for ticker linking.
64. [ ] Add explicit rumor/unconfirmed classifier and confidence penalty.
65. [ ] Add policy/economic event calendar risk tags.
66. [ ] Add recency decay curve tuning per event type.
67. [ ] Add contradiction detector for conflicting headline clusters.
68. [ ] Add post-event realized impact backfill to refine news weights.
69. [ ] Add “news blackout” mode for unstable breaking stories.
70. [ ] Add symbol-to-theme graph for second-order news impacts.

## 5) Risk and Portfolio Controls

71. [ ] Add intraday VaR budget with hard fail-close threshold.
72. [ ] Add dynamic gross/net caps tied to realized volatility.
73. [ ] Add concentration cap by sector and theme.
74. [ ] Add liquidity shock stress test before order placement.
75. [ ] Add stop distance sanity checks by spread and ATR regime.
76. [ ] Add drawdown phase classifier (normal/stress/recovery) with policy map.
77. [ ] Add adaptive leverage haircut after loss clusters.
78. [ ] Add kill-switch requiring N independent triggers before disabling.
79. [ ] Add risk budget attribution by strategy sleeve.
80. [ ] Add overnight risk profile separate from intraday profile.

## 6) Backtesting and Research Workflow

81. [x] Add backtest tests for symbol sanitation and CSV row validation helpers.
82. [ ] Add cross-validation by time blocks (rolling origin).
83. [ ] Add transaction-cost stress ladders (base/high/extreme).
84. [ ] Add slippage model by participation and spread regime.
85. [ ] Add feature ablation runner and ranking table.
86. [ ] Add benchmark comparison (SPY/QQQ + risk parity baseline).
87. [ ] Add confidence intervals for all key metrics via bootstrap.
88. [ ] Add backtest reproducibility manifest (inputs, seed, commit hash).
89. [x] Add auto-report generation in Markdown and CSV.
90. [ ] Add regression guard: fail CI when performance degrades beyond threshold.

## 7) Observability, Ops, and Product UX

91. [ ] Add structured logging with consistent event IDs.
92. [ ] Add per-order lifecycle timeline events in API and UI.
93. [x] Add readiness endpoint with broker/data/news health matrix.
94. [ ] Add dashboards for drawdown, heat, slippage, reject rate, latency.
95. [ ] Add alerting rules (email/Telegram/webhook) for risk and connectivity.
96. [x] Add startup self-check pipeline and actionable diagnostics.
97. [ ] Add runbook docs for market-open, halt, and recovery procedures.
98. [ ] Add one-click paper session report export.
99. [ ] Add immutable audit log for compliance-grade traceability.
100. [x] Add continuous paper-trading evaluation job with daily scorecards.

