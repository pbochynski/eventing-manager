package jetstream

import (
	"testing"

	"github.com/stretchr/testify/require"

	eventingv1alpha2 "github.com/kyma-project/eventing-manager/api/eventing/v1alpha2"
	eventingtesting "github.com/kyma-project/eventing-manager/testing"
)

// TestJetStream_isJsSubAssociatedWithKymaSub tests the isJsSubAssociatedWithKymaSub method.
func TestJetStream_isJsSubAssociatedWithKymaSub(t *testing.T) {
	t.Parallel()
	// given
	testEnvironment := setupTestEnvironment(t)
	jsBackend := testEnvironment.jsBackend
	defer testEnvironment.natsServer.Shutdown()
	defer testEnvironment.jsClient.natsConn.Close()
	initErr := jsBackend.Initialize(nil)
	require.NoError(t, initErr)

	// create subscription 1 and its JetStream subscription
	cleanSubject1 := "subOne"
	sub1 := eventingtesting.NewSubscription(cleanSubject1, "foo", eventingtesting.WithNotCleanEventSourceAndType())
	jsSub1Key := NewSubscriptionSubjectIdentifier(sub1, cleanSubject1)

	// create subscription 2 and its JetStream subscription
	cleanSubject2 := "subOneTwo"
	sub2 := eventingtesting.NewSubscription(cleanSubject2, "foo", eventingtesting.WithNotCleanEventSourceAndType())
	jsSub2Key := NewSubscriptionSubjectIdentifier(sub2, cleanSubject2)

	testCases := []struct {
		name            string
		givenJSSubKey   SubscriptionSubjectIdentifier
		givenKymaSubKey *eventingv1alpha2.Subscription
		wantResult      bool
	}{
		{
			name:            "",
			givenJSSubKey:   jsSub1Key,
			givenKymaSubKey: sub1,
			wantResult:      true,
		},
		{
			name:            "",
			givenJSSubKey:   jsSub2Key,
			givenKymaSubKey: sub2,
			wantResult:      true,
		},
		{
			name:            "",
			givenJSSubKey:   jsSub1Key,
			givenKymaSubKey: sub2,
			wantResult:      false,
		},
		{
			name:            "",
			givenJSSubKey:   jsSub2Key,
			givenKymaSubKey: sub1,
			wantResult:      false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotResult := isJsSubAssociatedWithKymaSub(tc.givenJSSubKey, tc.givenKymaSubKey)
			require.Equal(t, tc.wantResult, gotResult)
		})
	}
}
