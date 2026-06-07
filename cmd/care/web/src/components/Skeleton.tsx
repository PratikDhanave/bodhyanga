interface SkeletonProps {
  variant?: 'text' | 'card' | 'circle' | 'table-row';
  lines?: number;
  className?: string;
}

const bar = 'rounded bg-bodh-100 dark:bg-gray-700';
const widths = ['w-full', 'w-4/5', 'w-3/5'];

export function Skeleton({ variant = 'text', lines = 3, className = '' }: SkeletonProps) {
  if (variant === 'circle') {
    return (
      <div className={`animate-pulse ${className}`}>
        <div className={`${bar} h-10 w-10 rounded-full`} />
      </div>
    );
  }

  if (variant === 'table-row') {
    return (
      <div className={`animate-pulse flex gap-4 ${className}`}>
        {[...Array(4)].map((_, i) => (
          <div key={i} className={`${bar} h-4 flex-1`} />
        ))}
      </div>
    );
  }

  if (variant === 'card') {
    return (
      <div className={`animate-pulse card p-4 space-y-3 ${className}`}>
        <div className={`${bar} h-5 w-2/5`} />
        <div className={`${bar} h-3 w-full`} />
        <div className={`${bar} h-3 w-4/5`} />
        <div className={`${bar} h-3 w-3/5`} />
      </div>
    );
  }

  return (
    <div className={`animate-pulse space-y-2 ${className}`}>
      {[...Array(lines)].map((_, i) => (
        <div key={i} className={`${bar} h-3 ${widths[i % widths.length]}`} />
      ))}
    </div>
  );
}

interface SkeletonGroupProps {
  count?: number;
  variant?: SkeletonProps['variant'];
  className?: string;
}

export function SkeletonGroup({ count = 3, variant = 'text', className = '' }: SkeletonGroupProps) {
  return (
    <div className={`space-y-4 ${className}`}>
      {[...Array(count)].map((_, i) => (
        <Skeleton key={i} variant={variant} />
      ))}
    </div>
  );
}
