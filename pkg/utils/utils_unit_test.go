package utils

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GetPortNumberFromURL(t *testing.T) {
	testCases := []struct {
		givenURL url.URL
		wantPort uint32
	}{
		{
			givenURL: url.URL{},
			wantPort: 80,
		},
		{
			givenURL: url.URL{
				Host: "domain.com:####",
			},
			wantPort: 80,
		},
		{
			givenURL: url.URL{
				Host: "domain.com",
			},
			wantPort: 80,
		},
		{
			givenURL: url.URL{
				Scheme: "http",
			},
			wantPort: 80,
		},
		{
			givenURL: url.URL{
				Scheme: "https",
			},
			wantPort: 443,
		},
		{
			givenURL: url.URL{
				Scheme: "http",
				Host:   "domain.com:8080",
			},
			wantPort: 8080,
		},
		{
			givenURL: url.URL{
				Scheme: "https",
				Host:   "domain.com:8081",
			},
			wantPort: 8081,
		},
	}

	for _, tc := range testCases {
		gotPort, err := GetPortNumberFromURL(tc.givenURL)
		if err != nil {
			t.Errorf("test failed with error: [%v]", err)
			continue
		}
		if tc.wantPort != gotPort {
			t.Errorf("test failed with given URL: {Scheme:%s Host:%s}, want port: [%d] but got: [%d]",
				tc.givenURL.Scheme, tc.givenURL.Host, tc.wantPort, gotPort)
		}
	}
}

func Test_ContainsString(t *testing.T) {
	testCases := []struct {
		sl   []string
		s    string
		want bool
	}{
		{
			sl:   []string{"kyma", "eventing"},
			s:    "kyma",
			want: true,
		},
		{
			sl:   []string{"kyma", "eventing"},
			s:    "nats",
			want: false,
		},
		{
			sl:   []string{"kyma.eventing", "nats"},
			s:    "kyma",
			want: false,
		},
	}
	for _, tc := range testCases {
		result := ContainsString(tc.sl, tc.s)
		if tc.want != result {
			t.Errorf("test failed with give slice of strings: %s and string: %s, expected: %v but got: %v",
				tc.sl, tc.s, tc.want, result)
		}
	}
}

func Test_RemoveString(t *testing.T) {
	testCases := []struct {
		sl   []string
		s    string
		want []string
	}{
		{
			sl:   []string{"kyma", "eventing"},
			s:    "kyma",
			want: []string{"eventing"},
		},
		{
			sl:   []string{"kyma", "eventing"},
			s:    "nats",
			want: []string{"kyma", "eventing"},
		},
		{
			sl:   []string{"kyma.eventing", "nats"},
			s:    "kyma",
			want: []string{"kyma.eventing", "nats"},
		},
	}
	for _, tc := range testCases {
		result := RemoveString(tc.sl, tc.s)
		if !reflect.DeepEqual(tc.want, result) {
			t.Errorf("test failed with give slice of strings: %s and string: %s, expected: %s but got: %s",
				tc.sl, tc.s, tc.want, result)
		}
	}
}

func TestGetRandSuffix(t *testing.T) {
	totalExecutions := 10
	lengthOfRandomSuffix := 6
	results := make(map[string]bool)
	for i := 0; i < totalExecutions; i++ {
		result := GetRandString(lengthOfRandomSuffix)
		if _, ok := results[result]; ok {
			t.Fatalf("generated string already exists: %s", result)
		}
		results[result] = true
	}
}

func TestIsValidScheme(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		givenSink string
		wantValid bool
	}{
		{
			name:      "invalid scheme should return false",
			givenSink: "invalid",
			wantValid: false,
		},
		{
			name:      "valid scheme http should return true",
			givenSink: "http://valid",
			wantValid: true,
		},
		{
			name:      "valid scheme https should return true",
			givenSink: "https://valid",
			wantValid: true,
		},
	}
	for _, testCase := range testCases {
		tc := testCase
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotValid := IsValidScheme(tc.givenSink)
			require.Equal(t, tc.wantValid, gotValid)
		})
	}
}

func TestGetSinkData(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		givenSink       string
		wantTrimmedHost string
		wantSubDomains  []string
		wantError       error
	}{
		{
			name:      "invalid uri should return error",
			givenSink: "http://invalid sink",
			wantError: ErrParseSink,
		},
		{
			name:            "valid uri should not return error",
			givenSink:       "http://valid1.valid2",
			wantError:       nil,
			wantTrimmedHost: "valid1.valid2",
			wantSubDomains:  []string{"valid1", "valid2"},
		},
	}
	for _, testCase := range testCases {
		tc := testCase
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotTrimmedHost, gotSubDomain, gotErr := GetSinkData(tc.givenSink)
			require.ErrorIs(t, gotErr, tc.wantError)
			require.Equal(t, tc.wantTrimmedHost, gotTrimmedHost)
			require.Equal(t, tc.wantSubDomains, gotSubDomain)
		})
	}
}

func Test_IsEmpty(t *testing.T) {
	assert.True(t, IsEmpty(""))
	assert.True(t, IsEmpty(" "))
	assert.False(t, IsEmpty("value"))
}
