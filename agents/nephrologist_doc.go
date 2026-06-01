// nephrologist.go — rule-based nephrology specialist agent.
//
// Overview:
//   Calculates eGFR via the CKD-EPI (2021) race-free formula, stages CKD
//   per KDIGO G-categories, detects AKI via KDIGO creatinine criteria,
//   classifies proteinuria per KDIGO A-categories, and flags hyperkalemia
//   and metabolic acidosis. Generates a structured nephrology_report and
//   forwards it to the supervisor for routing.
//
// Key declarations:
//   - NephrologistAgent: stateless specialist agent (ID="nephrologist").
//   - NephrologistAgent.HandleMessage: decodes nephrology_consult, runs
//     interpretNephrology, emits nephrology_report to supervisor.
//   - nephroConsultRequest: wire format for incoming consult messages with
//     optional pointer fields for BUN, urine_protein, potassium,
//     bicarbonate, and prior_creatinine.
//   - nephroReport: wire format for outgoing report messages including
//     egfr_ml_min, ckd_stage, aki_stage, aki_cause, proteinuria_category,
//     dialysis_indicated, recommendations, and urgent flag.
//   - interpretNephrology: rule engine — fires CKD staging, AKI detection,
//     proteinuria classification, hyperkalemia, and acidosis rules.
//   - calcEGFR: CKD-EPI (2021) race-free eGFR calculation.
//   - ckdStageFromEGFR: maps eGFR to KDIGO G1–G5 stage label.
//
// Clinical disclaimer:
//   All numeric thresholds and rule logic are illustrative educational values,
//   not clinical guidelines. The CKD-EPI (2021) formula is implemented
//   without lab-calibration correction; KDIGO AKI staging omits urine output
//   criteria; CKD staging from a single eGFR measurement is insufficient per
//   KDIGO (requires ≥3 months persistence). This agent is a demo component
//   and must not be used for real patient care. Clinical-grade implementation
//   would require FDA SaMD validation under 21 CFR Part 820 / IEC 62304.
//
// See doc.go for package-level documentation.

package agents
