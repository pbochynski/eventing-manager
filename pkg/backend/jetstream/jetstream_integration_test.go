package jetstream

import (
	"fmt"
	"net"
	"testing"
	"time"

	kymalogger "github.com/kyma-project/kyma/common/logging/logger"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	eventingv1alpha2 "github.com/kyma-project/eventing-manager/api/eventing/v1alpha2"
	"github.com/kyma-project/eventing-manager/pkg/backend/cleaner"
	"github.com/kyma-project/eventing-manager/pkg/backend/metrics"
	"github.com/kyma-project/eventing-manager/pkg/ems/api/events/types"
	"github.com/kyma-project/eventing-manager/pkg/env"
	"github.com/kyma-project/eventing-manager/pkg/logger"
	eventingtesting "github.com/kyma-project/eventing-manager/testing"
)

// TestJetStreamSubAfterSync_SinkChange tests the SyncSubscription method
// when only the sink is changed in subscription, then it should not re-create
// NATS subjects on nats-server.
func TestJetStreamSubAfterSync_SinkChange(t *testing.T) {
	// given
	testEnvironment := setupTestEnvironment(t)
	jsBackend := testEnvironment.jsBackend
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	initErr := jsBackend.Initialize(nil)
	require.NoError(t, initErr)

	// create New Subscribers
	subscriber1 := eventingtesting.NewSubscriber()
	defer subscriber1.Shutdown()
	require.True(t, subscriber1.IsRunning())
	subscriber2 := eventingtesting.NewSubscriber()
	defer subscriber2.Shutdown()
	require.True(t, subscriber2.IsRunning())

	// create a new Subscription
	sub := eventingtesting.NewSubscription("sub", "foo",
		eventingtesting.WithNotCleanEventSourceAndType(),
		eventingtesting.WithSinkURL(subscriber1.SinkURL),
		eventingtesting.WithTypeMatchingStandard(),
		eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
	)
	AddJSCleanEventTypesToStatus(sub, testEnvironment.cleaner)

	// when
	err := jsBackend.SyncSubscription(sub)

	// then
	require.NoError(t, err)

	// get cleaned subject
	subject, err := testEnvironment.cleaner.CleanEventType(sub.Spec.Types[0])
	require.NoError(t, err)
	require.NotEmpty(t, subject)

	// test if subscription is working properly by sending an event
	// and checking if it is received by the subscriber
	require.NoError(t,
		SendCloudEventToJetStream(jsBackend,
			jsBackend.GetJetStreamSubject(sub.Spec.Source, subject, sub.Spec.TypeMatching),
			eventingtesting.CloudEventData,
			types.ContentModeBinary),
	)
	require.NoError(t, subscriber1.CheckEvent(eventingtesting.CloudEventData))

	// set metadata on NATS subscriptions
	msgLimit, bytesLimit := 2048, 2048
	require.Len(t, jsBackend.subscriptions, 1)
	for _, jsSub := range jsBackend.subscriptions {
		require.True(t, jsSub.IsValid())
		require.NoError(t, jsSub.SetPendingLimits(msgLimit, bytesLimit))
	}

	// given
	// NATS subscription should not be re-created in sync when sink is changed.
	// change the sink
	sub.Spec.Sink = subscriber2.SinkURL

	// when
	err = jsBackend.SyncSubscription(sub)

	// then
	require.NoError(t, err)

	// check if the NATS subscription are the same (have same metadata)
	// by comparing the metadata of nats subscription
	require.Len(t, jsBackend.subscriptions, 1)
	jsSubject := jsBackend.GetJetStreamSubject(sub.Spec.Source, subject, sub.Spec.TypeMatching)
	jsSubKey := NewSubscriptionSubjectIdentifier(sub, jsSubject)
	jsSub := jsBackend.subscriptions[jsSubKey]
	require.NotNil(t, jsSub)
	require.True(t, jsSub.IsValid())

	// check the metadata, if they are now same then it means that NATS subscription
	// were not re-created by SyncSubscription method
	subMsgLimit, subBytesLimit, err := jsSub.PendingLimits()
	require.NoError(t, err)
	require.Equal(t, subMsgLimit, msgLimit)
	require.Equal(t, subBytesLimit, bytesLimit)

	// Test if the subscription is working for new sink only
	require.NoError(t,
		SendCloudEventToJetStream(jsBackend,
			jsBackend.GetJetStreamSubject(sub.Spec.Source, subject, sub.Spec.TypeMatching),
			eventingtesting.CloudEventData,
			types.ContentModeBinary),
	)

	// Old sink should not have received the event, the new sink should have
	require.Error(t, subscriber1.CheckEvent(eventingtesting.CloudEventData))
	require.NoError(t, subscriber2.CheckEvent(eventingtesting.CloudEventData))
}

