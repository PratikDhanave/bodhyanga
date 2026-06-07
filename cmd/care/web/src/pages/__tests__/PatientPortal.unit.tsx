import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import PatientPortal from '../PatientPortal';
import I18nProvider from '../../i18n/I18nProvider';
import { mockFetch } from '../../test/setup';
import type { PatientRecord, PanelReport } from '../../types';

const PATIENT: PatientRecord = {
  patient_id: 'pt-synth-001',
  name: 'Synthetic Patient A',
  dob: '1980-03-15',
  conditions: ['diabetes', 'hypertension'],
  medications: ['metformin', 'lisinopril'],
  last_a1c: 7.2,
  last_bp_systolic: 132,
  last_bp_diastolic: 84,
  last_weight_lbs: 185.4,
};

const PANEL: PanelReport = {
  patients: 1,
  gaps: [
    { patient_id: 'pt-synth-001', measure: 'a1c', severity: 'high', reason: 'Last A1c > 7.0' },
    { patient_id: 'pt-synth-001', measure: 'bp', severity: 'medium', reason: 'BP trending above target' },
  ],
  by_condition: { diabetes: 1, hypertension: 1 },
};

const EMPTY_PANEL: PanelReport = {
  patients: 0,
  gaps: [],
  by_condition: {},
};

function renderPortal() {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <PatientPortal />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe('PatientPortal', () => {
  it('renders patient name and care plan when data loads', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [PATIENT];
      if (url.includes('/panel')) return PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText(/Synthetic Patient A/)).toBeInTheDocument();
    expect(screen.getByText('Your Care Plan')).toBeInTheDocument();
  });

  it('renders conditions and medications from the patient record', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [PATIENT];
      if (url.includes('/panel')) return PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText('diabetes')).toBeInTheDocument();
    expect(screen.getByText('hypertension')).toBeInTheDocument();
    expect(screen.getByText('metformin')).toBeInTheDocument();
    expect(screen.getByText('lisinopril')).toBeInTheDocument();
  });

  it('renders vitals table with A1c, BP, and weight', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [PATIENT];
      if (url.includes('/panel')) return PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText('Recent Vitals')).toBeInTheDocument();
    expect(screen.getByText('7.2')).toBeInTheDocument();
    expect(screen.getByText('132/84')).toBeInTheDocument();
    expect(screen.getByText('185.4')).toBeInTheDocument();
  });

  it('renders panel gaps relevant to the patient', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [PATIENT];
      if (url.includes('/panel')) return PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText('Last A1c > 7.0')).toBeInTheDocument();
    expect(screen.getByText('BP trending above target')).toBeInTheDocument();
  });

  it('shows empty state when no patients exist', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [];
      if (url.includes('/panel')) return EMPTY_PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText('No patient record found')).toBeInTheDocument();
  });

  it('shows error state when patient fetch fails', async () => {
    mockFetch(() => {
      return new Response('Internal Server Error', { status: 500, statusText: 'Internal Server Error' });
    });
    renderPortal();

    expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
  });

  it('renders health goals based on conditions', async () => {
    mockFetch((input) => {
      const url = String(input);
      if (url.includes('/patients')) return [PATIENT];
      if (url.includes('/panel')) return EMPTY_PANEL;
      return [];
    });
    renderPortal();

    expect(await screen.findByText('Maintain A1c below 7.0%')).toBeInTheDocument();
    expect(screen.getByText('Keep blood pressure below 130/80 mmHg')).toBeInTheDocument();
  });
});
