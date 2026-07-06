import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import RPM from '../RPM';
import I18nProvider from '../../i18n/I18nProvider';

// RPM uses hardcoded stub data -- no API calls needed, no mockFetch required.

function renderRPM() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <RPM />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('RPM', () => {
  it('renders the page title and subtitle', () => {
    renderRPM();

    expect(screen.getByText('RPM observations')).toBeInTheDocument();
    expect(screen.getByText(/Remote patient monitoring/)).toBeInTheDocument();
  });

  it('renders all stub patients in the table', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('Alice Okafor')).toBeInTheDocument();
    expect(view.getByText('Bob Lindström')).toBeInTheDocument();
    expect(view.getByText('Carlos Reyes')).toBeInTheDocument();
    expect(view.getByText('Daria Novak')).toBeInTheDocument();
    expect(view.getByText('Emi Tanaka')).toBeInTheDocument();
  });

  it('renders patient IDs', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('P-001')).toBeInTheDocument();
    expect(view.getByText('P-002')).toBeInTheDocument();
    expect(view.getByText('P-003')).toBeInTheDocument();
    expect(view.getByText('P-004')).toBeInTheDocument();
    expect(view.getByText('P-005')).toBeInTheDocument();
  });

  it('renders heart rate values', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('88')).toBeInTheDocument();
    expect(view.getByText('64')).toBeInTheDocument();
    expect(view.getByText('102')).toBeInTheDocument();
    expect(view.getByText('76')).toBeInTheDocument();
    expect(view.getByText('58')).toBeInTheDocument();
  });

  it('renders SpO2 and BP values', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('97%')).toBeInTheDocument();
    // Two patients have 99% SpO2 (Bob and Emi)
    expect(view.getAllByText('99%').length).toBe(2);
    expect(view.getByText('95%')).toBeInTheDocument();
    expect(view.getByText('128/82')).toBeInTheDocument();
    expect(view.getByText('145/90')).toBeInTheDocument();
  });

  it('renders alert indicators for patients with alerts', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('HR trending up')).toBeInTheDocument();
    expect(view.getByText('Tachycardia + elevated BP')).toBeInTheDocument();
  });

  it('renders table header columns', () => {
    renderRPM();

    const table = screen.getByRole('table', { name: 'Patient monitoring observations' });
    const view = within(table);
    expect(view.getByText('Patient')).toBeInTheDocument();
    expect(view.getByText('HR (bpm)')).toBeInTheDocument();
    expect(view.getByText('7-day trend')).toBeInTheDocument();
    expect(view.getByText('SpO2')).toBeInTheDocument();
    expect(view.getByText('BP')).toBeInTheDocument();
    expect(view.getByText('Alert')).toBeInTheDocument();
  });

  it('renders sparkline SVGs with aria labels', () => {
    const { container } = renderRPM();

    const sparklines = container.querySelectorAll('svg[aria-label]');
    expect(sparklines.length).toBe(5);
  });
});
