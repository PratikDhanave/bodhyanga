import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import Loader, { ErrorBox } from '../Loader';

describe('Loader', () => {
  it('renders default loading text', () => {
    render(<Loader />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });

  it('renders custom "what" text', () => {
    render(<Loader what="patients" />);
    expect(screen.getByText('Loading patients…')).toBeInTheDocument();
  });
});

describe('ErrorBox', () => {
  it('renders error message', () => {
    render(<ErrorBox error={new Error('Something broke')} />);
    expect(screen.getByText('Something went wrong.')).toBeInTheDocument();
    expect(screen.getByText('Something broke')).toBeInTheDocument();
  });

  it('renders retry button when retry prop is provided', () => {
    const retry = () => {};
    render(<ErrorBox error={new Error('fail')} retry={retry} />);
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('does not render retry button when retry is not provided', () => {
    render(<ErrorBox error={new Error('fail')} />);
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
  });

  it('calls retry when button is clicked', async () => {
    let called = false;
    const retry = () => { called = true; };
    const user = userEvent.setup();
    render(<ErrorBox error={new Error('fail')} retry={retry} />);

    await user.click(screen.getByRole('button', { name: 'Retry' }));
    expect(called).toBe(true);
  });
});
