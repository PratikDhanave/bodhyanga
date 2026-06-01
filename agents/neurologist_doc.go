// neurologist.go — rule-based neurology specialist agent.
//
// Overview:
//   Interprets neurology consult requests to generate a structured
//   neurology_report, then forwards it to the supervisor for routing.
//   Rules cover stroke recognition (FAST+ criteria), tPA eligibility window,
//   thrombectomy window assessment, LVO detection (NIHSS ≥6), NIHSS
//   scoring (provided or keyword-estimated), stroke type classification
//   (TOAST-inspired: cardioembolic / large vessel / small vessel), and
//   ILAE 2017-inspired seizure classification (generalised tonic-clonic,
//   absence, focal onset, provoked vs unprovoked).
//
// Key declarations:
//   - NeurologistAgent: stateless specialist agent (ID="neurologist").
//   - NeurologistAgent.HandleMessage: decodes neurology_consult, runs
//     interpretNeurology, emits neurology_report to supervisor.
//   - neuroConsultRequest: wire format for incoming consult messages.
//   - neuroReport: wire format for outgoing report messages.
//   - interpretNeurology: rule engine — fires stroke, tPA, thrombectomy,
//     stroke-type, and seizure rules in urgency order.
//   - hasStrokeCriteria: FAST+ keyword detection for stroke presentation.
//   - estimateNIHSS: uses provided NIHSS or estimates from presentation keywords.
//   - classifyStrokeType: TOAST-inspired cardioembolic/large vessel/small vessel.
//   - classifySeizure: ILAE 2017-inspired seizure type from presentation text.
//   - isProvokedSeizure: identifies provoked (acute symptomatic) seizures.
//   - buildSeizureDiagnosis: narrative diagnosis string for seizure-only cases.
//
// Clinical disclaimer:
//   All numeric thresholds (NIHSS ≥6 for LVO, onset <4.5h for tPA,
//   BP >185/110 contraindication) and rule logic are illustrative educational
//   values, not clinical guidelines. This agent is a demo component and must
//   not be used for real patient care. Clinical-grade implementation would
//   require FDA SaMD validation under 21 CFR Part 820 / IEC 62304, and
//   compliance with AHA/ASA stroke guidelines, ILAE seizure taxonomy, and
//   applicable state medical practice acts.
//
// See doc.go for package-level documentation.

package agents
