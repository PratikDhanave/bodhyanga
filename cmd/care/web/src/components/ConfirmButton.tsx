import { useState, useEffect, useRef } from 'react';

interface ConfirmButtonProps {
  /** Text shown in the initial state */
  label: string;
  /** Text shown in the confirmation state */
  confirmLabel?: string;
  /** Called when the user confirms (clicks twice) */
  onConfirm: () => void;
  /** Optional delay in ms before confirm button activates (default: 0) */
  confirmDelay?: number;
  /** Button variant */
  variant?: 'primary' | 'danger';
  /** Whether the button is disabled */
  disabled?: boolean;
  /** Optional className overrides */
  className?: string;
}

/**
 * Two-step confirmation button for irreversible actions.
 * First click enters "confirm" mode; second click triggers the action.
 * Clicking outside or waiting 5s reverts to the initial state.
 */
export function ConfirmButton({
  label,
  confirmLabel,
  onConfirm,
  confirmDelay = 0,
  variant = 'primary',
  disabled = false,
  className = '',
}: ConfirmButtonProps) {
  const [confirming, setConfirming] = useState(false);
  const [delayActive, setDelayActive] = useState(false);
  const revertTimer = useRef<ReturnType<typeof setTimeout>>();
  const ref = useRef<HTMLButtonElement>(null);

  // Auto-revert after 5 seconds
  useEffect(() => {
    if (confirming) {
      revertTimer.current = setTimeout(() => setConfirming(false), 5000);
      return () => clearTimeout(revertTimer.current);
    }
  }, [confirming]);

  // Delay before confirm button activates
  useEffect(() => {
    if (confirming && confirmDelay > 0) {
      setDelayActive(true);
      const timer = setTimeout(() => setDelayActive(false), confirmDelay);
      return () => clearTimeout(timer);
    }
    setDelayActive(false);
  }, [confirming, confirmDelay]);

  // Click outside to cancel
  useEffect(() => {
    if (!confirming) return;
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setConfirming(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [confirming]);

  function handleClick() {
    if (!confirming) {
      setConfirming(true);
      return;
    }
    if (delayActive) return;
    setConfirming(false);
    onConfirm();
  }

  const baseClasses =
    variant === 'danger'
      ? 'bg-red-600 hover:bg-red-700 text-white dark:bg-red-700 dark:hover:bg-red-600'
      : 'bg-bodh-800 hover:bg-bodh-700 text-white dark:bg-bodh-500 dark:hover:bg-bodh-400';

  const confirmClasses =
    variant === 'danger'
      ? 'bg-red-700 ring-2 ring-red-300 dark:ring-red-500'
      : 'bg-amber-600 hover:bg-amber-700 ring-2 ring-amber-300 dark:ring-amber-500';

  return (
    <button
      ref={ref}
      type="button"
      onClick={handleClick}
      disabled={disabled || (confirming && delayActive)}
      className={`
        px-3 py-1.5 text-sm font-medium rounded-md transition-all duration-150
        focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-bodh-500
        disabled:opacity-50 disabled:cursor-not-allowed
        ${confirming ? `${confirmClasses} text-white` : baseClasses}
        ${className}
      `}
    >
      {confirming ? (confirmLabel ?? `Confirm ${label}`) : label}
    </button>
  );
}
