#!/usr/bin/env bash
# stress-test.sh — run the crypto-backtest across multiple historical
# windows and parameter combinations to measure strategy robustness.
#
# Output: a tab-separated results table suitable for piping to column or
# loading into a spreadsheet. Each row is (window × parameter combo).
#
# Metrics captured per run:
#   window         — end date of the 12-month backtest slice
#   params         — short label for the parameter combo
#   trades         — total trades in that window
#   ann_equity     — annualized compounded equity return (%)
#   max_dd         — max equity drawdown (%)
#   sharpe         — annualized Sharpe on log returns
#   calmar         — ann_equity / max_dd
#
# Runtime: ~20-40 min depending on Binance latency. Each combo fetches
# fresh data (2000 bars × 6 symbols) so the first fetch of each window
# dominates. Parameter variations reuse the connection cache in practice.

set -u
BACKTEST=./crypto-backtest.exe
SYMBOLS="BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,XRPUSDT,DOGEUSDT"
OUTDIR=stress-test-results
mkdir -p "$OUTDIR"
RESULTS="$OUTDIR/results.tsv"
LOGDIR="$OUTDIR/logs"
mkdir -p "$LOGDIR"

echo -e "window\tparams\ttrades\tann_equity%\tmax_dd%\tsharpe\tcalmar" > "$RESULTS"

# Historical windows — each end-date captures the prior 12 months.
# 2000 4h bars ≈ 333 days ≈ 11 months. Good enough coverage.
WINDOWS=(
  "2022-06-30"
  "2022-12-31"
  "2023-06-30"
  "2023-12-31"
  "2024-06-30"
  "2024-12-31"
  "2025-06-30"
  "2025-12-31"
  ""  # current (no end-date = now)
)

# Parameter combos. Labels are short so the output table fits on screen.
# Each line: label|min-score|risk-pct|funding-thresh|dd-halt|cooldown
PARAM_COMBOS=(
  "default|1.0|0.0075|0.0004|0.12|14"
  "tight|1.2|0.0050|0.0004|0.10|21"
  "loose|0.8|0.0100|0.0004|0.15|7"
  "nofilt|1.0|0.0075|0.0000|0.00|0"
)

run_one() {
  local window="$1"
  local combo="$2"
  local label min_score risk_pct fund_thresh dd_halt cooldown
  IFS='|' read -r label min_score risk_pct fund_thresh dd_halt cooldown <<< "$combo"

  local window_arg=""
  local window_label="$window"
  if [ -n "$window" ]; then
    window_arg="-end-date $window"
  else
    window_label="now"
  fi

  local funding_flags=""
  if [ "$fund_thresh" != "0.0000" ]; then
    funding_flags="-funding-filter -funding-thresh $fund_thresh -collect-funding"
  fi

  local ks_flags=""
  if [ "$dd_halt" != "0.00" ]; then
    ks_flags="-portfolio-dd-halt $dd_halt -cooldown-days $cooldown"
  fi

  local logfile="$LOGDIR/${window_label}_${label}.log"
  echo "  [$window_label / $label] running..." >&2

  $BACKTEST -symbols "$SYMBOLS" -bars 2000 -interval 4h \
    $window_arg -min-score "$min_score" -risk-pct "$risk_pct" \
    $funding_flags $ks_flags \
    > "$logfile" 2>&1

  # Extract metrics from the log with grep. Format varies slightly per
  # run but these patterns are stable.
  local trades=$(grep -E "^Total trades:" "$logfile" | head -1 | awk '{print $3}')
  local ann=$(grep -E "^Annualized equity:" "$logfile" | head -1 | awk '{print $3}' | sed 's/%.*//' | sed 's/+//')
  local dd=$(grep -E "^Max equity DD:" "$logfile" | head -1 | awk '{print $4}' | sed 's/%//')
  local sharpe=$(grep -E "^Annualized Sharpe:" "$logfile" | head -1 | awk '{print $3}')
  local calmar=$(grep -E "^Calmar ratio:" "$logfile" | head -1 | awk '{print $3}')

  # Defensive defaults for missing values (e.g., zero trades)
  trades="${trades:-0}"
  ann="${ann:-0}"
  dd="${dd:-0}"
  sharpe="${sharpe:-0}"
  calmar="${calmar:-0}"

  echo -e "${window_label}\t${label}\t${trades}\t${ann}\t${dd}\t${sharpe}\t${calmar}" >> "$RESULTS"
}

echo "=== STRESS TEST: $((${#WINDOWS[@]} * ${#PARAM_COMBOS[@]})) combos ===" >&2
total=0
for w in "${WINDOWS[@]}"; do
  for c in "${PARAM_COMBOS[@]}"; do
    run_one "$w" "$c"
    total=$((total + 1))
  done
done

echo "" >&2
echo "=== RESULTS: $RESULTS ===" >&2
column -t -s $'\t' "$RESULTS"
