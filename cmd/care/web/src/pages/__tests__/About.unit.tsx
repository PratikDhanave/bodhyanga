import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import About from '../About';
import I18nProvider from '../../i18n/I18nProvider';

function renderAbout() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <About />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('About', () => {
  it('renders the page title and subtitle', () => {
    renderAbout();
    expect(screen.getByText('About')).toBeInTheDocument();
    expect(screen.getByText(/Medical Diagnostic Orchestrator/)).toBeInTheDocument();
  });

  it('renders the research disclaimer banner', () => {
    renderAbout();
    expect(screen.getByText('Research disclaimer')).toBeInTheDocument();
    expect(screen.getByText(/Do not use for real patient care/)).toBeInTheDocument();
  });

  it('renders repository links', () => {
    renderAbout();
    const repoLink = screen.getByText('github.com/PratikDhanave/bodh');
    expect(repoLink).toBeInTheDocument();
    expect(repoLink.closest('a')).toHaveAttribute('href', 'https://github.com/PratikDhanave/bodh');
  });

  it('renders documentation links', () => {
    renderAbout();
    expect(screen.getByText('docs/ai-features.md')).toBeInTheDocument();
    expect(screen.getByText('docs/setup.md')).toBeInTheDocument();
    expect(screen.getByText('docs/diagrams.md')).toBeInTheDocument();
  });

  it('renders MARA reference link', () => {
    renderAbout();
    const maraLink = screen.getByText(/Multi-Agent Reference Architecture/);
    expect(maraLink.closest('a')).toHaveAttribute(
      'href',
      'https://microsoft.github.io/multi-agent-reference-architecture/',
    );
  });
});
