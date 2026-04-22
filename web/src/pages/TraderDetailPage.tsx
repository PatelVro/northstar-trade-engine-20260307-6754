import { useState } from 'react';
import useSWR from 'swr';
import { api } from '../lib/api';
import { Card } from '../components/ui/Card';
import { Stat } from '../components/ui/Stat';
import { Badge } from '../components/ui/Badge';
import { PageHeader } from '../components/ui/PageHeader';
import { Tabs, type TabItem } from '../components/ui/Tabs';
import { PositionsTable } from '../components/PositionsTable';
import { DecisionFeed } from '../components/DecisionFeed';
import { EquityChart } from '../components/EquityChart';
import { TradeExecutionMonitor } from '../components/TradeExecutionMonitor';
import { StrategyComplianceMonitor } from '../components/StrategyComplianceMonitor';
import AILearning from '../components/AILearning';
import { useNavigate } from '../lib/router';
import { getTraderMeta, getModeLabel, formatCompactUsd } from '../lib/traderMeta';
import type { SystemStatus, AccountInfo, Position, DecisionRecord } from '../types';

/**
 * TraderDetailPage — drill-down view for a single trader.
 *
 * Sections:
 *   - Header: name + exchange/instrument/strategy badges + mode + running state
 *   - KPI strip: equity, total P&L, unrealized, positions
 *   - Tabs: Performance (equity chart), Positions, Decisions, Execution+Compliance, AI Learning
 */
interface TraderDetailPageProps {
  traderId: string;
}

type TabId = 'performance' | 'positions' | 'decisions' | 'execution' | 'ai';

export function TraderDetailPage({ traderId }: TraderDetailPageProps) {
  const navigate = useNavigate();
  const [tab, setTab] = useState<TabId>('performance');

  const { data: status } = useSWR<SystemStatus>(`status-${traderId}`, () => api.getStatus(traderId), {
    refreshInterval: 6000,
  });
  const { data: account } = useSWR<AccountInfo>(`account-${traderId}`, () => api.getAccount(traderId), {
    refreshInterval: 6000,
  });
  const { data: positions } = useSWR<Position[]>(`positions-${traderId}`, () => api.getPositions(traderId), {
    refreshInterval: 6000,
  });
  const { data: decisions } = useSWR<DecisionRecord[]>(`decisions-${traderId}`, () => api.getDecisions(traderId), {
    refreshInterval: 10000,
  });

  const meta = getTraderMeta({ trader_id: traderId, trader_name: status?.trader_name ?? traderId, ai_model: status?.ai_model ?? '' }, status);
  const mode = getModeLabel(status);

  // strategy_equity is per-trader; account_equity reports the whole broker
  // account and double-counts when multiple traders share one brokerage.
  const equity = account?.strategy_equity ?? 0;
  const initialBalance = account?.strategy_initial_capital ?? status?.initial_balance ?? 0;
  const returnPct = account?.strategy_return_pct ?? 0;
  const totalPnl = (account?.realized_pnl ?? 0) + (account?.unrealized_pnl ?? 0);
  const unrealized = account?.unrealized_pnl ?? 0;
  const positionCount = account?.position_count ?? 0;

  const tabItems: TabItem[] = [
    { id: 'performance', label: 'Performance' },
    { id: 'positions', label: 'Positions', badge: positionCount > 0 ? positionCount : undefined },
    { id: 'decisions', label: 'Decisions', badge: decisions?.length || undefined },
    { id: 'execution', label: 'Execution & Compliance' },
    { id: 'ai', label: 'AI Learning' },
  ];

  return (
    <>
      {/* Back link */}
      <button
        type="button"
        onClick={() => navigate('/')}
        className="text-xs text-fg-muted hover:text-fg-primary transition-colors mb-4 flex items-center gap-1"
      >
        ← Back to overview
      </button>

      <PageHeader
        eyebrow={
          <div className="flex items-center gap-2">
            <span
              className="w-2 h-2 rounded-full"
              style={{ background: meta.color }}
            />
            {meta.exchangeLabel} · {meta.instrumentLabel}
          </div>
        }
        title={status?.trader_name ?? traderId}
        subtitle={
          <span className="flex items-center gap-2 flex-wrap">
            <Badge variant={mode.variant} size="xs" dot>{mode.label}</Badge>
            <Badge variant={meta.strategyVariant} size="xs">{meta.strategyLabel}</Badge>
            <Badge variant="muted" size="xs">AI: {status?.ai_model ?? '—'}</Badge>
            <span className="text-fg-muted">·</span>
            <span>Scan interval {status?.scan_interval ?? '—'} · {status?.call_count ?? 0} cycles</span>
          </span>
        }
        actions={
          status?.is_running ? (
            <Badge variant="profit" dot>RUNNING</Badge>
          ) : (
            <Badge variant="loss">OFFLINE</Badge>
          )
        }
      />

      {/* KPI strip */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <Card>
          <div className="p-5">
            <Stat
              label="Equity"
              value={<span className="font-mono">{formatCompactUsd(equity)}</span>}
              subtitle={`Started at ${formatCompactUsd(initialBalance)}`}
              delta={returnPct}
              deltaFormat="pct"
              size="lg"
            />
          </div>
        </Card>
        <Card>
          <div className="p-5">
            <Stat
              label="Total P&L"
              value={<span className={`font-mono ${totalPnl > 0 ? 'text-profit' : totalPnl < 0 ? 'text-loss' : ''}`}>{formatCompactUsd(totalPnl)}</span>}
              subtitle="Realized + unrealized"
              size="lg"
            />
          </div>
        </Card>
        <Card>
          <div className="p-5">
            <Stat
              label="Unrealized"
              value={<span className={`font-mono ${unrealized > 0 ? 'text-profit' : unrealized < 0 ? 'text-loss' : ''}`}>{formatCompactUsd(unrealized)}</span>}
              subtitle={positionCount > 0 ? `${positionCount} open` : 'Flat'}
              size="lg"
            />
          </div>
        </Card>
        <Card>
          <div className="p-5">
            <Stat
              label="Win Rate"
              value={<span className="font-mono">—</span>}
              subtitle="Available after first closed trade"
              size="lg"
            />
          </div>
        </Card>
      </div>

      {/* Tabs */}
      <Tabs items={tabItems} value={tab} onChange={(v) => setTab(v as TabId)} className="mb-6" />

      {/* Tab panels */}
      {tab === 'performance' && (
        <Card>
          <div className="p-4">
            <EquityChart traderId={traderId} />
          </div>
        </Card>
      )}

      {tab === 'positions' && <PositionsTable positions={positions} />}

      {tab === 'decisions' && <DecisionFeed decisions={decisions} />}

      {tab === 'execution' && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <TradeExecutionMonitor positions={positions} decisions={decisions} />
          <StrategyComplianceMonitor records={decisions} />
        </div>
      )}

      {tab === 'ai' && <AILearning traderId={traderId} />}
    </>
  );
}
