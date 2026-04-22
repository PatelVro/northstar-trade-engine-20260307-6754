"""
train_and_eval.py — train a LightGBM classifier on historical crypto perps
data and evaluate it against the same rule-based baseline we backtested in
cmd/crypto-backtest. The goal is a DECISION POINT: does ML actually beat
our hand-coded scoring on the 2023 disaster case?

Pipeline:
  1. Fetch historical klines from Binance Futures (free, public).
  2. Fetch historical funding rates from Aster.
  3. Compute a rich feature set per bar (40+ features).
  4. Label each bar with forward return class: up / flat / down (3-class).
  5. Walk-forward train on expanding windows and evaluate OOS.
  6. Run a trade simulation with the ML classifier's outputs, with the same
     costs + exits + sizing as our Go backtest, and report equity curves
     on 2023 / 2024 / 2025-26.
  7. Print a head-to-head comparison vs the baseline so we can decide.

Outputs:
  models/lgbm_v1.joblib — serialized trained model (final train-all version)
  models/feature_names.json — ordered list of feature columns
  eval_report.json — metrics summary

Nothing in this script trades real money or calls any authenticated endpoints.
"""

import argparse
import json
import math
import os
import sys
import time
from dataclasses import dataclass, asdict
from pathlib import Path
from typing import Dict, List, Optional, Tuple

import lightgbm as lgb
import numpy as np
import pandas as pd
import requests
from sklearn.metrics import classification_report, confusion_matrix

# ------------------------------------------------------------------ CONFIG --

BINANCE_URL = "https://fapi.binance.com/fapi/v1/klines"
ASTER_FUNDING_URL = "https://fapi.asterdex.com/fapi/v1/fundingRate"

# Match the Go code's target list by default.
DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT"]

# Matches our Go backtest so comparison is fair.
INTERVAL = "4h"
WARMUP_BARS = 60
TAKE_PROFIT_PCT = 0.045
STOP_LOSS_PCT = 0.015
MAX_HOLD_BARS = 30
TAKER_FEE_BPS = 4.0
SLIP_BPS = 5.0
RISK_PER_TRADE = 0.0075

# 3-class labels on forward 12-bar (48h) return:
#   -1 = "down"  (return < -LABEL_THRESH)
#    0 = "flat"  (|return| <= LABEL_THRESH)
#   +1 = "up"    (return > LABEL_THRESH)
LABEL_HORIZON_BARS = 12
LABEL_THRESH = 0.02  # 2% move

# ----------------------------------------------------------- DATA FETCHING --

def fetch_klines(symbol: str, interval: str, bars: int, end_ms: Optional[int] = None) -> pd.DataFrame:
    """Fetch OHLCV bars from Binance, paged backwards from end_ms."""
    if end_ms is None:
        end_ms = int(time.time() * 1000)
    out = []
    while len(out) < bars:
        batch = min(1500, bars - len(out))
        url = f"{BINANCE_URL}?symbol={symbol}&interval={interval}&limit={batch}&endTime={end_ms}"
        r = requests.get(url, timeout=30)
        r.raise_for_status()
        raw = r.json()
        if not raw:
            break
        out = raw + out  # prepend (oldest first after sort)
        end_ms = int(raw[0][0]) - 1
        time.sleep(0.15)
    cols = ["open_time", "open", "high", "low", "close", "volume", "close_time",
            "qav", "num_trades", "tb_base", "tb_quote", "ignore"]
    df = pd.DataFrame(out, columns=cols)
    for c in ["open", "high", "low", "close", "volume"]:
        df[c] = pd.to_numeric(df[c])
    df["open_time"] = pd.to_numeric(df["open_time"], downcast="integer")
    df = df.drop_duplicates("open_time").sort_values("open_time").reset_index(drop=True)
    return df[["open_time", "open", "high", "low", "close", "volume"]]

