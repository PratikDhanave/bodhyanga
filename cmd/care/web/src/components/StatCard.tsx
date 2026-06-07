interface StatCardProps {
  /** Label above the value (e.g. "Patients", "Spend today") */
  label: string;
  /** Primary display value */
  value: string | number;
  /** Optional subtitle below the value */
  sub?: string;
  /** Optional highlight colour variant */
  variant?: 'default' | 'green' | 'amber' | 'red';
  /** Optional icon element */
  icon?: React.ReactNode;
}

const VARIANTS: Record<string, string> = {
  default: '',
  green: 'border-green-300 dark:border-green-700 bg-green-50 dark:bg-green-900/20',
  amber: 'border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-900/20',
  red: 'border-red-300 dark:border-red-700 bg-red-50 dark:bg-red-900/20',
};

/**
 * Reusable stat card used on Dashboard, ClinicianDashboard, Cost, FineTune pages.
 * Renders a label, large value, optional subtitle, and optional icon.
 */
export function StatCard({ label, value, sub, variant = 'default', icon }: StatCardProps) {
  return (
    <div className={`card ${VARIANTS[variant]}`}>
      <div className="card-body">
        <div className="flex items-start justify-between">
          <div>
            <div className="text-xs uppercase tracking-wide text-bodh-400 dark:text-gray-400">
              {label}
            </div>
            <div className="text-2xl font-semibold mt-2 text-bodh-800 dark:text-gray-100">
              {value}
            </div>
            {sub && (
              <div className="text-xs text-bodh-400 dark:text-gray-500 mt-1">{sub}</div>
            )}
          </div>
          {icon && (
            <div className="text-bodh-300 dark:text-gray-600 mt-0.5">{icon}</div>
          )}
        </div>
      </div>
    </div>
  );
}
