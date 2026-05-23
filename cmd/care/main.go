// Command care starts an HTTP API that publishes care-services messages
// onto the bus and exposes read endpoints for the nurse task queue, the panel
// report, and the notifier inbox.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/fhir"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	"github.com/PratikDhanave/bodh/pkg/registry"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func main() {
	addr := flag.String("addr", ":8088", "HTTP listen address")
	provider := flag.String("diagnostician", agents.ProviderRule,
		"diagnostician backend: rule | llm-anthropic | llm-openai | llm-ollama")
	flag.Parse()

	diag, err := agents.NewDiagnostician(*provider)
	if err != nil {
		log.Fatalf("diagnostician: %v", err)
	}

	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
	)

	patients := medical.NewPatientRegistry()
	queue := tasks.NewQueue()
	notifier := engagement.NewInMemory()
	reviews := hitl.NewQueue()
	reviewAgent := &agents.HumanReviewAgent{Queue: reviews}

	ctx := context.Background()
	for _, a := range []agent.Agent{
		&agents.IntakeAgent{},
		&agents.RPMMonitorAgent{},
		&agents.DiagnosticSupervisor{},
		&agents.QuestionerAgent{},
		&agents.TestPlannerAgent{},
		&agents.CostGuardianAgent{},
		diag,
		&agents.ReasoningVerifierAgent{HITL: true},
		&agents.CDMPlannerAgent{Registry: patients, HITL: true},
		&agents.TCMCoordinatorAgent{Tasks: queue, Registry: patients, HITL: true},
		&agents.PanelManagerAgent{Registry: patients, HITL: true},
		&agents.PreVisitPrepAgent{Registry: patients},
		&agents.InboxTriageAgent{Tasks: queue},
		&agents.RefillManagerAgent{Tasks: queue, Registry: patients, HITL: true},
		&agents.PriorAuthAgent{Tasks: queue, HITL: true},
		&agents.EngagementAgent{Notifier: notifier},
		&agents.ReadmissionTrackerAgent{Registry: patients, HITL: true},
		reviewAgent,
	} {
		_ = reg.Register(ctx, a)
	}
	orchestration.NewOrchestrator(reg, bus, policy, env).Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/patients", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var p medical.PatientRecord
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			patients.Upsert(p)
			writeJSON(w, map[string]string{"status": "ok", "patient_id": p.PatientID})
		case http.MethodGet:
			writeJSON(w, patients.List())
		}
	})

	mux.HandleFunc("/cdm", forward(bus, "cdm_planner", "cdm_request"))
	mux.HandleFunc("/tcm/discharge", func(w http.ResponseWriter, r *http.Request) {
		var d medical.DischargeSummary
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", "tcm_coordinator",
			agent.RoleUser, "discharge_event", medical.EncodeDischarge(d),
			map[string]any{"patient_id": d.PatientID, "case_id": d.CaseID}))
		writeJSON(w, map[string]string{"status": "queued"})
	})
	mux.HandleFunc("/panel", forwardBroadcast(bus, "panel_manager", "panel_request"))
	mux.HandleFunc("/pre_visit", forward(bus, "pre_visit_prep", "pre_visit_request"))
	mux.HandleFunc("/inbox", forward(bus, "inbox_triage", "inbox_message"))
	mux.HandleFunc("/refill", forward(bus, "refill_manager", "refill_request"))
	mux.HandleFunc("/priorauth", forward(bus, "prior_auth", "prior_auth_request"))

	mux.HandleFunc("/adt", func(w http.ResponseWriter, r *http.Request) {
		var e medical.ADTEvent
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", "readmission_tracker",
			agent.RoleUser, "adt_event", medical.EncodeADT(e),
			map[string]any{"patient_id": e.PatientID}))
		writeJSON(w, map[string]string{"status": "queued"})
	})

	mux.HandleFunc("/rpm/observation", func(w http.ResponseWriter, r *http.Request) {
		var obs medical.RPMObservation
		if err := json.NewDecoder(r.Body).Decode(&obs); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		alerts := medical.EvaluateRPMAlerts([]medical.RPMObservation{obs})
		writeJSON(w, map[string]any{"alerts": alerts})
	})

	mux.HandleFunc("/fhir/Patient", func(w http.ResponseWriter, r *http.Request) {
		var p fhir.Patient
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		patients.Upsert(fhir.PatientToRecord(p))
		writeJSON(w, map[string]string{"status": "imported", "patient_id": p.ID})
	})
	mux.HandleFunc("/fhir/Encounter", func(w http.ResponseWriter, r *http.Request) {
		var e fhir.Encounter
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ev, ok := fhir.EncounterToADT(e)
		if !ok {
			http.Error(w, "unsupported encounter status", http.StatusBadRequest)
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", "readmission_tracker",
			agent.RoleUser, "adt_event", medical.EncodeADT(ev),
			map[string]any{"patient_id": ev.PatientID}))
		writeJSON(w, map[string]string{"status": "imported"})
	})
	mux.HandleFunc("/fhir/Condition", func(w http.ResponseWriter, r *http.Request) {
		var c fhir.Condition
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		patientID := strings.TrimPrefix(c.Subject.Reference, "Patient/")
		rec, ok := patients.Get(patientID)
		if !ok {
			rec = medical.PatientRecord{PatientID: patientID}
		}
		rec.Conditions = appendUnique(rec.Conditions, fhir.ConditionCode(c))
		patients.Upsert(rec)
		writeJSON(w, map[string]string{"status": "imported"})
	})

	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, queue.List())
	})
	mux.HandleFunc("/notifications", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, notifier.List())
	})

	// Human-in-the-loop review endpoints.
	mux.HandleFunc("GET /reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") == "pending" {
			writeJSON(w, reviews.Pending())
			return
		}
		writeJSON(w, reviews.List())
	})
	mux.HandleFunc("GET /reviews/{id}", func(w http.ResponseWriter, r *http.Request) {
		req, ok := reviews.Get(r.PathValue("id"))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, req)
	})
	mux.HandleFunc("POST /reviews/{id}/decide", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ReviewerID   string `json:"reviewer_id"`
			Decision     string `json:"decision"`
			Reason       string `json:"reason"`
			Modification string `json:"modification"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, resume, ok := reviewAgent.ApplyDecision(
			r.PathValue("id"), body.ReviewerID, body.Decision, body.Reason, body.Modification)
		if !ok {
			http.Error(w, "not found, already decided, or invalid decision", http.StatusBadRequest)
			return
		}
		for _, m := range resume {
			bus.Publish(r.Context(), m)
		}
		writeJSON(w, req)
	})

	logger.Printf("care HTTP API listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

// forward returns an http.HandlerFunc that decodes the request body to a
// generic JSON envelope and publishes it as a Message on the bus.
func forward(bus comm.Bus, to, typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		meta := metaFromBody(body)
		if _, ok := meta["case_id"]; !ok {
			meta["case_id"] = "api-" + typ
		}
		bus.Publish(r.Context(), agent.NewMessage("user", to, agent.RoleUser, typ, body, meta))
		writeJSON(w, map[string]string{"status": "queued"})
	}
}

// forwardBroadcast publishes without requiring a case_id (panel_request is
// roster-scoped, not patient-scoped).
func forwardBroadcast(bus comm.Bus, to, typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", to, agent.RoleUser, typ, body, map[string]any{}))
		writeJSON(w, map[string]string{"status": "queued"})
	}
}

func readBody(r *http.Request) (string, error) {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf), nil
}

func metaFromBody(body string) map[string]any {
	m := map[string]any{}
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err == nil {
		if v, ok := raw["patient_id"].(string); ok {
			m["patient_id"] = v
		}
		if v, ok := raw["case_id"].(string); ok {
			m["case_id"] = v
		}
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
