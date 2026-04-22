"""
walk_forward.py — PROPER walk-forward validation for the ML signal model.

Earlier we trained once on pre-2023 data and tested on 2023/2024/2025. The
ML model performed worse than rule-based in 2023 because it couldn't adapt
to the regime shift. This script tests whether ROLLING retrain fixes that.

For each test period T:
  1. Fetch data up to T.
  2. Train model on [T-365d .. T-30d] (leave last 30d for val).
  3. Test OOS on [T .. T+90d].
  4. Record trade metrics.
  5. Slide T forward 90 days and repeat.

This gives us one trade-simulation sequence per 90-day period, with the
model retrained fresh each time. If walk-forward ML Sharpe is consistently
positive across all periods, we have real evidence the model adapts.

Fallback behavior: if a test window has <100 bars of data or training
fails, the window is skipped and logged. A single bad window doesn't
abort the whole sweep.

Output:
  walk_forward_results.json — per-window metrics + aggregate
  walk_forward_summary.tsv  — one-line-per-window table for quick scan
"""

from __future__ import annotations
import json
import math
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import List, Optional

import numpy as np
import pandas as pd

# Reuse the training + simulation machinery from train_and_eval.py
from train_and_eval import (
    INTERVAL, WARMUP_BARS, TAKE_PROFIT_PCT, STOP_LOSS_PCT, MAX_HOLD_BARS,
    TAKER_FEE_BPS, SLIP_BPS, RISK_PER_TRADE,
    LABEL_HORIZON_BARS, LABEL_THRESH,
    DEFAULT_SYMBOLS,
    fetch_klines, fetch_aster_funding,
    compute_features, make_labels,
    train_model, simulate_trading,
    apply_kill_switch, compute_metrics,
)

ROOT = Path(__file__).parent

@dataclass
class WalkForwardWindow:
    period_start_iso: str
    period_end_iso: str
    train_days: int
    test_days: int
    n_train_rows: int
    n_test_trades: int
    test_metrics_raw: dict
    test_metrics_killswitch: dict
    status: str
    note: str = ""

def build_per_symbol_dataset(symbols: List[str], earliest_ms: int, latest_ms: int, bars: int = 5500):
    """Fetch OHLCV + funding once for each symbol, then all walk-forward
    iterations slice from the cache. Much faster than re-fetching per
    iteration."""
    data = {}
    for sym in symbols:
        print(f"  fetching {sym}...", flush=True)
        df = fetch_klines(sym, INTERVAL, bars, end_ms=latest_ms)
        if df.empty:
            print(f"    no data for {sym}, skipping")
            continue
        funding = fetch_aster_funding(sym, int(df.iloc[0]["open_time"]), int(df.iloc[-1]["open_time"]))
        feats = compute_features(df, funding)
        labels = make_labels(df["close"], LABEL_HORIZON_BARS, LABEL_THRESH)
        data[sym] = {"df": df, "features": feats, "labels": labels, "funding": funding}
    return data

