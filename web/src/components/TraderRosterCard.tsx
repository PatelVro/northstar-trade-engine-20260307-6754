import useSWR from 'swr';
import { LineChart, Line, ResponsiveContainer, YAxis } from 'recharts';
import { Card } from './ui/Card';
import { Badge } from './ui/Badge';
import { api } from '../lib/api';
import { getTraderMeta, getModeLabel, formatCompactUsd } from '../lib/traderMeta';
import { useNavigate } from '../lib/router';
import type { TraderInfo, SystemStatus, AccountInfo } from '../types';

/**
 * TraderRosterCard — the primary unit of the Overview page.
 * Shows one trader's at-a-glance health: equity, return%, positions, mode,
 * strategy flavor, exchange, and a sparkline of recent equity.
 * Clicks through to the detail page.
 */
interface TraderRosterCardProps {
  trader: TraderInfo;
}

export function TraderRosterCard({ trader }: TraderRosterCardProps) {
  const navigate = useNavigate();
  const { data: status } = useSWR<SystemStatus>(
    `status-${trader.trader_id}`,
    () => api.getStatus(trader.trader_id),
    { refreshInterval: 8000 },
  );
  const { data: account } = useSWR<AccountInfo>(
    `account-${trader.trader_id}`,
    () => api.getAccount(trader.trader_id),
    { refreshInterval: 8000 },
  );
  const { data: equityHistory } = useSWR<Array<{ timestamp: string; equity: number }>>(
    `equity-${trader.trader_id}`,
    () => api.getEquityHistory(trader.trader_id),
    { refreshInterval: 30000 },
  );

  const meta = getTraderMeta(trader, status);
  const mode = getModeLabel(status);

  // Use strategy_equity (our tracking, per-trader) instead of account_equity
  // (broker-reported, can be shared across traders on the same brokerage
  // account — like IBKR paper where both rule-based + ML-Confirmed point at
  // the same DUP200062 account and each would otherwise report the full
  // broker balance, inflating aggregates).
  const equity = account?.strategy_equity ?? status?.initial_balance ?? 0;
  const initialBalance = account?.strategy_initial_capital ?? status?.initial_balance ?? 0;
  const returnPct = account?.strategy_return_pct ?? 0;
  const positions = account?.position_count ?? 0;
  const isRunning = status?.is_running ?? false;

  // Normalize equity history into sparkline data. Handle missing/empty data.
  const sparkData = (equityHistory ?? []).slice(-40).map((pt, i) => ({
    t: i,
    equity: pt.equity,
  }));
  const sparkDomain = sparkData.length > 0
    ? [Math.min(...sparkData.map((p) => p.equity)), Math.max(...sparkData.map((p) => p.equity))]
    : [0, 1];

  return (
    <Card
      interactive
      onClick={() => navigate(`/trader/${trader.trader_id}`)}
      className="flex flex-col"
    >
      <div className="p-5">
        {/* Header: name + mode + running indicator */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-1">
              <span
                className="w-2 h-2 rounded-full flex-shrink-0"
                style={{ background: meta.color }}
              />
              <h3 className="text-sm font-semibold text-fg-primary truncate">
                {trader.trader_name}
              </h3>
            </div>
            <div className="flex items-center gap-1.5 flex-wrap">
              <Badge variant="muted" size="xs">{meta.exchangeLabel}</Badge>
              <Badge variant="muted" size="xs">{meta.instrumentLabel}</Badge>
              <Badge variant={meta.strategyVariant} size="xs">{meta.strategyLabel}</Badge>
            </div>
          </div>
          <div className="flex flex-col items-end gap-1">
            <Badge variant={mode.variant} size="xs" dot>{mode.label}</Badge>
            {!isRunning && (
              <Badge variant="loss" size="xs">OFFLINE</Badge>
            )}
          </div>
        </div>

        {/* Main equity value */}
        <div className="tabular-nums mb-3">
          <div className="text-[11px] uppercase tracking-wider text-fg-muted font-medium mb-0.5">
            Equity
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-2xl font-semibold font-mono">
              {formatCompactUsd(equity)}
            </span>
            <span className={
              returnPct > 0
                ? 'text-profit text-sm font-medium'
                : returnPct < 0
                  ? 'text-loss text-sm font-medium'
                  : 'text-fg-secondary text-sm'
            }>
              {returnPct > 0 ? '+' : ''}{returnPct.toFixed(2)}%
            </span>
          </div>
          <div className="text-[11px] text-fg-muted mt-0.5">
            Started at {formatCompactUsd(initialBalance)}
          </div>
        </div>

        {/* Sparkline */}
        <div className="h-10 -mx-1 mb-3">
          {sparkData.length > 1 ? (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={sparkData}>
                <YAxis hide domain={sparkDomain} />
                <Line
                  type="monotone"
                  dataKey="equity"
                  stroke={meta.color}
                  strokeWidth={1.5}
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <div className="h-full flex items-center justify-center text-[10px] text-fg-muted">
              awaiting equity samples…
            </div>
          )}
        </div>

        {/* Footer metrics */}
        <div className="flex items-center justify-between text-xs tabular-nums border-t border-border-subtle/60 pt-3">
          <div className="flex flex-col">
            <span className="text-[10px] uppercase tracking-wider text-fg-muted">Positions</span>
            <span className="text-fg-primary font-medium">{positions}</span>
          </div>
          <div className="flex flex-col">
            <span className="text-[10px] uppercase tracking-wider text-fg-muted">Cycles</span>
            <span className="text-fg-primary font-medium">{status?.call_count ?? 0}</span>
          </div>
          <div className="flex flex-col text-right">
            <span className="text-[10px] uppercase tracking-wider text-fg-muted">Unrealized</span>
            <span className={
              (account?.unrealized_pnl ?? 0) > 0
                ? 'text-profit font-medium'
                : (account?.unrealized_pnl ?? 0) < 0
                  ? 'text-loss font-medium'
                  : 'text-fg-primary font-medium'
            }>
              {account?.unrealized_pnl != null ? formatCompactUsd(account.unrealized_pnl) : '—'}
            </span>
          </div>
        </div>
      </div>
    </Card>
  );
}
