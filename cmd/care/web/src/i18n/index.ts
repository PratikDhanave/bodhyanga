import { useContext, useCallback } from 'react';
import en from './en';
import es from './es';
import fr from './fr';
import zh from './zh';
import { I18nContext } from './I18nProvider';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type Locale = 'en' | 'es' | 'fr' | 'zh';
export type TranslationKey = string;
export type Translations = Record<TranslationKey, string>;

// ---------------------------------------------------------------------------
// Translation registry — build-time, no LLM dependency
// ---------------------------------------------------------------------------

const translations: Record<Locale, Translations> = { en, es, fr, zh };

export const SUPPORTED_LOCALES: readonly Locale[] = ['en', 'es', 'fr', 'zh'] as const;

export const LOCALE_LABELS: Record<Locale, string> = {
  en: 'English',
  es: 'Español',
  fr: 'Français',
  zh: '中文',
};

// ---------------------------------------------------------------------------
// Standalone translation function (works outside React)
// ---------------------------------------------------------------------------

export function t(key: TranslationKey, locale: Locale = 'en'): string {
  return translations[locale]?.[key] ?? translations.en[key] ?? key;
}

// ---------------------------------------------------------------------------
// React hook — reads locale from I18nContext
// ---------------------------------------------------------------------------

export function useI18n(): {
  t: (key: string) => string;
  locale: Locale;
  setLocale: (l: Locale) => void;
} {
  const ctx = useContext(I18nContext);
  if (!ctx) {
    throw new Error('useI18n must be used within an <I18nProvider>');
  }

  const boundT = useCallback(
    (key: string) => t(key, ctx.locale),
    [ctx.locale],
  );

  return { t: boundT, locale: ctx.locale, setLocale: ctx.setLocale };
}
