// bus_test.go — tests for InMemoryBus pub/sub semantics.
//
// Overview:
//   The tests cover targeted delivery, broadcast subscribers, isolation of
//   targeted subscribers from broadcasts, multi-subscriber fan-out,
//   unsubscribe behaviour, asynchronous publish guarantees, and context
//   cancellation semantics.
//
// Key declarations:
//   - TestInMemoryBus_PublishDeliversToTargetedSubscriber
//   - TestInMemoryBus_BroadcastDeliversToEmptySubscription
//   - TestInMemoryBus_TargetedSubscriberDoesNotReceiveBroadcasts
//   - TestInMemoryBus_MultipleSubscribersForSameID_AllReceive
//   - TestInMemoryBus_UnsubscribeStopsDelivery
//   - TestInMemoryBus_PublishIsAsync
//   - TestInMemoryBus_HandlerPanicDoesNotKillBus (skipped — bus lacks recovery)
//   - TestInMemoryBus_PublishWithContextDoneSkipsDelivery
//
// See doc.go for package-level documentation.

package comm
