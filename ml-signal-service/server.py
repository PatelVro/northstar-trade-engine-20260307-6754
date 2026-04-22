"""
server.py — HTTP service for ML-driven trade signal scoring.

The Go trader POSTs a feature payload per candidate symbol/bar; the service
returns a probability distribution over {down, flat, up}. This lets the Go
execution layer stay in Go while the ML inference runs in Python with
LightGBM (where the ecosystem is mature).

The service is stateless per-request. Models are loaded once at startup.
A background rolling-retrain thread refreshes the model periodically using
fresh historical data (configurable, default 24h between retrains once
enough new data has accumulated).

Endpoints:
  GET  /health                 — liveness + model metadata
  POST /signal                 — score a single bar's feature vector
  GET  /model/info             — current model version + feature list
  POST /model/reload           — force reload from disk (after external retrain)

Usage:
  python server.py             # listens on 127.0.0.1:9091 by default
  python server.py --port 9100 # custom port
  python server.py --no-retrain # disable background retrain (useful in tests)

Deployment: intended to run as a sidecar alongside northstar.exe. Both on
localhost, communicating via HTTP. The Go client falls back to rule-based
scoring if this service is unreachable.
"""

from __future__ import annotations

import argparse
import json
import logging
import threading
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import List, Optional

import joblib
import numpy as np
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import uvicorn

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("ml-signal")

ROOT = Path(__file__).parent

# The sidecar can serve different models for different markets. The default
# is the crypto-trained LightGBM; equity deployments set MODEL_KIND=equity
# (or override MODEL_PATH directly) so they load the equity-specific model.
# This way we keep one Python entrypoint and configure variants purely by env.
import os as _os
_MODEL_KIND = (_os.environ.get("MODEL_KIND") or "crypto").lower()
if _MODEL_KIND == "equity":
    _default_model = "lgbm_equity_v1.joblib"
    _default_features = "equity_feature_names.json"
else:
    _default_model = "lgbm_v1.joblib"
    _default_features = "feature_names.json"

MODEL_PATH = Path(_os.environ.get("MODEL_PATH") or (ROOT / "models" / _default_model))
FEATURE_NAMES_PATH = Path(_os.environ.get("FEATURE_NAMES_PATH") or (ROOT / "models" / _default_features))

# -------------------------------------------------------------- Model state --

class ModelState:
    """Thread-safe holder for the current model + metadata.

    We use a simple RW lock via threading.Lock. Reads hold the lock only long
    enough to capture references to immutable objects (model, feature_names);
    predict() runs outside the lock so one slow inference can't block another.
    """
    def __init__(self):
        self._lock = threading.Lock()
        self.model = None
        self.feature_names: List[str] = []
        self.loaded_at: Optional[datetime] = None
        self.version: str = "uninitialized"
        self.n_predictions: int = 0

    def load(self, model_path: Path = MODEL_PATH, names_path: Path = FEATURE_NAMES_PATH):
        with self._lock:
            if not model_path.exists():
                raise FileNotFoundError(f"no model at {model_path}; run train_and_eval.py first")
            if not names_path.exists():
                raise FileNotFoundError(f"no feature names at {names_path}")
            self.model = joblib.load(model_path)
            with names_path.open() as f:
                self.feature_names = json.load(f)
            self.loaded_at = datetime.now(timezone.utc)
            mtime = datetime.fromtimestamp(model_path.stat().st_mtime, timezone.utc)
            self.version = f"lgbm-{mtime.strftime('%Y%m%dT%H%M%SZ')}"
            log.info(f"model loaded: version={self.version} features={len(self.feature_names)}")

    def snapshot(self):
        """Capture the current model + feature list for a prediction call.

        Returns immutable refs — safe to use outside the lock.
        """
        with self._lock:
            return self.model, list(self.feature_names), self.version

    def record_prediction(self):
        with self._lock:
            self.n_predictions += 1

STATE = ModelState()

# ------------------------------------------------------------ API schemas --

class SignalRequest(BaseModel):
    """Features for one bar. Missing features default to 0. Extras are ignored.

    The Go client should send all features the model expects (returned by
    /model/info). Alignment to training-time feature ordering happens inside
    this service — the client doesn't need to preserve order.
    """
    symbol: str = Field(..., description="e.g. BTCUSDT")
    features: dict = Field(..., description="name→value map, missing features default to 0")
    timestamp_ms: Optional[int] = Field(None, description="optional bar open time in ms")

class SignalResponse(BaseModel):
    symbol: str
    score: float = Field(..., description="directional score in [-1, +1]. Positive = bullish, negative = bearish")
    confidence: float = Field(..., description="probability of the predicted class (up or down), in [0, 1]")
    probabilities: dict = Field(..., description="{'down': p, 'flat': p, 'up': p}")
    model_version: str
    action_hint: str = Field(..., description="'open_long' / 'open_short' / 'wait' based on prob_threshold")
    served_at: str

# ---------------------------------------------------- Signal scoring logic --

def _default_prob_threshold() -> float:
    """Probability threshold for emitting a directional call.
    Overridable via ML_PROB_THRESHOLD env var so we can tune without a code change.
    Random baseline for a 3-class classifier is 0.333; anything meaningfully above
    that is a directional signal. We ship at 0.33 — just above random — so the
    ML gate blocks only weak-signal trades and lets moderate ones through. Raise
    to 0.40+ for a stricter filter once the distribution of model confidences
    settles.
    """
    try:
        raw = _os.environ.get("ML_PROB_THRESHOLD", "0.33")
        val = float(raw)
        if val < 0.0 or val > 1.0:
            raise ValueError(f"out of range: {val}")
        return val
    except Exception as e:
        log.warning(f"ML_PROB_THRESHOLD invalid ({e!r}), using 0.33")
        return 0.33

