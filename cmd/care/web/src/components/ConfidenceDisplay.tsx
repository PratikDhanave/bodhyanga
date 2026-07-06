import { useState } from 'react';

interface ConfidenceDisplayProps {
  value: number;
  showTooltip?: boolean;
  size?: 'sm' | 'md';
}

interface Category {
  label: string;
  colorLight: string;
  colorDark: string;
  bgLight: string;
  bgDark: string;
}

function categorize(v: number): Category {
  const clamped = Math.max(0, Math.min(1, v));
  if (clamped < 0.5)
    return {
      label: 'Low',
      colorLight: 'text-red-700',
      colorDark: 'dark:text-red-400',
      bgLight: 'bg-red-50',
      bgDark: 'dark:bg-red-900/30',
    };
  if (clamped < 0.7)
    return {
      label: 'Moderate',
      colorLight: 'text-amber-700',
      colorDark: 'dark:text-amber-400',
      bgLight: 'bg-amber-50',
      bgDark: 'dark:bg-amber-900/30',
    };
  if (clamped < 0.9)
    return {
      label: 'High',
      colorLight: 'text-blue-700',
      colorDark: 'dark:text-blue-400',
      bgLight: 'bg-blue-50',
      bgDark: 'dark:bg-blue-900/30',
    };
  return {
    label: 'Very High',
    colorLight: 'text-emerald-700',
    colorDark: 'dark:text-emerald-400',
    bgLight: 'bg-emerald-50',
    bgDark: 'dark:bg-emerald-900/30',
  };
}

const TOOLTIP_TEXT =
  'AI confidence based on diagnostic criteria match. Low (<50%): uncertain — review carefully. Moderate (50-70%): partial match. High (70-90%): strong match. Very High (90%+): near-certain match.';

export default function ConfidenceDisplay({
  value,
  showTooltip = true,
  size = 'md',
}: ConfidenceDisplayProps) {
  const [tipOpen, setTipOpen] = useState(false);
  const cat = categorize(value);
  const pct = `${Math.round(Math.max(0, Math.min(1, value)) * 100)}%`;
  const sizeClass = size === 'sm' ? 'text-xs px-1.5 py-0.5' : 'text-sm px-2 py-1';

  return (
    <span
      className={`relative inline-flex items-center gap-1 rounded-md font-medium ${sizeClass} ${cat.colorLight} ${cat.colorDark} ${cat.bgLight} ${cat.bgDark}`}
      onMouseEnter={() => showTooltip && setTipOpen(true)}
      onMouseLeave={() => setTipOpen(false)}
      aria-label={`Confidence: ${cat.label} (${pct})`}
    >
      {cat.label}
      <span className="opacity-70">({pct})</span>
      {tipOpen && (
        <span
          role="tooltip"
          className="absolute left-1/2 -translate-x-1/2 bottom-full mb-2 w-64
                     px-3 py-2 rounded-md text-xs font-normal leading-relaxed
                     bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900
                     shadow-lg z-50 pointer-events-none"
        >
          {TOOLTIP_TEXT}
        </span>
      )}
    </span>
  );
}

export { categorize, TOOLTIP_TEXT };
