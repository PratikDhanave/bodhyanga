package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	return body
}

func TestLiveness_Always200(t *testing.T) {
	rec := httptest.NewRecorder()
	LivenessHandler()(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := decodeBody(t, rec); body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestReadiness_NoChecks_200(t *testing.T) {
	rec := httptest.NewRecorder()
	ReadinessHandler()(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("no-checks readiness = %d, want 200 (liveness-equivalent)", rec.Code)
	}
	if body := decodeBody(t, rec); body["status"] != "ready" {
		t.Errorf("status field = %q, want ready", body["status"])
	}
}

func TestReadiness_AllPass_200(t *testing.T) {
	ok := ReadyCheck{Name: "bus", Check: func(context.Context) error { return nil }}
	pg := ReadyCheck{Name: "postgres", Check: func(context.Context) error { return nil }}

	rec := httptest.NewRecorder()
	ReadinessHandler(ok, pg)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("all-pass readiness = %d, want 200", rec.Code)
	}
}

func TestReadiness_FirstFail_503_ShortCircuits(t *testing.T) {
	secondRan := false
	failing := ReadyCheck{Name: "bus", Check: func(context.Context) error { return errors.New("nats down") }}
	second := ReadyCheck{Name: "postgres", Check: func(context.Context) error {
		secondRan = true
		return nil
	}}

	rec := httptest.NewRecorder()
	ReadinessHandler(failing, second)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failing readiness = %d, want 503", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["status"] != "unavailable" {
		t.Errorf("status field = %q, want unavailable", body["status"])
	}
	if body["reason"] != "bus check failed" {
		t.Errorf("reason = %q, want it to name the failed check", body["reason"])
	}
	if secondRan {
		t.Error("readiness must short-circuit on the first failing check")
	}
}

// A failing check's error string must not leak into the response body verbatim
// (it could carry PHI). Only the check NAME is surfaced.
func TestReadiness_FailureReasonUsesNameNotError(t *testing.T) {
	leaky := ReadyCheck{Name: "store", Check: func(context.Context) error {
		return errors.New("dsn=postgres://user:secret@host/db patient=" + phiMarker)
	}}

	rec := httptest.NewRecorder()
	ReadinessHandler(leaky)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if strings.Contains(rec.Body.String(), phiMarker) || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("readiness body leaked the check's error text: %s", rec.Body.String())
	}
}

// Each check runs under a bounded context so a wedged dependency cannot hang
// the probe past the kubelet's own deadline.
func TestReadiness_CheckReceivesDeadline(t *testing.T) {
	var hadDeadline bool
	probe := ReadyCheck{Name: "x", Check: func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		return nil
	}}

	rec := httptest.NewRecorder()
	ReadinessHandler(probe)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if !hadDeadline {
		t.Error("ReadyCheck must run under a context with a deadline")
	}
}
