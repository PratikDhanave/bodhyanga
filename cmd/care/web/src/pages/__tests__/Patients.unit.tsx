import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Patients from '../Patients';
import { mockFetch } from '../../test/setup';
import type { PatientRecord } from '../../types';

const PATIENTS: PatientRecord[] = [
  {
    patient_id: 'PAT-001',
    name: 'Jane Doe',
    conditions: ['CHF', 'T2DM'],
    medications: ['Metformin', 'Lisinopril'],
    last_a1c: 7.2,
    last_bp_systolic: 130,
    last_bp_diastolic: 85,
  },
  {
    patient_id: 'PAT-002',
    name: 'John Smith',
    conditions: ['COPD'],
    medications: [],
    discharged_at: '2026-06-05T14:00:00Z',
  },
];

function renderPatients() {
  return render(
    <MemoryRouter>
      <Patients />
    </MemoryRouter>,
  );
}

describe('Patients', () => {
  it('renders patient list table', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    expect(await screen.findByText('Jane Doe')).toBeInTheDocument();
    expect(screen.getByText('John Smith')).toBeInTheDocument();
  });

  it('renders conditions as badges', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByText('CHF')).toBeInTheDocument();
    expect(screen.getByText('T2DM')).toBeInTheDocument();
    expect(screen.getByText('COPD')).toBeInTheDocument();
  });

  it('renders medications list', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByText('Metformin, Lisinopril')).toBeInTheDocument();
  });

  it('shows blood pressure', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByText('130/85')).toBeInTheDocument();
  });

  it('shows empty state when no patients', async () => {
    mockFetch(() => []);
    renderPatients();
    expect(await screen.findByText('Registry is empty')).toBeInTheDocument();
  });

  it('shows subtitle with count', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByText('2 in registry')).toBeInTheDocument();
  });

  it('shows dashes for missing values', async () => {
    mockFetch(() => [{ patient_id: 'PAT-003' }]);
    renderPatients();

    await screen.findByText('PAT-003');
    // Name, medications, A1c, BP, discharged should all show dash
    const dashes = screen.getAllByText('–');
    expect(dashes.length).toBeGreaterThanOrEqual(4);
  });

  it('shows error state with retry', async () => {
    mockFetch(() => { throw new Error('Server error'); });
    renderPatients();
    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('renders search input', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByPlaceholderText(/Search by ID/)).toBeInTheDocument();
  });

  it('renders patient ID as link', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    const link = await screen.findByText('PAT-001');
    expect(link.closest('a')).toHaveAttribute('href', '/patients/PAT-001');
  });

  it('renders Refresh button', async () => {
    mockFetch(() => PATIENTS);
    renderPatients();

    await screen.findByText('Jane Doe');
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
  });
});
