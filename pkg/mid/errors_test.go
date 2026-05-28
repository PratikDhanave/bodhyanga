package mid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/errs"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/web"
)

func TestErrors_TranslatesTypedError(t *testing.T) {
	handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return nil, errs.Errorf(errs.NotFound, "patient missing")
	}
	var buf bytes.Buffer
	wrapped := Errors(observability.NewLoggerTo(&buf))(handler)
	req := httptest.NewRequest("GET", "/x", nil)
	ctx := web.SetRequestID(req.Context(), "trace-1")
	resp, err := wrapped(ctx, req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	appErr, ok := resp.(*errs.Error)
	if !ok {
		t.Fatalf("resp should be *errs.Error, got %T", resp)
	}
	if !appErr.Code.Equal(errs.NotFound) {
		t.Errorf("code: got %v", appErr.Code)
	}
	if appErr.RequestID != "trace-1" {
		t.Errorf("request id: got %q", appErr.RequestID)
	}
	data, _, _ := appErr.Encode()
	var env struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(data, &env)
	if env.Code != "not_found" || env.Message != "patient missing" || env.RequestID != "trace-1" {
		t.Errorf("envelope: %+v", env)
	}
}

func TestErrors_UntypedErrorBecomesGenericInternal(t *testing.T) {
	handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		// A bare error whose .Error() string contains PHI-shaped content.
		return nil, errors.New("patient SSN 123-45-6789 caused failure")
	}
	var buf bytes.Buffer
	wrapped := Errors(observability.NewLoggerTo(&buf))(handler)
	req := httptest.NewRequest("GET", "/x", nil)
	resp, _ := wrapped(req.Context(), req)
	appErr, ok := resp.(*errs.Error)
	if !ok {
		t.Fatalf("resp should be *errs.Error, got %T", resp)
	}
	if !appErr.Code.Equal(errs.Internal) {
		t.Errorf("code: got %v", appErr.Code)
	}
	if appErr.Message != "internal error" {
		t.Errorf("message: should be generic, got %q", appErr.Message)
	}
	// Critically: the wire-side envelope must NOT contain the SSN.
	data, _, _ := appErr.Encode()
	if strings.Contains(string(data), "123-45-6789") {
		t.Errorf("envelope leaked PHI: %s", data)
	}
	// And the LOG must not contain it raw either — redactor should fire.
	if strings.Contains(buf.String(), "123-45-6789") {
		t.Errorf("log leaked PHI: %s", buf.String())
	}
}

func TestErrors_InternalOnlyLogRedactsMessage(t *testing.T) {
	handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return nil, errs.Errorf(errs.InternalOnlyLog, "stacktrace with details %s", "abc")
	}
	var buf bytes.Buffer
	wrapped := Errors(observability.NewLoggerTo(&buf))(handler)
	req := httptest.NewRequest("GET", "/x", nil)
	resp, _ := wrapped(req.Context(), req)
	appErr := resp.(*errs.Error)
	if !appErr.Code.Equal(errs.Internal) {
		t.Errorf("code should be Internal after redaction, got %v", appErr.Code)
	}
	if appErr.Message != "internal error" {
		t.Errorf("message: should be generic, got %q", appErr.Message)
	}
}

func TestErrors_NilErrorPassesThrough(t *testing.T) {
	want := web.NewJSON(map[string]string{"a": "b"})
	handler := func(ctx context.Context, r *http.Request) (web.Encoder, error) {
		return want, nil
	}
	var buf bytes.Buffer
	wrapped := Errors(observability.NewLoggerTo(&buf))(handler)
	req := httptest.NewRequest("GET", "/x", nil)
	resp, err := wrapped(req.Context(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp == nil {
		t.Fatal("resp should not be nil")
	}
}
