import { useEffect, useRef, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { useWebSocket } from '../hooks/useWebSocket';
import { useToast } from './Toast';

type AlertPriority = 'urgent' | 'high' | 'normal' | 'low';

interface HitlAlert {
  id: string;
  message: string;
  timestamp: string;
  priority: AlertPriority;
}

interface DNDState {
  active: boolean;
  expiresAt: number | null;
}

const DND_DURATIONS = [
  { label: '15 min', ms: 15 * 60 * 1000 },
  { label: '30 min', ms: 30 * 60 * 1000 },
  { label: '1 hour', ms: 60 * 60 * 1000 },
];

function isHighPriority(p: AlertPriority): boolean {
  return p === 'urgent' || p === 'high';
}

export default function NotificationBell() {
  const { lastMessage, connected } = useWebSocket('/ws/hitl?tenant_id=default');
  const { showToast } = useToast();
  const [alerts, setAlerts] = useState<HitlAlert[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [open, setOpen] = useState(false);
  const [dnd, setDnd] = useState<DNDState>({ active: false, expiresAt: null });
  const panelRef = useRef<HTMLDivElement>(null);
  const prevMessageRef = useRef<unknown>(null);

  // Check if DND has expired
  const isDndActive = useCallback(() => {
    if (!dnd.active) return false;
    if (dnd.expiresAt !== null && Date.now() >= dnd.expiresAt) {
      return false;
    }
    return true;
  }, [dnd]);

  // Auto-expire DND
  useEffect(() => {
    if (!dnd.active || dnd.expiresAt === null) return;
    const remaining = dnd.expiresAt - Date.now();
    if (remaining <= 0) {
      setDnd({ active: false, expiresAt: null });
      return;
    }
    const timer = setTimeout(() => {
      setDnd({ active: false, expiresAt: null });
    }, remaining);
    return () => clearTimeout(timer);
  }, [dnd]);

  function enableDnd(durationMs: number) {
    setDnd({ active: true, expiresAt: Date.now() + durationMs });
  }

  function disableDnd() {
    setDnd({ active: false, expiresAt: null });
  }

  // Accumulate incoming HITL events
  useEffect(() => {
    if (lastMessage === null || lastMessage === prevMessageRef.current) return;
    prevMessageRef.current = lastMessage;

    const msg = lastMessage as Record<string, unknown>;
    const priority = (msg.priority as AlertPriority) ?? 'normal';
    const alert: HitlAlert = {
      id: (msg.id as string) ?? String(Date.now()),
      message: (msg.message as string) ?? 'New HITL review request',
      timestamp: (msg.timestamp as string) ?? new Date().toISOString(),
      priority,
    };

    setAlerts((prev) => [alert, ...prev].slice(0, 50));
    setUnreadCount((n) => n + 1);

    // Only toast for urgent/high priority, and only if DND is not active
    if (isHighPriority(priority) && !isDndActive()) {
      showToast('New review needs attention', 'warning');
    }
  }, [lastMessage, showToast, isDndActive]);

  // Close dropdown on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  // Badge color: red if any unread alert is urgent/high, amber otherwise
  const hasHighPriorityUnread = alerts.slice(0, unreadCount).some((a) => isHighPriority(a.priority));
  const badgeColor = hasHighPriorityUnread ? 'bg-red-500' : 'bg-amber-500';

  function handleBellClick() {
    setOpen((v) => !v);
    if (!open) setUnreadCount(0);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      setOpen(false);
    }
  }

  return (
    <div className="relative" ref={panelRef}>
      <button
        onClick={handleBellClick}
        aria-label="Notifications"
        className="relative p-1 rounded-md hover:bg-bodh-100 dark:hover:bg-gray-700 text-bodh-700 dark:text-gray-300"
      >
        {/* Bell SVG — crossed out when DND active */}
        <svg
          xmlns="http://www.w3.org/2000/svg"
          className={`h-5 w-5 ${isDndActive() ? 'opacity-50' : ''}`}
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          strokeWidth={2}
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11
               a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67
               6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595
               1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
          />
        </svg>
        {isDndActive() && (
          <span className="absolute top-0 left-0 w-full h-full flex items-center justify-center text-[10px] text-bodh-400 dark:text-gray-500 font-bold">
            DND
          </span>
        )}
        {/* Unread badge */}
        {unreadCount > 0 && (
          <span className={`absolute -top-1 -right-1 h-4 min-w-[1rem] px-0.5 flex items-center justify-center
                           rounded-full ${badgeColor} text-white text-[10px] font-bold leading-none`}>
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
        {/* Connection indicator */}
        <span
          className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full border border-white
                      ${connected ? 'bg-emerald-400' : 'bg-gray-400'}`}
          title={connected ? 'Connected' : 'Disconnected'}
        />
      </button>

      {/* Dropdown panel */}
      {open && (
        <div
          className="absolute right-0 top-8 z-40 w-72 rounded-lg shadow-lg border
                     bg-white border-bodh-200 dark:bg-gray-800 dark:border-gray-700"
          onKeyDown={handleKeyDown}
        >
          <div className="flex items-center justify-between px-3 py-2 border-b border-bodh-200 dark:border-gray-700">
            <span className="text-sm font-medium text-bodh-700 dark:text-gray-100">
              HITL Alerts
            </span>
            <div className="flex items-center gap-2">
              <Link
                to="/reviews"
                onClick={() => setOpen(false)}
                className="text-xs text-bodh-600 hover:underline dark:text-bodh-400"
              >
                View all
              </Link>
            </div>
          </div>

          {/* DND controls */}
          <div className="px-3 py-2 border-b border-bodh-100 dark:border-gray-700 flex items-center gap-2 text-xs">
            {isDndActive() ? (
              <>
                <span className="text-amber-600 dark:text-amber-400 font-medium">DND active</span>
                <button
                  onClick={disableDnd}
                  className="ml-auto text-bodh-600 dark:text-bodh-400 hover:underline"
                >
                  Turn off
                </button>
              </>
            ) : (
              <>
                <span className="text-bodh-400 dark:text-gray-500">Do not disturb:</span>
                {DND_DURATIONS.map((d) => (
                  <button
                    key={d.label}
                    onClick={() => enableDnd(d.ms)}
                    className="px-1.5 py-0.5 rounded bg-bodh-100 dark:bg-gray-700 text-bodh-600 dark:text-gray-300 hover:bg-bodh-200 dark:hover:bg-gray-600"
                  >
                    {d.label}
                  </button>
                ))}
              </>
            )}
          </div>

          <ul className="max-h-64 overflow-y-auto divide-y divide-bodh-100 dark:divide-gray-700">
            {alerts.length === 0 ? (
              <li className="px-3 py-4 text-xs text-center text-bodh-400 dark:text-gray-500">
                No alerts yet
              </li>
            ) : (
              alerts.map((a) => (
                <li key={a.id}>
                  <Link
                    to="/reviews"
                    onClick={() => setOpen(false)}
                    className="block px-3 py-2 hover:bg-bodh-50 dark:hover:bg-gray-700"
                  >
                    <div className="flex items-center gap-1.5">
                      {isHighPriority(a.priority) && (
                        <span className="inline-block w-1.5 h-1.5 rounded-full bg-red-500 flex-shrink-0" />
                      )}
                      <p className="text-xs text-bodh-700 dark:text-gray-200 truncate">
                        {a.message}
                      </p>
                    </div>
                    <p className="text-[10px] text-bodh-400 dark:text-gray-500 mt-0.5">
                      {new Date(a.timestamp).toLocaleString()}
                    </p>
                  </Link>
                </li>
              ))
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
