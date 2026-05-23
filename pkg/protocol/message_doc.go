// message.go — the protocol Message type and role taxonomy.
//
// Overview:
//   Defines the unit of communication exchanged between agents, tools, and
//   observers across the platform. Roles classify the semantic origin of a
//   message; Type carries a short machine-readable label; Metadata is the
//   extensibility escape hatch for trace ids, tenant ids, scopes, and other
//   correlators.
//
// Key declarations:
//   - Message: addressed, typed, role-tagged payload with timestamps and metadata.
//   - MessageRole: enumerated source/content category.
//   - RoleUser, RoleSystem, RoleAgent, RoleTool, RoleObserver, RoleEvaluator: role constants.
//   - NewMessage: constructor with UUID id and UTC CreatedAt defaults.
//
// See doc.go for package-level documentation.

package protocol
