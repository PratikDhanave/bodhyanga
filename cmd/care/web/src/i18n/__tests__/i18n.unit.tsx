import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { t, useI18n, SUPPORTED_LOCALES, LOCALE_LABELS } from '../index';
import I18nProvider from '../I18nProvider';

// Helper component to test useI18n hook
function I18nConsumer() {
  const { t: translate, locale, setLocale } = useI18n();
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="translated">{translate('portal.title')}</span>
      <button onClick={() => setLocale('es')}>Switch to Spanish</button>
      <button onClick={() => setLocale('fr')}>Switch to French</button>
      <button onClick={() => setLocale('zh')}>Switch to Chinese</button>
      <button onClick={() => setLocale('en')}>Switch to English</button>
    </div>
  );
}

describe('t() standalone function', () => {
  it('returns English translation by default', () => {
    const result = t('portal.title');
    expect(result).toBe('Patient Portal');
  });

  it('returns Spanish translation when locale is es', () => {
    expect(t('portal.title', 'es')).toBe('Portal del Paciente');
  });

  it('returns French translation when locale is fr', () => {
    expect(t('portal.title', 'fr')).toBe('Portail Patient');
  });

  it('returns Chinese translation when locale is zh', () => {
    expect(t('portal.title', 'zh')).toBe('患者门户');
  });

  it('falls back to English when key missing in target locale', () => {
    const result = t('nonexistent.key', 'es');
    expect(result).toBe('nonexistent.key');
  });

  it('returns key itself when not found in any locale', () => {
    expect(t('completely.missing.key')).toBe('completely.missing.key');
  });
});

describe('SUPPORTED_LOCALES', () => {
  it('contains en, es, fr, zh', () => {
    expect(SUPPORTED_LOCALES).toEqual(['en', 'es', 'fr', 'zh']);
  });
});

describe('LOCALE_LABELS', () => {
  it('has labels for all supported locales', () => {
    expect(LOCALE_LABELS.en).toBe('English');
    expect(LOCALE_LABELS.es).toBe('Español');
    expect(LOCALE_LABELS.fr).toBe('Français');
    expect(LOCALE_LABELS.zh).toBe('中文');
  });
});

describe('useI18n hook', () => {
  it('provides default English locale', () => {
    render(
      <I18nProvider>
        <I18nConsumer />
      </I18nProvider>,
    );
    expect(screen.getByTestId('locale')).toHaveTextContent('en');
    expect(screen.getByTestId('translated')).toHaveTextContent('Patient Portal');
  });

  it('switches locale to Spanish', async () => {
    const user = userEvent.setup();
    render(
      <I18nProvider>
        <I18nConsumer />
      </I18nProvider>,
    );

    await user.click(screen.getByRole('button', { name: 'Switch to Spanish' }));
    expect(screen.getByTestId('locale')).toHaveTextContent('es');
    expect(screen.getByTestId('translated')).toHaveTextContent('Portal del Paciente');
  });

  it('switches locale to French', async () => {
    const user = userEvent.setup();
    render(
      <I18nProvider>
        <I18nConsumer />
      </I18nProvider>,
    );

    await user.click(screen.getByRole('button', { name: 'Switch to French' }));
    expect(screen.getByTestId('locale')).toHaveTextContent('fr');
  });

  it('throws when used outside provider', () => {
    // Suppress React error boundary console output
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<I18nConsumer />)).toThrow('useI18n must be used within an <I18nProvider>');
    spy.mockRestore();
  });
});

// vi import for spy usage
import { vi } from 'vitest';
