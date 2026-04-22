import type { ReactNode } from 'react';
import clsx from 'clsx';

/**
 * Badge — compact status/category pill. Variant drives semantic color.
 * - profit/loss: P&L values
 * - brand: strategy/algo emphasis
 * - warn: attention-needed
 * - neutral: metadata like exchange or instrument
 * - muted: secondary info
 */
type Variant = 'profit' | 'loss' | 'brand' | 'warn' | 'neutral' | 'muted';
type Size = 'xs' | 'sm';

interface BadgeProps {
  children: ReactNode;
  variant?: Variant;
  size?: Size;
  className?: string;
  /** Optional small dot before the label */
  dot?: boolean;
}

const variantClasses: Record<Variant, string> = {
  profit:  'bg-profit-soft text-profit border-profit/20',
  loss:    'bg-loss-soft text-loss border-loss/20',
  brand:   'bg-brand-500/10 text-brand-300 border-brand-500/30',
  warn:    'bg-warn-soft text-warn border-warn/20',
  neutral: 'bg-bg-elevated text-fg-secondary border-border-subtle',
  muted:   'bg-bg-hover text-fg-muted border-border-subtle/50',
};

const dotClasses: Record<Variant, string> = {
  profit:  'bg-profit',
  loss:    'bg-loss',
  brand:   'bg-brand-400',
  warn:    'bg-warn',
  neutral: 'bg-fg-secondary',
  muted:   'bg-fg-muted',
};

export function Badge({ children, variant = 'neutral', size = 'sm', className, dot }: BadgeProps) {
  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1.5 rounded-md border font-medium tracking-wide',
        size === 'xs' ? 'px-1.5 py-0.5 text-[10px]' : 'px-2 py-0.5 text-[11px]',
        variantClasses[variant],
        className,
      )}
    >
      {dot && <span className={clsx('w-1.5 h-1.5 rounded-full', dotClasses[variant])} />}
      {children}
    </span>
  );
}
