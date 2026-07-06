import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import DemoBanner from '../DemoBanner';

beforeEach(() => {
  sessionStorage.clear();
});

describe('DemoBanner', () => {
  it('renders banner text on mount', () => {
    render(<DemoBanner />);
    expect(screen.getByText(/RESEARCH DEMO/)).toBeInTheDocument();
  });

  it('dismiss hides the banner', () => {
    render(<DemoBanner />);
    fireEvent.click(screen.getByLabelText('Dismiss demo banner'));
    expect(screen.queryByText(/RESEARCH DEMO/)).not.toBeInTheDocument();
  });

  it('stores dismiss timestamp in sessionStorage', () => {
    render(<DemoBanner />);
    fireEvent.click(screen.getByLabelText('Dismiss demo banner'));
    const stored = sessionStorage.getItem('bodh-demo-dismissed');
    expect(stored).toBeTruthy();
    expect(Number(stored)).toBeGreaterThan(0);
  });

  it('does not show if dismissed less than 30 minutes ago', () => {
    sessionStorage.setItem('bodh-demo-dismissed', String(Date.now()));
    render(<DemoBanner />);
    expect(screen.queryByText(/RESEARCH DEMO/)).not.toBeInTheDocument();
  });

  it('re-shows after 30 minutes since dismiss', () => {
    const thirtyOneMinAgo = Date.now() - 31 * 60 * 1000;
    sessionStorage.setItem('bodh-demo-dismissed', String(thirtyOneMinAgo));
    render(<DemoBanner />);
    expect(screen.getByText(/RESEARCH DEMO/)).toBeInTheDocument();
  });

  it('has role="status" for screen readers', () => {
    render(<DemoBanner />);
    expect(screen.getByRole('status')).toBeInTheDocument();
  });
});
