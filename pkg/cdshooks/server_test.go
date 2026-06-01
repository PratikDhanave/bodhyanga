package cdshooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/cdshooks"
)

// newTestServer creates a Server with all built-in hooks registered.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := cdshooks.NewServer()
	for _, pair := range cdshooks.BuiltinServices() {
		srv.Register(pair.Service, pair.Handler)
	}
	return httptest.NewServer(srv.Handler())
}

// ─── Discovery ────────────────────────────────────────────────────────────────

func TestDiscovery_ListsAllServices(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/cds-services")
	if err != nil {
		t.Fatalf("GET /cds-services: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var discovery struct {
		Services []cdshooks.Service `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}

	if len(discovery.Services) != 4 {
		t.Errorf("expected 4 services, got %d", len(discovery.Services))
	}

	// Verify required fields are present on each service.
	for _, svc := range discovery.Services {
		if svc.ID == "" {
			t.Errorf("service has empty ID: %+v", svc)
		}
		if svc.Hook == "" {
			t.Errorf("service %s has empty Hook", svc.ID)
		}
		if svc.Title == "" {
			t.Errorf("service %s has empty Title", svc.ID)
		}
	}
}

func TestDiscovery_ContentType(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/cds-services")
	if err != nil {
		t.Fatalf("GET /cds-services: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

// ─── patient-view ─────────────────────────────────────────────────────────────

func TestPatientView_DefaultGaps(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookPatientView,
		HookInstance: "test-instance-1",
		Context:      map[string]any{"patientId": "patient-123"},
	}
	resp := postHook(t, ts.URL, "bodh-patient-view", req)

	if len(resp.Cards) == 0 {
		t.Fatal("expected at least one card for default gaps")
	}

	for _, card := range resp.Cards {
		if card.Indicator != "warning" {
			t.Errorf("patient-view cards should be 'warning', got %q", card.Indicator)
		}
		if len(card.Summary) > 140 {
			t.Errorf("card summary exceeds 140 chars: %d chars", len(card.Summary))
		}
		if card.Source.Label == "" {
			t.Error("card source label must not be empty")
		}
	}
}

func TestPatientView_ExplicitGaps(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookPatientView,
		HookInstance: "test-instance-2",
		Context: map[string]any{
			"patientId": "patient-456",
			"gaps":      []any{"Mammogram (overdue)", "Colonoscopy (overdue)"},
		},
	}
	resp := postHook(t, ts.URL, "bodh-patient-view", req)

	if len(resp.Cards) != 2 {
		t.Errorf("expected 2 cards for 2 explicit gaps, got %d", len(resp.Cards))
	}
}

func TestPatientView_EmptyGaps_NoCards(t *testing.T) {
	// When gaps field is present but empty, server returns default gaps.
	// Confirm at minimum one card is returned.
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookPatientView,
		HookInstance: "test-instance-3",
		Context:      map[string]any{"patientId": "patient-789"},
	}
	resp := postHook(t, ts.URL, "bodh-patient-view", req)

	if len(resp.Cards) == 0 {
		t.Error("expected default gap cards when no explicit gaps provided")
	}
}

// ─── medication-prescribe ─────────────────────────────────────────────────────

func TestMedPrescribe_HighDDI_ReturnsCritical(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookMedPrescribe,
		HookInstance: "test-ddi-1",
		Context: map[string]any{
			"patientId":   "patient-001",
			"medications": []any{"warfarin", "aspirin"},
		},
	}
	resp := postHook(t, ts.URL, "bodh-medication-prescribe", req)

	if len(resp.Cards) == 0 {
		t.Fatal("expected a critical DDI card for warfarin + aspirin")
	}
	if resp.Cards[0].Indicator != "critical" {
		t.Errorf("expected 'critical' indicator, got %q", resp.Cards[0].Indicator)
	}
}

func TestMedPrescribe_NoInteraction_NoCards(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookMedPrescribe,
		HookInstance: "test-ddi-2",
		Context: map[string]any{
			"patientId":   "patient-002",
			"medications": []any{"lisinopril", "atorvastatin"},
		},
	}
	resp := postHook(t, ts.URL, "bodh-medication-prescribe", req)

	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards for safe medication pair, got %d", len(resp.Cards))
	}
}

func TestMedPrescribe_EmptyMeds_NoCards(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookMedPrescribe,
		HookInstance: "test-ddi-3",
		Context:      map[string]any{"patientId": "patient-003"},
	}
	resp := postHook(t, ts.URL, "bodh-medication-prescribe", req)

	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards for no medications, got %d", len(resp.Cards))
	}
}

// ─── order-sign ───────────────────────────────────────────────────────────────

func TestOrderSign_OverBudget_ReturnsInfo(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookOrderSign,
		HookInstance: "test-order-1",
		Context: map[string]any{
			"patientId":         "patient-010",
			"estimatedCostUSD":  750.0,
			"budgetThresholdUSD": 500.0,
		},
	}
	resp := postHook(t, ts.URL, "bodh-order-sign", req)

	if len(resp.Cards) != 1 {
		t.Fatalf("expected 1 info card, got %d", len(resp.Cards))
	}
	if resp.Cards[0].Indicator != "info" {
		t.Errorf("expected 'info' indicator, got %q", resp.Cards[0].Indicator)
	}
}

func TestOrderSign_UnderBudget_NoCards(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookOrderSign,
		HookInstance: "test-order-2",
		Context: map[string]any{
			"patientId":        "patient-011",
			"estimatedCostUSD": 200.0,
		},
	}
	resp := postHook(t, ts.URL, "bodh-order-sign", req)

	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards under budget, got %d", len(resp.Cards))
	}
}

// ─── encounter-discharge ──────────────────────────────────────────────────────

func TestEncounterDischarge_ReturnsTCMCard(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := cdshooks.CDSRequest{
		Hook:         cdshooks.HookEncounterDischarge,
		HookInstance: "test-discharge-1",
		Context:      map[string]any{"patientId": "patient-020"},
	}
	resp := postHook(t, ts.URL, "bodh-encounter-discharge", req)

	if len(resp.Cards) != 1 {
		t.Fatalf("expected 1 TCM card, got %d", len(resp.Cards))
	}
	card := resp.Cards[0]
	if card.Indicator != "info" {
		t.Errorf("expected 'info' indicator, got %q", card.Indicator)
	}
	if len(card.Suggestions) == 0 {
		t.Error("expected at least one TCM suggestion")
	}
}

// ─── error handling ───────────────────────────────────────────────────────────

func TestInvalidBody_ReturnsBadRequest(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/cds-services/bodh-patient-view", "application/json", bytes.NewBufferString("{bad json}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

// ─── handler-level unit tests ─────────────────────────────────────────────────

func TestBuiltinServices_FourEntries(t *testing.T) {
	pairs := cdshooks.BuiltinServices()
	if len(pairs) != 4 {
		t.Errorf("expected 4 builtin services, got %d", len(pairs))
	}
	hooks := map[cdshooks.Hook]bool{}
	for _, p := range pairs {
		hooks[p.Service.Hook] = true
		if p.Handler == nil {
			t.Errorf("service %s has nil handler", p.Service.ID)
		}
	}
	expected := []cdshooks.Hook{
		cdshooks.HookPatientView,
		cdshooks.HookOrderSign,
		cdshooks.HookMedPrescribe,
		cdshooks.HookEncounterDischarge,
	}
	for _, h := range expected {
		if !hooks[h] {
			t.Errorf("missing builtin service for hook %q", h)
		}
	}
}

func TestPatientViewHandler_Direct(t *testing.T) {
	pairs := cdshooks.BuiltinServices()
	var h cdshooks.Handler
	for _, p := range pairs {
		if p.Service.Hook == cdshooks.HookPatientView {
			h = p.Handler
			break
		}
	}
	if h == nil {
		t.Fatal("patient-view handler not found")
	}

	resp, err := h(context.Background(), cdshooks.CDSRequest{
		Hook:    cdshooks.HookPatientView,
		Context: map[string]any{"patientId": "test-direct"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(resp.Cards) == 0 {
		t.Error("expected cards from direct handler call")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// postHook POSTs a CDSRequest to /cds-services/{id} and returns the parsed response.
func postHook(t *testing.T, baseURL, id string, req cdshooks.CDSRequest) cdshooks.CDSResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(baseURL+"/cds-services/"+id, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /cds-services/%s: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d for /cds-services/%s", resp.StatusCode, id)
	}

	var cdsResp cdshooks.CDSResponse
	if err := json.NewDecoder(resp.Body).Decode(&cdsResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return cdsResp
}
