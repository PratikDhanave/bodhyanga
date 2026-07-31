// Command care starts an HTTP API that publishes care-services messages
// onto the bus and exposes read endpoints for the nurse task queue, the panel
// report, and the notifier inbox.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audio"
	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/auth"
	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/engagement"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/fhir"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/hl7v2"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/llm/pool"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/orchestration"
	pgstore "github.com/PratikDhanave/bodh/pkg/persistence/postgres"
	"github.com/PratikDhanave/bodh/pkg/registry"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

func main() {
	addr := flag.String("addr", ":8088", "HTTP listen address")
	provider := flag.String("diagnostician", agents.ProviderRule,
		"diagnostician backend: rule | llm-anthropic | llm-openai | llm-ollama | llm-kronk")
	flag.Parse()

	ctx := context.Background()

	store := loadRAGStore(ctx, *provider)

	// Multi-model pool. Opt-in via BODH_LLM_POOL=1. The pool sits beside the
	// existing per-agent llm.Client construction — it's the foundation for
	// the kronk on-prem track (#21), where several models share one GPU and
	// need refcounted, TTL-evicted residency. With BODH_LLM_POOL unset, the
	// existing single-client path is unchanged.
	llmPool := maybeStartLLMPool()
	if llmPool != nil {
		defer llmPool.Close()
	}

	diag, err := agents.NewDiagnosticianWithKnowledge(*provider, store)
	if err != nil {
		log.Fatalf("diagnostician: %v", err)
	}

	logger := observability.NewStdLogger()
	env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}
	reg := registry.NewInMemory()
	bus := comm.NewInMemoryBus()
	agentMeshGate := governance.NewAgentMeshGate()
	policy := governance.NewComposite(
		governance.MaxContentLengthPolicy{Max: 65536},
		governance.RequireCaseIDPolicy{},
		governance.MaxDiagnosticCostPolicy{},
		governance.ResearchDisclaimerPolicy{},
		agentMeshGate, // kill-switch + ring + prompt-injection defense
	)

	patients := medical.NewPatientRegistry()
	pg := loadPostgresStore(ctx, logger, patients)
	if pg != nil {
		defer pg.Close()
	}
	queue := tasks.NewQueue()
	notifier := engagement.NewInMemory()
	reviews := hitl.NewQueue()
	reviewAgent := &agents.HumanReviewAgent{Queue: reviews}

	// Audio transcription. The default build links the MockTranscriber
	// (no model runtime); `-tags kronk` swaps in the on-prem Whisper
	// backend via pkg/audio/kronk. The agent's Audit field is set
	// later once selectBackends has returned the operator's recorder.
	audioTranscriber := &agents.AudioTranscriberAgent{
		Transcriber: audio.NewMock(),
	}

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
		audioTranscriber,
	} {
		_ = reg.Register(ctx, a)
	}
	// HIPAA §164.312(b) audit + eval mirror. Defaults are in-memory so the
	// demo path stays self-contained; when BODH_DB_DSN is set the Postgres
	// adapters (RLS-scoped to BODH_TENANT_ID) replace them. The recorders
	// must be wired BEFORE Start — subscriptions capture them at subscribe
	// time and a post-Start swap is silently ignored.
	auditRec, evalStore := selectBackends(pg, logger)
	// Now that the audit recorder is chosen, attach it to the audio
	// transcriber so per-call PHI-free events flow into the same sink.
	audioTranscriber.Audit = auditRec
	orch := orchestration.NewOrchestrator(reg, bus, policy, env)
	orch.SetAuditRecorder(auditRec)
	orch.SetEvalStore(evalStore)
	orch.Start(ctx)

	mux := http.NewServeMux()

	// Kubernetes liveness + readiness probes. These must be registered before
	// any middleware chain so they are reachable even when the application is
	// partially initialised. /healthz is always 200; /readyz pings Postgres.
	mux.HandleFunc("GET /healthz", livenessHandler())
	mux.HandleFunc("GET /readyz", readinessHandler(pg))

	// Prometheus scrape endpoint. Exposes Go runtime metrics + bodh_http_*
	// and bodh_llm_* families. No auth — the NetworkPolicy restricts ingress
	// to the API gateway / monitoring namespace in Kubernetes.
	mux.Handle("GET /metrics", observability.PrometheusHandler())

	mux.HandleFunc("/patients", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var p medical.PatientRecord
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			patients.Upsert(p)
			if pg != nil {
				if err := pg.UpsertPatient(r.Context(), defaultTenant(), p); err != nil {
					logger.Printf("[care] postgres upsert failed: %v", err)
				}
			}
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

	// HL7 v2.x ADT ingress — the "front door" used by most US hospital
	// integrations. Accepts pipe-delimited messages with either Content-Type
	// "application/hl7-v2" or "text/plain". Publishes adt_event onto the bus
	// and responds with an HL7 ACK in the body (AA for accepted, AE for
	// parse/business errors, HTTP 200 / 400 respectively). No PHI in logs:
	// we log only the message control id and event type.
	mux.HandleFunc("/hl7/adt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		raw, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/hl7-v2")
		msg, err := hl7v2.ParseMessage(raw)
		if err != nil {
			logger.Printf("hl7 adt parse error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(hl7v2.BuildACK(hl7v2.MSH{}, "AE", err.Error())))
			return
		}
		ev, err := hl7v2.MessageToADTEvent(msg)
		if err != nil {
			logger.Printf("hl7 adt map error: control_id=%s trigger=%s err=%v",
				msg.Header.ControlID, msg.Header.TriggerEvent, err)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(hl7v2.BuildACK(msg.Header, "AE", err.Error())))
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", "readmission_tracker",
			agent.RoleUser, "adt_event", medical.EncodeADT(ev),
			map[string]any{"patient_id": ev.PatientID, "hl7_control_id": msg.Header.ControlID}))
		logger.Printf("hl7 adt accepted: control_id=%s trigger=%s type=%s",
			msg.Header.ControlID, msg.Header.TriggerEvent, ev.Type)
		_, _ = w.Write([]byte(hl7v2.BuildACK(msg.Header, "AA", "")))
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

	// Kill-switch admin endpoints. Operators can halt the entire fleet or
	// quarantine individual agents without restarting the process. Routes are
	// under /admin/ which is not exposed outside the internal network by the
	// Kubernetes NetworkPolicy (zarf/k8s/base/networkpolicy.yaml).
	ksServer := &killSwitchServer{reg: agentMeshGate.KillSwitches()}
	ksServer.mount(mux)

	// Audio transcription endpoint. Multipart upload → transcribed
	// text on the response. The agent runs synchronously here (see
	// cmd/care/audio.go for the rationale); the audit event the agent
	// records is correlated back via audit_event_id in the response.
	mux.HandleFunc("POST /api/transcribe", transcribeHandler(audioTranscriber, env, auditRec))

	// Audit log read endpoint. Developer / operator visibility into the
	// HIPAA §164.312(b) sink — reads from whichever Recorder was wired at
	// boot (in-memory or Postgres-backed via tenant adapter). Query params:
	//
	//   ?kind=message|policy_decision|review_decision|agent_error  filter
	//   ?limit=N                                                   cap (default 100)
	//
	// For Postgres-backed deployments tenant scoping is fixed at startup
	// (BODH_TENANT_ID); a true multi-tenant binary would route per-request.
	mux.HandleFunc("GET /audit", auditListHandler(auditRec))

	// Right-to-explanation endpoint. Returns a structured, PHI-free
	// explanation of every agent invocation, every governance decision,
	// every human review, and the final metrics for a given case.
	// Aligns with GDPR Article 22(3) + Article 15. See cmd/care/explain.go.
	mux.HandleFunc("GET /explain/{case_id}", explainHandler(defaultTenant(), auditRec, evalStore, reviews))

	// /api/* routes + SSE + go:embed SPA for the clinician console.
	traces := newLLMTraceBuffer(500)
	// Capture every LLMTrace into the buffer so the SPA can show recent calls.
	// The observability hook only fires when BODH_OTEL_ENABLED=1, so set it
	// here unconditionally — cmd/care's hook just goes to the in-process
	// buffer (no external OTel collector required).
	_ = os.Setenv("BODH_OTEL_ENABLED", "1")
	hook := traces.record
	// Opt-in OTLP export: when BODH_OTEL_EXPORT=1 (and the binary was built
	// with -tags otel) ship every LLMTrace to an OTLP/HTTP collector in
	// addition to the in-process buffer. Endpoint defaults to localhost:4318.
	if os.Getenv("BODH_OTEL_EXPORT") == "1" {
		endpoint := envDefault("BODH_OTEL_ENDPOINT", "http://localhost:4318")
		exp, err := observability.NewOTLPLLMExporter(ctx, endpoint)
		if err != nil {
			logger.Printf("[care] OTLP exporter disabled: %v", err)
		} else {
			logger.Printf("[care] OTLP exporter enabled → %s", endpoint)
			hook = func(t observability.LLMTrace) {
				traces.record(t)
				exp.Record(t)
			}
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = exp.Shutdown(shutdownCtx)
			}()
		}
	}
	observability.SetLLMOTelHook(hook)

	rm := newRunManager(bus)
	api := &apiServer{
		bus:         bus,
		reg:         reg,
		queue:       queue,
		reviews:     reviews,
		patients:    patients,
		notifier:    notifier,
		traces:      traces,
		runs:        rm,
		fixturesDir: envDefault("BODH_FIXTURES_DIR", "data/cases"),
		provider:    *provider,
		auditRec:    auditRec,
	}
	api.mount(mux)

	// JWT auth (demo / self-hosted path). Seed the credential store with the
	// illustrative users (clinician/clinic123, admin/admin123), mount the
	// /api/auth/* endpoints, and gate every other /api/* route behind a Bearer
	// token via authMiddleware in the handler chain below. POST /api/auth/login,
	// /healthz and /readyz stay public (see publicRoutes).
	authStore := auth.NewCredentialStore()
	// The login form input is type=email, so also seed an email-format demo
	// credential that matches the form's placeholder. (The bare clinician/admin
	// seeds remain for API and test callers.)
	_ = authStore.AddUser("clinician@hospital.org", "clinic123", auth.RoleClinician)
	authSecret := auth.JWTSecret()
	(&authServer{store: authStore, secret: authSecret}).mountAuth(mux)

	if h, ok := spaHandler(); ok {
		mux.Handle("/ui/", h)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/ui/", http.StatusFound)
				return
			}
			http.NotFound(w, r)
		})
		logger.Printf("clinician console (SPA) served at /ui/")
	} else {
		logger.Printf("clinician console NOT embedded — run `npm run build` in cmd/care/web/")
	}

	// Wrap the mux with:
	//   1. Prometheus HTTP middleware — records bodh_http_* metrics for every
	//      request (always active, no build tag required).
	//   2. OTel HTTP trace handler — adds an OTel span per request (active only
	//      when built with -tags otel AND NewOTLPLLMExporter was called above
	//      to set the global TracerProvider).
	handler := observability.TraceHandler(
		observability.PromMiddleware(
			authMiddleware(authStore, authSecret)(mux),
		),
		"bodh-care",
	)

	logger.Printf("care HTTP API listening on %s", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

// noopCloser is what the pool gets back for cloud-provider clients: nothing
// to release on eviction, since the underlying http.Client is GC-managed.
// Kept as a named type (rather than io.NopCloser, which is for ReadClosers)
// so the intent is clear at call sites.
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// maybeStartLLMPool constructs a pool when BODH_LLM_POOL=1 and returns it
// (or nil). The pool routes by ModelSpec.Provider: "kronk" specs go through
// the kronk Factory (which errors with ErrKronkNotBuilt unless the binary
// was built with -tags kronk); cloud specs go through the standard llm
// constructors. TTL and MaxResident come from env vars so operators can
// tune GPU sizing without recompiling.
//
// See docs/operations.md ("Multi-model pool") for the env vars and tuning
// notes; see docs/ai-features.md for the design.
func maybeStartLLMPool() *pool.Pool {
	if os.Getenv("BODH_LLM_POOL") != "1" {
		return nil
	}
	maxResident := envInt("BODH_LLM_POOL_MAX", 0)
	ttl := envDuration("BODH_LLM_POOL_TTL", 0)
	logger := observability.NewStdLogger()
	kronkFactory := pool.NewKronkFactory(pool.KronkOptions{
		ModelsDir:        os.Getenv("BODH_KRONK_MODELS_DIR"),
		MaxContextTokens: envInt("BODH_KRONK_CTX", 0),
		GPULayers:        envInt("BODH_KRONK_GPU_LAYERS", -1),
	})
	factory := func(ctx context.Context, spec pool.ModelSpec) (llm.Client, io.Closer, error) {
		switch spec.Provider {
		case "kronk":
			return kronkFactory(ctx, spec)
		case "anthropic":
			key := os.Getenv("ANTHROPIC_API_KEY")
			if key == "" {
				return nil, nil, fmt.Errorf("pool: ANTHROPIC_API_KEY not set")
			}
			return llm.NewAnthropic(key, spec.Model), noopCloser{}, nil
		case "openai":
			key := os.Getenv("OPENAI_API_KEY")
			if key == "" {
				return nil, nil, fmt.Errorf("pool: OPENAI_API_KEY not set")
			}
			return llm.NewOpenAI(key, spec.Model), noopCloser{}, nil
		case "ollama":
			return llm.NewOllama(os.Getenv("OLLAMA_HOST"), spec.Model), noopCloser{}, nil
		default:
			return nil, nil, fmt.Errorf("pool: unknown provider %q", spec.Provider)
		}
	}
	p := pool.NewPool(factory, pool.Options{
		MaxResident: maxResident,
		TTL:         ttl,
		Logger:      logger,
	})
	logger.Printf("[care] llm pool: enabled (max_resident=%d ttl=%s)", maxResident, ttl)
	return p
}

// envInt reads an integer-valued env var, returning the default when unset
// or unparseable. Kept here rather than in a shared util because it's only
// used by the pool wiring.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envDuration reads a duration env var (e.g. "5m", "30s"). Returns the
// default on missing or malformed values — failing soft is correct here,
// the pool just falls back to no-TTL behaviour.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// loadRAGStore builds the knowledge.Store used by the diagnostician when
// BODH_KNOWLEDGE_CORPUS is set and an embedder credential is available for
// the supplied provider. Prints a one-line startup log either way.
func loadRAGStore(ctx context.Context, provider string) knowledge.Store {
	corpus := os.Getenv("BODH_KNOWLEDGE_CORPUS")
	if corpus == "" {
		log.Println("[care] RAG disabled (BODH_KNOWLEDGE_CORPUS not set)")
		return nil
	}
	embedder, err := agents.DefaultEmbedderForProvider(provider)
	if err != nil {
		log.Printf("[care] RAG disabled (embedder for %s: %v)", provider, err)
		return nil
	}
	if embedder == nil {
		log.Printf("[care] RAG disabled (no embedder credential for %s)", provider)
		return nil
	}
	store, err := agents.LoadDefaultKnowledge(ctx, embedder)
	if err != nil {
		log.Printf("[care] RAG disabled (corpus load failed: %v)", err)
		return nil
	}
	if store == nil {
		log.Println("[care] RAG disabled (BODH_KNOWLEDGE_CORPUS not set)")
		return nil
	}
	log.Printf("[care] RAG enabled with %d docs", store.Size())
	return store
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// defaultTenant returns the operator-supplied tenant ID. cmd/care is a
// single-tenant binary today; multi-tenant routing belongs in a future PR
// that reads tenant_id from request metadata (auth header / JWT claim).
func defaultTenant() string {
	if v := os.Getenv("BODH_TENANT_ID"); v != "" {
		return v
	}
	return "default"
}

// selectBackends returns the audit.Recorder and eval.Store the orchestrator
// should use. When pg is non-nil (BODH_DB_DSN set) the Postgres adapters
// scoped to the default tenant are returned; otherwise the in-memory
// implementations are used. The chosen backends are logged at startup so
// operators can see at a glance whether they're on the demo or production
// data path.
func selectBackends(pg *pgstore.Store, logger interface{ Printf(string, ...any) }) (audit.Recorder, eval.Store) {
	if pg != nil {
		tenant := defaultTenant()
		logger.Printf("[care] audit backend: postgres (tenant=%s)", tenant)
		logger.Printf("[care] eval backend: postgres (tenant=%s)", tenant)
		return pg.AuditRecorder(tenant), pg.EvalStore(tenant)
	}
	logger.Printf("[care] audit backend: in-memory")
	logger.Printf("[care] eval backend: in-memory")
	return audit.NewInMemory(), eval.NewInMemoryStore()
}

// auditListHandler returns a GET /audit handler that reads from rec, applies
// the optional ?kind= filter, and caps at ?limit= (default 100). Pulling
// this out of the main.go inline closure makes it directly testable.
func auditListHandler(rec audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events := rec.List()
		kindFilter := r.URL.Query().Get("kind")
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := parseLimit(v); err == nil {
				limit = n
			}
		}
		out := make([]audit.Event, 0, len(events))
		for _, e := range events {
			if kindFilter != "" && string(e.Kind) != kindFilter {
				continue
			}
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
		writeJSON(w, out)
	}
}

// parseLimit converts the ?limit=N query parameter; returns an error on a
// bad value (non-numeric, zero, or negative) so the caller can fall back
// to the default rather than blowing up the request.
func parseLimit(v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("limit must be positive, got %d", n)
	}
	return n, nil
}

