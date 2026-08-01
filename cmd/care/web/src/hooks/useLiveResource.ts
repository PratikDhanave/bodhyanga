import { useEffect, useRef } from 'react';
import { useApi } from './useApi';
import { subscribeLive, CONNECTED } from './liveEvents';

// useLiveResource is a drop-in replacement for useApi that makes a page
// event-driven: it fetches like useApi, and re-reads whenever a live event of a
// matching `kinds` type arrives (or on reconnect). No polling, no manual reload.
//
//   const { data, loading, error, reload } =
//     useLiveResource(() => api.reviews(scope), [scope], ['review_submitted']);
export function useLiveResource<T>(
  fn: () => Promise<T>,
  deps: unknown[],
  kinds: string[],
) {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const res = useApi(fn, deps);

  // Keep the latest reload in a ref so the long-lived subscription always calls
  // the current fetch closure.
  const reloadRef = useRef(res.reload);
  reloadRef.current = res.reload;

  const kindsKey = kinds.join(',');
  useEffect(() => {
    const want = new Set(kinds);
    const unsub = subscribeLive((ev) => {
      if (ev.type === CONNECTED || want.has(ev.type)) {
        reloadRef.current();
      }
    });
    return unsub;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kindsKey]);

  return res;
}
