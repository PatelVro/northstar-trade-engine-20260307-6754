import useSWR from 'swr';
import { api } from '../lib/api';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { PageHeader } from '../components/ui/PageHeader';
import { Stat } from '../components/ui/Stat';
import { TraderRosterCard } from '../components/TraderRosterCard';
import { getTraderMeta, formatCompactUsd } from '../lib/traderMeta';
import type { TraderInfo, SystemStatus, AccountInfo } from '../types';

/**
 * OverviewPage — the landing surface.
 * Structure:
 *   1. KPI strip        (aggregate equity, total P&L, open positions, running traders)
 *   2. Trader roster    (grid of TraderRosterCard, one per trader)
 *   3. Fleet notes      (empty state / operator guidance)
 *
 * Each card is self-contained — it owns its own SWR fetches — so adding a
 * new trader to config.json automatically adds a card here once the
 * aggregator's /api/traders picks it up.
 */

function AggregateKpis({ traders }: { traders: TraderInfo[] }) {
  // Fetch account info for every trader in parallel. SWR key pattern matches
  // other places so they share cache.
  const accountQueries = traders.map((t) =>
    // eslint-disable-next-line react-hooks/rules-of-hooks
    useSWR<AccountInfo>(`account-${t.trader_id}`, () => api.getAccount(t.trader_id), {
      refreshInterval: 8000,
    }),
  );
  const statusQueries = traders.map((t) =>
    // eslint-disable-next-line react-hooks/rules-of-hooks
    useSWR<SystemStatus>(`status-${t.trader_id}`, () => api.getStatus(t.trader_id), {
      refreshInterval: 8000,
    }),
  );

  // Sum strategy_equity (per-trader tracked) not account_equity (broker-reported).
  // When multiple traders share a brokerage account (e.g. both IBKR paper
  // strategies share DUP200062), account_equity reports the full broker
  // balance from EACH trader — double-counting across N traders on that
  // account. strategy_equity is our per-strategy ledger and sums cleanly.
  const totalEquity = accountQueries.reduce((s, q) => s + (q.data?.strategy_equity ?? 0), 0);
  const totalInitial = accountQueries.reduce((s, q) => s + (q.data?.strategy_initial_capital ?? 0), 0);
  const totalPnl = accountQueries.reduce((s, q) => s + (q.data?.realized_pnl ?? 0) + (q.data?.unrealized_pnl ?? 0), 0);
  const totalPositions = accountQueries.reduce((s, q) => s + (q.data?.position_count ?? 0), 0);
  const runningTraders = statusQueries.filter((q) => q.data?.is_running).length;
  const totalCycles = statusQueries.reduce((s, q) => s + (q.data?.call_count ?? 0), 0);

  const aggReturnPct = totalInitial > 0 ? ((totalEquity - totalInitial) / totalInitial) * 100 : 0;

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
      <Card>
        <div className="p-5">
          <Stat
            label="Aggregate Equity"
            value={<span className="font-mono">{formatCompactUsd(totalEquity)}</span>}
            subtitle={`Across ${traders.length} trader${traders.length === 1 ? '' : 's'}`}
            delta={aggReturnPct}
            deltaFormat="pct"
            size="lg"
          />
        </div>
      </Card>
      <Card>
        <div className="p-5">
          <Stat
            label="Total P&L"
            value={<span className="font-mono">{formatCompactUsd(totalPnl)}</span>}
            subtitle="Realized + unrealized"
            delta={totalPnl === 0 ? undefined : (totalPnl > 0 ? 0.01 : -0.01)}
            deltaFormat="raw"
            size="lg"
          />
        </div>
      </Card>
      <Card>
        <div className="p-5">
          <Stat
            label="Open Positions"
            value={<span className="font-mono">{totalPositions}</span>}
            subtitle={totalPositions === 0 ? 'No active exposure' : 'Across all markets'}
            size="lg"
          />
        </div>
      </Card>
      <Card>
        <div className="p-5">
          <Stat
            label="Traders Running"
            value={
              <span className="flex items-baseline gap-2 font-mono">
                {runningTraders}
                <span className="text-sm text-fg-muted">/ {traders.length}</span>
              </span>
            }
            subtitle={`${totalCycles.toLocaleString()} decision cycles`}
            size="lg"
          />
        </div>
      </Card>
    </div>
  );
}

export function OverviewPage() {
  const { data: traders, error, isLoading } = useSWR<TraderInfo[]>(
    'traders',
    api.getTraders,
    { refreshInterval: 10000 },
  );

  return (
    <>
      <PageHeader
        eyebrow="Fleet Overview"
        title="All traders"
        subtitle="Live comparison across the Cirelay trading fleet. Each card shows equity, strategy flavor, and recent performance."
        actions={
          <Badge variant="brand" dot>
            {traders ? `${traders.length} active` : 'loading'}
          </Badge>
        }
      />

      {/* Aggregate KPIs */}
      {traders && traders.length > 0 && <AggregateKpis traders={traders} />}

      {/* Section header */}
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-sm font-semibold text-fg-primary uppercase tracking-wider">
          Strategies
        </h2>
        <div className="text-xs text-fg-muted flex items-center gap-4">
          <span className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-profit" />
            Profit
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-loss" />
            Loss
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-brand-500" />
            ML-Confirmed
          </span>
        </div>
      </div>

      {/* Error state */}
      {error && (
        <Card>
          <div className="p-6 text-center">
            <div className="text-loss font-medium mb-1">Unable to reach the API aggregator</div>
            <div className="text-xs text-fg-secondary">
              Check that <code className="text-fg-primary bg-bg-elevated px-1 py-0.5 rounded">api-aggregator</code> is running on port 8082.
            </div>
          </div>
        </Card>
      )}

      {/* Loading skeleton */}
      {isLoading && !traders && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {[1, 2, 3].map((i) => (
            <Card key={i}>
              <div className="p-5 h-56 bg-grid opacity-40 animate-pulse" />
            </Card>
          ))}
        </div>
      )}

      {/* Trader cards */}
      {traders && traders.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {traders.map((t) => <TraderRosterCard key={t.trader_id} trader={t} />)}
        </div>
      )}

      {/* Empty state */}
      {traders && traders.length === 0 && (
        <Card>
          <div className="p-10 text-center bg-grid">
            <div className="text-fg-primary font-medium mb-2">No traders registered</div>
            <div className="text-sm text-fg-secondary max-w-md mx-auto">
              Add a trader entry to <code className="text-fg-primary bg-bg-elevated px-1 py-0.5 rounded">config.json</code>
              {' '}and restart the backend.
            </div>
          </div>
        </Card>
      )}

      {/* Fleet-level note */}
      {traders && traders.length > 0 && (
        <div className="mt-10 text-xs text-fg-muted max-w-3xl">
          <span className="font-semibold text-fg-secondary">A/B note:</span>{' '}
          <span>
            The control and ML-Confirmed traders analyze identical market data. ML-Confirmed skips any entry where the model disagrees with the rule-based signal. Diverging equity curves over time reveal whether the ML filter adds real value after costs.
          </span>
        </div>
      )}
    </>
  );
}

// Map known exchange strings to colors for the legend dot (kept small).
export function _getExchangeTint(exchange?: string): string {
  const meta = getTraderMeta({ trader_id: exchange ?? '', trader_name: '', ai_model: '' });
  return meta.color;
}
