// authen_test.go — HTTP middleware end-to-end tests.
//
// Each test runs a full Authenticate(authn) -> AuthorizeXxx(authn) ->
// inner handler chain against an httptest.ResponseRecorder and asserts
// the resulting status code matches the documented contract in
// docs/auth.md.
//
// Coverage:
//   - BODHYANGA_AUTH=disabled bypass (Authenticator == nil): 200 with no token.
//   - Missing token in jwt mode: 401.
//   - Expired token: 401.
//   - Clinician same-tenant on /explain: 200.
//   - Auditor cross-tenant on /explain: 200.
//   - Nurse on /explain: 403.
//   - Clinician wrong-tenant on /explain: 403.
//   - Nurse GET /reviews: 200; nurse POST decide: 403; clinician POST
//     decide: 200.
//   - Auditor /audit: 200; clinician /audit: 403.
package mid