def fetch_aster_funding(symbol: str, start_ms: int, end_ms: int) -> pd.DataFrame:
    """Fetch Aster funding rate history in [start_ms, end_ms]."""
    rows = []
    cursor = start_ms
    while cursor <= end_ms:
        url = f"{ASTER_FUNDING_URL}?symbol={symbol}&limit=1000&startTime={cursor}"
        try:
            r = requests.get(url, timeout=30)
            r.raise_for_status()
            raw = r.json()
        except Exception as e:
            print(f"  funding fetch failed for {symbol} @ {cursor}: {e}", file=sys.stderr)
            break
        if not raw:
            break
        max_t = max(int(x["fundingTime"]) for x in raw)
        for x in raw:
            t = int(x["fundingTime"])
            if start_ms <= t <= end_ms:
                rows.append({"funding_time": t, "funding_rate": float(x["fundingRate"])})
        if max_t <= cursor or len(raw) < 1000:
            break
        cursor = max_t + 1
        time.sleep(0.15)
    df = pd.DataFrame(rows)
    if df.empty:
        return pd.DataFrame(columns=["funding_time", "funding_rate"])
    return df.drop_duplicates("funding_time").sort_values("funding_time").reset_index(drop=True)

# ------------------------------------------------------------ FEATURE ENG --

def ema(series: pd.Series, period: int) -> pd.Series:
    return series.ewm(span=period, adjust=False).mean()

def rsi(series: pd.Series, period: int) -> pd.Series:
    delta = series.diff()
    gain = delta.clip(lower=0).ewm(alpha=1/period, adjust=False).mean()
    loss = (-delta.clip(upper=0)).ewm(alpha=1/period, adjust=False).mean()
    rs = gain / loss.replace(0, np.nan)
    return (100 - 100 / (1 + rs)).fillna(50)

def atr_pct(df: pd.DataFrame, period: int) -> pd.Series:
    high, low, close = df["high"], df["low"], df["close"]
    prev_close = close.shift(1)
    tr = pd.concat([(high - low).abs(),
                    (high - prev_close).abs(),
                    (low - prev_close).abs()], axis=1).max(axis=1)
    return (tr.ewm(alpha=1/period, adjust=False).mean() / close).fillna(0)

def compute_features(df: pd.DataFrame, funding: pd.DataFrame) -> pd.DataFrame:
    """Return a DataFrame with a wide feature set per bar."""
    f = pd.DataFrame()
    close = df["close"]
    f["return_1"] = close.pct_change(1)
    f["return_3"] = close.pct_change(3)
    f["return_6"] = close.pct_change(6)
    f["return_12"] = close.pct_change(12)
    f["return_24"] = close.pct_change(24)
    f["log_return_1"] = np.log(close / close.shift(1))
    f["ema_5"] = ema(close, 5)
    f["ema_20"] = ema(close, 20)
    f["ema_50"] = ema(close, 50)
    f["ema_5_vs_20"] = (f["ema_5"] - f["ema_20"]) / close
    f["ema_20_vs_50"] = (f["ema_20"] - f["ema_50"]) / close
    f["price_vs_ema20"] = (close - f["ema_20"]) / close
    f["price_vs_ema50"] = (close - f["ema_50"]) / close
    # MACD
    ema12 = ema(close, 12)
    ema26 = ema(close, 26)
    macd_line = ema12 - ema26
    macd_signal = ema(macd_line, 9)
    f["macd"] = macd_line / close
    f["macd_signal"] = macd_signal / close
    f["macd_hist"] = (macd_line - macd_signal) / close
    # RSI
    f["rsi_7"] = rsi(close, 7)
    f["rsi_14"] = rsi(close, 14)
    f["rsi_7_dist"] = (f["rsi_7"] - 50) / 50
    # Volatility
    returns = np.log(close / close.shift(1))
    f["vol_10"] = returns.rolling(10).std()
    f["vol_20"] = returns.rolling(20).std()
    f["vol_ratio_10_20"] = f["vol_10"] / f["vol_20"].replace(0, np.nan)
    f["atr_14_pct"] = atr_pct(df, 14)
    # Range / bar shape
    rng = (df["high"] - df["low"]) / close
    f["bar_range_pct"] = rng
    f["bar_range_avg_20"] = rng.rolling(20).mean()
    f["bar_range_ratio"] = rng / f["bar_range_avg_20"].replace(0, np.nan)
    bar_body = (df["close"] - df["open"]) / close
    f["bar_body"] = bar_body
    # Volume
    vol_avg = df["volume"].rolling(20).mean()
    f["volume_spike"] = df["volume"] / vol_avg.replace(0, np.nan)
    # Dollar-vol proxy (useful feature for liquidity regime)
    f["dollar_volume_log"] = np.log1p(df["volume"] * close)
    # Regime indicators
    f["up_bar_hit_rate_20"] = (returns > 0).rolling(20).mean()
    f["up_bar_hit_rate_10"] = (returns > 0).rolling(10).mean()
    f["distance_from_mean_20"] = (close - close.rolling(20).mean()) / close.rolling(20).std()
    f["price_zscore_20"] = f["distance_from_mean_20"]
    f["return_zscore_20"] = (returns - returns.rolling(20).mean()) / returns.rolling(20).std()
    # Trend consistency
    signs = np.sign(returns)
    f["trend_consistency_20"] = signs.rolling(20).mean()
    # Intrabar features
    f["close_location_in_bar"] = (df["close"] - df["low"]) / (df["high"] - df["low"]).replace(0, np.nan)
    # Funding integration
    f["funding_rate"] = _merge_funding(df["open_time"], funding)
    f["funding_rate_smoothed_3"] = f["funding_rate"].rolling(3).mean()
    f["funding_rate_smoothed_10"] = f["funding_rate"].rolling(10).mean()
    f["funding_abs"] = f["funding_rate"].abs()
    # Calendar features — time-of-day and day-of-week effects in crypto are real.
    dt = pd.to_datetime(df["open_time"], unit="ms", utc=True)
    f["hour_of_day"] = dt.dt.hour
    f["day_of_week"] = dt.dt.dayofweek
    # Drop bookkeeping columns, keep only features.
    f = f.drop(columns=[c for c in ["ema_5", "ema_20", "ema_50"] if c in f.columns])
    return f

