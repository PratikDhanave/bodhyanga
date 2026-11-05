// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/web/request.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.
//
// Bodhyanga modifications vs upstream:
//   - Decode is generic: it uses encoding/json directly so callers do not
//     need to implement a Decoder interface. The upstream form requires a
//     Decode([]byte) method on every request body type; Bodhyanga's API surface
//     is mostly thin DTOs that the handler decodes once, so the interface
//     was unnecessary boilerplate.
//   - Validate hook preserved verbatim — if the decoded value implements
//     a `Validate() error` method, it runs after decode. This is the seam
//     where a handler can reject `case_id == ""` or `patient_id` mismatch
//     before any agent sees the message.

package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Param reads a stdlib net/http path parameter (Go 1.22+ pattern matching).
func Param(r *http.Request, key string) string {
	return r.PathValue(key)
}

// Validator is the optional hook for input validation. Decode calls
// Validate after a successful JSON unmarshal; a non-nil return is bubbled
// up to the caller, which typically maps it to errs.InvalidArgument.
type Validator interface {
	Validate() error
}

// Decode reads r.Body as JSON into v. Returns a non-nil error on read,
// unmarshal, or validation failure. The error is plain — handlers should
// wrap it via errs.New(errs.InvalidArgument, err) before returning.
func Decode(r *http.Request, v any) error {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	if vv, ok := v.(Validator); ok {
		if err := vv.Validate(); err != nil {
			return err
		}
	}
	return nil
}
