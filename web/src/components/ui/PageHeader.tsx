import type { ReactNode } from 'react';

/**
 * PageHeader — consistent top-of-page layout: title + optional subtitle +
 * right-slot for actions (refresh, filters, etc.). Intentionally opinionated
 * on spacing so pages stay consistent with each other.
 */
interface PageHeaderProps {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}

export function PageHeader({ title, subtitle, actions, eyebrow }: PageHeaderProps) {
  return (
    <div className="flex items-end justify-between gap-6 pb-6 border-b border-border-subtle mb-6">
      <div className="min-w-0">
        {eyebrow && (
          <div className="text-[11px] uppercase tracking-widest text-brand-400 font-semibold mb-2">
            {eyebrow}
          </div>
        )}
        <h1 className="text-2xl font-semibold text-fg-primary tracking-tight">
          {title}
        </h1>
        {subtitle && (
          <p className="text-sm text-fg-secondary mt-1.5 max-w-2xl">
            {subtitle}
          </p>
        )}
      </div>
      {actions && (
        <div className="flex-shrink-0 flex items-center gap-2">{actions}</div>
      )}
    </div>
  );
}
