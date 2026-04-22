import useSWR from 'swr';
import { api } from '../lib/api';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { PageHeader } from '../components/ui/PageHeader';
import { ComparisonChart } from '../components/ComparisonChart';
import { getTraderMeta, formatCompactUsd } from '../lib/traderMeta';
import type { TraderInfo, SystemStatus, AccountInfo, CompetitionTraderData } from '../types';

/**
 * ComparePage — A/B surface for ML-Confirmed vs Rule-Based (and any other
 * traders configured). Shows:
 *   - Side-by-side KPI matrix
 *   - Overlay equity curves via ComparisonChart
 */
export function ComparePage() {
  const { data: traders } = useSWR<TraderInfo[]>('traders', api.getTraders, { refreshInterval: 10000 });

  // ComparisonChart expects CompetitionTraderData[]. We stub minimum fields here;
  // ComparisonChart re-fetches equity history per trader_id on its own.
  const chartTraders: CompetitionTraderData[] = (traders ?? []).map((t) => ({
    trader_id: t.trader_id,
    trader_name: t.trader_name,
    ai_model: t.ai_model,
    account_equity: 0,
    strategy_equity: 0,
    total_pnl: 0,
    strategy_return_pct: 0,
    position_count: 0,
    margin_used_pct: 0,
    call_count: 0,
    is_running: true,
  }));

  return (
    <>
      <PageHeader
        eyebrow="A/B Comparison"
        title="Strategy Head-to-Head"
        subtitle="Directly compare trader performance. Useful for validating whether the ML filter actually improves results over pure rule-based signals."
      />

      {traders && traders.length > 0 && (
        <>
          {/* KPI matrix */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
            {traders.map((t) => <ComparisonTile key={t.trader_id} trader={t} />)}
          </div>

          {/* Overlay chart */}
          <Card>
            <Card.Header>
              <Card.Title>Equity Curves (overlaid)</Card.Title>
              <Badge variant="muted" size="xs">Live</Badge>
            </Card.Header>
            <div className="p-4">
              <ComparisonChart traders={chartTraders} />
            </div>
          </Card>
        </>
      )}

      {(!traders || traders.length === 0) && (
        <Card>
          <div className="p-8 text-center bg-grid">
            <div className="text-fg-primary font-medium">No traders to compare</div>
          </div>
        </Card>
      )}
    </>
  );
}

function ComparisonTile({ trader }: { trader: TraderInfo }) {
  const { data: status } = useSWR<SystemStatus>(`status-${trader.trader_id}`, () => api.getStatus(trader.trader_id), { refreshInterval: 8000 });
  const { data: account } = useSWR<AccountInfo>(`account-${trader.trader_id}`, () => api.getAccount(trader.trader_id), { refreshInterval: 8000 });

  const meta = getTraderMeta(trader, status);
  const returnPct = account?.strategy_return_pct ?? 0;
  // strategy_equity for per-trader isolation (see TraderRosterCard comment).
  const equity = account?.strategy_equity ?? 0;
  const initial = account?.strategy_initial_capital ?? 0;
  const delta = equity - initial;

  return (
    <Card>
      <div className="p-5">
        <div className="flex items-start justify-between gap-3 mb-3">
          <div>
            <div className="text-sm font-semibold text-fg-primary mb-1">{trader.trader_name}</div>
            <div className="flex items-center gap-1.5">
              <span className="w-1.5 h-1.5 rounded-full" style={{ background: meta.color }} />
              <Badge variant={meta.strategyVariant} size="xs">{meta.strategyLabel}</Badge>
            </div>
          </div>
        </div>

        <div className="text-[10px] uppercase tracking-wider text-fg-muted">Equity</div>
        <div className="text-2xl font-semibold font-mono tabular-nums">{formatCompactUsd(equity)}</div>
        <div className={`text-sm tabular-nums font-mono mt-0.5 ${returnPct > 0 ? 'text-profit' : returnPct < 0 ? 'text-loss' : 'text-fg-secondary'}`}>
          {returnPct > 0 ? '+' : ''}{returnPct.toFixed(2)}% ({delta > 0 ? '+' : ''}{formatCompactUsd(delta)})
        </div>

        <div className="grid grid-cols-2 gap-3 mt-4 pt-3 border-t border-border-subtle/60 text-xs">
          <div>
            <div className="text-[10px] uppercase tracking-wider text-fg-muted">Positions</div>
            <div className="text-fg-primary font-medium tabular-nums">{account?.position_count ?? 0}</div>
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wider text-fg-muted">Cycles</div>
            <div className="text-fg-primary font-medium tabular-nums">{status?.call_count ?? 0}</div>
          </div>
        </div>
      </div>
    </Card>
  );
}
