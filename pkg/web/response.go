// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/web/response.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - Respond accepts an Encoder; nil → 204 No Content; NoResponse → caller
//     already wrote the body (used by RawHandlerFunc).
//   - The httpStatus interface assertion is preserved so *errs.Error sets
//     the response status from its code without a per-handler switch.
//   - Respond does NOT call addSpan (Bodh tracing lives in pkg/observability
//     behind a build tag).

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// NoResponse signals that the handler already wrote the body. Used by
// RawHandlerFunc-style routes (file servers, SSE streams, HL7 ACK
// responses).
type NoResponse struct{}

// Encode satisfies the Encoder interface but writes nothing — Respond
// short-circuits on this type.
func (NoResponse) Encode() ([]byte, string, error) {
	return nil, "", nil
}

// JSON wraps any value as a JSON-encodable Encoder with a default 200 OK
// status. The most common return from a Bodh handler.
type JSON struct {
	Status int
	Value  any
}

// NewJSON is sugar for `JSON{Status: 200, Value: v}`.
func NewJSON(v any) JSON {
	return JSON{Status: http.StatusOK, Value: v}
}

// Encode marshals the JSON envelope.
func (j JSON) Encode() ([]byte, string, error) {
	data, err := json.Marshal(j.Value)
	if err != nil {
		return nil, "", fmt.Errorf("respond: json marshal: %w", err)
	}
	return data, "application/json", nil
}

// HTTPStatus lets Respond pick up the status without a separate switch.
func (j JSON) HTTPStatus() int {
	if j.Status == 0 {
		return http.StatusOK
	}
	return j.Status
}

// httpStatus is the optional interface an Encoder may implement to
// override the default response status. *errs.Error and JSON both
// satisfy it.
type httpStatus interface {
	HTTPStatus() int
}

// Respond writes resp to w. Handles three special cases before the
// generic path:
//
//  1. NoResponse → return immediately, caller already wrote the body.
//  2. ctx.Err() == Canceled → client disconnected, do not write.
//  3. resp == nil → 204 No Content.
//
// All other cases marshal resp.Encode(), set Content-Type + Status, write.
func Respond(ctx context.Context, w http.ResponseWriter, resp Encoder) error {
	if _, ok := resp.(NoResponse); ok {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return errors.New("client disconnected, skipping response write")
		}
	}

	status := http.StatusOK
	switch v := resp.(type) {
	case nil:
		w.WriteHeader(http.StatusNoContent)
		return nil
	case httpStatus:
		status = v.HTTPStatus()
	case error:
		status = http.StatusInternalServerError
	}

	data, contentType, err := resp.Encode()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return fmt.Errorf("respond: encode: %w", err)
	}
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("respond: write: %w", err)
	}
	return nil
}
