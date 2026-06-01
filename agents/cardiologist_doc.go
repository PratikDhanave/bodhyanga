// cardiologist.go — rule-based cardiology specialist agent.
//
// Overview:
//   Interprets ECG findings and cardiac biomarkers to generate a structured
//   cardiology_report, then forwards it to the supervisor for routing.
//   Rules cover STEMI equivalent, complete heart block, NSTEMI/ACS (troponin),
//   atrial fibrillation, HFrEF/decompensated heart failure, and a simplified
//   TIMI-like risk score.
//
// Key declarations:
//   - CardiologistAgent: stateless specialist agent (ID="cardiologist").
//   - CardiologistAgent.HandleMessage: decodes cardiology_consult, runs
//     interpretCardiology, emits cardiology_report to supervisor.
//   - cardioConsultRequest: wire format for incoming consult messages.
//   - cardioReport: wire format for outgoing report messages.
//   - interpretCardiology: rule engine — fires STEMI, heart block, ACS,
//     AFib, CHF, and TIMI rules in urgency order.
//   - computeSimplifiedTIMI: 0–7 risk score from available structured fields.
//   - dedupStrings: deduplicates recommendations while preserving order.
//
// Clinical disclaimer:
//   All numeric thresholds (troponin >0.04 ng/mL, BNP >400 pg/mL, EF <40%)
//   and rule logic are illustrative educational values, not clinical
//   guidelines. This agent is a demo component and must not be used for
//   real patient care. Clinical-grade implementation would require FDA
//   SaMD validation under 21 CFR Part 820 / IEC 62304.
//
// See doc.go for package-level documentation.

package agents
