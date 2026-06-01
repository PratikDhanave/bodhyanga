// pulmonologist.go — rule-based pulmonology specialist agent.
//
// Overview:
//   Interprets pulmonary function tests (PFTs) and clinical data to generate
//   a structured pulmonology_report, then forwards it to the supervisor for
//   routing. Rules cover PFT pattern classification (obstruction, restriction,
//   mixed), COPD GOLD spirometric grading (GOLD 1–4) and ABE symptom-group
//   classification, asthma severity assessment (GINA step classification),
//   supplemental O2 indication (SpO2 ≤ 88%), DLCO impairment (DLCO < 40%),
//   and pulmonary rehabilitation referral.
//
// Key declarations:
//   - PulmonologistAgent: stateless specialist agent (ID="pulmonologist").
//   - PulmonologistAgent.HandleMessage: decodes pulmonology_consult, runs
//     interpretPulmonology, emits pulmonology_report to supervisor.
//   - pulmoConsultRequest: wire format for incoming consult messages.
//   - pulmoReport: wire format for outgoing report messages.
//   - interpretPulmonology: rule engine — fires O2, DLCO, PFT-pattern,
//     GOLD staging, asthma severity, and rehab rules in urgency order.
//   - copdGOLDGrade: maps FEV1 % predicted to GOLD 1–4 grade string.
//   - copdGOLDGroup: maps CAT/mMRC/exacerbation history to GOLD ABE group.
//   - asthmaSeverity: keyword-based GINA step classifier from symptom text.
//
// Clinical disclaimer:
//   All numeric thresholds (FEV1/FVC < 0.70, GOLD grade FEV1 boundaries,
//   SpO2 ≤ 88%, DLCO < 40%, CAT ≥ 10, mMRC ≥ 2) and rule logic are
//   illustrative educational values derived from GOLD 2023 and GINA 2023
//   textbook summaries, not validated clinical guidelines. This agent is a
//   demo component and must not be used for real patient care. Post-
//   bronchodilator spirometry, haemoglobin-corrected DLCO, structured
//   exacerbation history, and clinical correlation are all required for
//   any real-world pulmonology assessment. Clinical-grade implementation
//   would require FDA SaMD validation under 21 CFR Part 820 / IEC 62304.
//
// See doc.go for package-level documentation.

package agents
