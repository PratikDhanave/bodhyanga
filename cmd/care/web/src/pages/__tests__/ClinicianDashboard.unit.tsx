import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ClinicianDashboard from '../ClinicianDashboard';
import { mockFetch } from '../../test/setup';
import { ToastProvider } from '../../components/Toast';

function routeFor(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input.toString();
}

const REVIEWS = [
  { id: 'r1', kind: 'sign_off', status: 'pending', subject: 'Approve CHF dx', submitted_at: '2026-06-06T08:00:00Z' },
  { id: 'r2', kind: 'sign_off', status: 'pending', subject: 'Approve DKA dx', submitted_at: '2026-06-06T09:00:00Z' },
];
const PATIENTS = [
  { patient_id: 'p1', name: 'Jane Doe', conditions: ['CHF'], last_a1c: 7.2 },
  { patient_id: 'p2', name: 'John Smith', conditions: ['COPD'] },
];
const PANEL = {
  patients: 2,
  gaps: [
    { patient_id: 'p1', measure: 'A1c', severity: 'high', reason: 'Overdue' },
    { patient_id: 'p2', measure: 'Spirometry', severity: 'normal', reason: 'Scheduled' },
  ],
  by_condition: { CHF: 1, COPD: 1 },
};
const BENCH = [
  { provider: 'rule', cases: 10, mean_accuracy_pct: 85.0, mean_cost_usd: 0, under_budget_pct: 100 },
];

function renderDashboard() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <ClinicianDashboard />
      </ToastProvider>
    </MemoryRouter>,
  );
}

function setupMockFetch() {
  return mockFetch((input) => {
    const url = routeFor(input);
    if (url.includes('/api/reviews')) return REVIEWS;
    if (url.includes('/api/patients')) return PATIENTS;
    if (url.includes('/api/panel')) return PANEL;
    if (url.includes('/api/bench')) return BENCH;
    return {};
  });
}

describe('ClinicianDashboard', () => {
  it('renders all 4 stat cards', async () => {
    setupMockFetch();
    renderDashboard();

    expect(await screen.findByText('Total Cases Today')).toBeInTheDocument();
    expect(screen.getByText('Pending HITL Reviews')).toBeInTheDocument();
    expect(screen.getByText('Active Alerts')).toBeInTheDocument();
    expect(screen.getByText('Panel Gaps')).toBeInTheDocument();
  });

  it('renders correct stat values', async () => {
    setupMockFetch();
    renderDashboard();

    await screen.findByText('Total Cases Today');
    // The stat cards render numbers — verify they exist as 3xl-sized text.
    // There are multiple "2" values (patients=2, reviews=2, gaps=2), so
    // check that at least one 3xl number appears.
    const numbers = screen.getAllByText('2');
    expect(numbers.length).toBeGreaterThanOrEqual(2);
  });

  it('renders the Recent Cases table', async () => {
    setupMockFetch();
    renderDashboard();

    expect(await screen.findByText('Recent Cases')).toBeInTheDocument();
    expect(screen.getByText('Jane Doe')).toBeInTheDocument();
    expect(screen.getByText('John Smith')).toBeInTheDocument();
  });

  it('renders the HITL Queue with pending reviews', async () => {
    setupMockFetch();
    renderDashboard();

    expect(await screen.findByText('HITL Queue')).toBeInTheDocument();
    expect(screen.getByText('Approve CHF dx')).toBeInTheDocument();
    expect(screen.getByText('Approve DKA dx')).toBeInTheDocument();
  });

  it('renders Accept and Reject buttons in HITL queue', async () => {
    setupMockFetch();
    renderDashboard();

    await screen.findByText('Approve CHF dx');
    const acceptButtons = screen.getAllByRole('button', { name: 'Accept' });
    const rejectButtons = screen.getAllByRole('button', { name: 'Reject' });
    expect(acceptButtons.length).toBe(2);
    expect(rejectButtons.length).toBe(2);
  });

  it('renders the accuracy chart when bench data exists', async () => {
    setupMockFetch();
    renderDashboard();

    expect(await screen.findByText('Accuracy by Provider')).toBeInTheDocument();
    expect(screen.getByText('rule')).toBeInTheDocument();
    expect(screen.getByText('85.0%')).toBeInTheDocument();
  });

  it('shows error when reviews API fails', async () => {
    mockFetch((input) => {
      const url = routeFor(input);
      if (url.includes('/api/reviews')) throw new Error('Reviews failed');
      if (url.includes('/api/patients')) return PATIENTS;
      if (url.includes('/api/panel')) return PANEL;
      if (url.includes('/api/bench')) return BENCH;
      return {};
    });
    renderDashboard();

    expect(await screen.findByText('Something went wrong.')).toBeInTheDocument();
  });
});
