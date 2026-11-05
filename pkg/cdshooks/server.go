package cdshooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Handler is a function that processes a CDS request and returns a response.
// Implementations should be fast (< 500 ms) to avoid EHR timeouts.
type Handler func(ctx context.Context, req CDSRequest) (CDSResponse, error)

// Server routes incoming CDS Hooks HTTP requests to registered handlers and
// serves the discovery endpoint required by the specification.
type Server struct {
	services map[Hook]Service
	handlers map[Hook]Handler
	mux      *http.ServeMux
}

// NewServer creates an empty CDS Hooks server with no registered services.
// Call Register for each hook before calling Handler.
func NewServer() *Server {
	s := &Server{
		services: make(map[Hook]Service),
		handlers: make(map[Hook]Handler),
		mux:      http.NewServeMux(),
	}
	// Discovery endpoint — returns all registered services.
	s.mux.HandleFunc("GET /cds-services", s.handleDiscovery)
	return s
}

// Register adds a CDS service and its handler to the server.
// The service ID is used as the path segment: POST /cds-services/{id}.
// Calling Register after Handler has been called is not safe for concurrent use.
func (s *Server) Register(svc Service, h Handler) {
	s.services[svc.Hook] = svc
	s.handlers[svc.Hook] = h
	pattern := fmt.Sprintf("POST /cds-services/%s", svc.ID)
	s.mux.HandleFunc(pattern, s.makeHookHandler(svc.Hook))
}

