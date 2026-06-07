import { useId } from 'react';

export interface Tab {
  key: string;
  label: string;
  count?: number;
}

interface TabsProps {
  tabs: Tab[];
  active: string;
  onChange: (key: string) => void;
  /** Optional size variant */
  size?: 'sm' | 'md';
}

/**
 * Segmented control / tab bar for filtering views.
 * Replaces ad-hoc button groups in Tasks, Reviews, AuditLog, etc.
 *
 * Accessibility: uses role="tablist" + role="tab" with aria-selected,
 * keyboard arrows to navigate between tabs.
 */
export function Tabs({ tabs, active, onChange, size = 'md' }: TabsProps) {
  const id = useId();
  const sizeClasses = size === 'sm' ? 'text-xs px-2.5 py-1' : 'text-sm px-3.5 py-1.5';

  function handleKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    const idx = tabs.findIndex((t) => t.key === active);
    if (idx < 0) return;
    let next = idx;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      next = (idx + 1) % tabs.length;
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      next = (idx - 1 + tabs.length) % tabs.length;
    } else if (e.key === 'Home') {
      next = 0;
    } else if (e.key === 'End') {
      next = tabs.length - 1;
    } else {
      return;
    }
    e.preventDefault();
    onChange(tabs[next].key);
    // Focus the newly active tab button
    const btn = document.getElementById(`${id}-tab-${tabs[next].key}`);
    btn?.focus();
  }

  return (
    <div
      role="tablist"
      aria-label="Filter tabs"
      className="inline-flex rounded-lg bg-bodh-100 dark:bg-gray-800 p-0.5 gap-0.5"
      onKeyDown={handleKeyDown}
    >
      {tabs.map((tab) => {
        const isActive = tab.key === active;
        return (
          <button
            key={tab.key}
            id={`${id}-tab-${tab.key}`}
            role="tab"
            aria-selected={isActive}
            tabIndex={isActive ? 0 : -1}
            onClick={() => onChange(tab.key)}
            className={`
              ${sizeClasses} rounded-md font-medium transition-all duration-150
              focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-bodh-500
              ${
                isActive
                  ? 'bg-white dark:bg-gray-700 text-bodh-800 dark:text-gray-100 shadow-sm'
                  : 'text-bodh-500 dark:text-gray-400 hover:text-bodh-700 dark:hover:text-gray-200'
              }
            `}
          >
            {tab.label}
            {tab.count !== undefined && (
              <span
                className={`ml-1.5 text-xs font-normal ${
                  isActive
                    ? 'text-bodh-400 dark:text-gray-400'
                    : 'text-bodh-300 dark:text-gray-500'
                }`}
              >
                {tab.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
