import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatCard } from '../StatCard';

describe('StatCard', () => {
  it('renders label and value', () => {
    render(<StatCard label="Patients" value={42} />);

    expect(screen.getByText('Patients')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
  });

  it('renders subtitle when provided', () => {
    render(<StatCard label="Spend" value="$12.50" sub="Budget $10.00" />);
    expect(screen.getByText('Budget $10.00')).toBeInTheDocument();
  });

  it('does not render subtitle when not provided', () => {
    const { container } = render(<StatCard label="Count" value={0} />);
    // Only label and value text should exist
    expect(container.querySelectorAll('.text-xs.text-bodh-400')).toHaveLength(1); // just the label
  });

  it('renders with green variant styling', () => {
    const { container } = render(<StatCard label="Savings" value="$5" variant="green" />);
    const card = container.querySelector('.card');
    expect(card?.className).toContain('border-green-300');
  });

  it('renders with amber variant styling', () => {
    const { container } = render(<StatCard label="Warning" value="!" variant="amber" />);
    const card = container.querySelector('.card');
    expect(card?.className).toContain('border-amber-300');
  });

  it('renders icon when provided', () => {
    render(<StatCard label="Status" value="OK" icon={<span data-testid="icon">★</span>} />);
    expect(screen.getByTestId('icon')).toBeInTheDocument();
  });

  it('renders string values correctly', () => {
    render(<StatCard label="Cost" value="$0.245" />);
    expect(screen.getByText('$0.245')).toBeInTheDocument();
  });
});