// Handler returns the http.Handler that serves all CDS Hooks routes.
// It is safe to call this once all services have been registered.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// handleDiscovery serves GET /cds-services and lists all registered services.
func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	list := make([]Service, 0, len(s.services))
	for _, svc := range s.services {
		list = append(list, svc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": list})
}

// makeHookHandler returns an http.HandlerFunc that dispatches to the Hook handler.
func (s *Server) makeHookHandler(hook Hook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CDSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		h, ok := s.handlers[hook]
		if !ok {
			writeError(w, http.StatusNotFound, "no handler for hook "+string(hook))
			return
		}

		resp, err := h(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ─── Built-in Bodhyanga hook handlers ─────────────────────────────────────────────

// BuiltinServices returns the four built-in Bodhyanga CDS services and their handlers.
// Register all of them in one call:
//
//	for _, pair := range cdshooks.BuiltinServices() {
//	    srv.Register(pair.Service, pair.Handler)
//	}
func BuiltinServices() []ServiceHandler {
	return []ServiceHandler{
		{
			Service: Service{
				Hook:        HookPatientView,
				ID:          "bodhyanga-patient-view",
				Title:       "Bodhyanga — Preventive Care Gaps",
				Description: "Checks for overdue panel gaps (A1c, BP, mammogram, colonoscopy) and returns warning cards for each gap.",
				Prefetch: map[string]string{
					"patient": "Patient/{{context.patientId}}",
				},
			},
			Handler: patientViewHandler,
		},
		{
			Service: Service{
				Hook:        HookMedPrescribe,
				ID:          "bodhyanga-medication-prescribe",
				Title:       "Bodhyanga — Drug-Drug Interaction Check",
				Description: "Screens draft medication orders for high-severity drug-drug interactions and returns critical cards.",
				Prefetch: map[string]string{
					"medications": "MedicationRequest?patient={{context.patientId}}&status=active",
				},
			},
			Handler: medPrescribeHandler,
		},
		{
			Service: Service{
				Hook:        HookOrderSign,
				ID:          "bodhyanga-order-sign",
				Title:       "Bodhyanga — Diagnostic Cost Check",
				Description: "Compares the cost of draft diagnostic orders against the patient's plan budget and returns an info card when over threshold.",
			},
			Handler: orderSignHandler,
		},
		{
			Service: Service{
				Hook:        HookEncounterDischarge,
				ID:          "bodhyanga-encounter-discharge",
				Title:       "Bodhyanga — TCM Follow-Up",
				Description: "Suggests a Transitional Care Management follow-up appointment after discharge to reduce readmission risk.",
			},
			Handler: encounterDischargeHandler,
		},
	}
}

// ServiceHandler pairs a Service descriptor with its Handler for bulk registration.
type ServiceHandler struct {
	Service Service
	Handler Handler
}

// ─── patient-view ─────────────────────────────────────────────────────────────

// patientViewHandler checks for overdue preventive care gaps and returns a
// warning card for each gap found in the context.
func patientViewHandler(_ context.Context, req CDSRequest) (CDSResponse, error) {
	// Extract gap list from context; EHR may populate "gaps" or we infer defaults.
	gaps := extractStringSlice(req.Context, "gaps")
	if len(gaps) == 0 {
		// Default: flag common panel gaps for demonstration purposes.
		gaps = []string{"HbA1c (overdue > 12 months)", "Blood pressure screening (overdue)"}
	}

	cards := make([]Card, 0, len(gaps))
	for _, gap := range gaps {
		cards = append(cards, Card{
			Summary:   fmt.Sprintf("Care gap: %s", truncate(gap, 130)),
			Detail:    fmt.Sprintf("**%s** is overdue per current preventive care guidelines. Consider scheduling or ordering today.", gap),
			Indicator: "warning",
			Source: Source{
				Label: "Bodhyanga Preventive Care Engine",
				URL:   "https://github.com/PratikDhanave/bodhyanga",
			},
			Suggestions: []Suggestion{
				{
					Label: "Schedule " + gap,
					Actions: []Action{
						{
							Type:        "create",
							Description: "Create ServiceRequest for " + gap,
							Resource: map[string]any{
								"resourceType": "ServiceRequest",
								"status":       "draft",
								"intent":       "proposal",
								"code":         map[string]any{"text": gap},
							},
						},
					},
				},
			},
		})
	}

	return CDSResponse{Cards: cards}, nil
}

// ─── medication-prescribe ─────────────────────────────────────────────────────

// ddiTable is a minimal hard-coded drug-drug interaction table for demonstration.
// Keys use the lexicographic sorted pair "smaller|larger" produced by sortedPair.
var ddiTable = map[string]string{
	"aspirin|warfarin":      "HIGH",
	"contrast|metformin":    "HIGH",
	"maoi|ssri":             "HIGH",
	"amiodarone|digoxin":    "HIGH",
	"clopidogrel|omeprazole": "MODERATE",
}

// medPrescribeHandler runs a drug-drug interaction check on the draft medication
// list and returns critical cards for any HIGH-severity interactions found.
func medPrescribeHandler(_ context.Context, req CDSRequest) (CDSResponse, error) {
	// Extract draft medications from context.
	draftMeds := extractStringSlice(req.Context, "medications")
	if len(draftMeds) == 0 {
		return CDSResponse{}, nil
	}

	var cards []Card
	checked := make(map[string]bool)

	for i, medA := range draftMeds {
		for _, medB := range draftMeds[i+1:] {
			key := sortedPair(strings.ToLower(medA), strings.ToLower(medB))
			if checked[key] {
				continue
			}
			checked[key] = true
			severity, ok := ddiTable[key]
			if !ok || severity != "HIGH" {
				continue
			}
			cards = append(cards, Card{
				Summary:   fmt.Sprintf("HIGH drug interaction: %s + %s", medA, medB),
				Detail:    fmt.Sprintf("**%s** and **%s** have a known HIGH-severity drug-drug interaction. Review patient medications before signing.", medA, medB),
				Indicator: "critical",
				Source: Source{
					Label: "Bodhyanga DDI Engine",
					URL:   "https://github.com/PratikDhanave/bodhyanga",
				},
				Links: []Link{
					{
						Label: "DrugBank interaction reference",
						URL:   "https://go.drugbank.com/drug-interaction-checker",
						Type:  "absolute",
					},
				},
			})
		}
	}

	return CDSResponse{Cards: cards}, nil
}

// ─── order-sign ───────────────────────────────────────────────────────────────

const defaultCostThresholdUSD = 500.0

// orderSignHandler compares the estimated cost of draft diagnostic orders against
// a configurable budget threshold and returns an info card if over threshold.
func orderSignHandler(_ context.Context, req CDSRequest) (CDSResponse, error) {
	// Pull estimated total cost from context (EHR populates this field).
	estimatedCost := extractFloat(req.Context, "estimatedCostUSD")
	threshold := extractFloat(req.Context, "budgetThresholdUSD")
	if threshold == 0 {
		threshold = defaultCostThresholdUSD
	}

	if estimatedCost <= threshold {
		return CDSResponse{}, nil
	}

	return CDSResponse{
		Cards: []Card{
			{
				Summary:   fmt.Sprintf("Order cost $%.0f exceeds plan threshold $%.0f", estimatedCost, threshold),
				Detail:    fmt.Sprintf("The estimated cost of the current order set (**$%.2f**) exceeds the patient's plan diagnostic budget (**$%.2f**). Review orders for alternatives.", estimatedCost, threshold),
				Indicator: "info",
				Source: Source{
					Label: "Bodhyanga Diagnostic Cost Engine",
					URL:   "https://github.com/PratikDhanave/bodhyanga",
				},
			},
		},
	}, nil
}

// ─── encounter-discharge ─────────────────────────────────────────────────────

// encounterDischargeHandler suggests a TCM (Transitional Care Management)
// follow-up appointment to reduce 30-day readmission risk.
func encounterDischargeHandler(_ context.Context, req CDSRequest) (CDSResponse, error) {
	patientID, _ := req.Context["patientId"].(string)
	detail := "A **Transitional Care Management** (TCM) follow-up call within 2 business days and an office visit within 7 days (CPT 99496) can significantly reduce 30-day readmission risk."
	if patientID != "" {
		detail = fmt.Sprintf("Patient %s: %s", patientID, detail)
	}

	return CDSResponse{
		Cards: []Card{
			{
				Summary:   "Schedule TCM follow-up to reduce readmission risk",
				Detail:    detail,
				Indicator: "info",
				Source: Source{
					Label: "Bodhyanga Care Transitions Engine",
					URL:   "https://github.com/PratikDhanave/bodhyanga",
				},
				Suggestions: []Suggestion{
					{
						Label: "Schedule TCM follow-up (CPT 99496)",
						Actions: []Action{
							{
								Type:        "create",
								Description: "Create TCM follow-up appointment",
								Resource: map[string]any{
									"resourceType": "Appointment",
									"status":       "proposed",
									"serviceType": []map[string]any{
										{"text": "Transitional Care Management"},
									},
									"description": "TCM follow-up within 7 days of discharge (CPT 99496)",
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// extractStringSlice pulls a []string or []any from ctx[key].
func extractStringSlice(ctx map[string]any, key string) []string {
	v, ok := ctx[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// extractFloat pulls a float64 from ctx[key], accepting float64 or int types.
func extractFloat(ctx map[string]any, key string) float64 {
	v, ok := ctx[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	}
	return 0
}

// sortedPair returns "a|b" with a ≤ b lexicographically.
func sortedPair(a, b string) string {
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// truncate shortens s to at most n runes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
