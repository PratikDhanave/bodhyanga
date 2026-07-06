import { useEffect, useRef, useState } from 'react';
import { Modal } from './Modal';
import ConfidenceDisplay from './ConfidenceDisplay';

interface ConfirmationModalProps {
  isOpen: boolean;
  title: string;
  diagnosisSummary: string;
  patientContext: string;
  confidence?: number;
  onConfirm: (reason: string) => void;
  onCancel: () => void;
  confirmDelay?: number;
  /** When true, applies danger (red) styling to the modal */
  danger?: boolean;
}

export default function ConfirmationModal({
  isOpen,
  title,
  diagnosisSummary,
  patientContext,
  confidence,
  onConfirm,
  onCancel,
  confirmDelay = 2000,
  danger,
}: ConfirmationModalProps) {
  const [reason, setReason] = useState('');
  const [delayActive, setDelayActive] = useState(true);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (isOpen) {
      setReason('');
      setDelayActive(true);
      timerRef.current = setTimeout(() => setDelayActive(false), confirmDelay);
    }
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [isOpen, confirmDelay]);

  const canConfirm = reason.trim().length > 0 && !delayActive;

  return (
    <Modal open={isOpen} onClose={onCancel} title={title} danger={danger}>
      <div className="space-y-4">
        {/* Diagnosis summary */}
        <div>
          <div className="text-xs font-semibold uppercase tracking-wider text-bodh-400 dark:text-gray-500 mb-1">
            Diagnosis
          </div>
          <div className="text-sm text-bodh-800 dark:text-gray-100">{diagnosisSummary}</div>
        </div>

        {/* Patient context */}
        <div>
          <div className="text-xs font-semibold uppercase tracking-wider text-bodh-400 dark:text-gray-500 mb-1">
            Patient
          </div>
          <div className="text-sm text-bodh-800 dark:text-gray-100">{patientContext}</div>
        </div>

        {/* Confidence — only shown when provided */}
        {confidence != null && (
          <div>
            <div className="text-xs font-semibold uppercase tracking-wider text-bodh-400 dark:text-gray-500 mb-1">
              AI Confidence
            </div>
            <ConfidenceDisplay value={confidence} />
          </div>
        )}

        {/* Reason (required) */}
        <div>
          <label
            htmlFor="confirm-reason"
            className="block text-xs font-semibold uppercase tracking-wider text-bodh-400 dark:text-gray-500 mb-1"
          >
            Reason (required)
          </label>
          <textarea
            id="confirm-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            placeholder="Describe your clinical reasoning..."
            className="w-full rounded-md border border-bodh-200 dark:border-gray-600
                       bg-white dark:bg-gray-700 px-3 py-2 text-sm
                       text-bodh-800 dark:text-gray-100
                       placeholder:text-bodh-300 dark:placeholder:text-gray-500
                       focus:outline-none focus:ring-2 focus:ring-bodh-600 dark:focus:ring-blue-500"
          />
        </div>

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-2 border-t border-bodh-200 dark:border-gray-700">
          <button
            onClick={onCancel}
            className="btn-secondary min-h-[44px]"
          >
            Cancel
          </button>
          <button
            onClick={() => canConfirm && onConfirm(reason.trim())}
            disabled={!canConfirm}
            className="btn min-h-[44px] disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {delayActive ? 'Please wait...' : 'Confirm'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
