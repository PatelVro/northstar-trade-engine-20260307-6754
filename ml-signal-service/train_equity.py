"""
train_equity.py — LightGBM classifier trained on US equity 4h bars.

Mirrors train_and_eval.py (crypto) but:
  - Data source is yfinance (free, no API key)
  - Symbol universe is major US equities
  - No funding-rate features (not applicable to stocks)
  - Time features kept (day_of_week, hour_of_day matter for equities)

Output:
  models/lgbm_equity_v1.joblib — serialized trained model
  models/equity_feature_names.json — ordered feature list for serving

Usage:
  python train_equity.py
"""

from __future__ import annotations
import json
import math
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional

import lightgbm as lgb
import numpy as np
import pandas as pd
import yfinance as yf
from sklearn.metrics import classification_report

# --------------------------------------------------------------- CONFIG --

ROOT = Path(__file__).parent
MODELS_DIR = ROOT / "models"
MODEL_PATH = MODELS_DIR / "lgbm_equity_v1.joblib"
FEATURES_PATH = MODELS_DIR / "equity_feature_names.json"

# Major liquid US equities + ETFs. Mix of stocks and index ETFs so the model
# sees both idiosyncratic + broad-market patterns.
DEFAULT_SYMBOLS = [
    "AAPL", "MSFT", "NVDA", "GOOGL", "META", "AMZN", "TSLA",
    "AMD", "AVGO", "NFLX", "SPY", "QQQ",
]

# Match the crypto pipeline's 4h cadence and label horizon so the two models
# are comparable. yfinance doesn't expose 4h directly — we fetch 1h and
# resample to 4h. Equity hours differ from crypto (6.5h/day vs 24/7) so the
# bar count will be lower per calendar day.
INTERVAL_YF = "1h"     # yfinance supports: 1m, 5m, 15m, 30m, 1h, 1d, 1wk, 1mo
RESAMPLE_RULE = "4h"   # 1h -> 4h for parity with crypto model
HISTORY_YEARS = 3      # yfinance hourly cap is 730 days; we'll stay under that
LABEL_HORIZON_BARS = 12
LABEL_THRESH = 0.02    # 2% move defines the up/down class

# Simulation params for backtest (same as crypto so Sharpe etc. are comparable)
WARMUP_BARS = 60
TAKE_PROFIT_PCT = 0.045
STOP_LOSS_PCT = 0.015
MAX_HOLD_BARS = 30
# Equity fees are lower than crypto perps on IBKR fixed plan.
# Assume $0.005/share rebate = ~0.3bps for mid-cap names, plus small spread.
TAKER_FEE_BPS = 1.0
SLIP_BPS = 3.0
RISK_PER_TRADE = 0.0075

# --------------------------------------------------------- DATA FETCH --

