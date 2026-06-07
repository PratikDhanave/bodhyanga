import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Modal } from '../Modal';

describe('Modal', () => {
  it('renders nothing when open is false', () => {
    const { container } = render(
      <Modal open={false} onClose={() => {}} title="Test">
        <p>Content</p>
      </Modal>,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders dialog when open is true', () => {
    render(
      <Modal open={true} onClose={() => {}} title="Confirm Action">
        <p>Are you sure?</p>
      </Modal>,
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Confirm Action')).toBeInTheDocument();
    expect(screen.getByText('Are you sure?')).toBeInTheDocument();
  });

  it('has correct ARIA attributes', () => {
    render(
      <Modal open={true} onClose={() => {}} title="Title">
        <p>Body</p>
      </Modal>,
    );
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAttribute('aria-labelledby', 'modal-title');
  });

  it('calls onClose when close button is clicked', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Modal open={true} onClose={onClose} title="Title">
        <p>Body</p>
      </Modal>,
    );

    await user.click(screen.getByLabelText('Close'));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('calls onClose when Escape is pressed', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Modal open={true} onClose={onClose} title="Title">
        <p>Body</p>
      </Modal>,
    );

    await user.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('calls onClose when backdrop is clicked', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Modal open={true} onClose={onClose} title="Title">
        <p>Body</p>
      </Modal>,
    );

    // The backdrop is the first div with aria-hidden="true"
    const backdrop = screen.getByRole('dialog').parentElement!.querySelector('[aria-hidden="true"]')!;
    await user.click(backdrop);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('renders with danger styling when danger prop is true', () => {
    render(
      <Modal open={true} onClose={() => {}} title="Delete?" danger>
        <p>This is dangerous</p>
      </Modal>,
    );
    const dialog = screen.getByRole('dialog');
    expect(dialog.className).toContain('border-red');

    const title = screen.getByText('Delete?');
    expect(title.className).toContain('text-red');
  });

  it('renders children content', () => {
    render(
      <Modal open={true} onClose={() => {}} title="Title">
        <button>Custom Action</button>
      </Modal>,
    );
    expect(screen.getByRole('button', { name: 'Custom Action' })).toBeInTheDocument();
  });
});
