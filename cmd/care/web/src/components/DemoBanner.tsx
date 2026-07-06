import { useCallback, useEffect, useState } from 'react';

const STORAGE_KEY = 'bodh-demo-dismissed';
const RESHOW_MS = 30 * 60 * 1000; // 30 minutes

export default function DemoBanner() {
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    try {
      const raw = sessionStorage.getItem(STORAGE_KEY);
      if (raw) {
        const ts = Number(raw);
        if (Date.now() - ts < RESHOW_MS) {
          setVisible(false);
        } else {
          sessionStorage.removeItem(STORAGE_KEY);
        }
      }
    } catch {
      // sessionStorage unavailable — keep visible
    }
  }, []);

  const dismiss = useCallback(() => {
    setVisible(false);
    try {
      sessionStorage.setItem(STORAGE_KEY, String(Date.now()));
    } catch {
      // ignore
    }
  }, []);

  if (!visible) return null;

  return (
    <div
      role="status"
      className="flex items-center justify-between gap-4 px-4 py-2 text-sm font-medium
                 bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200
                 border-b border-amber-200 dark:border-amber-800"
    >
      <span>RESEARCH DEMO — Not for clinical use</span>
      <button
        onClick={dismiss}
        aria-label="Dismiss demo banner"
        className="shrink-0 rounded-md px-2 py-1 text-xs font-semibold
                   hover:bg-amber-200 dark:hover:bg-amber-800 transition-colors"
      >
        Dismiss
      </button>
    </div>
  );
}