def run_walk_forward(
    symbols: List[str],
    test_period_days: int = 90,
    train_lookback_days: int = 365,
    min_train_rows: int = 1500,
) -> List[WalkForwardWindow]:
    """Build sliding test periods and evaluate each.

    Test windows are non-overlapping 90-day slices. Training window for each
    test is a 365-day lookback ending just before the test period (with 12
    bars = 48h gap to avoid label leakage from the horizon).
    """
    # Fetch 11000 bars (~5 years) per symbol so we can include 2022-2023
    # in the walk-forward test windows. With 365d train + 90d test requirement,
    # more history = more windows = covers the 2023 V-shape regime failure case.
    now_ms = int(time.time() * 1000)
    print(f"\n=== Fetching per-symbol data ({len(symbols)} symbols, 11000 bars ~ 5yr) ===", flush=True)
    per_symbol = build_per_symbol_dataset(symbols, 0, now_ms, bars=11000)
    if not per_symbol:
        raise RuntimeError("no symbol data fetched")

    # Determine time span available across all symbols
    earliest = max(int(d["df"].iloc[0]["open_time"]) for d in per_symbol.values())
    latest = min(int(d["df"].iloc[-1]["open_time"]) for d in per_symbol.values())
    earliest_dt = datetime.fromtimestamp(earliest / 1000, tz=timezone.utc)
    latest_dt = datetime.fromtimestamp(latest / 1000, tz=timezone.utc)
    print(f"Common span: {earliest_dt.date()} -> {latest_dt.date()}\n", flush=True)

    # Build combined feature DataFrame for training slices
    combined_rows = []
    for sym, d in per_symbol.items():
        row = d["features"].copy()
        row["symbol"] = sym
        row["open_time"] = d["df"]["open_time"].values
        row["close"] = d["df"]["close"].values
        row["label"] = d["labels"].values
        combined_rows.append(row)
    combined = pd.concat(combined_rows).reset_index(drop=True).dropna()
    combined = combined.sort_values("open_time").reset_index(drop=True)
    feature_cols = [c for c in combined.columns if c not in ("symbol", "open_time", "close", "label")]

    # Generate test windows: slide forward from first-feasible start
    first_test_start = earliest_dt + timedelta(days=train_lookback_days)
    # Stop test windows such that the full period fits in data
    last_test_start = latest_dt - timedelta(days=test_period_days)
    if first_test_start >= last_test_start:
        raise RuntimeError(f"not enough data for walk-forward: need >{train_lookback_days + test_period_days} days, have {(latest_dt - earliest_dt).days}")

    test_starts = []
    t = first_test_start
    while t < last_test_start:
        test_starts.append(t)
        t += timedelta(days=test_period_days)

    print(f"Walk-forward: {len(test_starts)} test windows of {test_period_days}d each\n", flush=True)

    results: List[WalkForwardWindow] = []

    for i, test_start in enumerate(test_starts):
        test_end = test_start + timedelta(days=test_period_days)
        train_end = test_start - timedelta(hours=LABEL_HORIZON_BARS * 4)  # 4h bars × horizon
        train_start = test_start - timedelta(days=train_lookback_days)

        train_start_ms = int(train_start.timestamp() * 1000)
        train_end_ms = int(train_end.timestamp() * 1000)
        test_start_ms = int(test_start.timestamp() * 1000)
        test_end_ms = int(test_end.timestamp() * 1000)

        label = f"[{i+1}/{len(test_starts)}] {test_start.date()}→{test_end.date()}"
        print(f"{label} training...", flush=True)

        # Slice training data
        tmask = (combined["open_time"] >= train_start_ms) & (combined["open_time"] < train_end_ms)
        X_train = combined.loc[tmask, feature_cols]
        y_train = combined.loc[tmask, "label"].astype(int) + 1

        if len(X_train) < min_train_rows:
            results.append(WalkForwardWindow(
                period_start_iso=test_start.isoformat(),
                period_end_iso=test_end.isoformat(),
                train_days=train_lookback_days,
                test_days=test_period_days,
                n_train_rows=len(X_train),
                n_test_trades=0,
                test_metrics_raw={}, test_metrics_killswitch={},
                status="skipped", note=f"only {len(X_train)} train rows (<{min_train_rows})",
            ))
            print(f"    SKIPPED: only {len(X_train)} train rows")
            continue

        # Internal val split: last 15% of train set
        vcut = int(len(X_train) * 0.85)
        X_tr, X_vl = X_train.iloc[:vcut], X_train.iloc[vcut:]
        y_tr, y_vl = y_train.iloc[:vcut], y_train.iloc[vcut:]

        try:
            model = train_model(X_tr, y_tr, X_vl, y_vl)
        except Exception as e:
            results.append(WalkForwardWindow(
                period_start_iso=test_start.isoformat(),
                period_end_iso=test_end.isoformat(),
                train_days=train_lookback_days,
                test_days=test_period_days,
                n_train_rows=len(X_train),
                n_test_trades=0,
                test_metrics_raw={}, test_metrics_killswitch={},
                status="train_failed", note=str(e)[:200],
            ))
            print(f"    TRAIN FAILED: {e}")
            continue

        # Simulate per symbol on test slice
        all_trades = []
        for sym, d in per_symbol.items():
            df = d["df"]
            feats_full = d["features"].fillna(0)
            in_window = (df["open_time"] >= test_start_ms) & (df["open_time"] < test_end_ms)
            if not in_window.any():
                continue
            idx_arr = in_window.to_numpy().nonzero()[0]
            if len(idx_arr) == 0:
                continue
            start_idx = max(WARMUP_BARS, int(idx_arr[0]))
            end_idx = int(idx_arr[-1]) + 1
            if end_idx - start_idx < 30:
                continue
            slice_feats = feats_full.iloc[start_idx:end_idx][feature_cols]
            probs_slice = model.predict_proba(slice_feats.values)
            full_probs = np.tile(np.array([0, 1, 0], dtype=float), (len(df), 1))
            full_probs[start_idx:end_idx] = probs_slice
            trades = simulate_trading(df, feats_full, full_probs, sym, warmup=start_idx)
            trades = [t for t in trades if test_start_ms <= t.entry_time < test_end_ms]
            all_trades.extend(trades)

        metrics_raw = compute_metrics(all_trades)
        kept = apply_kill_switch(all_trades, dd_halt=0.12, cooldown_days=14)
        metrics_ks = compute_metrics(kept)

        results.append(WalkForwardWindow(
            period_start_iso=test_start.isoformat(),
            period_end_iso=test_end.isoformat(),
            train_days=train_lookback_days,
            test_days=test_period_days,
            n_train_rows=len(X_train),
            n_test_trades=len(all_trades),
            test_metrics_raw=metrics_raw,
            test_metrics_killswitch=metrics_ks,
            status="ok",
        ))
        print(f"    trades={len(all_trades)} raw_ann={metrics_raw['annualized']*100:+.2f}% "
              f"raw_DD={metrics_raw['max_dd']*100:.2f}% sharpe={metrics_raw['sharpe']:.2f} | "
              f"ks_ann={metrics_ks['annualized']*100:+.2f}% ks_DD={metrics_ks['max_dd']*100:.2f}%", flush=True)

    return results

