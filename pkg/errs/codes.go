// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/errs/codes.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.
//
// Bodhyanga modifications vs upstream:
//   - HITLPending is a Bodhyanga-specific category. When a handler stages a
//     clinically consequential action behind agents/human_review.go the
//     response is "202 + envelope with code=hitl_pending" rather than a
//     synchronous result — clients must poll /reviews/{id} for the
//     clinician verdict. See docs/governance.md.
//   - SafetyRefusal is a Bodhyanga-specific category. Returned when an LLM
//     refusal is detected (pkg/safety.IsRefusal) or when the rule-based
//     fallback explicitly declines to answer; surfaces as HTTP 422 so
//     clients can show a graceful degradation message rather than a 500.
//   - PolicyDenied surfaces a governance.Policy.Evaluate Deny verdict
//     to the API caller. HTTP 403 to mirror Forbidden semantics while
//     keeping the audit trail clean (the actual Deny event lives in the
//     audit log; this is just the wire representation).

package errs

import "net/http"

// Error categories. Values are immutable singletons; do not construct
// ErrCode literals at call sites — always use one of these vars so the
// httpStatus + codeNames maps stay authoritative.
var (
	// None indicates the operation was successful.
	None = ErrCode{value: 0}

	// NoContent indicates a successful no-body response.
	NoContent = ErrCode{value: 1}

	// Canceled indicates the operation was canceled (typically by the caller).
	Canceled = ErrCode{value: 2}

	// Unknown is the catch-all for errors that don't fit any other code.
	Unknown = ErrCode{value: 3}

	// InvalidArgument indicates the client specified an invalid argument.
	InvalidArgument = ErrCode{value: 4}

	// DeadlineExceeded means the operation expired before completion.
	DeadlineExceeded = ErrCode{value: 5}

	// NotFound means the requested entity was not found.
	NotFound = ErrCode{value: 6}

	// AlreadyExists means an attempt to create an entity failed because
	// one already exists.
	AlreadyExists = ErrCode{value: 7}

	// PermissionDenied indicates the caller lacks permission. Use
	// Unauthenticated for missing credentials.
	PermissionDenied = ErrCode{value: 8}

	// ResourceExhausted indicates a quota/rate-limit was hit.
	ResourceExhausted = ErrCode{value: 9}

	// FailedPrecondition indicates the operation was rejected because
	// the system is not in the required state.
	FailedPrecondition = ErrCode{value: 10}

	// Aborted indicates the operation was aborted (typically a concurrency
	// conflict).
	Aborted = ErrCode{value: 11}

	// OutOfRange means the operation was attempted past the valid range.
	OutOfRange = ErrCode{value: 12}

	// Unimplemented indicates the operation is not implemented.
	Unimplemented = ErrCode{value: 13}

	// Internal indicates an internal invariant was violated.
	Internal = ErrCode{value: 14}

	// Unavailable indicates the service is currently unavailable.
	Unavailable = ErrCode{value: 15}

	// DataLoss indicates unrecoverable data loss or corruption.
	DataLoss = ErrCode{value: 16}

	// Unauthenticated indicates the request lacks valid credentials.
	Unauthenticated = ErrCode{value: 17}

	// TooManyRequests indicates the client has exceeded its rate limit.
	TooManyRequests = ErrCode{value: 18}

	// InternalOnlyLog flags an internal error whose message must NOT be
	// echoed to the client. The Errors middleware translates this to a
	// generic Internal envelope before responding.
	InternalOnlyLog = ErrCode{value: 19}

	// HITLPending — Bodhyanga-specific. The handler has queued a clinically
	// consequential action onto pkg/hitl.Queue; the caller must poll
	// /reviews/{id} for the clinician verdict. HTTP 202 Accepted.
	HITLPending = ErrCode{value: 20}

	// SafetyRefusal — Bodhyanga-specific. The LLM (or its rule-based fallback)
	// refused to answer. HTTP 422 so clients can render a graceful
	// degradation message without a generic 500 alert. See pkg/safety.
	SafetyRefusal = ErrCode{value: 21}

	// PolicyDenied — Bodhyanga-specific. A pkg/governance Policy.Evaluate
	// Deny verdict surfaced at the API boundary. HTTP 403.
	PolicyDenied = ErrCode{value: 22}
)