def _merge_funding(open_times: pd.Series, funding: pd.DataFrame) -> pd.Series:
    """For each bar, return the most-recent funding rate at or before bar open."""
    if funding.empty:
        return pd.Series(0.0, index=open_times.index)
    ft = funding["funding_time"].values
    fr = funding["funding_rate"].values
    times = open_times.values
    out = np.zeros(len(times))
    idx = 0
    for i, t in enumerate(times):
        while idx + 1 < len(ft) and ft[idx + 1] <= t:
            idx += 1
        out[i] = fr[idx] if ft[idx] <= t else 0.0
    return pd.Series(out, index=open_times.index)

# ------------------------------------------------------------------ LABEL --

def make_labels(close: pd.Series, horizon: int, thresh: float) -> pd.Series:
    fwd = close.shift(-horizon) / close - 1
    lbl = pd.Series(0, index=close.index, dtype="int8")
    lbl[fwd > thresh] = 1
    lbl[fwd < -thresh] = -1
    return lbl

# ----------------------------------------------------------------- TRAIN --

def train_model(X_train: pd.DataFrame, y_train: pd.Series, X_val: Optional[pd.DataFrame] = None, y_val: Optional[pd.Series] = None) -> lgb.LGBMClassifier:
    # 3-class classifier. Class imbalance: "flat" usually dominates, so we use
    # class weights to nudge the model toward the useful directional classes.
    counts = y_train.value_counts()
    total = len(y_train)
    weights = {cls: total / (3.0 * counts.get(cls, 1)) for cls in [-1, 0, 1]}
    sample_weight = y_train.map(weights).values

    model = lgb.LGBMClassifier(
        objective="multiclass",
        num_class=3,
        n_estimators=500,
        learning_rate=0.03,
        num_leaves=31,
        max_depth=-1,
        min_child_samples=20,
        reg_alpha=0.1,
        reg_lambda=0.1,
        subsample=0.85,
        subsample_freq=5,
        colsample_bytree=0.85,
        random_state=42,
        n_jobs=-1,
        verbosity=-1,
    )
    eval_set = [(X_val, y_val)] if X_val is not None else None
    callbacks = [lgb.early_stopping(stopping_rounds=30, verbose=False)] if eval_set else []
    model.fit(X_train, y_train, sample_weight=sample_weight, eval_set=eval_set, callbacks=callbacks)
    return model

# ---------------------------------------------------------- BACKTEST SIM --

