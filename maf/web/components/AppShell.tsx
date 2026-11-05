"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

const NAV = [
  { href: "/", label: "Overview", exact: true },
  { href: "/reviews", label: "Review queue" },
];

function Mark() {
  // A pulse line inside a clinical-teal tile — the instrument, not a logo cliché.
  return (
    <span className="grid h-8 w-8 place-items-center rounded-lg bg-clinical-600">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
        <path
          d="M2 12h4l2-6 4 12 3-9 2 3h5"
          stroke="#fff"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}

function ConnectionDot() {
  const [ok, setOk] = useState<boolean | null>(null);
  useEffect(() => {
    let alive = true;
    const ping = async () => {
      try {
        const r = await fetch("/api/care/healthz", { cache: "no-store" });
        if (alive) setOk(r.ok);
      } catch {
        if (alive) setOk(false);
      }
    };
    ping();
    const id = setInterval(ping, 10000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);
  const color = ok == null ? "#b0bdc9" : ok ? "#0d7378" : "#b42318";
  const label = ok == null ? "Checking care API…" : ok ? "Care API connected" : "Care API unreachable";
  return (
    <span className="flex items-center gap-2 text-xs text-ink-400" title={label}>
      <span className="h-2 w-2 rounded-full" style={{ background: color }} />
      {label}
    </span>
  );
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);

  const isActive = (href: string, exact?: boolean) =>
    exact ? pathname === href : pathname.startsWith(href);

  return (
    <div className="flex min-h-screen">
      <a
        href="#main-content"
        className="sr-only rounded-md bg-clinical-700 px-4 py-2 text-sm font-medium text-white focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50"
      >
        Skip to content
      </a>
      {open && (
        <div
          className="fixed inset-0 z-30 bg-ink-900/30 lg:hidden"
          onClick={() => setOpen(false)}
          aria-hidden
        />
      )}

      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-60 transform flex-col border-r border-ink-200 bg-white transition-transform duration-200 lg:static lg:translate-x-0 ${
          open ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        <div className="flex items-center gap-3 px-4 py-4">
          <Mark />
          <div className="leading-tight">
            <div className="text-sm font-semibold text-ink-800">Bodhyanga</div>
            <div className="text-xs text-ink-400">Clinician console</div>
          </div>
        </div>

        <nav className="flex-1 px-3 py-2">
          <div className="eyebrow px-2 pb-1">Workflow</div>
          <ul className="space-y-0.5">
            {NAV.map((n) => {
              const active = isActive(n.href, n.exact);
              return (
                <li key={n.href}>
                  <Link
                    href={n.href}
                    onClick={() => setOpen(false)}
                    aria-current={active ? "page" : undefined}
                    className={`relative flex items-center rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                      active
                        ? "bg-clinical-50 text-clinical-800"
                        : "text-ink-600 hover:bg-ink-50 hover:text-ink-800"
                    }`}
                  >
                    {active && (
                      <span className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r bg-clinical-600" />
                    )}
                    {n.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="border-t border-ink-100 px-4 py-3">
          <ConnectionDot />
          <p className="mt-2 text-[11px] leading-snug text-ink-400">
            Research console. Not a medical device — not for clinical use.
          </p>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b border-ink-200 bg-white px-4 lg:px-6">
          <button
            onClick={() => setOpen((v) => !v)}
            aria-label="Toggle navigation"
            className="btn-ghost lg:hidden"
            style={{ padding: "0.4rem 0.5rem" }}
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
              <path d="M4 7h16M4 12h16M4 17h16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
          <span className="text-sm font-medium text-ink-500">
            AI diagnostic decision support
          </span>
          <Link href="/" className="btn ml-auto">
            New case
          </Link>
        </header>

        <main
          id="main-content"
          className="mx-auto w-full max-w-4xl flex-1 px-4 py-6 lg:px-8 lg:py-8"
        >
          {children}
        </main>
      </div>
    </div>
  );
}
