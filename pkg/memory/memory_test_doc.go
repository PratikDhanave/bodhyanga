// memory_test.go — tests for InMemoryKV behaviour.
//
// Overview:
//   The tests cover Set/Get round trips, the lookup-miss return contract,
//   Delete behaviour (including no-op deletes), and concurrent Set safety.
//   The Keys() test is skipped because the type does not expose Keys.
//
// Key declarations:
//   - TestKV_PutGetRoundTrip
//   - TestKV_GetUnknownKeyReturnsErrOrZero
//   - TestKV_DeleteRemovesKey
//   - TestKV_KeysReturnsAllKeysSorted (skipped — no Keys method)
//   - TestKV_ConcurrentPutsAreSafe
//
// See doc.go for package-level documentation.

package memory
