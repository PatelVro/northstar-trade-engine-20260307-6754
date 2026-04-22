import { Card } from './ui/Card';
import { Badge } from './ui/Badge';
import { formatNumber } from '../lib/traderMeta';
import type { DecisionRecord, DecisionAction } from '../types';

/**
 * DecisionFeed — chronological list of recent decision cycles.
 * Focuses on the ACTIONABLE info: action, symbol, P&L, reason.
 */
interface DecisionFeedProps {
  decisions: DecisionRecord[] | undefined;
  loading?: boolean;
  limit?: number;
}

function actionBadge(action: string) {
  const a = action.toLowerCase();
  if (a === 'open_long') return <Badge variant="profit" size="xs">OPEN LONG</Badge>;
  if (a === 'open_short') return <Badge variant="loss" size="xs">OPEN SHORT</Badge>;
  if (a === 'close_long' || a === 'close_short') return <Badge variant="brand" size="xs">CLOSE</Badge>;
  if (a === 'wait' || a === 'hold') return <Badge variant="muted" size="xs">{a.toUpperCase()}</Badge>;
  return <Badge variant="neutral" size="xs">{action.toUpperCase()}</Badge>;
}

function formatTime(ts: string): string {
  try {
    return new Date(ts).toLocaleString(undefined, {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch { return ts; }
}

export function DecisionFeed({ decisions, loading, limit = 30 }: DecisionFeedProps) {
  if (loading) {
    return (
      <Card>
        <div className="p-6 text-center text-sm text-fg-muted">Loading decisions…</div>
      </Card>
    );
  }

  if (!decisions || decisions.length === 0) {
    return (
      <Card>
        <div className="p-6 text-center bg-grid">
          <div className="text-sm text-fg-secondary">No decision history yet</div>
          <div className="text-xs text-fg-muted mt-1">Once the trader runs its first cycle, decisions appear here.</div>
        </div>
      </Card>
    );
  }

  const items = decisions.slice(0, limit);

  return (
    <Card>
      <div className="divide-y divide-border-subtle/40">
        {items.map((rec, i) => {
          const primary: DecisionAction | undefined = rec.decisions?.[0];
          const action = primary?.action ?? 'wait';
          const symbol = primary?.symbol || '—';
          const realizedPnl = primary?.realized_pnl ?? 0;
          const hasPnl = Math.abs(realizedPnl) > 0.001;

          return (
            <div key={`${rec.timestamp}-${i}`} className="p-4 hover:bg-bg-elevated/40 transition-colors">
              <div className="flex items-center justify-between gap-3 mb-1.5">
                <div className="flex items-center gap-2.5 min-w-0">
                  {actionBadge(action)}
                  <span className="font-mono text-sm font-medium text-fg-primary">{symbol}</span>
                  {hasPnl && (
                    <span
                      className={realizedPnl > 0 ? 'text-profit text-xs font-mono' : 'text-loss text-xs font-mono'}
                    >
                      {realizedPnl > 0 ? '+' : ''}{formatNumber(realizedPnl, 2)}
                    </span>
                  )}
                </div>
                <div className="text-[11px] text-fg-muted tabular-nums flex-shrink-0">
                  cycle {rec.cycle_number} · {formatTime(rec.timestamp)}
                </div>
              </div>
              {rec.cot_trace && (
                <div className="text-xs text-fg-secondary line-clamp-2">
                  {rec.cot_trace}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}
