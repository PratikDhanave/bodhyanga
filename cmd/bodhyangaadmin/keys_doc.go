// keys.go — bodhyangaadmin "keys generate", "keys list", and "token issue"
// subcommand handlers.
//
// Adapted from upstream ardanlabs/service patterns for genkey + gentoken
// (Apache 2.0, copyright Ardan Labs). The bodhyangaadmin binary collapses
// those two upstream commands into a single binary with subcommands.
//
// Modifications for Bodhyanga:
//   - Token issue takes a repeatable -role flag (clinician, nurse,
//     auditor, …) instead of upstream's binary ADMIN / USER split.
//   - Subjects are opaque strings, not registered users.
//   - Self-verify round-trips every freshly minted token through Parse
//     so misconfiguration (e.g. issuer mismatch) fails at issue time.
package main
