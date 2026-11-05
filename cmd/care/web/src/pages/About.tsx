import Page from '../components/Page';

export default function About() {
  return (
    <Page title="About" subtitle="Bodhyanga — Medical Diagnostic Orchestrator & Care Services Platform">
      <div className="card">
        <div className="card-body space-y-4 text-sm leading-relaxed">
          <p>
            <strong>Bodhyanga</strong> is an open-source medical AI platform implemented in Go. It follows Microsoft's
            <a href="https://microsoft.github.io/multi-agent-reference-architecture/" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline"> Multi-Agent Reference Architecture (MARA)</a>, implements a virtual physician panel inspired by
            <a href="https://microsoft.ai/news/the-path-to-medical-superintelligence/" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline"> MAI-DxO</a>, and adds a care-services layer (CDM, TCM, panel management, RPM, engagement, readmission tracking) on the same bus.
          </p>
          <div className="border-l-4 border-amber-400 bg-amber-50 p-3 rounded">
            <p className="font-semibold text-amber-900">Research disclaimer</p>
            <p className="text-amber-900 mt-1">Bodhyanga is a research and engineering reference. Do not use for real patient care, triage, treatment, or billing.</p>
          </div>
          <ul className="list-disc pl-5 space-y-1">
            <li>Repository: <a href="https://github.com/PratikDhanave/bodhyanga" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline">github.com/PratikDhanave/bodhyanga</a></li>
            <li>Detailed AI features: <a href="https://github.com/PratikDhanave/bodhyanga/blob/main/docs/ai-features.md" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline">docs/ai-features.md</a></li>
            <li>Setup: <a href="https://github.com/PratikDhanave/bodhyanga/blob/main/docs/setup.md" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline">docs/setup.md</a></li>
            <li>Architecture diagrams: <a href="https://github.com/PratikDhanave/bodhyanga/blob/main/docs/diagrams.md" target="_blank" rel="noreferrer" className="text-bodhyanga-600 underline">docs/diagrams.md</a></li>
          </ul>
        </div>
      </div>
    </Page>
  );
}