// TestMultipleJSSubscriptionsToSameEvent tests the behaviour of JS
// when multiple subscriptions need to receive the same event.
func TestMultipleJSSubscriptionsToSameEvent(t *testing.T) {
	// given
	testEnvironment := setupTestEnvironment(t)
	jsBackend := testEnvironment.jsBackend
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	initErr := jsBackend.Initialize(nil)
	require.NoError(t, initErr)

	subscriber := eventingtesting.NewSubscriber()
	defer subscriber.Shutdown()
	require.True(t, subscriber.IsRunning())

	// Create 3 subscriptions having the same sink and the same event type
	var subs [3]*eventingv1alpha2.Subscription
	for i := 0; i < len(subs); i++ {
		subs[i] = eventingtesting.NewSubscription(fmt.Sprintf("sub-%d", i), "foo",
			eventingtesting.WithSourceAndType(eventingtesting.EventSource, eventingtesting.OrderCreatedEventType),
			eventingtesting.WithSinkURL(subscriber.SinkURL),
			eventingtesting.WithTypeMatchingStandard(),
			eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
		)
		AddJSCleanEventTypesToStatus(subs[i], testEnvironment.cleaner)
		// when
		err := jsBackend.SyncSubscription(subs[i])
		// then
		require.NoError(t, err)
	}

	// Send only one event. It should be multiplexed to 3 by NATS, cause 3 subscriptions exist
	require.NoError(t,
		SendCloudEventToJetStream(jsBackend,
			jsBackend.GetJetStreamSubject(eventingtesting.EventSource,
				eventingtesting.OrderCreatedEventType,
				eventingv1alpha2.TypeMatchingStandard),
			eventingtesting.CloudEventData,
			types.ContentModeBinary),
	)
	// Check for the 3 events that should be received by the subscriber
	for i := 0; i < len(subs); i++ {
		require.NoError(t, subscriber.CheckEvent(eventingtesting.CloudEventData))
	}
	// Delete all 3 subscription
	for i := 0; i < len(subs); i++ {
		require.NoError(t, jsBackend.DeleteSubscription(subs[i]))
	}
	// Check if all subscriptions are deleted in NATS
	// Send an event again which should not be delivered to subscriber
	require.NoError(t,
		SendCloudEventToJetStream(jsBackend,
			jsBackend.GetJetStreamSubject(eventingtesting.EventSource,
				eventingtesting.OrderCreatedEventType, eventingv1alpha2.TypeMatchingStandard),
			eventingtesting.CloudEventData2,
			types.ContentModeBinary),
	)
	// Check for the event that did not reach the subscriber
	// Store should never return evtesting.CloudEventData2
	// hence CheckEvent should fail to match evtesting.CloudEventData2
	require.Error(t, subscriber.CheckEvent(eventingtesting.CloudEventData2))
}

// TestJSSubscriptionRedeliverWithFailedDispatch tests the redelivering
// of event when the dispatch fails.
func TestJSSubscriptionRedeliverWithFailedDispatch(t *testing.T) {
	// given
	testEnvironment := setupTestEnvironment(t)
	jsBackend := testEnvironment.jsBackend
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	initErr := jsBackend.Initialize(nil)
	require.NoError(t, initErr)

	// create New Subscriber
	subscriber := eventingtesting.NewSubscriber()
	subscriber.Shutdown() // shutdown the subscriber intentionally
	require.False(t, subscriber.IsRunning())

	// create a new Subscription
	sub := eventingtesting.NewSubscription("sub", "foo",
		eventingtesting.WithSourceAndType(eventingtesting.EventSource, eventingtesting.OrderCreatedCleanEvent),
		eventingtesting.WithSinkURL(subscriber.SinkURL),
		eventingtesting.WithTypeMatchingExact(),
		eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
	)
	AddJSCleanEventTypesToStatus(sub, testEnvironment.cleaner)

	// when
	err := jsBackend.SyncSubscription(sub)

	// then
	require.NoError(t, err)

	// when
	// send an event

	require.NoError(t,
		SendCloudEventToJetStream(jsBackend,
			jsBackend.GetJetStreamSubject(eventingtesting.EventSource,
				eventingtesting.OrderCreatedCleanEvent,
				eventingv1alpha2.TypeMatchingExact),
			eventingtesting.CloudEventData,
			types.ContentModeBinary),
	)

	// then
	// it should have failed to dispatch
	require.Error(t, subscriber.CheckEvent(eventingtesting.CloudEventData))

	// when
	// start a new subscriber
	subscriber = eventingtesting.NewSubscriber()
	defer subscriber.Shutdown()
	require.True(t, subscriber.IsRunning())
	// and update sink in the subscription
	sub.Spec.Sink = subscriber.SinkURL
	require.NoError(t, jsBackend.SyncSubscription(sub))

	// then
	// the same event should be redelivered
	require.Eventually(t, func() bool {
		return subscriber.CheckEvent(eventingtesting.CloudEventData) == nil
	}, 60*time.Second, 5*time.Second)
}

