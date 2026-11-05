// Package cdshooks implements the HL7 CDS Hooks standard for real-time
// clinical decision support. EHRs such as Epic and Cerner call Bodhyanga at
// key workflow moments (patient-view, order-sign, medication-prescribe,
// encounter-discharge) and receive decision-support cards in response.
//
// Spec: https://cds-hooks.org/
package cdshooks

// Hook identifies the workflow moment at which an EHR fires a CDS request.
type Hook string

const (
	// HookPatientView fires when a clinician opens a patient's record.
	HookPatientView Hook = "patient-view"
	// HookOrderSign fires when a clinician is about to sign an order set.
	HookOrderSign Hook = "order-sign"
	// HookMedPrescribe fires when a medication is being prescribed.
	HookMedPrescribe Hook = "medication-prescribe"
	// HookEncounterDischarge fires when an encounter is being discharged.
	HookEncounterDischarge Hook = "encounter-discharge"
)

// CDSRequest is the payload the EHR sends to a CDS service endpoint.
type CDSRequest struct {
	// Hook identifies the hook that triggered this request.
	Hook Hook `json:"hook"`
	// HookInstance is a UUID identifying this specific firing of the hook.
	HookInstance string `json:"hookInstance"`
	// Context contains hook-specific context data (patient ID, draft orders, etc.).
	Context map[string]any `json:"context"`
	// Prefetch contains pre-fetched FHIR resources requested by the service.
	Prefetch map[string]any `json:"prefetch,omitempty"`
}

// Card is a single piece of decision-support guidance returned to the EHR.
// The EHR renders the card inline in the clinician's workflow.
type Card struct {
	// UUID is an optional unique identifier for this card instance.
	UUID string `json:"uuid,omitempty"`
	// Summary is a brief human-readable description (≤ 140 characters).
	Summary string `json:"summary"`
	// Detail is an optional extended Markdown-formatted description.
	Detail string `json:"detail,omitempty"`
	// Indicator conveys the urgency: "info", "warning", or "critical".
	Indicator string `json:"indicator"`
	// Source identifies the knowledge base or guideline behind this card.
	Source Source `json:"source"`
	// Suggestions are optional actions the clinician can accept.
	Suggestions []Suggestion `json:"suggestions,omitempty"`
	// Links are optional external references for additional context.
	Links []Link `json:"links,omitempty"`
}

// Source identifies the origin of the clinical guidance in a Card.
type Source struct {
	// Label is the human-readable name of the source.
	Label string `json:"label"`
	// URL is an optional link to the source guideline or policy.
	URL string `json:"url,omitempty"`
	// Icon is an optional URL to a 100×100 PNG icon representing the source.
	Icon string `json:"icon,omitempty"`
}

// Suggestion represents an optional action the clinician can accept.
type Suggestion struct {
	// Label is the human-readable description of this suggestion.
	Label string `json:"label"`
	// UUID is an optional identifier for this suggestion.
	UUID string `json:"uuid,omitempty"`
	// Actions are the FHIR operations to perform if this suggestion is accepted.
	Actions []Action `json:"actions,omitempty"`
}

// Action is a FHIR operation (create, update, delete) within a Suggestion.
type Action struct {
	// Type is "create", "update", or "delete".
	Type string `json:"type"`
	// Description is a human-readable summary of the action.
	Description string `json:"description"`
	// Resource is the FHIR resource to create, update, or delete.
	Resource map[string]any `json:"resource,omitempty"`
}

// Link is an external reference attached to a Card.
type Link struct {
	// Label is the human-readable link text.
	Label string `json:"label"`
	// URL is the target of the link.
	URL string `json:"url"`
	// Type is "absolute" for a plain URL, or "smart" for a SMART app link.
	Type string `json:"type"`
}

// CDSResponse is the payload returned by a CDS service endpoint.
type CDSResponse struct {
	// Cards is the list of decision-support cards to display to the clinician.
	Cards []Card `json:"cards"`
}

// Service describes a single CDS hook service for the discovery endpoint.
type Service struct {
	// Hook is the hook this service subscribes to.
	Hook Hook `json:"hook"`
	// ID is the service identifier used in the endpoint path (/cds-services/{id}).
	ID string `json:"id"`
	// Title is a human-readable name for this service.
	Title string `json:"title"`
	// Description explains what this service does.
	Description string `json:"description"`
	// Prefetch declares FHIR resources the EHR should pre-fetch and include.
	Prefetch map[string]string `json:"prefetch,omitempty"`
}
