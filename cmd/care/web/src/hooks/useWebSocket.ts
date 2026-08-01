import { useEffect, useRef, useState } from 'react';
import { getToken } from './useAuth';

// streamTicket exchanges the session JWT (sent as a header) for a short-lived
// token safe to place in a WebSocket URL. Returns '' on failure.
async function streamTicket(): Promise<string> {
  try {
    const token = getToken();
    const res = await fetch('/api/auth/stream-ticket', {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) return '';
    return (await res.json()).token ?? '';
  } catch {
    return '';
  }
}

export interface UseWebSocketResult {
  lastMessage: unknown;
  connected: boolean;
}

export function useWebSocket(path: string): UseWebSocketResult {
  const [lastMessage, setLastMessage] = useState<unknown>(null);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const backoffRef = useRef<number>(1000);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const unmountedRef = useRef(false);

  useEffect(() => {
    unmountedRef.current = false;

    async function connect() {
      if (unmountedRef.current) return;

      const ticket = await streamTicket();
      if (unmountedRef.current) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.host || 'localhost:8088';
      const sep = path.includes('?') ? '&' : '?';
      const auth = ticket ? `${sep}access_token=${encodeURIComponent(ticket)}` : '';
      const url = `${protocol}//${host}${path}${auth}`;

      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        if (unmountedRef.current) { ws.close(); return; }
        setConnected(true);
        backoffRef.current = 1000;
      };

      ws.onmessage = (event: MessageEvent) => {
        if (unmountedRef.current) return;
        try {
          const data = JSON.parse(event.data as string);
          setLastMessage(data);
        } catch {
          setLastMessage(event.data);
        }
      };

      ws.onclose = () => {
        if (unmountedRef.current) return;
        setConnected(false);
        const delay = Math.min(backoffRef.current, 30000);
        backoffRef.current = Math.min(backoffRef.current * 2, 30000);
        timerRef.current = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        ws.close();
      };
    }

    connect();

    return () => {
      unmountedRef.current = true;
      if (timerRef.current !== null) clearTimeout(timerRef.current);
      wsRef.current?.close();
    };
  }, [path]);

  return { lastMessage, connected };
}
