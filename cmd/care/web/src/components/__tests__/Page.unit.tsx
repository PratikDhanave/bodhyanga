import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Page from '../Page';

describe('Page', () => {
  it('renders title in an h1', () => {
    render(<Page title="Dashboard"><div>content</div></Page>);
    const heading = screen.getByRole('heading', { level: 1 });
    expect(heading).toHaveTextContent('Dashboard');
  });

  it('renders subtitle when provided', () => {
    render(<Page title="Title" subtitle="A subtitle"><div /></Page>);
    expect(screen.getByText('A subtitle')).toBeInTheDocument();
  });

  it('does not render subtitle element when not provided', () => {
    const { container } = render(<Page title="Title"><div /></Page>);
    const paragraphs = container.querySelectorAll('p');
    expect(paragraphs.length).toBe(0);
  });

  it('renders actions slot', () => {
    render(
      <Page title="Title" actions={<button>Save</button>}>
        <div />
      </Page>,
    );
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
  });

  it('does not render actions wrapper when not provided', () => {
    const { container } = render(<Page title="Title"><div /></Page>);
    // Actions are rendered in a flex gap-2 div inside header
    const header = container.querySelector('header');
    expect(header?.children.length).toBe(1); // only the title div
  });

  it('renders children', () => {
    render(<Page title="Title"><p>Child content</p></Page>);
    expect(screen.getByText('Child content')).toBeInTheDocument();
  });
});