// codeNumbers maps the lowercase-snake name back to the singleton. Used by
// ErrCode.UnmarshalText so JSON round-trips survive.
var codeNumbers = map[string]ErrCode{
	"ok":                  None,
	"no_content":          NoContent,
	"canceled":            Canceled,
	"unknown":             Unknown,
	"invalid_argument":    InvalidArgument,
	"deadline_exceeded":   DeadlineExceeded,
	"not_found":           NotFound,
	"already_exists":      AlreadyExists,
	"permission_denied":   PermissionDenied,
	"resource_exhausted":  ResourceExhausted,
	"failed_precondition": FailedPrecondition,
	"aborted":             Aborted,
	"out_of_range":        OutOfRange,
	"unimplemented":       Unimplemented,
	"internal":            Internal,
	"unavailable":         Unavailable,
	"data_loss":           DataLoss,
	"unauthenticated":     Unauthenticated,
	"too_many_requests":   TooManyRequests,
	"internal_only_log":   InternalOnlyLog,
	"hitl_pending":        HITLPending,
	"safety_refusal":      SafetyRefusal,
	"policy_denied":       PolicyDenied,
}

// codeNames is the inverse of codeNumbers. ErrCode.String / MarshalText
// read from this.
var codeNames = map[ErrCode]string{
	None:               "ok",
	NoContent:          "no_content",
	Canceled:           "canceled",
	Unknown:            "unknown",
	InvalidArgument:    "invalid_argument",
	DeadlineExceeded:   "deadline_exceeded",
	NotFound:           "not_found",
	AlreadyExists:      "already_exists",
	PermissionDenied:   "permission_denied",
	ResourceExhausted:  "resource_exhausted",
	FailedPrecondition: "failed_precondition",
	Aborted:            "aborted",
	OutOfRange:         "out_of_range",
	Unimplemented:      "unimplemented",
	Internal:           "internal",
	Unavailable:        "unavailable",
	DataLoss:           "data_loss",
	Unauthenticated:    "unauthenticated",
	TooManyRequests:    "too_many_requests",
	InternalOnlyLog:    "internal_only_log",
	HITLPending:        "hitl_pending",
	SafetyRefusal:      "safety_refusal",
	PolicyDenied:       "policy_denied",
}

// httpStatus maps each error code to its HTTP response status. The Errors
// middleware reads this when no per-handler status is supplied.
var httpStatus = map[ErrCode]int{
	None:               http.StatusOK,
	NoContent:          http.StatusNoContent,
	Canceled:           http.StatusGatewayTimeout,
	Unknown:            http.StatusInternalServerError,
	InvalidArgument:    http.StatusBadRequest,
	DeadlineExceeded:   http.StatusGatewayTimeout,
	NotFound:           http.StatusNotFound,
	AlreadyExists:      http.StatusConflict,
	PermissionDenied:   http.StatusForbidden,
	ResourceExhausted:  http.StatusTooManyRequests,
	FailedPrecondition: http.StatusBadRequest,
	Aborted:            http.StatusConflict,
	OutOfRange:         http.StatusBadRequest,
	Unimplemented:      http.StatusNotImplemented,
	Internal:           http.StatusInternalServerError,
	Unavailable:        http.StatusServiceUnavailable,
	DataLoss:           http.StatusInternalServerError,
	Unauthenticated:    http.StatusUnauthorized,
	TooManyRequests:    http.StatusTooManyRequests,
	InternalOnlyLog:    http.StatusInternalServerError,
	HITLPending:        http.StatusAccepted,
	SafetyRefusal:      http.StatusUnprocessableEntity,
	PolicyDenied:       http.StatusForbidden,
}