// TestJetStreamSubAfterSync_DeleteOldFilterConsumerForFilterChangeWhileNatsDown tests the SyncSubscription method
// when subscription CR filters change while NATS JetStream is down.
func TestJetStreamSubAfterSync_DeleteOldFilterConsumerForTypeChangeWhileNatsDown(t *testing.T) {
	// given
	// prepare JS file storage test environment
	testEnv := prepareTestEnvironment(t)
	defer cleanUpTestEnvironment(testEnv)
	// create a subscriber
	subscriber := eventingtesting.NewSubscriber()
	require.True(t, subscriber.IsRunning())
	defer subscriber.Shutdown()
	// create subscription and make sure it is functioning
	secondSubKey, sub := createSubscriptionAndAssert(t, testEnv, subscriber)

	// when
	// shutdown the JetStream
	shutdownJetStream(t, testEnv)
	// Now, remove the second filter from subscription while NATS JetStream is down
	deleteSecondFilter(testEnv, sub)
	err := testEnv.jsBackend.SyncSubscription(sub)
	require.Error(t, err)
	// restart the NATS server and sync subscription
	startJetStream(t, testEnv)
	err = testEnv.jsBackend.SyncSubscription(sub)
	require.NoError(t, err)

	// then
	// get new cleaned subject
	firstSubKey := assertNewSubscriptionReturnItsKey(t, testEnv, sub)

	// then
	// make sure first filter does have JetStream consumer
	firstCon, err := testEnv.jsBackend.jsCtx.ConsumerInfo(testEnv.jsBackend.Config.JSStreamName,
		firstSubKey.consumerName)
	require.NotNil(t, firstCon)
	require.NoError(t, err)
	// make sure second filter doesn't have any JetStream consumer
	secondCon, err := testEnv.jsBackend.jsCtx.ConsumerInfo(testEnv.jsBackend.Config.JSStreamName,
		secondSubKey.consumerName)
	require.Nil(t, secondCon)
	require.ErrorIs(t, err, nats.ErrConsumerNotFound)
}

// HELPER functions

func prepareTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()
	testEnvironment := setupTestEnvironment(t)
	testEnvironment.jsBackend.Config.JSStreamStorageType = StorageTypeFile
	testEnvironment.jsBackend.Config.MaxReconnects = 0
	initErr := testEnvironment.jsBackend.Initialize(nil)
	require.NoError(t, initErr)
	return testEnvironment
}

func createSubscriptionAndAssert(t *testing.T, testEnv *TestEnvironment, subscriber *eventingtesting.Subscriber) (SubscriptionSubjectIdentifier, *eventingv1alpha2.Subscription) {
	t.Helper()
	sub := eventingtesting.NewSubscription("sub", "foo",
		eventingtesting.WithCleanEventSourceAndType(),
		eventingtesting.WithNotCleanEventSourceAndType(),
		eventingtesting.WithSinkURL(subscriber.SinkURL),
		eventingtesting.WithTypeMatchingStandard(),
		eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
	)
	AddJSCleanEventTypesToStatus(sub, testEnv.cleaner)

	err := testEnv.jsBackend.SyncSubscription(sub)
	require.NoError(t, err)

	// get cleaned subject
	subject, err := testEnv.cleaner.CleanEventType(sub.Spec.Types[1])
	require.NoError(t, err)
	require.NotEmpty(t, subject)
	require.Len(t, testEnv.jsBackend.subscriptions, 2)
	// store first subscription key
	subKey := NewSubscriptionSubjectIdentifier(sub,
		testEnv.jsBackend.GetJetStreamSubject(sub.Spec.Source, subject, sub.Spec.TypeMatching))
	return subKey, sub
}

