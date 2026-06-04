package errs

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestErrCode_Value(t *testing.T) {
	if NotFound.Value() != 6 {
		t.Fatalf("expected 6, got %d", NotFound.Value())
	}
}

func TestErrCode_String_Unknown(t *testing.T) {
	unknown := ErrCode{value: 9999}
	if unknown.String() != "unknown" {
		t.Fatalf("expected unknown, got %q", unknown.String())
	}
}

func TestErrCode_String_Known(t *testing.T) {
	if NotFound.String() != "not_found" {
		t.Fatalf("expected not_found, got %q", NotFound.String())
	}
}

func TestErrCode_MarshalUnmarshal_Coverage(t *testing.T) {
	data, err := NotFound.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not_found" {
		t.Fatalf("expected not_found, got %q", string(data))
	}

	var ec ErrCode
	if err := ec.UnmarshalText(data); err != nil {
		t.Fatal(err)
	}
	if !ec.Equal(NotFound) {
		t.Fatalf("expected NotFound, got %v", ec)
	}
}

func TestErrCode_UnmarshalUnknown_Coverage(t *testing.T) {
	var ec ErrCode
	err := ec.UnmarshalText([]byte("totally_bogus"))
	if err == nil {
		t.Fatal("expected error for unknown code")
	}
}

func TestErrCode_Equal(t *testing.T) {
	if !NotFound.Equal(NotFound) {
		t.Fatal("expected equal")
	}
	if NotFound.Equal(Internal) {
		t.Fatal("expected not equal")
	}
}

func TestError_Error_Nil(t *testing.T) {
	var e *Error
	if e.Error() != "" {
		t.Fatalf("expected empty string for nil error, got %q", e.Error())
	}
}

func TestError_Error_NonNil(t *testing.T) {
	e := New(NotFound, errors.New("item not found"))
	if e.Error() != "item not found" {
		t.Fatalf("expected 'item not found', got %q", e.Error())
	}
}

func TestError_Encode(t *testing.T) {
	e := New(NotFound, errors.New("missing"))
	data, ct, err := e.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["message"] != "missing" {
		t.Fatalf("expected 'missing', got %v", decoded["message"])
	}
}

func TestError_HTTPStatus_Known(t *testing.T) {
	e := New(NotFound, errors.New("x"))
	if e.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", e.HTTPStatus())
	}
}

func TestError_HTTPStatus_Unknown(t *testing.T) {
	e := &Error{Code: ErrCode{value: 99999}, Message: "x"}
	if e.HTTPStatus() != 500 {
		t.Fatalf("expected 500 for unknown code, got %d", e.HTTPStatus())
	}
}

func TestError_Equal(t *testing.T) {
	e1 := New(NotFound, errors.New("x"))
	e2 := New(NotFound, errors.New("x"))
	e3 := New(Internal, errors.New("x"))
	e4 := New(NotFound, errors.New("y"))

	if !e1.Equal(e2) {
		t.Fatal("expected equal")
	}
	if e1.Equal(e3) {
		t.Fatal("expected not equal (different code)")
	}
	if e1.Equal(e4) {
		t.Fatal("expected not equal (different message)")
	}
}

func TestError_Equal_NilCases(t *testing.T) {
	e := New(NotFound, errors.New("x"))
	var nilErr *Error

	if !nilErr.Equal(nilErr) {
		t.Fatal("expected two nils equal")
	}
	if e.Equal(nilErr) {
		t.Fatal("expected non-nil != nil")
	}
	if nilErr.Equal(e) {
		t.Fatal("expected nil != non-nil")
	}
}

func TestError_WithRequestID(t *testing.T) {
	e := New(NotFound, errors.New("x"))
	e2 := e.WithRequestID("req-123")
	if e2.RequestID != "req-123" {
		t.Fatalf("expected req-123, got %q", e2.RequestID)
	}
	// Original should be unchanged.
	if e.RequestID != "" {
		t.Fatal("original should not be mutated")
	}
}

func TestError_WithRequestID_Nil(t *testing.T) {
	var e *Error
	if e.WithRequestID("x") != nil {
		t.Fatal("expected nil for nil receiver")
	}
}

func TestAs_Found(t *testing.T) {
	e := New(NotFound, errors.New("not here"))
	wrapped := errors.New("wrapper: " + e.Error())
	// errors.As needs the error interface.
	_, ok := As(wrapped)
	if ok {
		t.Fatal("plain error should not match *Error")
	}

	// Now wrap properly.
	target, ok := As(e)
	if !ok {
		t.Fatal("expected As to find *Error")
	}
	if target.Code.Value() != NotFound.Value() {
		t.Fatal("wrong code")
	}
}

func TestAs_NotFound(t *testing.T) {
	_, ok := As(errors.New("plain"))
	if ok {
		t.Fatal("expected false for plain error")
	}
}

func TestNewFieldErrors(t *testing.T) {
	e := NewFieldErrors("email", errors.New("required"))
	if e.Code.Value() != InvalidArgument.Value() {
		t.Fatalf("expected InvalidArgument, got %v", e.Code)
	}
	if len(e.Fields) != 1 {
		t.Fatalf("expected 1 field error, got %d", len(e.Fields))
	}
	if e.Fields[0].Field != "email" || e.Fields[0].Err != "required" {
		t.Fatalf("unexpected field error: %+v", e.Fields[0])
	}
}

func TestFieldErrors_Error(t *testing.T) {
	var fe FieldErrors
	fe.Add("name", errors.New("too short"))
	fe.Add("age", errors.New("must be positive"))

	errStr := fe.Error()
	var parsed []FieldError
	if err := json.Unmarshal([]byte(errStr), &parsed); err != nil {
		t.Fatalf("expected JSON, got: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 field errors, got %d", len(parsed))
	}
}

func TestFieldErrors_ToError_Coverage(t *testing.T) {
	var fe FieldErrors
	fe.Add("x", errors.New("y"))
	e := fe.ToError()
	if e.Message != "validation failed" {
		t.Fatalf("expected 'validation failed', got %q", e.Message)
	}
}

func TestErrorf(t *testing.T) {
	e := Errorf(Internal, "something broke: %d", 42)
	if e.Message != "something broke: 42" {
		t.Fatalf("expected formatted message, got %q", e.Message)
	}
	if e.FuncName == "" {
		t.Fatal("expected FuncName to be set")
	}
}

func TestAllHTTPStatuses(t *testing.T) {
	// Verify all known codes have HTTP status mappings.
	codes := []ErrCode{
		None, NoContent, Canceled, Unknown, InvalidArgument,
		DeadlineExceeded, NotFound, AlreadyExists, PermissionDenied,
		ResourceExhausted, FailedPrecondition, Aborted, OutOfRange,
		Unimplemented, Internal, Unavailable, DataLoss, Unauthenticated,
		TooManyRequests, InternalOnlyLog, HITLPending, SafetyRefusal,
		PolicyDenied,
	}
	for _, code := range codes {
		e := &Error{Code: code, Message: "test"}
		status := e.HTTPStatus()
		if status == 0 {
			t.Fatalf("code %v has no HTTP status mapping", code)
		}
	}
}

func TestAllCodeNames(t *testing.T) {
	// Verify all known codes have string names.
	for code, name := range codeNames {
		reverse, ok := codeNumbers[name]
		if !ok {
			t.Fatalf("code name %q not in codeNumbers", name)
		}
		if !reverse.Equal(code) {
			t.Fatalf("round-trip mismatch for %q", name)
		}
	}
}
