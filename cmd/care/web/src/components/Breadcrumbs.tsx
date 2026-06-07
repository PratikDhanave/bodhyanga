import { Link } from 'react-router-dom';

export interface Crumb {
  label: string;
  to?: string;
}

interface BreadcrumbsProps {
  items: Crumb[];
}

/**
 * Route-aware breadcrumbs for detail pages (ReviewDetail, RunTrace, etc.).
 * Last item renders as plain text (current page), others as links.
 */
export function Breadcrumbs({ items }: BreadcrumbsProps) {
  return (
    <nav aria-label="Breadcrumb" className="text-xs text-bodh-400 dark:text-gray-500 mb-4">
      <ol className="flex items-center gap-1.5">
        {items.map((crumb, i) => {
          const isLast = i === items.length - 1;
          return (
            <li key={i} className="flex items-center gap-1.5">
              {i > 0 && (
                <svg className="h-3 w-3 text-bodh-300 dark:text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
                </svg>
              )}
              {isLast || !crumb.to ? (
                <span
                  className={isLast ? 'text-bodh-600 dark:text-gray-300 font-medium' : ''}
                  aria-current={isLast ? 'page' : undefined}
                >
                  {crumb.label}
                </span>
              ) : (
                <Link
                  to={crumb.to}
                  className="hover:text-bodh-600 dark:hover:text-gray-300 transition-colors underline-offset-2 hover:underline"
                >
                  {crumb.label}
                </Link>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