func shutdownJetStream(t *testing.T, testEnv *TestEnvironment) {
	t.Helper()
	testEnv.natsServer.Shutdown()
	require.Eventually(t, func() bool {
		return !testEnv.jsBackend.Conn.IsConnected()
	}, 30*time.Second, 2*time.Second)
}

func deleteSecondFilter(testEnv *TestEnvironment, sub *eventingv1alpha2.Subscription) {
	sub.Spec.Types = sub.Spec.Types[:1]
	AddJSCleanEventTypesToStatus(sub, testEnv.cleaner)
}

func startJetStream(t *testing.T, testEnv *TestEnvironment) {
	t.Helper()
	_ = eventingtesting.RunNatsServerOnPort(
		eventingtesting.WithPort(testEnv.natsPort),
		eventingtesting.WithJetStreamEnabled())
	require.Eventually(t, func() bool {
		info, streamErr := testEnv.jsClient.StreamInfo(testEnv.natsConfig.JSStreamName)
		require.NoError(t, streamErr)
		return info != nil && streamErr == nil
	}, 60*time.Second, 5*time.Second)
}

func assertNewSubscriptionReturnItsKey(t *testing.T, testEnv *TestEnvironment, sub *eventingv1alpha2.Subscription) SubscriptionSubjectIdentifier {
	t.Helper()
	firstSubject, err := testEnv.cleaner.CleanEventType(sub.Spec.Types[0])
	require.NoError(t, err)
	require.NotEmpty(t, firstSubject)
	// now, there has to be only one subscription
	require.Len(t, testEnv.jsBackend.subscriptions, 1)
	firstJsSubKey := NewSubscriptionSubjectIdentifier(sub, testEnv.jsBackend.GetJetStreamSubject(sub.Spec.Source,
		firstSubject,
		sub.Spec.TypeMatching))
	firstJsSub := testEnv.jsBackend.subscriptions[firstJsSubKey]
	require.NotNil(t, firstJsSub)
	require.True(t, firstJsSub.IsValid())
	return firstJsSubKey
}

func cleanUpTestEnvironment(testEnv *TestEnvironment) {
	defer testEnv.natsServer.Shutdown()
	defer testEnv.jsClient.natsConn.Close()
	defer func() { _ = testEnv.jsClient.DeleteStream(testEnv.natsConfig.JSStreamName) }()
}

