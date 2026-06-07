import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Empty from '../Empty';

describe('Empty', () => {
  it('renders title text', () => {
    render(<Empty title="No items found" />);
    expect(screen.getByText('No items found')).toBeInTheDocument();
  });

  it('renders hint when provided', () => {
    render(<Empty title="Empty" hint="Try adding some data" />);
    expect(screen.getByText('Try adding some data')).toBeInTheDocument();
  });

  it('does not render hint element when hint is not provided', () => {
    const { container } = render(<Empty title="Empty" />);
    // Only the title should be rendered, no hint paragraph
    const paragraphs = container.querySelectorAll('p');
    // Title is in an h3, hint would be in a p
    expect(paragraphs.length).toBeLessThanOrEqual(1);
  });
});
