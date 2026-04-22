import type { ReactNode, HTMLAttributes } from 'react';
import clsx from 'clsx';

/**
 * Card — the foundational surface for every section on the Cirelay dashboard.
 * All values (padding, radius, border, shadow) are driven by Tailwind tokens
 * defined in tailwind.config.js so theme changes propagate without edits here.
 */
interface CardProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  className?: string;
  /**
   * `active` = currently-selected card, gets the brand glow.
   * `interactive` = clickable, gets hover lift.
   */
  active?: boolean;
  interactive?: boolean;
}

export function Card({ children, className, active, interactive, ...rest }: CardProps) {
  return (
    <div
      {...rest}
      className={clsx(
        'rounded-xl border bg-bg-surface',
        'transition-all duration-200',
        active
          ? 'border-brand-500/60 shadow-glow-brand'
          : 'border-border-subtle shadow-card',
        interactive && 'cursor-pointer hover:border-border-strong hover:bg-bg-elevated hover:shadow-card-hover',
        className,
      )}
    >
      {children}
    </div>
  );
}

Card.Header = function CardHeader({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={clsx('flex items-start justify-between gap-4 px-5 pt-5 pb-3 border-b border-border-subtle/50', className)}>
      {children}
    </div>
  );
};

Card.Body = function CardBody({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={clsx('p-5', className)}>{children}</div>;
};

Card.Title = function CardTitle({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <h3 className={clsx('text-sm font-semibold text-fg-primary tracking-wide', className)}>
      {children}
    </h3>
  );
};