// TestJetStream_NoNATSSubscription tests if the error is being triggered
// when expected entries in js.subscriptions map are missing.
func TestJetStream_NATSSubscriptionCount(t *testing.T) {
	// given
	testEnvironment := setupTestEnvironment(t)
	jsBackend := testEnvironment.jsBackend
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	initErr := jsBackend.Initialize(nil)
	require.NoError(t, initErr)

	// create New Subscriber
	subscriber := eventingtesting.NewSubscriber()
	defer subscriber.Shutdown()
	require.True(t, subscriber.IsRunning())

	testCases := []struct {
		name                            string
		subOpts                         []eventingtesting.SubscriptionOpt
		givenManuallyDeleteSubscription bool
		givenFilterToDelete             string
		wantNatsSubsLen                 int
		wantErr                         error
	}{
		{
			name: "No error should happen, when there is only one type",
			subOpts: []eventingtesting.SubscriptionOpt{
				eventingtesting.WithSinkURL(subscriber.SinkURL),
				eventingtesting.WithNotCleanEventSourceAndType(),
				eventingtesting.WithTypeMatchingStandard(),
				eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
			},
			givenManuallyDeleteSubscription: false,
			wantNatsSubsLen:                 1,
			wantErr:                         nil,
		},
		{
			name: "No error expected when js.subscriptions map has entries for all the eventTypes",
			subOpts: []eventingtesting.SubscriptionOpt{
				eventingtesting.WithNotCleanEventSourceAndType(),
				eventingtesting.WithCleanEventTypeOld(),
				eventingtesting.WithTypeMatchingStandard(),
				eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
			},
			givenManuallyDeleteSubscription: false,
			wantNatsSubsLen:                 2,
			wantErr:                         nil,
		},
		{
			name: "An error is expected, when we manually delete a subscription from js.subscriptions map",
			subOpts: []eventingtesting.SubscriptionOpt{
				eventingtesting.WithNotCleanEventSourceAndType(),
				eventingtesting.WithCleanEventTypeOld(),
				eventingtesting.WithTypeMatchingStandard(),
				eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
			},
			givenManuallyDeleteSubscription: true,
			givenFilterToDelete:             eventingtesting.OrderCreatedEventType,
			wantNatsSubsLen:                 2,
			wantErr:                         ErrMissingSubscription,
		},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// create a new subscription with no filters
			sub := eventingtesting.NewSubscription(fmt.Sprintf("sub%v", i), "foo",
				tc.subOpts...,
			)
			AddJSCleanEventTypesToStatus(sub, testEnvironment.cleaner)

			// when
			err := jsBackend.SyncSubscription(sub)
			require.NoError(t, err)
			require.Len(t, jsBackend.subscriptions, tc.wantNatsSubsLen)

			if tc.givenManuallyDeleteSubscription {
				// manually delete the subscription from map
				jsSubject := jsBackend.GetJetStreamSubject(sub.Spec.Source, tc.givenFilterToDelete, sub.Spec.TypeMatching)
				jsSubKey := NewSubscriptionSubjectIdentifier(sub, jsSubject)
				delete(jsBackend.subscriptions, jsSubKey)
			}

			err = jsBackend.SyncSubscription(sub)
			testEnvironment.logger.WithContext().Error(err)

			if tc.wantErr != nil {
				// the createConsumer function won't create a new Subscription,
				// because the subscription was manually deleted from the js.subscriptions map
				// hence the consumer will be shown in the NATS Backend as still bound
				err = jsBackend.SyncSubscription(sub)
				require.ErrorIs(t, err, tc.wantErr)
			}

			// empty the js.subscriptions map
			require.NoError(t, jsBackend.DeleteSubscription(sub))
		})
	}
}

// TestJetStream_ServerRestart tests that eventing works when NATS server is restarted
// for scenarios involving the stream storage type and when reconnect attempts are exhausted or not.
func TestJetStream_ServerRestart(t *testing.T) {
	// given
	subscriber := eventingtesting.NewSubscriber()
	defer subscriber.Shutdown()
	require.True(t, subscriber.IsRunning())

	testCases := []struct {
		name               string
		givenMaxReconnects int
		givenStorageType   string
	}{
		{
			name:               "with reconnects disabled and memory storage for streams",
			givenMaxReconnects: 0,
			givenStorageType:   StorageTypeMemory,
		},
		{
			name:               "with reconnects enabled and memory storage for streams",
			givenMaxReconnects: DefaultMaxReconnects,
			givenStorageType:   StorageTypeMemory,
		},
		{
			name:               "with reconnects disabled and file storage for streams",
			givenMaxReconnects: 0,
			givenStorageType:   StorageTypeFile,
		},
		{
			name:               "with reconnects enabled and file storage for streams",
			givenMaxReconnects: DefaultMaxReconnects,
			givenStorageType:   StorageTypeFile,
		},
	}

	for id, tc := range testCases {
		tc, id := tc, id
		t.Run(tc.name, func(t *testing.T) {
			// given
			testEnvironment := setupTestEnvironment(t)
			defer testEnvironment.natsServer.Shutdown()
			defer testEnvironment.jsClient.natsConn.Close()
			defer func() { _ = testEnvironment.jsClient.DeleteStream(testEnvironment.natsConfig.JSStreamName) }()
			var err error
			testEnvironment.jsBackend.Config.JSStreamStorageType = tc.givenStorageType
			testEnvironment.jsBackend.Config.MaxReconnects = tc.givenMaxReconnects
			err = testEnvironment.jsBackend.Initialize(nil)
			require.NoError(t, err)

			// Create a subscription
			subName := fmt.Sprintf("%s%d", "sub", id)
			subv2 := eventingtesting.NewSubscription(subName, "foo",
				eventingtesting.WithNotCleanEventSourceAndType(),
				eventingtesting.WithSinkURL(subscriber.SinkURL),
				eventingtesting.WithTypeMatchingStandard(),
				eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
			)
			AddJSCleanEventTypesToStatus(subv2, testEnvironment.cleaner)

			// when
			err = testEnvironment.jsBackend.SyncSubscription(subv2)

			// then
			require.NoError(t, err)

			ev1data := fmt.Sprintf("%s%d", "sampledata", id)
			require.NoError(t, SendEventToJetStream(testEnvironment.jsBackend, ev1data))
			expectedEv1Data := fmt.Sprintf("%q", ev1data)
			require.NoError(t, subscriber.CheckEvent(expectedEv1Data))

			// given
			testEnvironment.natsServer.Shutdown()
			require.Eventually(t, func() bool {
				return !testEnvironment.jsBackend.Conn.IsConnected()
			}, 30*time.Second, 2*time.Second)

			// when
			_ = eventingtesting.RunNatsServerOnPort(
				eventingtesting.WithPort(testEnvironment.natsPort),
				eventingtesting.WithJetStreamEnabled())

			// then
			if tc.givenMaxReconnects > 0 {
				require.Eventually(t, func() bool {
					return testEnvironment.jsBackend.Conn.IsConnected()
				}, 30*time.Second, 2*time.Second)
			}

			_, err = testEnvironment.jsClient.StreamInfo(testEnvironment.natsConfig.JSStreamName)
			if tc.givenStorageType == StorageTypeMemory && tc.givenMaxReconnects == 0 {
				// for memory storage with reconnects disabled
				require.ErrorIs(t, nats.ErrStreamNotFound, err)
			} else {
				// check that the stream is still present for file storage
				// or recreated via reconnect handler for memory storage
				require.NoError(t, err)
			}

			// sync the subscription again to recreate invalid subscriptions or consumers, if any
			err = testEnvironment.jsBackend.SyncSubscription(subv2)

			require.NoError(t, err)

			// stream exists
			_, err = testEnvironment.jsClient.StreamInfo(testEnvironment.natsConfig.JSStreamName)
			require.NoError(t, err)

			ev2data := fmt.Sprintf("%s%d", "newsampledata", id)
			require.NoError(t, SendEventToJetStream(testEnvironment.jsBackend, ev2data))
			expectedEv2Data := fmt.Sprintf("%q", ev2data)
			require.NoError(t, subscriber.CheckEvent(expectedEv2Data))
		})
	}
}

