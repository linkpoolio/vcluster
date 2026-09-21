package namespaces

import (
	"testing"

	"gotest.tools/assert"
)

func TestTranslateVirtualNamespace(t *testing.T) {
	mappings := map[string]string{
		"frontend":     "customer-frontend",
		"team-a-*":     "h-team-a-*",
		"datasets-*":   "${name}-data-*",
		"${name}-logs": "shared-logs",
	}

	testCases := []struct {
		name            string
		virtual         string
		expectedHost    string
		expectedMatched bool
	}{
		{name: "exact", virtual: "frontend", expectedHost: "customer-frontend", expectedMatched: true},
		{name: "pattern", virtual: "team-a-dev", expectedHost: "h-team-a-dev", expectedMatched: true},
		{name: "pattern with name placeholder", virtual: "datasets-raw", expectedHost: "my-vc-data-raw", expectedMatched: true},
		{name: "exact with name placeholder", virtual: "my-vc-logs", expectedHost: "shared-logs", expectedMatched: true},
		{name: "unmapped", virtual: "experimental", expectedHost: "", expectedMatched: false},
		{name: "pattern prefix without dash is not a match", virtual: "team-a", expectedHost: "", expectedMatched: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			host, matched := TranslateVirtualNamespace("my-vc", tc.virtual, mappings)
			assert.Equal(t, matched, tc.expectedMatched)
			assert.Equal(t, host, tc.expectedHost)
		})
	}
}

func TestTranslateVirtualNamespaceRoundTrip(t *testing.T) {
	mappings := map[string]string{"*": "${name}-*"}

	for _, vNamespace := range []string{"default", "kube-system", "team-a-dev"} {
		host, ok := TranslateVirtualNamespace("tenant", vNamespace, mappings)
		assert.Assert(t, ok)
		assert.Equal(t, host, "tenant-"+vNamespace)

		virtual, ok := TranslateHostNamespace("tenant", host, mappings)
		assert.Assert(t, ok)
		assert.Equal(t, virtual, vNamespace)
	}
}
