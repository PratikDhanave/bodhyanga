import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Tabs } from '../Tabs';
import type { Tab } from '../Tabs';

const TABS: Tab[] = [
  { key: 'all', label: 'All', count: 10 },
  { key: 'pending', label: 'Pending', count: 3 },
  { key: 'approved', label: 'Approved', count: 7 },
];

describe('Tabs', () => {
  it('renders all tab labels', () => {
    render(<Tabs tabs={TABS} active="all" onChange={() => {}} />);

    expect(screen.getByRole('tab', { name: /All/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Pending/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Approved/ })).toBeInTheDocument();
  });

  it('marks the active tab with aria-selected=true', () => {
    render(<Tabs tabs={TABS} active="pending" onChange={() => {}} />);

    expect(screen.getByRole('tab', { name: /Pending/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: /All/ })).toHaveAttribute('aria-selected', 'false');
  });

  it('calls onChange when a tab is clicked', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Tabs tabs={TABS} active="all" onChange={onChange} />);

    await user.click(screen.getByRole('tab', { name: /Approved/ }));
    expect(onChange).toHaveBeenCalledWith('approved');
  });

  it('supports keyboard navigation with arrow keys', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Tabs tabs={TABS} active="all" onChange={onChange} />);

    // Focus the active tab and press ArrowRight
    screen.getByRole('tab', { name: /All/ }).focus();
    await user.keyboard('{ArrowRight}');
    expect(onChange).toHaveBeenCalledWith('pending');
  });

  it('wraps around on ArrowRight from last tab', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Tabs tabs={TABS} active="approved" onChange={onChange} />);

    screen.getByRole('tab', { name: /Approved/ }).focus();
    await user.keyboard('{ArrowRight}');
    expect(onChange).toHaveBeenCalledWith('all');
  });

  it('supports Home and End keys', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Tabs tabs={TABS} active="pending" onChange={onChange} />);

    screen.getByRole('tab', { name: /Pending/ }).focus();
    await user.keyboard('{End}');
    expect(onChange).toHaveBeenCalledWith('approved');
  });

  it('renders counts when provided', () => {
    render(<Tabs tabs={TABS} active="all" onChange={() => {}} />);

    expect(screen.getByText('10')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
  });

  it('renders without counts', () => {
    const noCounts: Tab[] = [
      { key: 'a', label: 'Alpha' },
      { key: 'b', label: 'Beta' },
    ];
    render(<Tabs tabs={noCounts} active="a" onChange={() => {}} />);

    expect(screen.getByRole('tab', { name: 'Alpha' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Beta' })).toBeInTheDocument();
  });

  it('uses role=tablist on the container', () => {
    render(<Tabs tabs={TABS} active="all" onChange={() => {}} />);
    expect(screen.getByRole('tablist')).toBeInTheDocument();
  });
});
