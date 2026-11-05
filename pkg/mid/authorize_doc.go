// authorize.go — per-route OPA Rego authorization middleware.
//
// Adapted from app/sdk/mid/authorize.go in ardanlabs/service (Apache 2.0,
// Ardan Labs). Modifications for Bodhyanga:
//   - stdlib net/http (not web.MidFunc).
//   - No homebus / productbus / userbus dependencies.
//   - Per-route Rego module selection: AuthorizeExplain, AuthorizeReviews,
//     AuthorizeAudit pin the module + query so cmd/care's route table
//     stays declarative.
//
// All three Authorize* helpers are no-op-safe when Authenticator == nil
// (BODHYANGA_AUTH=disabled). On Rego deny they return HTTP 403; on a Rego
// engine failure (mis-deployed policy) they return 500.
package mid
