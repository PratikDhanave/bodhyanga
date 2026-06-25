package medical

// PatientStore is the patient-roster surface the population-health agents
// depend on. It is the UNION of the *PatientRegistry methods actually called
// across the six agents that hold a roster reference:
//
//	Get / Upsert            — cdm_planner, tcm_coordinator, readmission_tracker
//	Get                     — pre_visit_prep, refill_manager
//	List / FilterByCondition — panel_manager
//
// Why this exists (agent decoupling, Phase 0): agents currently hold the
// concrete *PatientRegistry (in-memory). Depending on this interface instead
// lets a later step inject the Postgres-backed
// *persistence/postgres.PatientRegistryAdapter — which implements the same
// four methods with identical signatures — without touching agent code. Both
// implementations satisfy PatientStore, so this change is behavior-preserving:
// every existing construction site passes *PatientRegistry, which still fits.
//
// Keep this interface NARROW. It is intentionally the minimal method set the
// agents call, not the full PatientRegistry API — a smaller surface is easier
// to back with a real datastore and cheaper to fake in tests.
type PatientStore interface {
	Get(id string) (PatientRecord, bool)
	Upsert(p PatientRecord)
	List() []PatientRecord
	FilterByCondition(code string) []PatientRecord
}

// Compile-time assertion that the in-memory registry satisfies the interface.
// The Postgres adapter (*persistence/postgres.PatientRegistryAdapter) also
// satisfies it, but asserting that here would create an import cycle
// (persistence imports medical, not the reverse), so that direction is proven
// by the adapter package's own usage.
var _ PatientStore = (*PatientRegistry)(nil)