def fetch_equity_bars(symbol: str, years: float = HISTORY_YEARS) -> pd.DataFrame:
    """Fetch hourly bars via yfinance and resample to 4h closes."""
    print(f"  fetching {symbol} ({years:.1f}y @ {INTERVAL_YF})...", flush=True)
    # yfinance caps hourly history at ~730 days. Clamp years.
    years = min(years, 2.0)
    t = yf.Ticker(symbol)
    df = t.history(period=f"{int(years * 365)}d", interval=INTERVAL_YF, auto_adjust=False)
    if df.empty:
        return pd.DataFrame()
    # Rename to lowercase for consistency with the crypto pipeline
    df = df.rename(columns=str.lower)[["open", "high", "low", "close", "volume"]]
    df.index.name = "dt"
    # Resample to 4h bars — aggregate properly
    bars = df.resample(RESAMPLE_RULE).agg({
        "open":   "first",
        "high":   "max",
        "low":    "min",
        "close":  "last",
        "volume": "sum",
    }).dropna()
    bars["open_time"] = (bars.index.astype("int64") // 10**6).astype("int64")
    bars = bars.reset_index(drop=True)
    return bars[["open_time", "open", "high", "low", "close", "volume"]]

# ------------------------------------------------------ FEATURE ENG --

def ema(series: pd.Series, period: int) -> pd.Series:
    return series.ewm(span=period, adjust=False).mean()

def rsi(series: pd.Series, period: int) -> pd.Series:
    delta = series.diff()
    gain = delta.clip(lower=0).ewm(alpha=1 / period, adjust=False).mean()
    loss = (-delta.clip(upper=0)).ewm(alpha=1 / period, adjust=False).mean()
    rs = gain / loss.replace(0, np.nan)
    return (100 - 100 / (1 + rs)).fillna(50)

def atr_pct(df: pd.DataFrame, period: int) -> pd.Series:
    high, low, close = df["high"], df["low"], df["close"]
    prev = close.shift(1)
    tr = pd.concat([(high - low).abs(),
                    (high - prev).abs(),
                    (low - prev).abs()], axis=1).max(axis=1)
    return (tr.ewm(alpha=1 / period, adjust=False).mean() / close).fillna(0)

def compute_features(df: pd.DataFrame) -> pd.DataFrame:
    """Equity feature set — no funding rate. Everything else mirrors crypto."""
    f = pd.DataFrame()
    close = df["close"]
    f["return_1"] = close.pct_change(1)
    f["return_3"] = close.pct_change(3)
    f["return_6"] = close.pct_change(6)
    f["return_12"] = close.pct_change(12)
    f["return_24"] = close.pct_change(24)
    f["log_return_1"] = np.log(close / close.shift(1))

    ema5 = ema(close, 5)
    ema20 = ema(close, 20)
    ema50 = ema(close, 50)
    f["ema_5_vs_20"] = (ema5 - ema20) / close
    f["ema_20_vs_50"] = (ema20 - ema50) / close
    f["price_vs_ema20"] = (close - ema20) / close
    f["price_vs_ema50"] = (close - ema50) / close

    ema12 = ema(close, 12)
    ema26 = ema(close, 26)
    macd_line = ema12 - ema26
    macd_sig = ema(macd_line, 9)
    f["macd"] = macd_line / close
    f["macd_signal"] = macd_sig / close
    f["macd_hist"] = (macd_line - macd_sig) / close

    f["rsi_7"] = rsi(close, 7)
    f["rsi_14"] = rsi(close, 14)
    f["rsi_7_dist"] = (f["rsi_7"] - 50) / 50

    returns = np.log(close / close.shift(1))
    f["vol_10"] = returns.rolling(10).std()
    f["vol_20"] = returns.rolling(20).std()
    f["vol_ratio_10_20"] = f["vol_10"] / f["vol_20"].replace(0, np.nan)
    f["atr_14_pct"] = atr_pct(df, 14)

    rng = (df["high"] - df["low"]) / close
    f["bar_range_pct"] = rng
    f["bar_range_avg_20"] = rng.rolling(20).mean()
    f["bar_range_ratio"] = rng / f["bar_range_avg_20"].replace(0, np.nan)

    vol_avg = df["volume"].rolling(20).mean()
    f["volume_spike"] = df["volume"] / vol_avg.replace(0, np.nan)
    f["dollar_volume_log"] = np.log1p(df["volume"] * close)

    f["up_bar_hit_rate_20"] = (returns > 0).rolling(20).mean()
    f["up_bar_hit_rate_10"] = (returns > 0).rolling(10).mean()
    f["distance_from_mean_20"] = (close - close.rolling(20).mean()) / close.rolling(20).std()
    f["price_zscore_20"] = f["distance_from_mean_20"]
    f["return_zscore_20"] = (returns - returns.rolling(20).mean()) / returns.rolling(20).std()
    signs = np.sign(returns)
    f["trend_consistency_20"] = signs.rolling(20).mean()
    f["close_location_in_bar"] = (df["close"] - df["low"]) / (df["high"] - df["low"]).replace(0, np.nan)

    # Equities have strong time-of-day effects (open volatility vs midday lull)
    dt = pd.to_datetime(df["open_time"], unit="ms", utc=True)
    f["hour_of_day"] = dt.dt.hour
    f["day_of_week"] = dt.dt.dayofweek

    return f

def make_labels(close: pd.Series, horizon: int, thresh: float) -> pd.Series:
    fwd = close.shift(-horizon) / close - 1
    lbl = pd.Series(0, index=close.index, dtype="int8")
    lbl[fwd > thresh] = 1
    lbl[fwd < -thresh] = -1
    return lbl

# --------------------------------------------------------- TRAIN --

def train_model(X_train, y_train, X_val=None, y_val=None):
    counts = y_train.value_counts()
    total = len(y_train)
    weights = {cls: total / (3.0 * counts.get(cls, 1)) for cls in [0, 1, 2]}
    sample_weight = y_train.map(weights).values

    model = lgb.LGBMClassifier(
        objective="multiclass",
        num_class=3,
        n_estimators=500,
        learning_rate=0.03,
        num_leaves=31,
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

# --------------------------------------------------------- MAIN --

def main():
    symbols = DEFAULT_SYMBOLS
    print(f"=== EQUITY ML TRAINING ===")
    print(f"Symbols:       {symbols}")
    print(f"Interval:      {RESAMPLE_RULE} (resampled from {INTERVAL_YF})")
    print(f"History:       {HISTORY_YEARS}y (capped at yfinance's hourly limit)")
    print(f"Label horizon: {LABEL_HORIZON_BARS} bars forward")
    print()

    # Fetch + combine
    combined_rows = []
    for sym in symbols:
        df = fetch_equity_bars(sym)
        if df.empty or len(df) < 200:
            print(f"    {sym}: insufficient data ({len(df)} bars) — skipping")
            continue
        feats = compute_features(df)
        labels = make_labels(df["close"], LABEL_HORIZON_BARS, LABEL_THRESH)
        row = feats.copy()
        row["symbol"] = sym
        row["open_time"] = df["open_time"].values
        row["close"] = df["close"].values
        row["label"] = labels.values
        combined_rows.append(row)
        print(f"    {sym}: {len(df)} bars, {len(row)} features")

    if not combined_rows:
        print("ERROR: no data. Check yfinance connectivity.")
        sys.exit(1)

    combined = pd.concat(combined_rows).reset_index(drop=True).dropna()
    combined = combined.sort_values("open_time").reset_index(drop=True)
    feature_cols = [c for c in combined.columns if c not in ("symbol", "open_time", "close", "label")]
    print(f"\nTotal training rows: {len(combined)}")
    print(f"Label distribution:\n{combined['label'].value_counts()}\n")

    # Time-based split: 70% train, 15% val, 15% test
    n = len(combined)
    train_end = int(n * 0.70)
    val_end = int(n * 0.85)
    X_train = combined.iloc[:train_end][feature_cols]
    y_train = combined.iloc[:train_end]["label"].astype(int) + 1
    X_val = combined.iloc[train_end:val_end][feature_cols]
    y_val = combined.iloc[train_end:val_end]["label"].astype(int) + 1
    X_test = combined.iloc[val_end:][feature_cols]
    y_test = combined.iloc[val_end:]["label"].astype(int) + 1

    print(f"Train: {len(X_train)}  Val: {len(X_val)}  Test: {len(X_test)}")

    print("Training LightGBM...")
    model = train_model(X_train, y_train, X_val, y_val)

    print("\nValidation classification report:")
    print(classification_report(y_val, model.predict(X_val), target_names=["down", "flat", "up"], zero_division=0))

    print("\nTest classification report (OOS):")
    print(classification_report(y_test, model.predict(X_test), target_names=["down", "flat", "up"], zero_division=0))

    # Feature importance — what does the model look at?
    importances = pd.Series(model.feature_importances_, index=feature_cols).sort_values(ascending=False)
    print("\nTop 15 feature importances:")
    for name, val in importances.head(15).items():
        print(f"  {name:<30} {val}")

    # Retrain on full data for production and persist
    print(f"\nRetraining on full dataset ({len(combined)} rows) for production model...")
    y_full = combined["label"].astype(int) + 1
    prod_model = train_model(combined[feature_cols], y_full)

    MODELS_DIR.mkdir(exist_ok=True)
    import joblib
    joblib.dump(prod_model, MODEL_PATH)
    with FEATURES_PATH.open("w") as f:
        json.dump(feature_cols, f, indent=2)
    print(f"\nSaved: {MODEL_PATH}")
    print(f"Saved: {FEATURES_PATH}")
    print(f"Model features: {len(feature_cols)}")

if __name__ == "__main__":
    main()