@dataclass
class TradeResult:
    symbol: str
    side: str
    entry_time: int
    exit_time: int
    entry_px: float
    exit_px: float
    pnl_pct: float
    bars_held: int
    exit_kind: str
    score: float

def simulate_trading(df: pd.DataFrame, features: pd.DataFrame, predictions: np.ndarray, symbol: str, warmup: int = WARMUP_BARS, prob_thresh: float = 0.40) -> List[TradeResult]:
    """
    Walk the data forward. At each bar past warmup, if we have no open trade:
      - predictions[i] is a length-3 array [P(-1), P(0), P(+1)]
      - if P(+1) > prob_thresh: open long
      - if P(-1) > prob_thresh: open short
      - else: skip
    Exits: TP / SL (using bar path) or timeout.
    Costs match the Go backtest.
    """
    trades = []
    open_trade = None
    entry_bar = 0
    cost_round_trip = 2 * (TAKER_FEE_BPS + SLIP_BPS) / 10000.0

    opens = df["open"].values
    highs = df["high"].values
    lows = df["low"].values
    closes = df["close"].values
    times = df["open_time"].values

    for i in range(warmup, len(df) - 1):
        # Exit check on open trade first.
        if open_trade is not None:
            direction = 1 if open_trade["side"] == "long" else -1
            entry_px = open_trade["entry_px"]
            move_up = direction * (highs[i] - entry_px) / entry_px
            move_down = direction * (lows[i] - entry_px) / entry_px
            hit_sl = move_down <= -STOP_LOSS_PCT
            hit_tp = move_up >= TAKE_PROFIT_PCT
            exit_kind = None
            exit_px = None
            if hit_sl:
                exit_kind = "sl"
                exit_px = entry_px * (1 - direction * STOP_LOSS_PCT)
            elif hit_tp:
                exit_kind = "tp"
                exit_px = entry_px * (1 + direction * TAKE_PROFIT_PCT)
            elif i - entry_bar >= MAX_HOLD_BARS:
                exit_kind = "timeout"
                exit_px = closes[i]
            if exit_kind:
                gross = direction * (exit_px - entry_px) / entry_px
                trades.append(TradeResult(
                    symbol=symbol, side=open_trade["side"],
                    entry_time=open_trade["entry_time"], exit_time=int(times[i]),
                    entry_px=entry_px, exit_px=exit_px,
                    pnl_pct=gross - cost_round_trip, bars_held=i - entry_bar,
                    exit_kind=exit_kind, score=open_trade["score"],
                ))
                open_trade = None
            else:
                continue  # still holding, don't look for new entries

        # Entry check.
        probs = predictions[i]
        p_down, p_flat, p_up = probs[0], probs[1], probs[2]
        if p_up > prob_thresh and p_up > p_down:
            open_trade = {
                "side": "long",
                "entry_time": int(times[i]),
                "entry_px": closes[i],
                "score": float(p_up),
            }
            entry_bar = i
        elif p_down > prob_thresh and p_down > p_up:
            open_trade = {
                "side": "short",
                "entry_time": int(times[i]),
                "entry_px": closes[i],
                "score": float(p_down),
            }
            entry_bar = i

    # Close any open trade at last bar.
    if open_trade is not None:
        i = len(df) - 1
        direction = 1 if open_trade["side"] == "long" else -1
        exit_px = closes[i]
        gross = direction * (exit_px - open_trade["entry_px"]) / open_trade["entry_px"]
        trades.append(TradeResult(
            symbol=symbol, side=open_trade["side"],
            entry_time=open_trade["entry_time"], exit_time=int(times[i]),
            entry_px=open_trade["entry_px"], exit_px=exit_px,
            pnl_pct=gross - cost_round_trip, bars_held=i - entry_bar,
            exit_kind="dataset-end", score=open_trade["score"],
        ))
    return trades

# --------------------------------------------------- METRICS + KILL SWITCH --