// TestJetStream_ServerAndSinkRestart tests that the messages persisted (not ack'd) in the stream
// when the sink is down reach the subscriber even when the NATS server is restarted.
func TestJetStream_ServerAndSinkRestart(t *testing.T) {
	// given
	subscriber := eventingtesting.NewSubscriber()
	defer subscriber.Shutdown()
	require.True(t, subscriber.IsRunning())
	listener := subscriber.GetSubscriberListener()
	listenerNetwork, listenerAddress := listener.Addr().Network(), listener.Addr().String()

	testEnvironment := setupTestEnvironment(t)
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	defer func() { _ = testEnvironment.jsClient.DeleteStream(testEnvironment.natsConfig.JSStreamName) }()

	var err error
	testEnvironment.jsBackend.Config.JSStreamStorageType = StorageTypeFile
	testEnvironment.jsBackend.Config.MaxReconnects = 0
	err = testEnvironment.jsBackend.Initialize(nil)
	require.NoError(t, err)

	subv2 := eventingtesting.NewSubscription("sub", "foo",
		eventingtesting.WithNotCleanEventSourceAndType(),
		eventingtesting.WithSinkURL(subscriber.SinkURL),
		eventingtesting.WithTypeMatchingStandard(),
		eventingtesting.WithMaxInFlight(DefaultMaxInFlights),
	)
	AddJSCleanEventTypesToStatus(subv2, testEnvironment.cleaner)

	// when
	err = testEnvironment.jsBackend.SyncSubscription(subv2)

	// then
	require.NoError(t, err)
	ev1data := "sampledata"
	require.NoError(t, SendEventToJetStream(testEnvironment.jsBackend, ev1data))
	expectedEv1Data := fmt.Sprintf("%q", ev1data)
	require.NoError(t, subscriber.CheckEvent(expectedEv1Data))

	// given
	subscriber.Shutdown() // shutdown the subscriber intentionally here
	require.False(t, subscriber.IsRunning())
	ev2data := "newsampletestdata"
	require.NoError(t, SendEventToJetStream(testEnvironment.jsBackend, ev2data))

	// check that the stream contains one message that was not acknowledged
	const expectedNotAcknowledgedMsgs = uint64(1)
	var info *nats.StreamInfo

	require.Eventually(t, func() bool {
		info, err = testEnvironment.jsClient.StreamInfo(testEnvironment.natsConfig.JSStreamName)
		require.NoError(t, err)
		return info.State.Msgs == expectedNotAcknowledgedMsgs
	}, 60*time.Second, 5*time.Second)

	// shutdown the nats server
	testEnvironment.natsServer.Shutdown()
	require.Eventually(t, func() bool {
		return !testEnvironment.jsBackend.Conn.IsConnected()
	}, 30*time.Second, 2*time.Second)

	// when
	// restart the NATS server
	_ = eventingtesting.RunNatsServerOnPort(
		eventingtesting.WithPort(testEnvironment.natsPort),
		eventingtesting.WithJetStreamEnabled())
	// the unacknowledged message must still be present in the stream
	require.Eventually(t, func() bool {
		info, err = testEnvironment.jsClient.StreamInfo(testEnvironment.natsConfig.JSStreamName)
		require.NoError(t, err)
		return info.State.Msgs == expectedNotAcknowledgedMsgs
	}, 60*time.Second, 5*time.Second)
	// sync the subscription again to recreate invalid subscriptions or consumers, if any
	err = testEnvironment.jsBackend.SyncSubscription(subv2)
	require.NoError(t, err)
	// restart the subscriber
	listener, err = net.Listen(listenerNetwork, listenerAddress)
	require.NoError(t, err)
	newSubscriber := eventingtesting.NewSubscriber(eventingtesting.WithListener(listener))
	defer newSubscriber.Shutdown()
	require.True(t, newSubscriber.IsRunning())

	// then
	// no messages should be present in the stream
	require.Eventually(t, func() bool {
		info, err = testEnvironment.jsClient.StreamInfo(testEnvironment.natsConfig.JSStreamName)
		require.NoError(t, err)
		return info.State.Msgs == uint64(0)
	}, 60*time.Second, 5*time.Second)
	// check if the event is received
	expectedEv2Data := fmt.Sprintf("%q", ev2data)
	require.NoError(t, newSubscriber.CheckEvent(expectedEv2Data))
}

