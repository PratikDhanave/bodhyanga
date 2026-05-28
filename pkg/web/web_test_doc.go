// web_test.go — table-driven tests for the Bodh HTTP framework.
//
// Overview:
//   Verifies handler registration (App.HandlerFunc → ServeHTTP path),
//   middleware composition (slice order on inbound, reverse on outbound,
//   per-handler middleware wrapping inside the default chain), and
//   Respond JSON encoding semantics (nil → 204, NoResponse → no write,
//   client-disconnect short-circuit, custom HTTPStatus override).
//
// See web_doc.go for package-level documentation.

package web
