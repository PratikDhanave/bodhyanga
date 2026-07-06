import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import ConfirmationModal from '../ConfirmationModal';

const defaults = {
  isOpen: true,
  title: 'Confirm Approval',
  diagnosisSummary: 'Congestive Heart Failure (CHF)',
  patientContext: 'Jane Doe, 72, Female',
  confidence: 0.85,
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
};

describe('ConfirmationModal', () => {
  it('renders diagnosis summary and patient context', () => {
    render(<ConfirmationModal {...defaults} />);
    expect(screen.getByText('Congestive Heart Failure (CHF)')).toBeInTheDocument();
    expect(screen.getByText('Jane Doe, 72, Female')).toBeInTheDocument();
  });

  it('renders ConfidenceDisplay with passed value', () => {
    render(<ConfirmationModal {...defaults} />);
    expect(screen.getByText('High')).toBeInTheDocument();
    expect(screen.getByText('(85%)')).toBeInTheDocument();
  });

  it('has a required reason textarea', () => {
    render(<ConfirmationModal {...defaults} />);
    expect(screen.getByLabelText(/Reason/)).toBeInTheDocument();
  });

  it('disables Confirm when reason is empty', async () => {
    render(<ConfirmationModal {...defaults} confirmDelay={0} />);
    // setTimeout(fn, 0) needs one event-loop tick before delayActive clears
    const btn = await waitFor(() => screen.getByRole('button', { name: 'Confirm' }));
    expect(btn).toBeDisabled();
  });

  it('shows "Please wait..." during delay period', () => {
    render(<ConfirmationModal {...defaults} confirmDelay={5000} />);
    expect(screen.getByRole('button', { name: 'Please wait...' })).toBeDisabled();
  });

  it('enables Confirm after delay when reason is entered', async () => {
    vi.useFakeTimers();
    render(<ConfirmationModal {...defaults} confirmDelay={1000} />);

    // Use fireEvent.change (synchronous) instead of userEvent.type (uses internal timers)
    fireEvent.change(screen.getByLabelText(/Reason/), { target: { value: 'Clinical judgment' } });
    act(() => { vi.advanceTimersByTime(1000); });

    const btn = screen.getByRole('button', { name: 'Confirm' });
    expect(btn).not.toBeDisabled();
    vi.useRealTimers();
  });

  it('calls onCancel when Cancel is clicked', () => {
    const cancel = vi.fn();
    render(<ConfirmationModal {...defaults} onCancel={cancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(cancel).toHaveBeenCalledOnce();
  });

  it('calls onConfirm with reason when Confirm is clicked', async () => {
    const confirm = vi.fn();
    render(<ConfirmationModal {...defaults} onConfirm={confirm} confirmDelay={0} />);

    fireEvent.change(screen.getByLabelText(/Reason/), { target: { value: 'Agreed with AI' } });
    // Wait for the 0ms delay timer to clear
    await waitFor(() => screen.getByRole('button', { name: 'Confirm' }));

    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(confirm).toHaveBeenCalledWith('Agreed with AI');
  });

  it('does not render when isOpen is false', () => {
    render(<ConfirmationModal {...defaults} isOpen={false} />);
    expect(screen.queryByText('Confirm Approval')).not.toBeInTheDocument();
  });
});