func defaultNATSConfig(url string, port int) env.NATSConfig {
	streamName := fmt.Sprintf("%s%d", DefaultStreamName, port)
	return env.NATSConfig{
		URL:                     url,
		MaxReconnects:           DefaultMaxReconnects,
		ReconnectWait:           3 * time.Second,
		JSStreamName:            streamName,
		JSSubjectPrefix:         streamName,
		JSStreamStorageType:     StorageTypeMemory,
		JSStreamRetentionPolicy: RetentionPolicyInterest,
		JSStreamDiscardPolicy:   DiscardPolicyNew,
	}
}

// getJetStreamClient creates a client with JetStream context, or fails the caller test.
func getJetStreamClient(t *testing.T, serverURL string) *jetStreamClient {
	t.Helper()

	conn, err := nats.Connect(serverURL)
	if err != nil {
		t.Error(err.Error())
	}
	jsCtx, err := conn.JetStream()
	if err != nil {
		conn.Close()
		t.Error(err.Error())
	}
	return &jetStreamClient{
		JetStreamContext: jsCtx,
		natsConn:         conn,
	}
}

// setupTestEnvironment is a TestEnvironment constructor.
func setupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()
	natsServer, natsPort, err := StartNATSServer(eventingtesting.WithJetStreamEnabled())
	require.NoError(t, err)
	natsConfig := defaultNATSConfig(natsServer.ClientURL(), natsPort)
	defaultLogger, err := logger.New(string(kymalogger.JSON), string(kymalogger.INFO))
	require.NoError(t, err)

	// init the metrics collector
	metricsCollector := metrics.NewCollector()

	jsClient := getJetStreamClient(t, natsConfig.URL)
	jsCleaner := cleaner.NewJetStreamCleaner(defaultLogger)
	defaultSubsConfig := env.DefaultSubscriptionConfig{MaxInFlightMessages: 9}
	jsBackend := NewJetStream(natsConfig, metricsCollector, jsCleaner, defaultSubsConfig, defaultLogger)

	return &TestEnvironment{
		jsBackend:  jsBackend,
		logger:     defaultLogger,
		natsServer: natsServer,
		jsClient:   jsClient,
		natsConfig: natsConfig,
		cleaner:    jsCleaner,
		natsPort:   natsPort,
	}
}
