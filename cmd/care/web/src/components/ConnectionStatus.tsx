import { useState, useEffect } from 'react';

type Status = 'connected' | 'disconnected' | 'checking';

const STATUS_CONFIG: Record<Status, { color: string; label: string }> = {
  connected: { color: 'bg-green-500', label: 'Connected' },
  disconnected: { color: 'bg-red-500', label: 'Disconnected' },
  checking: { color: 'bg-yellow-500 animate-pulse', label: 'Checking…' },
};

/**
 * Global connection health indicator.
 * Checks GET /api/health every 30s and shows a green/red dot in the header.
 */
export function ConnectionStatus() {
  const [status, setStatus] = useState<Status>('checking');

  useEffect(() => {
    let mounted = true;

    async function check() {
      try {
        const res = await fetch('/api/health', { method: 'GET' });
        if (mounted) setStatus(res.ok ? 'connected' : 'disconnected');
      } catch {
        if (mounted) setStatus('disconnected');
      }
    }

    check();
    const interval = setInterval(check, 30_000);
    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, []);

  const config = STATUS_CONFIG[status];

  return (
    <div className="flex items-center gap-1.5 text-xs text-bodh-400 dark:text-gray-500" title={`API: ${config.label}`}>
      <span className={`inline-block w-2 h-2 rounded-full ${config.color}`} />
      <span className="hidden sm:inline">{config.label}</span>
    </div>
  );
}
