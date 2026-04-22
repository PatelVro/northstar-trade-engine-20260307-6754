import { useEffect, useState, useCallback } from 'react';

/**
 * Minimal hash router used across Cirelay pages.
 *
 * Supported routes:
 *   #/           → { path: '/', params: {} }
 *   #/trader/abc → { path: '/trader/:id', params: { id: 'abc' } }
 *   #/compare    → { path: '/compare', params: {} }
 *
 * We deliberately avoid react-router to keep the bundle lean. This is all
 * the routing we need for a 3-page app; if it grows beyond that we can
 * swap to a real router without disturbing pages.
 */

export interface Route {
  path: string;
  params: Record<string, string>;
}

function parseHash(hash: string): Route {
  const cleaned = hash.replace(/^#\/?/, '').trim();
  if (cleaned === '' || cleaned === '/') return { path: '/', params: {} };

  const parts = cleaned.split('/').filter(Boolean);
  if (parts[0] === 'trader' && parts.length >= 2) {
    return { path: '/trader/:id', params: { id: decodeURIComponent(parts.slice(1).join('/')) } };
  }
  if (parts[0] === 'compare') {
    return { path: '/compare', params: {} };
  }
  // Fallback to home so unrecognized hashes don't break the app.
  return { path: '/', params: {} };
}

export function useHashRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash));

  useEffect(() => {
    const onHashChange = () => setRoute(parseHash(window.location.hash));
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  return route;
}

export function navigate(path: string): void {
  // Ensure exactly one leading slash after the hash.
  const normalized = path.startsWith('/') ? path : `/${path}`;
  window.location.hash = normalized;
}

export function useNavigate() {
  return useCallback((path: string) => navigate(path), []);
}
