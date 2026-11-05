import { useEffect, useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import NotificationBell from './NotificationBell';
import { clearToken } from '../hooks/useAuth';
import { useI18n, SUPPORTED_LOCALES, LOCALE_LABELS } from '../i18n';
import type { Locale } from '../i18n';

const nav = [
  { to: '/', label: 'Dashboard' },
  { to: '/dashboard', label: 'Clinician Dashboard' },
  { to: '/portal', label: 'Patient Portal' },
  { to: '/audit', label: 'Audit Log' },
  { to: '/runs', label: 'Case runner' },
  { to: '/reviews', label: 'HITL reviews' },
  { to: '/tasks', label: 'Nurse tasks' },
  { to: '/patients', label: 'Patients' },
  { to: '/panel', label: 'Panel report' },
  { to: '/agents', label: 'Agents' },
  { to: '/rpm', label: 'RPM' },
  { to: '/bench', label: 'Bench results' },
  { to: '/llm-traces', label: 'LLM traces' },
  { to: '/fine-tune', label: 'Fine-tune export' },
  { to: '/about', label: 'About' },
];

function useDarkMode() {
  const [dark, setDark] = useState<boolean>(() => {
    try {
      return localStorage.getItem('bodhyanga-dark') === 'true';
    } catch {
      return false;
    }
  });

  useEffect(() => {
    const root = document.documentElement;
    if (dark) {
      root.classList.add('dark');
    } else {
      root.classList.remove('dark');
    }
    try {
      localStorage.setItem('bodhyanga-dark', String(dark));
    } catch {
      // ignore
    }
  }, [dark]);

  const toggle = () => {
    document.documentElement.classList.toggle('dark');
    setDark((prev) => !prev);
  };

  return { dark, toggle };
}

export default function Layout() {
  const { dark, toggle } = useDarkMode();
  const { locale, setLocale } = useI18n();

  return (
    <div className="min-h-screen flex bg-white dark:bg-gray-900 text-bodhyanga-800 dark:text-gray-100">
      <aside className="w-56 bg-white dark:bg-gray-800 border-r border-bodhyanga-200 dark:border-gray-700 flex flex-col">
        {/* Sidebar header */}
        <div className="px-4 py-4 border-b border-bodhyanga-200 dark:border-gray-700 flex items-center justify-between">
          <div>
            <div className="font-semibold text-bodhyanga-800 dark:text-gray-100">Bodhyanga</div>
            <div className="text-xs text-bodhyanga-400 dark:text-gray-400">Clinician console</div>
          </div>
          <div className="flex items-center gap-1">
            {/* Language selector */}
            <div className="relative">
              <select
                value={locale}
                onChange={(e) => setLocale(e.target.value as Locale)}
                aria-label="Select language"
                className="appearance-none bg-transparent text-xs pl-5 pr-1 py-1 rounded-md
                           hover:bg-bodhyanga-100 dark:hover:bg-gray-700
                           text-bodhyanga-700 dark:text-gray-300 transition-colors cursor-pointer
                           border-none outline-none"
              >
                {SUPPORTED_LOCALES.map((l) => (
                  <option key={l} value={l}>{LOCALE_LABELS[l]}</option>
                ))}
              </select>
              {/* Globe icon positioned over the select */}
              <svg
                xmlns="http://www.w3.org/2000/svg"
                className="h-3.5 w-3.5 absolute left-1 top-1/2 -translate-y-1/2 pointer-events-none
                           text-bodhyanga-500 dark:text-gray-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth={2}
              >
                <path strokeLinecap="round" strokeLinejoin="round"
                  d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0
                     01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657
                     0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9" />
              </svg>
            </div>
            {/* Dark / light toggle */}
            <button
              onClick={toggle}
              aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
              className="p-1 rounded-md hover:bg-bodhyanga-100 dark:hover:bg-gray-700
                         text-bodhyanga-700 dark:text-gray-300 transition-colors"
            >
              {dark ? (
                /* Sun icon */
                <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none"
                     viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round"
                    d="M12 3v1m0 16v1m8.66-9H21M3 12H2m15.364-6.364l-.707.707
                       M6.343 17.657l-.707.707m12.728 0l-.707-.707M6.343
                       6.343l-.707-.707M12 8a4 4 0 100 8 4 4 0 000-8z" />
                </svg>
              ) : (
                /* Moon icon */
                <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none"
                     viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round"
                    d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
                </svg>
              )}
            </button>
          </div>
        </div>

        {/* Navigation */}
        <nav className="flex-1 p-2 space-y-1 overflow-y-auto">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === '/'}
              className={({ isActive }) =>
                isActive ? 'nav-link nav-link-active' : 'nav-link'
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>

        <div className="px-4 py-3 border-t border-bodhyanga-200 dark:border-gray-700 space-y-2">
          <button
            onClick={() => {
              clearToken();
              window.location.href = '/ui/login';
            }}
            className="nav-link w-full text-left"
          >
            Log out
          </button>
          <div className="text-xs text-bodhyanga-400 dark:text-gray-500">
            Research demo. Not for clinical use.
          </div>
        </div>
      </aside>

      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Top header bar */}
        <header className="h-10 flex items-center justify-end px-4 border-b border-bodhyanga-200 dark:border-gray-700 bg-white dark:bg-gray-800 shrink-0">
          <NotificationBell />
        </header>

        {/* Page content */}
        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
