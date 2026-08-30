package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The webhook-secret requirement tracks the selected provider: a deployment
// never needs the other provider's credential.
func TestRequireProviderSecret_FollowsProvider(t *testing.T) {
	t.Run("github without a secret names the github variable", func(t *testing.T) {
		err := requireProviderSecret(&Config{GitProvider: gitProviderGitHub})

		var missing *MissingVarError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "GITHUB_WEBHOOK_SECRET", missing.Var)
	})

	t.Run("github ignores the bitbucket secret", func(t *testing.T) {
		err := requireProviderSecret(&Config{
			GitProvider:            gitProviderGitHub,
			GitHubWebhookSecret:    Secret("shh"),
			BitbucketWebhookSecret: "",
		})

		assert.NoError(t, err)
	})

	t.Run("bitbucket without a secret names the bitbucket variable", func(t *testing.T) {
		err := requireProviderSecret(&Config{GitProvider: gitProviderBitbucket})

		var missing *MissingVarError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "BITBUCKET_WEBHOOK_SECRET", missing.Var)
	})

	t.Run("bitbucket ignores the github secret", func(t *testing.T) {
		err := requireProviderSecret(&Config{
			GitProvider:            gitProviderBitbucket,
			BitbucketWebhookSecret: Secret("bb"),
			GitHubWebhookSecret:    "",
		})

		assert.NoError(t, err)
	})
}