def summarize(results: List[WalkForwardWindow]):
    ok = [r for r in results if r.status == "ok" and r.n_test_trades > 0]
    if not ok:
        print("\nNO OK WINDOWS — walk-forward infeasible.")
        return

    # Aggregate
    ann_raws = [r.test_metrics_raw.get("annualized", 0) for r in ok]
    ann_ks = [r.test_metrics_killswitch.get("annualized", 0) for r in ok]
    sharpes_raw = [r.test_metrics_raw.get("sharpe", 0) for r in ok]
    sharpes_ks = [r.test_metrics_killswitch.get("sharpe", 0) for r in ok]
    dds_raw = [r.test_metrics_raw.get("max_dd", 0) for r in ok]
    dds_ks = [r.test_metrics_killswitch.get("max_dd", 0) for r in ok]

    positive_raw = sum(1 for a in ann_raws if a > 0)
    positive_ks = sum(1 for a in ann_ks if a > 0)

    print(f"\n=== WALK-FORWARD AGGREGATE ({len(ok)} windows) ===")
    print(f"{'':<18} {'raw':>12} {'w/ killswitch':>16}")
    print(f"{'mean ann_equity%':<18} {np.mean(ann_raws)*100:>11.2f}% {np.mean(ann_ks)*100:>15.2f}%")
    print(f"{'median ann_equity%':<18} {np.median(ann_raws)*100:>11.2f}% {np.median(ann_ks)*100:>15.2f}%")
    print(f"{'mean sharpe':<18} {np.mean(sharpes_raw):>12.2f} {np.mean(sharpes_ks):>16.2f}")
    print(f"{'mean max DD%':<18} {np.mean(dds_raw)*100:>11.2f}% {np.mean(dds_ks)*100:>15.2f}%")
    print(f"{'windows positive':<18} {positive_raw:>5}/{len(ok):<6} {positive_ks:>9}/{len(ok):<6}")
    print()

    # Per-window table
    header = f"{'start':<12} {'trades':>6} {'raw_ann%':>9} {'raw_DD%':>8} {'raw_Shrp':>9} | {'ks_ann%':>8} {'ks_DD%':>7} {'ks_Shrp':>8}"
    print(header)
    print("-" * len(header))
    for r in ok:
        start = r.period_start_iso[:10]
        m, k = r.test_metrics_raw, r.test_metrics_killswitch
        print(f"{start:<12} {r.n_test_trades:>6} "
              f"{m.get('annualized',0)*100:>+8.2f} {m.get('max_dd',0)*100:>7.2f} {m.get('sharpe',0):>9.2f} | "
              f"{k.get('annualized',0)*100:>+7.2f} {k.get('max_dd',0)*100:>6.2f} {k.get('sharpe',0):>8.2f}")

    # Persist
    results_payload = {
        "windows": [asdict(r) for r in results],
        "aggregate": {
            "n_ok_windows": len(ok),
            "mean_ann_raw": float(np.mean(ann_raws)),
            "mean_ann_killswitch": float(np.mean(ann_ks)),
            "mean_sharpe_raw": float(np.mean(sharpes_raw)),
            "mean_sharpe_killswitch": float(np.mean(sharpes_ks)),
            "mean_dd_raw": float(np.mean(dds_raw)),
            "mean_dd_killswitch": float(np.mean(dds_ks)),
            "positive_windows_raw": positive_raw,
            "positive_windows_killswitch": positive_ks,
        },
    }
    with (ROOT / "walk_forward_results.json").open("w") as f:
        json.dump(results_payload, f, indent=2)
    print(f"\nSaved: walk_forward_results.json")

def main():
    symbols = DEFAULT_SYMBOLS
    print(f"=== ML WALK-FORWARD VALIDATION ===")
    print(f"Symbols:   {symbols}")
    print(f"Train lookback: 365 days  |  Test period: 90 days per window")
    print(f"The question: does rolling retrain fix the 2023 regime shift failure?\n")

    results = run_walk_forward(symbols, test_period_days=90, train_lookback_days=365)
    summarize(results)

if __name__ == "__main__":
    main()
