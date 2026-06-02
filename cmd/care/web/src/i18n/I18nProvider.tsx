import { createContext, useState, useEffect, type ReactNode } from 'react';
import type { Locale } from './index';

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
}

export const I18nContext = createContext<I18nContextValue | null>(null);

// ---------------------------------------------------------------------------
// Storage key
// ---------------------------------------------------------------------------

const STORAGE_KEY = 'bodh-locale';

function readStoredLocale(): Locale {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === 'en' || stored === 'es' || stored === 'fr' || stored === 'zh') {
      return stored;
    }
  } catch {
    // localStorage unavailable — fall back
  }
  return 'en';
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

export default function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readStoredLocale);

  const setLocale = (l: Locale) => {
    setLocaleState(l);
  };

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, locale);
    } catch {
      // ignore
    }
    // Update the lang attribute on <html> for accessibility / SEO
    document.documentElement.lang = locale;
  }, [locale]);

  return (
    <I18nContext.Provider value={{ locale, setLocale }}>
      {children}
    </I18nContext.Provider>
  );
}
