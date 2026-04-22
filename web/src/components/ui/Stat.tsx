import type { ReactNode } from 'react';
import clsx from 'clsx';

/**
 * Stat — a KPI tile. Aligned tabular numbers, optional delta with
 * profit/loss semantic color, optional trailing slot for sparklines or icons.
 *
 * The delta is pure presentation; the caller decides whether it's positive
 * or negative. A delta of undefined hides the row entirely.
 */
interface StatProps {
  label: string;
  value: ReactNode;
  /** Positive → profit green; negative → loss red; zero → neutral */
  delta?: number;
  /** Format for delta: 'pct' → "+2.15%", 'abs' → "+$12.34" */
  deltaFormat?: 'pct' | 'abs' | 'raw';
  deltaPrefix?: string;
  /** Subtle text shown under the label, e.g. "vs start" or "24h" */
  subtitle?: string;
  /** Right-slot for sparklines, badges, or icons */
  trailing?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}

export function Stat({
  label,
  value,
  delta,
  deltaFormat = 'pct',
  deltaPrefix,
  subtitle,
  trailing,
  size = 'md',
  className,
}: StatProps) {
  const valueClass = size === 'lg'
    ? 'text-3xl font-semibold'
    : size === 'sm'
      ? 'text-base font-medium'
      : 'text-xl font-semibold';

  const hasDelta = typeof delta === 'number' && !Number.isNaN(delta);
  const deltaIsUp = hasDelta && delta! > 0;
  const deltaIsDown = hasDelta && delta! < 0;

  const formatDelta = () => {
    if (!hasDelta) return null;
    const sign = delta! > 0 ? '+' : delta! < 0 ? '' : '';
    const prefix = deltaPrefix ?? '';
    if (deltaFormat === 'pct') return `${sign}${delta!.toFixed(2)}%`;
    if (deltaFormat === 'abs') return `${sign}${prefix}${Math.abs(delta!).toFixed(2)}`;
    return `${sign}${prefix}${delta}`;
  };

  return (
    <div className={clsx('flex items-start justify-between gap-3', className)}>
      <div className="flex-1 min-w-0">
        <div className="text-[11px] uppercase tracking-wider text-fg-muted font-medium">
          {label}
        </div>
        {subtitle && (
          <div className="text-xs text-fg-muted/70 mt-0.5">{subtitle}</div>
        )}
        <div className={clsx(valueClass, 'text-fg-primary tabular-nums mt-1')}>
          {value}
        </div>
        {hasDelta && (
          <div
            className={clsx(
              'text-xs tabular-nums mt-1 font-medium',
              deltaIsUp && 'text-profit',
              deltaIsDown && 'text-loss',
              !deltaIsUp && !deltaIsDown && 'text-fg-secondary',
            )}
          >
            {formatDelta()}
          </div>
        )}
      </div>
      {trailing && <div className="flex-shrink-0">{trailing}</div>}
    </div>
  );
}
