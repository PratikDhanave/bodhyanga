// pre_visit_prep.go — assembles a chart summary before a clinician visit.
//
// Overview:
//   On pre_visit_request, pulls the patient record and produces a summary
//   of highlights (recent vitals, A1c, weight, medications), open gaps, and
//   suggested diagnoses, emitted as pre_visit_ready.
//
// Key declarations:
//   - PreVisitPrepAgent: agent with Registry reference.
//   - PreVisitPrepAgent.HandleMessage: assembles and emits pre_visit_ready.
//   - preVisitSummary: JSON shape returned to downstream consumers.
//
// See doc.go for package-level documentation.

package agents
