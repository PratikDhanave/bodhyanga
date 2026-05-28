// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/errs/errs.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - The Encode() method writes the Bodh error envelope shape
//     {code, message, fields, request_id} (see docs/api.md) rather than
//     the bare upstream {code, message}. Field-level errors live in a
//     separate "fields" array so clients can render form-validation hints.
//   - RequestID is exported as a JSON field and set by the Errors middleware
//     from the request-scoped trace ID. This is the request_id propagated in
//     docs/operations.md.
//   - The Error.Error() method intentionally returns ONLY the Message —
//     never wrapped errors — because callers may log it through the standard
//     observability.Logger seam (which is in turn PHI-redacted via
//     pkg/phi.LoggerWrapper). If the wrapped chain may contain PHI the
//     caller must NOT log err.Error() directly; use the error code instead.

package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
)

// ErrCode represents a typed error category. Values are kept as a struct
// (not a bare int) so the zero value is meaningful (None) and so the type
// cannot be accidentally cast from an unrelated int.
type ErrCode struct {
	value int
}

// Value returns the underlying numeric value of the error code. Exported for
// tests and for the metrics middleware that wants to label by code.
func (ec ErrCode) Value() int {
	return ec.value
}

// String returns the canonical lowercase-snake name of the code.
func (ec ErrCode) String() string {
	if name, ok := codeNames[ec]; ok {
		return name
	}
	return "unknown"
}

// MarshalText emits the lowercase-snake name so JSON encoders see a stable
// string identifier rather than an integer (which would be brittle across
// language clients).
func (ec ErrCode) MarshalText() ([]byte, error) {
	return []byte(ec.String()), nil
}

// UnmarshalText accepts the lowercase-snake name written by MarshalText.
func (ec *ErrCode) UnmarshalText(data []byte) error {
	name := string(data)
	v, ok := codeNumbers[name]
	if !ok {
		return fmt.Errorf("errs: unknown error code %q", name)
	}
	*ec = v
	return nil
}

// Equal reports whether two error codes represent the same category. Provided
// for go-cmp / testing parity with the upstream template.
func (ec ErrCode) Equal(other ErrCode) bool {
	return ec.value == other.value
}

// Error is the Bodh error envelope. It serialises as the {code, message,
// fields, request_id} shape documented in docs/api.md. The FuncName and
// FileName fields are internal — they help operators correlate a 500 to a
// call site without exposing the call site to API clients (which would
// leak internal package layout).
type Error struct {
	Code      ErrCode     `json:"code"`
	Message   string      `json:"message"`
	Fields    FieldErrors `json:"fields,omitempty"`
	RequestID string      `json:"request_id,omitempty"`

	// Internal fields. Captured at construction time so the Errors
	// middleware can log "which line did this 500 come from" without
	// requiring a full stack trace per error. NOT serialised to JSON.
	FuncName string `json:"-"`
	FileName string `json:"-"`
}

// New constructs an Error from a Go error value. The call site is captured
// via runtime.Caller so the Errors middleware can log the originating file
// + function. The wrapped error's message becomes the Error.Message
// verbatim — callers must NOT pass an error whose .Error() string contains
// PHI; if it might, use Errorf with a redacted format instead.
func New(code ErrCode, err error) *Error {
	pc, filename, line, _ := runtime.Caller(1)
	return &Error{
		Code:     code,
		Message:  err.Error(),
		FuncName: runtime.FuncForPC(pc).Name(),
		FileName: fmt.Sprintf("%s:%d", filename, line),
	}
}

// Errorf is the Sprintf-equivalent of New. Same call-site capture semantics.
// The caller is responsible for ensuring the formatted message is PHI-free.
func Errorf(code ErrCode, format string, v ...any) *Error {
	pc, filename, line, _ := runtime.Caller(1)
	return &Error{
		Code:     code,
		Message:  fmt.Sprintf(format, v...),
		FuncName: runtime.FuncForPC(pc).Name(),
		FileName: fmt.Sprintf("%s:%d", filename, line),
	}
}

// Error implements the standard library error interface. Returns the Message
// only — never the wrapped chain — so log lines stay bounded in length and
// so the call site (which is captured separately) cannot accidentally appear
// in client-visible logs.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Encode satisfies pkg/web.Encoder. Marshals as application/json.
func (e *Error) Encode() ([]byte, string, error) {
	data, err := json.Marshal(e)
	return data, "application/json", err
}

// HTTPStatus satisfies the pkg/web httpStatus interface so the web framework
// chooses the right status code from the error category alone (no per-handler
// switch statement).
func (e *Error) HTTPStatus() int {
	if status, ok := httpStatus[e.Code]; ok {
		return status
	}
	return 500
}

// Equal compares two Error values by code + message. Used by tests; deliberately
// ignores FuncName/FileName so callers can match on semantics, not call site.
func (e *Error) Equal(other *Error) bool {
	if e == nil || other == nil {
		return e == other
	}
	return e.Code.Equal(other.Code) && e.Message == other.Message
}

// WithRequestID returns a copy of the Error with the request_id field set.
// Used by the Errors middleware to stamp the trace ID onto every outbound
// envelope.
func (e *Error) WithRequestID(id string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.RequestID = id
	return &cp
}

// As lets the standard errors.As mechanism find a wrapped *Error.
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// FieldError records a single per-field validation failure.
type FieldError struct {
	Field string `json:"field"`
	Err   string `json:"error"`
}

// FieldErrors is a collection of per-field validation failures.
type FieldErrors []FieldError

// NewFieldErrors creates an InvalidArgument Error containing one field error.
func NewFieldErrors(field string, err error) *Error {
	fe := FieldErrors{{Field: field, Err: err.Error()}}
	return fe.ToError()
}

// Add appends a field error. The collection is mutated in place; the standard
// idiom is `var fe errs.FieldErrors; fe.Add(...)` before calling ToError.
func (fe *FieldErrors) Add(field string, err error) {
	*fe = append(*fe, FieldError{Field: field, Err: err.Error()})
}

// ToError wraps the collection in an Error with code InvalidArgument and a
// human-readable message; the per-field detail lives in the Fields slice.
func (fe FieldErrors) ToError() *Error {
	return &Error{
		Code:    InvalidArgument,
		Message: "validation failed",
		Fields:  fe,
	}
}

// Error implements the error interface so FieldErrors can be returned directly
// from a handler (the Errors middleware will then call ToError on it).
func (fe FieldErrors) Error() string {
	d, err := json.Marshal(fe)
	if err != nil {
		return err.Error()
	}
	return string(d)
}
