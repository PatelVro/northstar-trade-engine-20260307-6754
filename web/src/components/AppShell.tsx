import type { ReactNode } from 'react';
import { useHashRoute, useNavigate } from '../lib/router';
import clsx from 'clsx';

/**
 * AppShell — persistent chrome for every Cirelay page.
 * Holds the brand mark, primary nav, and a right-slot for status indicators.
 */
interface AppShellProps {
  children: ReactNode;
  /** Shown in the top-right; typical use: last-update timestamp + health pill. */
  statusSlot?: ReactNode;
}

const NAV_ITEMS: Array<{ label: string; path: string; matchPrefix: string }> = [
  { label: 'Overview', path: '/', matchPrefix: '/' },
  { label: 'Compare', path: '/compare', matchPrefix: '/compare' },
];

export function AppShell({ children, statusSlot }: AppShellProps) {
  const route = useHashRoute();
  const navigate = useNavigate();

  const isActive = (matchPrefix: string) => {
    if (matchPrefix === '/') return route.path === '/';
    return route.path.startsWith(matchPrefix);
  };

  return (
    <div className="min-h-screen bg-bg-base text-fg-primary">
      <header className="sticky top-0 z-30 bg-bg-base/80 backdrop-blur-md border-b border-border-subtle">
        <div className="max-w-[1400px] mx-auto px-6 h-14 flex items-center gap-8">
          {/* Brand */}
          <button
            type="button"
            onClick={() => navigate('/')}
            className="flex items-center gap-2.5 group"
          >
            <span className="w-7 h-7 rounded-md bg-gradient-to-br from-brand-400 to-brand-600 flex items-center justify-center shadow-glow-brand">
              <span className="text-[11px] font-bold text-white tracking-wider">Ci</span>
            </span>
            <span className="text-base font-semibold tracking-tight group-hover:text-brand-300 transition-colors">
              Cirelay
            </span>
          </button>

          {/* Primary nav */}
          <nav className="flex items-center gap-1">
            {NAV_ITEMS.map((item) => (
              <button
                key={item.path}
                type="button"
                onClick={() => navigate(item.path)}
                className={clsx(
                  'px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
                  isActive(item.matchPrefix)
                    ? 'text-fg-primary bg-bg-elevated'
                    : 'text-fg-secondary hover:text-fg-primary hover:bg-bg-surface',
                )}
              >
                {item.label}
              </button>
            ))}
          </nav>

          {/* Right slot */}
          <div className="ml-auto flex items-center gap-3 text-xs text-fg-secondary">
            {statusSlot}
          </div>
        </div>
      </header>

      <main className="max-w-[1400px] mx-auto px-6 py-8 animate-fade-in">
        {children}
      </main>

      <footer className="max-w-[1400px] mx-auto px-6 py-6 mt-12 border-t border-border-subtle/60 text-xs text-fg-muted flex items-center justify-between">
        <span>Cirelay · Multi-Strategy Algorithmic Trading</span>
        <span className="flex items-center gap-2">
          <span className="w-1.5 h-1.5 rounded-full bg-profit animate-pulse-subtle" />
          Paper mode · No live capital at risk
        </span>
      </footer>
    </div>
  );
}
