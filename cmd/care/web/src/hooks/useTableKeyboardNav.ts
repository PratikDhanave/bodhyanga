import { useCallback, useState } from 'react';

/**
 * Keyboard navigation for data tables — implements WAI-ARIA grid navigation.
 *
 * Returns:
 *  - `activeIndex`: currently focused row (-1 = none)
 *  - `getRowProps(i)`: spread onto each `<tr>` for tabIndex, onKeyDown, aria, and focus ring
 *  - `onTableKeyDown`: attach to `<tbody>` to handle initial focus
 *
 * Usage:
 * ```tsx
 * const { activeIndex, getRowProps } = useTableKeyboardNav(rows.length, (i) => navigate(rows[i].id));
 * <tbody>
 *   {rows.map((r, i) => <tr key={r.id} {...getRowProps(i)}> ... </tr>)}
 * </tbody>
 * ```
 */
export function useTableKeyboardNav(
  rowCount: number,
  onActivate?: (index: number) => void,
) {
  const [activeIndex, setActiveIndex] = useState(-1);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, index: number) => {
      let next = index;

      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          next = Math.min(index + 1, rowCount - 1);
          break;
        case 'ArrowUp':
          e.preventDefault();
          next = Math.max(index - 1, 0);
          break;
        case 'Home':
          e.preventDefault();
          next = 0;
          break;
        case 'End':
          e.preventDefault();
          next = rowCount - 1;
          break;
        case 'Enter':
        case ' ':
          e.preventDefault();
          onActivate?.(index);
          return;
        default:
          return;
      }

      setActiveIndex(next);
      // Focus the target row element
      const tbody = (e.target as HTMLElement).closest('tbody');
      const rows = tbody?.querySelectorAll('tr');
      rows?.[next]?.focus();
    },
    [rowCount, onActivate],
  );

  const getRowProps = useCallback(
    (index: number) => ({
      tabIndex: index === activeIndex || (activeIndex === -1 && index === 0) ? 0 : -1,
      onKeyDown: (e: React.KeyboardEvent) => handleKeyDown(e, index),
      onFocus: () => setActiveIndex(index),
      'aria-rowindex': index + 1,
      className: 'focus-visible:outline-2 focus-visible:outline-bodh-600 focus-visible:outline-offset-[-2px] focus-visible:rounded',
    }),
    [activeIndex, handleKeyDown],
  );

  return { activeIndex, getRowProps };
}
