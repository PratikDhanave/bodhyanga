import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import RPM from '../RPM';

// RPM uses hardcoded stub data -- no API calls needed, no mockFetch required.

function renderRPM() {
  return render(
    <MemoryRouter>
      <RPM />
    </MemoryRouter>,
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

    expect(screen.getByText('Alice Okafor')).toBeInTheDocument();
    expect(screen.getByText('Bob Lindström')).toBeInTheDocument();
    expect(screen.getByText('Carlos Reyes')).toBeInTheDocument();
    expect(screen.getByText('Daria Novak')).toBeInTheDocument();
    expect(screen.getByText('Emi Tanaka')).toBeInTheDocument();
  });

  it('renders patient IDs', () => {
    renderRPM();

    expect(screen.getByText('P-001')).toBeInTheDocument();
    expect(screen.getByText('P-002')).toBeInTheDocument();
    expect(screen.getByText('P-003')).toBeInTheDocument();
    expect(screen.getByText('P-004')).toBeInTheDocument();
    expect(screen.getByText('P-005')).toBeInTheDocument();
  });

  it('renders heart rate values', () => {
    renderRPM();

    expect(screen.getByText('88')).toBeInTheDocument();
    expect(screen.getByText('64')).toBeInTheDocument();
    expect(screen.getByText('102')).toBeInTheDocument();
    expect(screen.getByText('76')).toBeInTheDocument();
    expect(screen.getByText('58')).toBeInTheDocument();
  });

  it('renders SpO2 and BP values', () => {
    renderRPM();

    expect(screen.getByText('97%')).toBeInTheDocument();
    // Two patients have 99% SpO2 (Bob and Emi)
    expect(screen.getAllByText('99%').length).toBe(2);
    expect(screen.getByText('95%')).toBeInTheDocument();
    expect(screen.getByText('128/82')).toBeInTheDocument();
    expect(screen.getByText('145/90')).toBeInTheDocument();
  });

  it('renders alert indicators for patients with alerts', () => {
    renderRPM();

    expect(screen.getByText('HR trending up')).toBeInTheDocument();
    expect(screen.getByText('Tachycardia + elevated BP')).toBeInTheDocument();
  });

  it('renders table header columns', () => {
    renderRPM();

    expect(screen.getByText('Patient')).toBeInTheDocument();
    expect(screen.getByText('HR (bpm)')).toBeInTheDocument();
    expect(screen.getByText('7-day trend')).toBeInTheDocument();
    expect(screen.getByText('SpO2')).toBeInTheDocument();
    expect(screen.getByText('BP')).toBeInTheDocument();
    expect(screen.getByText('Alert')).toBeInTheDocument();
  });

  it('renders sparkline SVGs with aria labels', () => {
    const { container } = renderRPM();

    const sparklines = container.querySelectorAll('svg[aria-label]');
    expect(sparklines.length).toBe(5);
  });
});
