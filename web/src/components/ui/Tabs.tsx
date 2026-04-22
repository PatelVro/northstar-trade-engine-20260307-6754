import type { ReactNode } from 'react';
import clsx from 'clsx';

/**
 * Tabs — a minimal controlled tab list with an active-underline style.
 * We keep it uncontrolled-friendly by letting the parent own the value;
 * no internal state, no headless dependency.
 */
export interface TabItem {
  id: string;
  label: ReactNode;
  /** Optional trailing count/badge */
  badge?: ReactNode;
  disabled?: boolean;
}

interface TabsProps {
  items: TabItem[];
  value: string;
  onChange: (id: string) => void;
  className?: string;
}

export function Tabs({ items, value, onChange, className }: TabsProps) {
  return (
    <div className={clsx('border-b border-border-subtle', className)}>
      <div className="flex items-end gap-1 -mb-px">
        {items.map((item) => {
          const active = item.id === value;
          return (
            <button
              key={item.id}
              type="button"
              disabled={item.disabled}
              onClick={() => !item.disabled && onChange(item.id)}
              className={clsx(
                'px-4 py-2.5 text-sm font-medium tracking-wide transition-colors',
                'border-b-2 flex items-center gap-2',
                active
                  ? 'border-brand-500 text-fg-primary'
                  : 'border-transparent text-fg-secondary hover:text-fg-primary',
                item.disabled && 'opacity-40 cursor-not-allowed',
              )}
            >
              {item.label}
              {item.badge && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-bg-elevated text-fg-muted tabular-nums">
                  {item.badge}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
