import { useEffect } from 'react';
import useSWR, { mutate } from 'swr';
import { api } from './lib/api';
import { useHashRoute } from './lib/router';
import { AppShell } from './components/AppShell';
import { OverviewPage } from './pages/OverviewPage';
import { TraderDetailPage } from './pages/TraderDetailPage';
import { ComparePage } from './pages/ComparePage';
import { LanguageProvider } from './contexts/LanguageContext';
import { Badge } from './components/ui/Badge';
import type { TraderInfo } from './types';

/**
 * App — Cirelay dashboard root. Keeps exactly three jobs:
 *   1. Wrap in LanguageProvider (legacy i18n context we preserve)
 *   2. Dispatch routing via useHashRoute → page component
 *   3. Wire WebSocket telemetry into SWR cache so live updates flow
 *
 * Everything else — layout, UI, data fetching — is owned by the pages/components.
 */

function AppInner() {
  const route = useHashRoute();
  const { data: traders } = useSWR<TraderInfo[]>('traders', api.getTraders, {
    refreshInterval: 10000,
  });

  // WebSocket telemetry: when any trader's state updates server-side,
  // invalidate that trader's SWR caches so UI reflects it immediately.
  // This mirrors the prior behavior but is now tolerant of multiple traders.
  useEffect(() => {
    const wsUrl = import.meta.env.DEV
      ? 'ws://localhost:8080/ws'
      : `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`;

    let ws: WebSocket;
    try {
      ws = new WebSocket(wsUrl);
    } catch {
      return; // WS is optional; SWR polling keeps us fresh
    }

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        const traderId: string | undefined = msg.trader_id ?? msg.data?.trader_id;
        if (!traderId) return;
        if (msg.type === 'portfolio_update') mutate(`account-${traderId}`, msg.data, false);
        else if (msg.type === 'order_update') mutate(`positions-${traderId}`, msg.data, false);
        else if (msg.type === 'strategy_update') mutate(`status-${traderId}`, msg.data, false);
      } catch (e) {
        console.error('WS parse error:', e);
      }
    };
    ws.onerror = () => { /* silently tolerate — polling keeps us live */ };
    return () => { if (ws && ws.readyState === 1) ws.close(); };
  }, []);

  // Render the right page based on current route.
  let page;
  if (route.path === '/trader/:id') {
    page = <TraderDetailPage traderId={route.params.id} />;
  } else if (route.path === '/compare') {
    page = <ComparePage />;
  } else {
    page = <OverviewPage />;
  }

  return (
    <AppShell
      statusSlot={
        <>
          <span className="tabular-nums">{new Date().toLocaleTimeString()}</span>
          <Badge variant={traders && traders.length > 0 ? 'profit' : 'muted'} dot size="xs">
            {traders ? `${traders.length} trader${traders.length === 1 ? '' : 's'}` : 'connecting…'}
          </Badge>
        </>
      }
    >
      {page}
    </AppShell>
  );
}

function App() {
  return (
    <LanguageProvider>
      <AppInner />
    </LanguageProvider>
  );
}

export default App;
