import { NavLink, Outlet } from 'react-router-dom';

const nav = [
  { to: '/', label: 'Dashboard' },
  { to: '/runs', label: 'Case runner' },
  { to: '/reviews', label: 'HITL reviews' },
  { to: '/tasks', label: 'Nurse tasks' },
  { to: '/patients', label: 'Patients' },
  { to: '/panel', label: 'Panel report' },
  { to: '/agents', label: 'Agents' },
  { to: '/rpm', label: 'RPM' },
  { to: '/bench', label: 'Bench results' },
  { to: '/llm-traces', label: 'LLM traces' },
  { to: '/fine-tune', label: 'Fine-tune export' },
  { to: '/about', label: 'About' },
];

export default function Layout() {
  return (
    <div className="min-h-screen flex">
      <aside className="w-56 bg-white border-r border-bodh-200 flex flex-col">
        <div className="px-4 py-4 border-b border-bodh-200">
          <div className="font-semibold text-bodh-800">Bodh</div>
          <div className="text-xs text-bodh-400">Clinician console</div>
        </div>
        <nav className="flex-1 p-2 space-y-1 overflow-y-auto">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === '/'}
              className={({ isActive }) =>
                isActive ? 'nav-link nav-link-active' : 'nav-link'
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="px-4 py-3 border-t border-bodh-200 text-xs text-bodh-400">
          Research demo. Not for clinical use.
        </div>
      </aside>
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