// loadPostgresStore opens the Postgres backend when BODH_DB_DSN is set,
// hydrates the in-memory patients registry from it, and returns the *Store
// for mirror-writes. When the DSN is empty the file/in-memory backends
// continue to drive the demo unchanged. A failure to connect logs the error
// and returns nil — the demo path is always available as a fallback.
func loadPostgresStore(ctx context.Context, logger interface{ Printf(string, ...any) }, patients *medical.PatientRegistry) *pgstore.Store {
	dsn := os.Getenv("BODH_DB_DSN")
	if dsn == "" {
		logger.Printf("[care] persistence backend: in-memory + file (BODH_DB_DSN not set)")
		return nil
	}
	store, err := pgstore.New(ctx, dsn)
	if err != nil {
		logger.Printf("[care] persistence backend: in-memory fallback (postgres open failed: %v)", err)
		return nil
	}
	tenant := defaultTenant()
	store.SetDefaultTenant(tenant)
	logger.Printf("[care] persistence backend: postgres (tenant=%s)", tenant)

	// Warm the in-memory registry from Postgres so agents (which hold the
	// concrete *medical.PatientRegistry) see existing rows on first request.
	// Mirror-writes back to Postgres happen on the /patients POST handler.
	rows, err := store.ListPatients(ctx, tenant)
	if err != nil {
		logger.Printf("[care] hydrate patients from postgres failed: %v", err)
		return store
	}
	for _, p := range rows {
		patients.Upsert(p)
	}
	if len(rows) > 0 {
		logger.Printf("[care] hydrated %d patients from postgres", len(rows))
	}
	return store
}
