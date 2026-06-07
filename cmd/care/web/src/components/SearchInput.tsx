import { useEffect, useRef, useState } from 'react';

interface SearchInputProps {
  /** Called after the debounce interval with the trimmed query */
  onSearch: (query: string) => void;
  /** Placeholder text (default: "Search…") */
  placeholder?: string;
  /** Debounce delay in milliseconds (default: 300) */
  debounce?: number;
  /** Optional controlled value — when undefined, internal state is used */
  value?: string;
  /** Optional class overrides */
  className?: string;
}

/**
 * Debounced search input with clear button.
 * Calls `onSearch` after `debounce` ms of inactivity.
 */
export function SearchInput({
  onSearch,
  placeholder = 'Search…',
  debounce = 300,
  value: controlledValue,
  className,
}: SearchInputProps) {
  const isControlled = controlledValue !== undefined;
  const [internal, setInternal] = useState('');
  const query = isControlled ? controlledValue : internal;
  const timerRef = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    // Skip debounce for controlled mode — parent manages timing
    if (isControlled) return;
    timerRef.current = setTimeout(() => {
      onSearch(internal.trim());
    }, debounce);
    return () => clearTimeout(timerRef.current);
  }, [internal, debounce, onSearch, isControlled]);

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    if (isControlled) {
      onSearch(e.target.value);
    } else {
      setInternal(e.target.value);
    }
  }

  function handleClear() {
    if (isControlled) {
      onSearch('');
    } else {
      setInternal('');
      onSearch('');
    }
  }

  return (
    <div className={`relative ${className ?? ''}`}>
      {/* Magnifying glass icon */}
      <svg
        className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-bodh-300 dark:text-gray-500 pointer-events-none"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        strokeWidth={2}
      >
        <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-4.35-4.35M11 19a8 8 0 100-16 8 8 0 000 16z" />
      </svg>
      <input
        type="search"
        value={query}
        onChange={handleChange}
        placeholder={placeholder}
        aria-label={placeholder}
        className={`
          w-full pl-8 pr-8 py-1.5 text-sm rounded-md border border-bodh-200 dark:border-gray-600
          bg-white dark:bg-gray-800 text-bodh-800 dark:text-gray-100
          placeholder:text-bodh-300 dark:placeholder:text-gray-500
          focus:outline-none focus:ring-2 focus:ring-bodh-500 focus:border-transparent
          transition-colors
        `}
      />
      {query.length > 0 && (
        <button
          onClick={handleClear}
          aria-label="Clear search"
          className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded text-bodh-300 dark:text-gray-500 hover:text-bodh-600 dark:hover:text-gray-300 transition-colors"
        >
          <svg className="h-3.5 w-3.5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      )}
    </div>
  );
}
