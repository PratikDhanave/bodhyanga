import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { useWebSocket } from '../hooks/useWebSocket';
import { useToast } from './Toast';

interface HitlAlert {
  id: string;
  message: string;
  timestamp: string;
}

export default function NotificationBell() {
  const { lastMessage, connected } = useWebSocket('/ws/hitl?tenant_id=default');
  const { showToast } = useToast();
  const [alerts, setAlerts] = useState<HitlAlert[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [open, setOpen] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const prevMessageRef = useRef<unknown>(null);

  // Accumulate incoming HITL events
  useEffect(() => {
    if (lastMessage === null || lastMessage === prevMessageRef.current) return;
    prevMessageRef.current = lastMessage;

    const msg = lastMessage as Record<string, unknown>;
    const alert: HitlAlert = {
      id: (msg.id as string) ?? String(Date.now()),
      message: (msg.message as string) ?? 'New HITL review request',
      timestamp: (msg.timestamp as string) ?? new Date().toISOString(),
    };

    setAlerts((prev) => [alert, ...prev].slice(0, 50));
    setUnreadCount((n) => n + 1);
    showToast('New review needs attention', 'warning');
  }, [lastMessage, showToast]);

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
        {/* Bell SVG */}
        <svg
          xmlns="http://www.w3.org/2000/svg"
          className="h-5 w-5"
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
        {/* Unread badge */}
        {unreadCount > 0 && (
          <span className="absolute -top-1 -right-1 h-4 min-w-[1rem] px-0.5 flex items-center justify-center
                           rounded-full bg-red-500 text-white text-[10px] font-bold leading-none">
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
            <Link
              to="/reviews"
              onClick={() => setOpen(false)}
              className="text-xs text-bodh-600 hover:underline dark:text-bodh-400"
            >
              View all
            </Link>
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
                    <p className="text-xs text-bodh-700 dark:text-gray-200 truncate">
                      {a.message}
                    </p>
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
