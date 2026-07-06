import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import PanelReport from '../PanelReport';
import { mockFetch } from '../../test/setup';
import type { PanelReport as PanelReportData } from '../../types';
import I18nProvider from '../../i18n/I18nProvider';

const REPORT: PanelReportData = {
  patients: 42,
  gaps: [
    { patient_id: 'synth-001', measure: 'A1c', severity: 'high', reason: 'A1c not checked in 6 months' },
    { patient_id: 'synth-002', measure: 'Mammogram', severity: 'urgent', reason: 'Overdue screening' },
    { patient_id: 'synth-003', measure: 'Flu vaccine', severity: 'normal', reason: 'Annual flu vaccine due' },
  ],
  by_condition: { diabetes: 18, hypertension: 12, copd: 7 },
};

function renderPanel() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <PanelReport />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('PanelReport', () => {
  it('renders the gaps table with all gap rows', async () => {
    mockFetch(() => REPORT);
    renderPanel();

    const table = await screen.findByRole('table', { name: 'Panel report metrics' });
    const view = within(table);
    expect(view.getByText('A1c not checked in 6 months')).toBeInTheDocument();
    expect(view.getByText('Overdue screening')).toBeInTheDocument();
    expect(view.getByText('Annual flu vaccine due')).toBeInTheDocument();
  });

  it('displays the subtitle with patient and gap counts', async () => {
    mockFetch(() => REPORT);
    renderPanel();

    expect(await screen.findByText(/42 patients/)).toBeInTheDocument();
    expect(screen.getByText(/3 open gaps/)).toBeInTheDocument();
  });

  it('renders the by-condition breakdown', async () => {
    mockFetch(() => REPORT);
    renderPanel();

    expect(await screen.findByText('diabetes')).toBeInTheDocument();
    expect(screen.getByText('18')).toBeInTheDocument();
    expect(screen.getByText('hypertension')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('copd')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
  });

  it('shows empty state when no gaps exist', async () => {
    mockFetch(() => ({ patients: 0, gaps: [], by_condition: {} }));
    renderPanel();

    expect(await screen.findByText('No gaps detected')).toBeInTheDocument();
  });

  it('shows error state when API fails', async () => {
    mockFetch(() => new Response('Server error', { status: 500 }));
    renderPanel();

    expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
  });

  it('renders severity badges for urgent and high gaps', async () => {
    mockFetch(() => REPORT);
    renderPanel();

    const table = await screen.findByRole('table', { name: 'Panel report metrics' });
    const view = within(table);
    const highBadge = view.getByText('high');
    expect(highBadge.className).toContain('badge-urgent');
    const urgentBadge = view.getByText('urgent');
    expect(urgentBadge.className).toContain('badge-urgent');
  });
});
