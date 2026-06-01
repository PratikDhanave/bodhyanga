// oncologist.go — Oncology Specialist agent for chemotherapy protocol
// selection, BSA computation, and dose adjustment.
//
// Overview:
//   Receives an oncology_consult message containing patient demographics,
//   cancer type, stage, ECOG performance status, and renal function; selects
//   the appropriate chemotherapy protocol (Carboplatin/Paclitaxel for NSCLC,
//   Trastuzumab+Pertuzumab for HER2+ breast, FOLFOX for stage III colon,
//   R-CHOP for DLBCL); computes BSA via the Mosteller formula; applies dose
//   reductions for ECOG ≥ 3 and moderate/severe renal impairment; and emits
//   an oncology_recommendation message with per-drug orders, dose reductions,
//   supportive care, and monitoring instructions.
//
//   All protocols, doses, and decision thresholds are simplified educational
//   examples — NOT clinical guidelines and not intended to drive real care.
//
// Key declarations:
//   - OncologistAgent: stateless, zero-value-usable oncology specialist.
//   - OncologyConsult: input struct (patient_id, case_id, cancer_type, stage,
//     ecog_score, weight_kg, height_cm, renal_function, prior_treatments).
//   - OncologyRecommendation: output struct (protocol, regimen, drugs, bsa_m2,
//     dose_reductions, supportive_care, monitoring, urgent).
//   - DrugOrder: per-drug detail within a recommendation.
//   - mosteller: BSA = sqrt(height_cm × weight_kg / 3600).
//   - selectProtocol: protocol router by cancer type / stage / ECOG.
//   - applyDoseReductions: ECOG ≥ 3 (−25% all) and renal adjustments.
//   - nsclcCarboPaclitaxel / breastHER2TrasTuz / colonFOLFOX / lymphomaRCHOP:
//     protocol builders.
//   - supportiveCareOnly: fallback for unrecognised cancer types (Urgent=true).
//
// See doc.go for package-level documentation.

package agents
