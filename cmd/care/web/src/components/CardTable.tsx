import type { ReactNode } from 'react';

export interface CardColumn {
  key: string;
  label: string;
  showInCard: boolean;
  primary?: boolean;
}

interface CardTableProps<T extends Record<string, unknown>> {
  columns: CardColumn[];
  data: T[];
  keyField: string;
  onRowClick?: (row: T) => void;
  /** The standard <table> to render on screens >= sm */
  children: ReactNode;
}

/**
 * Responsive wrapper: renders the children `<table>` on >=sm screens
 * and a card-based layout on <sm screens.
 */
export default function CardTable<T extends Record<string, unknown>>({
  columns,
  data,
  keyField,
  onRowClick,
  children,
}: CardTableProps<T>) {
  const primaryCol = columns.find((c) => c.primary) ?? columns[0];
  const cardCols = columns.filter((c) => c.showInCard && !c.primary);

  return (
    <>
      {/* Table visible on sm+ */}
      <div className="hidden sm:block">{children}</div>

      {/* Card layout visible on <sm */}
      <div className="sm:hidden space-y-3">
        {data.map((row) => {
          const key = String(row[keyField] ?? '');
          return (
            <div
              key={key}
              className="rounded-lg border border-bodh-200 dark:border-gray-700
                         bg-white dark:bg-gray-800 p-4 space-y-2"
            >
              {/* Primary field as card title */}
              <div className="font-semibold text-sm text-bodh-800 dark:text-gray-100">
                {String(row[primaryCol.key] ?? '')}
              </div>

              {/* Secondary fields */}
              {cardCols.map((col) => (
                <div key={col.key} className="flex justify-between text-xs">
                  <span className="text-bodh-400 dark:text-gray-500">{col.label}</span>
                  <span className="text-bodh-700 dark:text-gray-300">
                    {String(row[col.key] ?? '—')}
                  </span>
                </div>
              ))}

              {/* View details link */}
              {onRowClick && (
                <button
                  onClick={() => onRowClick(row)}
                  className="text-xs text-bodh-600 dark:text-blue-400 hover:underline mt-1"
                >
                  View details
                </button>
              )}
            </div>
          );
        })}

        {data.length === 0 && (
          <div className="text-center text-sm text-bodh-400 dark:text-gray-500 py-8">
            No data available
          </div>
        )}
      </div>
    </>
  );
}
