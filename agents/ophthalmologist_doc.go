// ophthalmologist.go — rule-based ophthalmology specialist agent.
//
// Overview:
//   Receives an ophthalmology_consult (patient_id, case_id, diagnosis_context,
//   va_right, va_left, iop_right, iop_left, fundus_description, cup_disc_ratio,
//   hba1c, bp_systolic, duration_diabetes_years) and emits an ophthalmology_report
//   (diabetic_retinopathy_stage, diabetic_macular_edema, glaucoma_suspect,
//   vision_threatening, referral_urgency, recommendations, next_exam_months)
//   back to the requesting agent.
//
//   All staging and threshold logic is rule-based and intentionally simple.
//   A production-grade engine would incorporate OCT measurements, validated
//   screening algorithms, and formal FDA Software as a Medical Device (SaMD)
//   validation. The cutoffs here are textbook-reasonable educational values only.
//
// Key declarations:
//   - OphthalmologistAgent: zero-value-usable agent struct.
//   - OphthalmologistAgent.HandleMessage: decodes consult, calls interpretOphthalmology, emits reply.
//   - interpretOphthalmology: pure function; orchestrates all rule groups.
//   - assessVisualAcuity: classifies Snellen VA strings into (impaired, legalBlind).
//   - detectDME: keyword match for diabetic macular edema indicators.
//   - stageDiabeticRetinopathy: ETDRS-inspired DR staging from fundus text.
//   - assessGlaucoma: IOP and cup-disc ratio thresholds for glaucoma suspect.
//   - computeOphthUrgency: picks the highest referral urgency level.
//   - buildOphthRecommendations: assembles prioritised action list.
//   - computeNextExamInterval: returns months until next dilated fundus exam.
//
// ETDRS staging hierarchy (fundus text, highest-first evaluation):
//   PDR:          neovascularisation (NVD, NVE, new vessels)
//   Severe NPDR:  4-2-1 rule — 4-quadrant haemorrhages, venous beading, IRMA
//   Moderate NPDR:microaneurysms + cotton-wool spots / hard exudates / haemorrhages
//   Mild NPDR:    microaneurysms only
//   No DR:        no retinopathy indicators found
//
// DME detection keywords:
//   "macular edema", "hard exudates near fovea", "retinal thickening",
//   "clinically significant macular edema", "csme"
//
// Glaucoma suspect thresholds (illustrative):
//   IOP > 21 mmHg → elevated (suspect)
//   IOP > 25 mmHg → urgent (within-1-week referral)
//   IOP > 35 mmHg → same-day emergency referral
//   CDR > 0.6     → glaucoma suspect
//
// Referral urgency:
//   "same-day":       PDR, VA ≤ 20/200 (legal blindness), IOP > 35 mmHg
//   "within-1-week":  DME, VA worse than 20/40 (driving limit), IOP > 25 mmHg
//   "routine":        stable findings, Mild/Moderate NPDR without DME
//
// See doc.go for package-level documentation.

package agents
