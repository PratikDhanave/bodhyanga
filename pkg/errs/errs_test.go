package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestErrCode_MarshalUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		code ErrCode
		want string
	}{
		{"not_found", NotFound, "not_found"},
		{"invalid_argument", InvalidArgument, "invalid_argument"},
		{"hitl_pending", HITLPending, "hitl_pending"},
		{"safety_refusal", SafetyRefusal, "safety_refusal"},
		{"policy_denied", PolicyDenied, "policy_denied"},
		{"internal", Internal, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.code.MarshalText()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Errorf("marshal: got %q want %q", b, tc.want)
			}
			var got ErrCode
			if err := got.UnmarshalText([]byte(tc.want)); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !got.Equal(tc.code) {
				t.Errorf("roundtrip mismatch")
			}
		})
	}
}

func TestErrCode_UnmarshalUnknown(t *testing.T) {
	var c ErrCode
	if err := c.UnmarshalText([]byte("nope")); err == nil {
		t.Fatal("expected error for unknown code")
	}
}

func TestError_HTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		code ErrCode
		want int
	}{
		{"not_found", NotFound, http.StatusNotFound},
		{"invalid_argument", InvalidArgument, http.StatusBadRequest},
		{"internal", Internal, http.StatusInternalServerError},
		{"hitl_pending", HITLPending, http.StatusAccepted},
		{"safety_refusal", SafetyRefusal, http.StatusUnprocessableEntity},
		{"policy_denied", PolicyDenied, http.StatusForbidden},
		{"unauthenticated", Unauthenticated, http.StatusUnauthorized},
		{"too_many_requests", TooManyRequests, http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Errorf(tc.code, "x")
			if got := e.HTTPStatus(); got != tc.want {
				t.Errorf("status: got %d want %d", got, tc.want)
			}
		})
	}
}

func TestError_EncodeEnvelope(t *testing.T) {
	e := Errorf(InvalidArgument, "bad %s", "thing").WithRequestID("trace-abc")
	data, ct, err := e.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if ct != "application/json" {
		t.Errorf("content-type: got %q", ct)
	}
	var env struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Code != "invalid_argument" {
		t.Errorf("code: got %q", env.Code)
	}
	if env.Message != "bad thing" {
		t.Errorf("message: got %q", env.Message)
	}
	if env.RequestID != "trace-abc" {
		t.Errorf("request_id: got %q", env.RequestID)
	}
}

func TestError_ErrorReturnsMessageOnly(t *testing.T) {
	e := Errorf(Internal, "redacted-summary")
	if got := e.Error(); got != "redacted-summary" {
		t.Errorf("Error(): got %q", got)
	}
}

func TestError_AsExtractsWrappedError(t *testing.T) {
	e := New(NotFound, errors.New("missing"))
	wrapped := fmt.Errorf("layer: %w", e)
	got, ok := As(wrapped)
	if !ok || got == nil {
		t.Fatal("expected to extract *Error from wrapped chain")
	}
	if !got.Code.Equal(NotFound) {
		t.Errorf("code: got %v", got.Code)
	}
}

func TestNew_CaptureCallSite(t *testing.T) {
	e := New(Internal, errors.New("boom"))
	if !strings.Contains(e.FuncName, "TestNew_CaptureCallSite") {
		t.Errorf("FuncName should reference the calling test, got %q", e.FuncName)
	}
	if !strings.Contains(e.FileName, "errs_test.go") {
		t.Errorf("FileName should reference this file, got %q", e.FileName)
	}
}

func TestFieldErrors_ToError(t *testing.T) {
	var fe FieldErrors
	fe.Add("name", errors.New("required"))
	fe.Add("age", errors.New("out of range"))
	e := fe.ToError()
	if !e.Code.Equal(InvalidArgument) {
		t.Errorf("code: got %v", e.Code)
	}
	if len(e.Fields) != 2 {
		t.Fatalf("fields: got %d, want 2", len(e.Fields))
	}
	data, _, _ := e.Encode()
	if !strings.Contains(string(data), `"fields"`) {
		t.Errorf("envelope should contain fields array: %s", data)
	}
}

func TestError_EqualIgnoresCallSite(t *testing.T) {
	a := Errorf(NotFound, "x")
	b := Errorf(NotFound, "x")
	if !a.Equal(b) {
		t.Errorf("expected equal regardless of call site")
	}
}

func TestErrCode_StringForUnknown(t *testing.T) {
	// A zero-value ErrCode constructed outside the package shouldn't happen
	// in practice, but defensively String() returns a sentinel.
	var c ErrCode
	if c.String() != "ok" {
		t.Errorf("zero ErrCode should map to None, got %q", c.String())
	}
}
