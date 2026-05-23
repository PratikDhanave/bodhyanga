// registry_test.go — tests for InMemoryRegistry behaviour.
//
// Overview:
//   The tests cover registration, retrieval, listing, capability-based
//   discovery, the empty-result case, and the replace-on-duplicate-ID
//   semantics. A small stubAgent implementing agent.Agent stands in for
//   real agents in this package's tests.
//
// Key declarations:
//   - stubAgent (helper)
//   - TestRegistry_RegisterAndGet
//   - TestRegistry_GetReturnsErrForUnknownID
//   - TestRegistry_ListReturnsAllRegistered
//   - TestRegistry_FindByCapabilityReturnsMatches
//   - TestRegistry_FindByCapabilityEmpty
//   - TestRegistry_RegisterSameIDTwiceReplaces
//
// See doc.go for package-level documentation.

package registry
