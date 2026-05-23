// refresh_test.go — tests for Orchestrator.Refresh and Orchestrator.Watch (hot-reload).
//
// Overview:
//   These tests pin down the hot-reload contract added to the orchestrator:
//   agents registered AFTER Start become message-routable once Refresh runs;
//   Refresh is idempotent and never double-subscribes; calling Refresh before
//   Start is a no-op; Refresh returns the IDs it just subscribed; the optional
//   Watch loop picks up newly-registered agents on its next tick; and Refresh
//   intentionally leaves existing subscriptions in place when agents disappear
//   from the registry.
//
// Key declarations:
//   - TestRefresh_SubscribesAgentsAddedAfterStart
//   - TestRefresh_DoesNotResubscribeExistingAgents
//   - TestRefresh_ReturnsNewlySubscribedIDs
//   - TestRefresh_BeforeStartIsNoOp
//   - TestWatch_PicksUpNewAgentsAfterInterval
//   - TestRefresh_IgnoresAgentsRemovedFromRegistry
//
// See doc.go for package-level documentation.

package orchestration
