import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ConfidenceDisplay, { categorize } from '../ConfidenceDisplay';

describe('categorize', () => {
  it('returns Low for values below 0.5', () => {
    expect(categorize(0).label).toBe('Low');
    expect(categorize(0.49).label).toBe('Low');
  });

  it('returns Moderate for values 0.5 to 0.7', () => {
    expect(categorize(0.5).label).toBe('Moderate');
    expect(categorize(0.69).label).toBe('Moderate');
  });

  it('returns High for values 0.7 to 0.9', () => {
    expect(categorize(0.7).label).toBe('High');
    expect(categorize(0.89).label).toBe('High');
  });

  it('returns Very High for values >= 0.9', () => {
    expect(categorize(0.9).label).toBe('Very High');
    expect(categorize(1.0).label).toBe('Very High');
  });

  it('clamps values outside 0-1', () => {
    expect(categorize(-0.5).label).toBe('Low');
    expect(categorize(1.5).label).toBe('Very High');
  });
});

describe('ConfidenceDisplay', () => {
  it('renders category label and percentage', () => {
    render(<ConfidenceDisplay value={0.85} />);
    expect(screen.getByText('High')).toBeInTheDocument();
    expect(screen.getByText('(85%)')).toBeInTheDocument();
  });

  it('rounds percentage correctly', () => {
    render(<ConfidenceDisplay value={0.923} />);
    expect(screen.getByText('(92%)')).toBeInTheDocument();
  });

  it('shows tooltip on hover', () => {
    render(<ConfidenceDisplay value={0.85} />);
    const el = screen.getByLabelText(/Confidence: High/);
    fireEvent.mouseEnter(el);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.mouseLeave(el);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('does not show tooltip when showTooltip is false', () => {
    render(<ConfidenceDisplay value={0.85} showTooltip={false} />);
    const el = screen.getByLabelText(/Confidence: High/);
    fireEvent.mouseEnter(el);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('renders sm size variant', () => {
    const { container } = render(<ConfidenceDisplay value={0.5} size="sm" />);
    expect(container.querySelector('.text-xs')).toBeTruthy();
  });

  it('has accessible aria-label', () => {
    render(<ConfidenceDisplay value={0.45} />);
    expect(screen.getByLabelText('Confidence: Low (45%)')).toBeInTheDocument();
  });
});
