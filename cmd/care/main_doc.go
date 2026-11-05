// main.go — HTTP API exposing the bodhyanga care-services agents over REST.
//
// Overview:
//   Boots the full agent roster (with HITL on for clinically consequential
//   actions), wires read endpoints for patients, tasks, notifications, and
//   reviews, and write endpoints that publish onto the bus (CDM, TCM, panel,
//   pre-visit, inbox, refill, prior-auth, ADT, FHIR import, RPM observations).
//
// Key declarations:
//   - main: builds the orchestrator and serves the HTTP API.
//   - forward / forwardBroadcast: handlers that publish to the bus.
//   - readBody / metaFromBody / writeJSON / appendUnique: I/O helpers.
//
// See doc.go for package-level documentation.

package main
