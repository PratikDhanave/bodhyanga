package cdshooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- makeHookHandler — no handler for hook ----

func TestMakeHookHandler_NoHandler(t *testing.T) {
	s := NewServer()
	// Register a service but remove the handler.
	s.services[HookPatientView] = Service{ID: "test", Hook: HookPatientView}
	// Don't register the handler — s.handlers[HookPatientView] is missing.

	handler := s.makeHookHandler(HookPatientView)
	rr := httptest.NewRecorder()
	body := `{"hookInstance":"test","hook":"patient-view","context":{}}`
	req := httptest.NewRequest(http.MethodPost, "/cds-services/test", strings.NewReader(body))
	handler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// ---- makeHookHandler — handler error ----

func TestMakeHookHandler_HandlerError(t *testing.T) {
	s := NewServer()
	svc := Service{ID: "err-hook", Hook: HookPatientView}
	s.Register(svc, func(_ context.Context, _ CDSRequest) (CDSResponse, error) {
		return CDSResponse{}, fmt.Errorf("handler error")
	})

	rr := httptest.NewRecorder()
	body := `{"hookInstance":"test","hook":"patient-view","context":{}}`
	req := httptest.NewRequest(http.MethodPost, "/cds-services/err-hook", strings.NewReader(body))
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---- makeHookHandler — invalid body ----

func TestMakeHookHandler_InvalidBody(t *testing.T) {
	s := NewServer()
	svc := Service{ID: "test", Hook: HookPatientView}
	s.Register(svc, func(_ context.Context, _ CDSRequest) (CDSResponse, error) {
		return CDSResponse{}, nil
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cds-services/test", strings.NewReader("not json"))
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---- medPrescribeHandler — no medications ----

func TestMedPrescribeHandler_NoMeds(t *testing.T) {
	_, handler := findBuiltinHandler(HookMedPrescribe)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(resp.Cards))
	}
}

// ---- medPrescribeHandler — moderate severity (no card) ----

func TestMedPrescribeHandler_ModerateSeverity(t *testing.T) {
	_, handler := findBuiltinHandler(HookMedPrescribe)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"medications": []any{"clopidogrel", "omeprazole"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// clopidogrel+omeprazole is MODERATE, not HIGH.
	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards for MODERATE severity, got %d", len(resp.Cards))
	}
}

// ---- extractStringSlice branches ----

func TestExtractStringSlice_NotPresent(t *testing.T) {
	got := extractStringSlice(map[string]any{}, "missing")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractStringSlice_StringSlice(t *testing.T) {
	got := extractStringSlice(map[string]any{
		"items": []string{"a", "b"},
	}, "items")
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("expected [a b], got %v", got)
	}
}

func TestExtractStringSlice_AnySlice(t *testing.T) {
	got := extractStringSlice(map[string]any{
		"items": []any{"a", 42, "b"},
	}, "items")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected [a b], got %v", got)
	}
}

func TestExtractStringSlice_WrongType(t *testing.T) {
	got := extractStringSlice(map[string]any{
		"items": "not a slice",
	}, "items")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---- extractFloat branches ----

func TestExtractFloat_NotPresent(t *testing.T) {
	got := extractFloat(map[string]any{}, "missing")
	if got != 0 {
		t.Errorf("expected 0, got %f", got)
	}
}

func TestExtractFloat_Float64(t *testing.T) {
	got := extractFloat(map[string]any{"val": 3.14}, "val")
	if got != 3.14 {
		t.Errorf("expected 3.14, got %f", got)
	}
}

func TestExtractFloat_Int(t *testing.T) {
	got := extractFloat(map[string]any{"val": 42}, "val")
	if got != 42 {
		t.Errorf("expected 42, got %f", got)
	}
}

func TestExtractFloat_Int64(t *testing.T) {
	got := extractFloat(map[string]any{"val": int64(99)}, "val")
	if got != 99 {
		t.Errorf("expected 99, got %f", got)
	}
}

func TestExtractFloat_WrongType(t *testing.T) {
	got := extractFloat(map[string]any{"val": "not a number"}, "val")
	if got != 0 {
		t.Errorf("expected 0, got %f", got)
	}
}

// ---- sortedPair ----

func TestSortedPair_Normal(t *testing.T) {
	got := sortedPair("b", "a")
	if got != "a|b" {
		t.Errorf("sortedPair(b,a) = %q, want a|b", got)
	}
}

func TestSortedPair_AlreadySorted(t *testing.T) {
	got := sortedPair("a", "b")
	if got != "a|b" {
		t.Errorf("sortedPair(a,b) = %q, want a|b", got)
	}
}

// ---- truncate ----

func TestTruncate_Short(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate(hello,10) = %q, want hello", got)
	}
}

func TestTruncate_Long(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello" {
		t.Errorf("truncate(hello world,5) = %q, want hello", got)
	}
}

// ---- orderSignHandler — within budget ----

func TestOrderSignHandler_WithinBudget(t *testing.T) {
	_, handler := findBuiltinHandler(HookOrderSign)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"estimatedCostUSD": 200.0,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 0 {
		t.Errorf("expected 0 cards within budget, got %d", len(resp.Cards))
	}
}

// ---- orderSignHandler — over budget ----

func TestOrderSignHandler_OverBudget(t *testing.T) {
	_, handler := findBuiltinHandler(HookOrderSign)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"estimatedCostUSD":   800.0,
			"budgetThresholdUSD": 500.0,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 1 {
		t.Errorf("expected 1 card over budget, got %d", len(resp.Cards))
	}
}

// ---- encounterDischargeHandler — with and without patientId ----

func TestEncounterDischargeHandler_WithPatientID(t *testing.T) {
	_, handler := findBuiltinHandler(HookEncounterDischarge)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{"patientId": "PT-123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(resp.Cards))
	}
	if !strings.Contains(resp.Cards[0].Detail, "PT-123") {
		t.Errorf("expected patient ID in detail: %q", resp.Cards[0].Detail)
	}
}

func TestEncounterDischargeHandler_WithoutPatientID(t *testing.T) {
	_, handler := findBuiltinHandler(HookEncounterDischarge)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(resp.Cards))
	}
}

