// liveEvents — one shared WebSocket to /ws/hitl, fanned out to all subscribers.
//
// This is the frontend half of the event-driven SPA: the backend mirrors every
// clinical bus message (and review submit/decide) onto the socket as a thin
// {type, payload} envelope; components subscribe to the kinds they care about
// and re-read the authoritative endpoint. One connection serves the whole app.
import { getToken } from './useAuth';

export interface LiveEvent {
  type: string;
  payload?: unknown;
  at?: string;
}

type Listener = (ev: LiveEvent) => void;

// CONNECTED is a synthetic event emitted on every (re)connect so subscribers can
// reload once and not miss events that occurred while the socket was down.
export const CONNECTED = '__connected__';

const listeners = new Set<Listener>();
let ws: WebSocket | null = null;
let starting = false;
let backoff = 1000;

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

async function connect(): Promise<void> {
  if (ws || starting || listeners.size === 0) return;
  starting = true;
  const ticket = await streamTicket();
  starting = false;
  if (listeners.size === 0) return;

  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = window.location.host || 'localhost:8088';
  const q = ticket ? `&access_token=${encodeURIComponent(ticket)}` : '';
  const sock = new WebSocket(`${proto}//${host}/ws/hitl?tenant_id=default${q}`);
  ws = sock;

  sock.onopen = () => {
    backoff = 1000;
    emit({ type: CONNECTED });
  };
  sock.onmessage = (e) => {
    try {
      emit(JSON.parse(e.data as string) as LiveEvent);
    } catch {
      /* ignore malformed frame */
    }
  };
  sock.onclose = () => {
    ws = null;
    if (listeners.size === 0) return;
    const delay = Math.min(backoff, 30000);
    backoff = Math.min(backoff * 2, 30000);
    setTimeout(connect, delay);
  };
  sock.onerror = () => sock.close();
}

function emit(ev: LiveEvent): void {
  listeners.forEach((l) => l(ev));
}

// subscribeLive registers a listener and ensures the shared socket is open.
// Returns an unsubscribe function; the socket closes when the last listener
// leaves.
export function subscribeLive(listener: Listener): () => void {
  listeners.add(listener);
  void connect();
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && ws) {
      ws.close();
      ws = null;
    }
  };
}
