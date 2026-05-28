// errs_test.go — table-driven tests for the typed error envelope.
//
// Overview:
//   Verifies the ErrCode JSON round-trip (including the Bodh-specific
//   hitl_pending / safety_refusal / policy_denied codes), the HTTP status
//   mapping for each code (the wire contract documented in docs/api.md),
//   the envelope shape {code, message, fields, request_id}, the
//   Error.Error() = Message-only invariant (keeps log lines bounded and
//   PHI-free when handlers wrap pkg/medical errors that may contain
//   narrative), errs.As extraction through fmt.Errorf chains, and call-site
//   capture (FuncName / FileName).
//
// See errs_doc.go for package-level documentation.

package errs
