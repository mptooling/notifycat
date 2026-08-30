package kernel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/kernel"
)

func TestProviderPullRequestWebURL(t *testing.T) {
	testCases := []struct {
		name       string
		provider   kernel.Provider
		repository string
		number     int
		want       string
	}{
		{
			name:       "github",
			provider:   kernel.ProviderGitHub,
			repository: "acme/api",
			number:     42,
			want:       "https://github.com/acme/api/pull/42",
		},
		{
			name:       "bitbucket",
			provider:   kernel.ProviderBitbucket,
			repository: "acme/api",
			number:     42,
			want:       "https://bitbucket.org/acme/api/pull-requests/42",
		},
		{
			name:       "unknown provider falls back to github",
			provider:   kernel.Provider(""),
			repository: "acme/api",
			number:     7,
			want:       "https://github.com/acme/api/pull/7",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.provider.PullRequestWebURL(testCase.repository, testCase.number)

			assert.Equal(t, testCase.want, got)
		})
	}
}
