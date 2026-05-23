// message_test.go — tests for Message construction, role constants, and JSON.
//
// Overview:
//   The tests verify NewMessage populates ID/CreatedAt, accepts empty
//   role/metadata gracefully, that the exported Role constants are
//   non-empty and unique, and that a Message survives a JSON round trip.
//
// Key declarations:
//   - TestNewMessage_PopulatesIDAndTimestamp
//   - TestNewMessage_DefaultsRoleAndMetadata
//   - TestRoleConstants
//   - TestMessageJSONRoundTrip
//
// See doc.go for package-level documentation.

package protocol