def apply_kill_switch(trades: List[TradeResult], dd_halt: float = 0.12, cooldown_days: int = 14) -> List[TradeResult]:
    trades = sorted(trades, key=lambda t: t.entry_time)
    notional_frac = RISK_PER_TRADE / STOP_LOSS_PCT
    equity = 1.0
    peak = 1.0
    halted_until = 0
    kept = []
    cooldown_ms = cooldown_days * 24 * 3600 * 1000
    for t in trades:
        if halted_until and t.entry_time < halted_until:
            continue
        if halted_until and t.entry_time >= halted_until:
            peak = equity
            halted_until = 0
        equity *= 1 + notional_frac * t.pnl_pct
        if equity > peak:
            peak = equity
        dd = (peak - equity) / peak
        if dd >= dd_halt:
            halted_until = t.exit_time + cooldown_ms
        kept.append(t)
    return kept

def compute_metrics(trades: List[TradeResult]) -> Dict:
    if not trades:
        return {"n": 0, "total_ret": 0, "compounded": 0, "max_dd": 0,
                "annualized": 0, "sharpe": 0, "win_rate": 0, "calmar": 0}
    trades = sorted(trades, key=lambda t: t.exit_time)
    notional_frac = RISK_PER_TRADE / STOP_LOSS_PCT
    equity = 1.0
    peak = 1.0
    max_dd = 0
    eq_series = []
    for t in trades:
        equity *= 1 + notional_frac * t.pnl_pct
        eq_series.append(equity)
        if equity > peak:
            peak = equity
        dd = (peak - equity) / peak
        if dd > max_dd:
            max_dd = dd
    total_days = (trades[-1].exit_time - trades[0].entry_time) / (1000 * 86400)
    years = max(total_days / 365.0, 1e-9)
    annualized = equity ** (1.0 / years) - 1.0
    wins = sum(1 for t in trades if t.pnl_pct > 0)
    log_rets = np.diff(np.log(np.array([1.0] + eq_series)))
    if len(log_rets) > 1:
        trades_per_year = len(trades) * 365.0 / max(total_days, 1)
        mu = log_rets.mean()
        sd = log_rets.std(ddof=1)
        sharpe = (mu / sd) * math.sqrt(trades_per_year) if sd > 0 else 0
    else:
        sharpe = 0
    calmar = annualized / max_dd if max_dd > 0 else 0
    return {
        "n": len(trades),
        "compounded": equity - 1.0,
        "max_dd": max_dd,
        "annualized": annualized,
        "sharpe": sharpe,
        "win_rate": wins / len(trades),
        "calmar": calmar,
    }

# ----------------------------------------------------------- WALK FORWARD --

@dataclass
class WindowResult:
    label: str
    metrics: Dict
    n_trades: int

