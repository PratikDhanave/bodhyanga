// main.go — operator CLI for the cmd/care HTTP API.
//
// Overview:
//   Stdlib-only client for the care server. Supports listing and deciding
//   HITL reviews, listing the nurse task queue and notifications inbox,
//   triggering panel runs, and submitting case fixtures as cdm_request.
//
// Key declarations:
//   - main: dispatches subcommands (reviews, tasks, notifications, panel, case).
//   - reviewsCmd / decide / panelCmd / caseCmd: subcommand handlers.
//   - simpleGet / postJSON / prettyPrint: HTTP and output helpers.
//   - defaultAPI: fallback BODH_API_URL.
//
// See doc.go for package-level documentation.

package main
