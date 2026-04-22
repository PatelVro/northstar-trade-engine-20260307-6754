import { Card } from './ui/Card';
import { Badge } from './ui/Badge';
import { formatNumber } from '../lib/traderMeta';
import type { Position } from '../types';

/**
 * PositionsTable — compact table of open positions. Designed for small
 * counts (0-6); at larger sizes we'd add pagination or sticky headers.
 */
interface PositionsTableProps {
  positions: Position[] | undefined;
  loading?: boolean;
}

export function PositionsTable({ positions, loading }: PositionsTableProps) {
  if (loading) {
    return (
      <Card>
        <div className="p-6 text-center text-sm text-fg-muted">Loading positions…</div>
      </Card>
    );
  }

  if (!positions || positions.length === 0) {
    return (
      <Card>
        <div className="p-6 text-center bg-grid">
          <div className="text-sm text-fg-secondary">No open positions</div>
          <div className="text-xs text-fg-muted mt-1">
            Trader is waiting for a qualifying signal.
          </div>
        </div>
      </Card>
    );
  }

  return (
    <Card>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-[10px] uppercase tracking-widest text-fg-muted">
              <th className="text-left px-5 py-3 font-medium">Symbol</th>
              <th className="text-left px-2 py-3 font-medium">Side</th>
              <th className="text-right px-2 py-3 font-medium">Qty</th>
              <th className="text-right px-2 py-3 font-medium">Entry</th>
              <th className="text-right px-2 py-3 font-medium">Mark</th>
              <th className="text-right px-2 py-3 font-medium">Unrealized</th>
              <th className="text-right px-5 py-3 font-medium">Lev</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border-subtle/40">
            {positions.map((p, i) => {
              const up = p.unrealized_pnl > 0;
              const down = p.unrealized_pnl < 0;
              return (
                <tr key={`${p.symbol}-${i}`} className="hover:bg-bg-elevated/50 transition-colors">
                  <td className="px-5 py-3 font-mono font-medium text-fg-primary">{p.symbol}</td>
                  <td className="px-2 py-3">
                    <Badge
                      variant={p.side.toLowerCase() === 'long' ? 'profit' : 'loss'}
                      size="xs"
                    >
                      {p.side.toUpperCase()}
                    </Badge>
                  </td>
                  <td className="px-2 py-3 text-right tabular-nums text-fg-primary">
                    {formatNumber(p.quantity, 4)}
                  </td>
                  <td className="px-2 py-3 text-right tabular-nums font-mono text-fg-secondary">
                    {formatNumber(p.entry_price, 2)}
                  </td>
                  <td className="px-2 py-3 text-right tabular-nums font-mono text-fg-secondary">
                    {formatNumber(p.mark_price, 2)}
                  </td>
                  <td className={`px-2 py-3 text-right tabular-nums font-mono font-medium ${up ? 'text-profit' : down ? 'text-loss' : 'text-fg-primary'}`}>
                    {up ? '+' : ''}{formatNumber(p.unrealized_pnl, 2)}
                    <div className={`text-[10px] font-normal ${up ? 'text-profit/70' : down ? 'text-loss/70' : 'text-fg-muted'}`}>
                      {up ? '+' : ''}{formatNumber(p.unrealized_pnl_pct, 2)}%
                    </div>
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums text-fg-secondary">
                    {p.leverage}x
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