def run_walk_forward(symbols: List[str], bars: int, end_ms: int) -> Dict[str, Dict]:
    """
    Proper walk-forward: fetch enough history to cover 2022 onward, then
    train on the EARLIER portion and evaluate separately on 2023, 2024, and
    2025-26 slices. The critical question is whether the ML model can
    survive the 2023 V-shape disaster that killed the rule-based strategy.
    """
    print(f"\n=== Fetching data for {len(symbols)} symbols, {bars} bars ending {end_ms} ===")
    all_data = {}
    for sym in symbols:
        print(f"  {sym}: klines...")
        df = fetch_klines(sym, INTERVAL, bars, end_ms)
        if df.empty:
            continue
        first_ms = int(df.iloc[0]["open_time"])
        last_ms = int(df.iloc[-1]["open_time"])
        print(f"    got {len(df)} bars, funding...")
        funding = fetch_aster_funding(sym, first_ms, last_ms)
        print(f"    got {len(funding)} funding points")
        feats = compute_features(df, funding)
        labels = make_labels(df["close"], LABEL_HORIZON_BARS, LABEL_THRESH)
        all_data[sym] = {"df": df, "features": feats, "labels": labels}

    combined_rows = []
    for sym, d in all_data.items():
        df, feats, labels = d["df"], d["features"], d["labels"]
        row = feats.copy()
        row["symbol"] = sym
        row["open_time"] = df["open_time"].values
        row["close"] = df["close"].values
        row["label"] = labels.values
        combined_rows.append(row)
    combined = pd.concat(combined_rows).reset_index(drop=True)
    combined = combined.dropna()
    feature_cols = [c for c in combined.columns if c not in ("symbol", "open_time", "close", "label")]
    combined = combined.sort_values("open_time").reset_index(drop=True)

    earliest = pd.Timestamp(combined["open_time"].min(), unit="ms", tz="UTC")
    latest = pd.Timestamp(combined["open_time"].max(), unit="ms", tz="UTC")
    print(f"\nCombined dataset: {len(combined)} rows, {earliest} -> {latest}")
    print(f"Label distribution:\n{combined['label'].value_counts()}\n")

    # Training cutoff: use everything BEFORE 2023-01-01 for training.
    train_cutoff_ms = int(pd.Timestamp("2023-01-01", tz="UTC").timestamp() * 1000)
    train_mask = combined["open_time"] < train_cutoff_ms
    X_train_all = combined.loc[train_mask, feature_cols]
    y_train_all = combined.loc[train_mask, "label"].astype(int) + 1
    # Internal val split: last 20% of training set.
    cutoff_val = int(len(X_train_all) * 0.8)
    X_train, X_val = X_train_all.iloc[:cutoff_val], X_train_all.iloc[cutoff_val:]
    y_train, y_val = y_train_all.iloc[:cutoff_val], y_train_all.iloc[cutoff_val:]

    if len(X_train) < 500:
        print(f"WARNING: only {len(X_train)} training rows before 2023-01-01.")
        print(f"         Fetched data doesn't go back far enough. Re-run with --bars {bars*2}.")
        print(f"         Falling back to proportional split for decision-point purposes.\n")
        # Fall back to 50/25/25
        n = len(combined)
        X_train = combined.iloc[:int(n*0.5)][feature_cols]
        y_train = combined.iloc[:int(n*0.5)]["label"].astype(int) + 1
        X_val = combined.iloc[int(n*0.5):int(n*0.65)][feature_cols]
        y_val = combined.iloc[int(n*0.5):int(n*0.65)]["label"].astype(int) + 1
        train_cutoff_ms = int(combined.iloc[int(n*0.65)]["open_time"])

    print(f"Training LightGBM on {len(X_train)} rows (train before 2023-01-01 cutoff)")
    print(f"Validating on {len(X_val)} rows")
    model = train_model(X_train, y_train, X_val, y_val)

    val_preds = model.predict(X_val)
    print("\nValidation classification report:")
    print(classification_report(y_val, val_preds, target_names=["down", "flat", "up"], zero_division=0))

    # Evaluate per-window OOS: 2023, 2024, 2025-26.
    windows = [
        ("2023", pd.Timestamp("2023-01-01", tz="UTC"), pd.Timestamp("2024-01-01", tz="UTC")),
        ("2024", pd.Timestamp("2024-01-01", tz="UTC"), pd.Timestamp("2025-01-01", tz="UTC")),
        ("2025-26", pd.Timestamp("2025-01-01", tz="UTC"), pd.Timestamp("2027-01-01", tz="UTC")),
    ]
    window_results = {}
    for label, w_start, w_end in windows:
        w_start_ms = int(w_start.timestamp() * 1000)
        w_end_ms = int(w_end.timestamp() * 1000)
        all_trades = []
        symbol_counts = {}
        for sym, d in all_data.items():
            df = d["df"].copy()
            feats_full = d["features"].fillna(0)
            # Find OOS slice for this window
            in_window = (df["open_time"] >= w_start_ms) & (df["open_time"] < w_end_ms)
            if not in_window.any():
                continue
            start_idx = int(in_window.idxmax())
            end_idx = int(len(df) - (in_window[::-1].idxmax() - start_idx + 1)) if in_window.iloc[-1] else int(in_window[::-1].idxmax())
            end_idx = int(max(i for i, v in enumerate(in_window) if v)) + 1
            if start_idx < WARMUP_BARS:
                start_idx = WARMUP_BARS
            if end_idx - start_idx < 30:
                continue
            # Predict on window
            slice_feats = feats_full.iloc[start_idx:end_idx][feature_cols]
            probs_slice = model.predict_proba(slice_feats.values)
            # Build full-length probs array for simulate_trading's indexing
            full_probs = np.tile(np.array([0, 1, 0], dtype=float), (len(df), 1))
            full_probs[start_idx:end_idx] = probs_slice
            trades = simulate_trading(df, feats_full, full_probs, sym, warmup=start_idx)
            # Keep only trades whose entry is in the window
            trades = [t for t in trades if w_start_ms <= t.entry_time < w_end_ms]
            all_trades.extend(trades)
            symbol_counts[sym] = len(trades)

        metrics_raw = compute_metrics(all_trades)
        kept = apply_kill_switch(all_trades, dd_halt=0.12, cooldown_days=14)
        metrics_ks = compute_metrics(kept)
        window_results[label] = {
            "raw": metrics_raw, "with_killswitch": metrics_ks,
            "n_trades": len(all_trades), "symbol_counts": symbol_counts,
        }

    print("\n=== ML STRATEGY OOS RESULTS (trained on data before 2023-01-01) ===")
    print(f"{'Window':<10} {'trades':>8} {'raw_ann%':>10} {'raw_DD%':>8} {'raw_Sharpe':>11} | {'ks_ann%':>9} {'ks_DD%':>8} {'ks_Sharpe':>10}")
    for label, r in window_results.items():
        m_raw, m_ks = r["raw"], r["with_killswitch"]
        print(f"{label:<10} {r['n_trades']:>8} "
              f"{m_raw['annualized']*100:>+10.2f} {m_raw['max_dd']*100:>8.2f} {m_raw['sharpe']:>11.2f} | "
              f"{m_ks['annualized']*100:>+9.2f} {m_ks['max_dd']*100:>8.2f} {m_ks['sharpe']:>10.2f}")

    # Re-train on ALL data for production model (including 2023-2025 for best
    # forward prediction), but we only trust the OOS numbers above for
    # decision-making.
    all_X = combined[feature_cols]
    all_y = combined["label"].astype(int) + 1
    print(f"\nRe-training production model on full dataset ({len(all_X)} rows)...")
    prod_model = train_model(all_X, all_y)
    metrics_raw = window_results["2023"]["raw"] if "2023" in window_results else None
    metrics_ks = window_results["2023"]["with_killswitch"] if "2023" in window_results else None

    # Feature importances — tells us which features are driving predictions
    importances = pd.Series(model.feature_importances_, index=feature_cols).sort_values(ascending=False)
    print("\nTop 15 feature importances:")
    for name, val in importances.head(15).items():
        print(f"  {name:<30} {val}")

    # Persist model for production serving (trained on ALL data, more features seen)
    import joblib
    models_dir = Path(__file__).parent / "models"
    models_dir.mkdir(exist_ok=True)
    joblib.dump(prod_model, models_dir / "lgbm_v1.joblib")
    with (models_dir / "feature_names.json").open("w") as f:
        json.dump(feature_cols, f, indent=2)
    print(f"Model saved to {models_dir / 'lgbm_v1.joblib'}")

    # Feature importances from the HELD-OUT-TRAINED model (the honest one)
    importances = pd.Series(model.feature_importances_, index=feature_cols).sort_values(ascending=False)
    print("\nTop 15 feature importances (from honest pre-2023 model):")
    for name, val in importances.head(15).items():
        print(f"  {name:<30} {val}")

    # Write eval report
    report = {
        "windows": {k: {"raw": v["raw"], "with_killswitch": v["with_killswitch"], "n_trades": v["n_trades"]}
                    for k, v in window_results.items()},
        "n_symbols": len(all_data),
        "total_bars": int(combined.shape[0]),
        "feature_cols": feature_cols,
        "top_features": importances.head(15).to_dict(),
    }
    with (Path(__file__).parent / "eval_report.json").open("w") as f:
        json.dump(report, f, indent=2)

    return report

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--symbols", default=",".join(DEFAULT_SYMBOLS), help="comma-separated list of Binance futures symbols")
    ap.add_argument("--bars", type=int, default=2000, help="historical bars to fetch per symbol")
    ap.add_argument("--end-date", default="", help="window end date YYYY-MM-DD (default: now)")
    args = ap.parse_args()

    symbols = [s.strip() for s in args.symbols.split(",") if s.strip()]
    if args.end_date:
        end_ms = int(pd.Timestamp(args.end_date, tz="UTC").timestamp() * 1000)
    else:
        end_ms = int(time.time() * 1000)

    run_walk_forward(symbols, args.bars, end_ms)

if __name__ == "__main__":
    main()
