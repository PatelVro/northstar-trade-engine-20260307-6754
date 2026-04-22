import type { TraderInfo, SystemStatus } from '../types';

/**
 * Derive display metadata for a trader from its id + optional SystemStatus.
 *
 * The `trader_id` encodes enough info for a sensible default (since we own
 * the IDs in config.json), with SystemStatus filling in runtime truth when
 * it arrives. This file is the one place that maps IDs → labels/colors, so
 * any future ID schema shifts stay contained.
 */

export interface TraderMeta {
  /** Human-friendly short label, e.g. "Aster" / "IBKR" */
  exchangeLabel: string;
  /** Instrument family, e.g. "Perp" / "Equity" */
  instrumentLabel: string;
  /** Strategy flavor label shown as a badge */
  strategyLabel: string;
  /** Variant for the strategy badge */
  strategyVariant: 'brand' | 'neutral' | 'warn';
  /** A distinct hex color used for charts/sparklines */
  color: string;
  /** Is this a control (baseline) trader? */
  isControl: boolean;
  /** Is this trader using ML-Confirmed gating? */
  isMLConfirmed: boolean;
}

// A fixed palette — we keep colors stable so charts don't swap hues between
// refreshes. Indexed by trader role, not id.
const palette = {
  control: '#8A94A6',     // muted gray — the baseline
  mlConfirmed: '#4F8CFF', // brand blue — the experiment
  equity: '#16C784',      // profit green — stock trader stands out
  generic: '#7AAFFF',     // lighter brand
};

export function getTraderMeta(trader: TraderInfo, status?: SystemStatus | null): TraderMeta {
  const id = trader.trader_id.toLowerCase();
  const name = (trader.trader_name || '').toLowerCase();

  // Exchange & instrument — prefer SystemStatus if available, else infer from id.
  let exchangeLabel = 'Exchange';
  let instrumentLabel = 'Market';
  if (status?.exchange === 'aster' || id.includes('aster')) {
    exchangeLabel = 'Aster';
    instrumentLabel = 'Perp';
  } else if (status?.exchange === 'ibkr' || id.includes('ibkr')) {
    exchangeLabel = 'IBKR';
    instrumentLabel = 'Equity';
  } else if (status?.exchange === 'hyperliquid' || id.includes('hyperliquid')) {
    exchangeLabel = 'Hyperliquid';
    instrumentLabel = 'Perp';
  } else if (status?.exchange === 'binance' || id.includes('binance')) {
    exchangeLabel = 'Binance';
    instrumentLabel = 'Perp';
  }

  // Strategy flavor
  const isMLConfirmed = id.includes('ml_confirmed') || id.includes('ml-confirmed') || name.includes('ml-confirmed');
  const isControl = id.includes('rulebased') || id.includes('rule_based') || name.includes('rule-based') || name.includes('control');

  let strategyLabel: string;
  let strategyVariant: TraderMeta['strategyVariant'];
  let color: string;

  if (isMLConfirmed) {
    strategyLabel = 'ML-CONFIRMED';
    strategyVariant = 'brand';
    color = palette.mlConfirmed;
  } else if (isControl) {
    strategyLabel = 'RULE-BASED';
    strategyVariant = 'neutral';
    color = palette.control;
  } else if (exchangeLabel === 'IBKR') {
    strategyLabel = 'EQUITY MOMENTUM';
    strategyVariant = 'neutral';
    color = palette.equity;
  } else {
    strategyLabel = 'MOMENTUM';
    strategyVariant = 'neutral';
    color = palette.generic;
  }

  return {
    exchangeLabel,
    instrumentLabel,
    strategyLabel,
    strategyVariant,
    color,
    isControl,
    isMLConfirmed,
  };
}

/** Short mode label for badges. Handles empty status gracefully. */
export function getModeLabel(status?: SystemStatus | null): { label: string; variant: 'brand' | 'warn' | 'neutral' } {
  const mode = (status?.mode || '').toLowerCase();
  if (mode === 'live') return { label: 'LIVE', variant: 'warn' };
  if (mode === 'paper') return { label: 'PAPER', variant: 'brand' };
  if (mode === 'replay') return { label: 'REPLAY', variant: 'neutral' };
  return { label: 'UNKNOWN', variant: 'neutral' };
}

/** Format a dollar amount compactly: $1.2k / $12.3M. */
export function formatCompactUsd(value: number): string {
  const abs = Math.abs(value);
  const sign = value < 0 ? '-' : '';
  if (abs >= 1_000_000) return `${sign}$${(abs / 1_000_000).toFixed(2)}M`;
  if (abs >= 1_000) return `${sign}$${(abs / 1_000).toFixed(2)}k`;
  return `${sign}$${abs.toFixed(2)}`;
}

/** Format a number with commas and fixed decimals; handles null/undefined. */
export function formatNumber(value: number | null | undefined, decimals = 2): string {
  if (value == null || Number.isNaN(value)) return '—';
  return value.toLocaleString(undefined, {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
}