// ---- patientViewHandler — custom gaps ----

func TestPatientViewHandler_CustomGaps(t *testing.T) {
	_, handler := findBuiltinHandler(HookPatientView)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"gaps": []any{"Mammogram (overdue)", "Colonoscopy (overdue)"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 2 {
		t.Errorf("expected 2 cards, got %d", len(resp.Cards))
	}
}

// ---- medPrescribeHandler — high severity ----

func TestMedPrescribeHandler_HighSeverity(t *testing.T) {
	_, handler := findBuiltinHandler(HookMedPrescribe)
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"medications": []any{"aspirin", "warfarin"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 1 {
		t.Errorf("expected 1 card for HIGH severity, got %d", len(resp.Cards))
	}
	if resp.Cards[0].Indicator != "critical" {
		t.Errorf("indicator = %q, want critical", resp.Cards[0].Indicator)
	}
}

// ---- medPrescribeHandler — duplicate pair check ----

func TestMedPrescribeHandler_DuplicatePair(t *testing.T) {
	_, handler := findBuiltinHandler(HookMedPrescribe)
	// Same pair repeated — should only produce 1 card.
	resp, err := handler(context.Background(), CDSRequest{
		Context: map[string]any{
			"medications": []any{"aspirin", "warfarin", "metformin"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Cards) != 1 {
		t.Errorf("expected 1 card, got %d", len(resp.Cards))
	}
}

// ---- writeError ----

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, http.StatusBadRequest, "bad request")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["error"] != "bad request" {
		t.Errorf("error = %q, want 'bad request'", resp["error"])
	}
}

// helper to find a builtin handler by hook.
func findBuiltinHandler(hook Hook) (Service, Handler) {
	for _, sh := range BuiltinServices() {
		if sh.Service.Hook == hook {
			return sh.Service, sh.Handler
		}
	}
	panic("builtin handler not found for hook: " + string(hook))
}
