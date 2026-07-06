import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import FineTune from '../FineTune';
import { mockFetch } from '../../test/setup';
import I18nProvider from '../../i18n/I18nProvider';

const STATUS = {
  decided_reviews: 145,
  pending_reviews: 23,
  sft_lines: 290,
  dpo_pairs: 72,
};

function renderFineTune() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <FineTune />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('FineTune', () => {
  it('renders stat cards with all counts', async () => {
    mockFetch(() => STATUS);
    renderFineTune();

    expect(await screen.findByText('145')).toBeInTheDocument();
    expect(screen.getByText('23')).toBeInTheDocument();
    expect(screen.getByText('290')).toBeInTheDocument();
    expect(screen.getByText('72')).toBeInTheDocument();
  });

  it('renders stat labels', async () => {
    mockFetch(() => STATUS);
    renderFineTune();

    expect(await screen.findByText('Decided reviews')).toBeInTheDocument();
    expect(screen.getByText('Pending reviews')).toBeInTheDocument();
    expect(screen.getByText('Projected SFT lines')).toBeInTheDocument();
    expect(screen.getByText('Projected DPO pairs')).toBeInTheDocument();
  });

  it('renders the export instructions section', async () => {
    mockFetch(() => STATUS);
    renderFineTune();

    expect(await screen.findByText('How to export')).toBeInTheDocument();
    expect(screen.getByText(/export-hitl-feedback/)).toBeInTheDocument();
  });

  it('shows subtitle text', async () => {
    mockFetch(() => STATUS);
    renderFineTune();

    expect(await screen.findByText('HITL decisions as training data')).toBeInTheDocument();
  });

  it('renders the SFT and DPO output file names in the instructions', async () => {
    mockFetch(() => STATUS);
    renderFineTune();

    await screen.findByText('How to export');
    expect(screen.getByText(/sft\.jsonl/)).toBeInTheDocument();
    expect(screen.getByText(/dpo\.jsonl/)).toBeInTheDocument();
  });

  it('shows error state when API fails', async () => {
    mockFetch(() => new Response('Service Unavailable', { status: 503 }));
    renderFineTune();

    expect(await screen.findByText(/HTTP 503/)).toBeInTheDocument();
  });
});
