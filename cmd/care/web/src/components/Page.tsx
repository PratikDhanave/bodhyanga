import type { ReactNode } from 'react';

interface PageProps {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
}

export default function Page({ title, subtitle, actions, children }: PageProps) {
  return (
    <div className="p-6 space-y-6">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold text-bodh-800">{title}</h1>
          {subtitle && <p className="text-sm text-bodh-400 mt-1">{subtitle}</p>}
        </div>
        {actions && <div className="flex gap-2">{actions}</div>}
      </header>
      {children}
    </div>
  );
}
