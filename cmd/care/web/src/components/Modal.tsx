import { useCallback, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  danger?: boolean;
}

const FOCUSABLE = 'a[href],button:not([disabled]),textarea,input,select,[tabindex]:not([tabindex="-1"])';

export function Modal({ open, onClose, title, children, danger }: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (open) triggerRef.current = document.activeElement as HTMLElement;
  }, [open]);

  useEffect(() => {
    if (!open || !dialogRef.current) return;
    const first = dialogRef.current.querySelector<HTMLElement>(FOCUSABLE);
    if (first) first.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    return () => { triggerRef.current?.focus(); };
  }, [open]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { onClose(); return; }
    if (e.key !== 'Tab' || !dialogRef.current) return;
    const nodes = dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE);
    if (nodes.length === 0) return;
    const first = nodes[0], last = nodes[nodes.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }, [onClose]);

  if (!open) return null;

  return createPortal(
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions
    <div className="fixed inset-0 z-50 flex items-center justify-center" onKeyDown={handleKeyDown}>
      <div className="absolute inset-0 bg-black/50 transition-opacity" onClick={onClose} aria-hidden="true" />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        className={`relative w-full max-w-lg mx-4 rounded-lg shadow-xl animate-slide-in
                    bg-white dark:bg-gray-800 border
                    ${danger ? 'border-red-300 dark:border-red-700' : 'border-bodh-200 dark:border-gray-700'}`}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-bodh-200 dark:border-gray-700">
          <h2
            id="modal-title"
            className={`text-lg font-semibold ${danger ? 'text-red-700 dark:text-red-400' : 'text-bodh-800 dark:text-gray-100'}`}
          >
            {title}
          </h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="p-1 rounded-md hover:bg-bodh-100 dark:hover:bg-gray-700
                       text-bodh-600 dark:text-gray-400 transition-colors text-xl leading-none"
          >
            &times;
          </button>
        </div>
        <div className="px-5 py-4 text-sm text-bodh-800 dark:text-gray-100">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
