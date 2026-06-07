import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Breadcrumbs } from '../Breadcrumbs';

function renderBreadcrumbs(items: Array<{ label: string; to?: string }>) {
  return render(
    <MemoryRouter>
      <Breadcrumbs items={items} />
    </MemoryRouter>,
  );
}

describe('Breadcrumbs', () => {
  it('renders all items', () => {
    renderBreadcrumbs([
      { label: 'Reviews', to: '/reviews' },
      { label: 'Review #42' },
    ]);

    expect(screen.getByText('Reviews')).toBeInTheDocument();
    expect(screen.getByText('Review #42')).toBeInTheDocument();
  });

  it('renders links for non-last items with a "to" prop', () => {
    renderBreadcrumbs([
      { label: 'Home', to: '/' },
      { label: 'Reviews', to: '/reviews' },
      { label: 'Detail' },
    ]);

    expect(screen.getByRole('link', { name: 'Home' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'Reviews' })).toHaveAttribute('href', '/reviews');
  });

  it('renders the last item as plain text with aria-current=page', () => {
    renderBreadcrumbs([
      { label: 'Home', to: '/' },
      { label: 'Current' },
    ]);

    const current = screen.getByText('Current');
    expect(current.tagName).not.toBe('A');
    expect(current).toHaveAttribute('aria-current', 'page');
  });

  it('has nav landmark with aria-label', () => {
    renderBreadcrumbs([{ label: 'Home' }]);
    expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toBeInTheDocument();
  });

  it('renders separator chevrons between items', () => {
    const { container } = renderBreadcrumbs([
      { label: 'A', to: '/a' },
      { label: 'B', to: '/b' },
      { label: 'C' },
    ]);

    // Should have 2 separators (between A→B and B→C)
    const svgs = container.querySelectorAll('svg');
    expect(svgs.length).toBe(2);
  });
});