def predict_one(symbol: str, features: dict, prob_threshold: Optional[float] = None) -> SignalResponse:
    if prob_threshold is None:
        prob_threshold = _default_prob_threshold()
    """Run one inference and translate it to a trading score.

    The scoring convention matches the Go backtest:
      - If P(up) > prob_threshold AND > P(down): action_hint=open_long, score=+P(up)
      - If P(down) > prob_threshold AND > P(up): action_hint=open_short, score=-P(down)
      - Otherwise: action_hint=wait, score=0

    "confidence" is the probability of whichever class won — tells the caller
    how certain the model is regardless of direction.
    """
    model, feature_names, version = STATE.snapshot()
    if model is None:
        raise HTTPException(status_code=503, detail="model not loaded")

    # Build input vector in training-time feature order. Missing/None → 0.
    x = np.zeros((1, len(feature_names)), dtype=np.float64)
    missing: List[str] = []
    for i, name in enumerate(feature_names):
        v = features.get(name)
        if v is None:
            missing.append(name)
            continue
        try:
            x[0, i] = float(v)
        except (TypeError, ValueError):
            missing.append(name)

    if missing and len(missing) > len(feature_names) // 2:
        # Half the features are missing — caller probably has a bug.
        log.warning(f"predict for {symbol}: {len(missing)}/{len(feature_names)} features missing")

    probs = model.predict_proba(x)[0]
    # Training used y∈{0,1,2} for {down,flat,up} — preserve that ordering here.
    p_down, p_flat, p_up = float(probs[0]), float(probs[1]), float(probs[2])

    if p_up > prob_threshold and p_up > p_down:
        score = p_up
        confidence = p_up
        action_hint = "open_long"
    elif p_down > prob_threshold and p_down > p_up:
        score = -p_down
        confidence = p_down
        action_hint = "open_short"
    else:
        score = 0.0
        confidence = max(p_up, p_down)
        action_hint = "wait"

    STATE.record_prediction()
    return SignalResponse(
        symbol=symbol,
        score=score,
        confidence=confidence,
        probabilities={"down": p_down, "flat": p_flat, "up": p_up},
        model_version=version,
        action_hint=action_hint,
        served_at=datetime.now(timezone.utc).isoformat(),
    )

# --------------------------------------------------------- Rolling retrain --

def rolling_retrain_loop(interval_seconds: int = 86400):
    """Background thread: periodically kick off train_and_eval.py as a
    subprocess, then reload the model. Conservative cadence (default daily)
    since training fetches fresh data from Binance + Aster and takes ~60s.

    Failures are logged and the old model stays loaded — never promote a
    half-trained model. This is the safety property that makes rolling
    retrain less risky than "train on startup and never update."
    """
    import subprocess
    log.info(f"rolling retrain thread armed (interval={interval_seconds}s)")
    # Skip the first immediate retrain — we assume the operator ran
    # train_and_eval.py manually to produce the initial model.
    while True:
        time.sleep(interval_seconds)
        try:
            log.info("starting scheduled retrain...")
            proc = subprocess.run(
                ["python", str(ROOT / "train_and_eval.py"), "--bars", "5500"],
                cwd=ROOT, capture_output=True, text=True, timeout=600,
            )
            if proc.returncode != 0:
                log.warning(f"retrain failed rc={proc.returncode}: {proc.stderr[-400:]}")
                continue
            log.info("retrain completed, reloading model...")
            STATE.load()
        except subprocess.TimeoutExpired:
            log.warning("retrain timed out after 600s — keeping previous model")
        except Exception as e:
            log.warning(f"retrain exception: {e!r} — keeping previous model")

# ------------------------------------------------------------------ App --

def build_app(enable_retrain: bool = True, retrain_interval: int = 86400) -> FastAPI:
    app = FastAPI(title="Northstar ML Signal Service", version="0.1.0")
    STATE.load()

    if enable_retrain:
        t = threading.Thread(target=rolling_retrain_loop, args=(retrain_interval,), daemon=True)
        t.start()

    @app.get("/health")
    def health():
        return {
            "status": "ok",
            "model_version": STATE.version,
            "feature_count": len(STATE.feature_names),
            "loaded_at": STATE.loaded_at.isoformat() if STATE.loaded_at else None,
            "predictions_served": STATE.n_predictions,
        }

    @app.get("/model/info")
    def model_info():
        _, feats, version = STATE.snapshot()
        return {"version": version, "features": feats}

    @app.post("/model/reload")
    def model_reload():
        try:
            STATE.load()
            return {"status": "ok", "version": STATE.version}
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"reload failed: {e!r}")

    @app.post("/signal", response_model=SignalResponse)
    def signal(req: SignalRequest):
        return predict_one(req.symbol, req.features)

    return app

# ----------------------------------------------------------------- main --

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=9091)
    ap.add_argument("--no-retrain", action="store_true", help="disable scheduled retrain thread")
    ap.add_argument("--retrain-interval", type=int, default=86400, help="retrain cadence in seconds (default: 24h)")
    args = ap.parse_args()

    app = build_app(enable_retrain=not args.no_retrain, retrain_interval=args.retrain_interval)
    log.info(f"serving on http://{args.host}:{args.port}")
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")

if __name__ == "__main__":
    main()
