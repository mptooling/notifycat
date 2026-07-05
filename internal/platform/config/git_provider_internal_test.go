package config

import (
	"errors"
	"testing"
)

// TestRequireProviderSecret_FollowsProvider proves the webhook-secret requirement
// tracks the selected provider (D8): github gates on GITHUB_WEBHOOK_SECRET and is
// blind to the bitbucket secret; bitbucket gates on BITBUCKET_WEBHOOK_SECRET and
// is blind to the github one. A deployment never needs the other provider's
// credential.
func TestRequireProviderSecret_FollowsProvider(t *testing.T) {
	var missing *MissingVarError

	// github: requires GITHUB_WEBHOOK_SECRET, ignores the bitbucket secret.
	err := requireProviderSecret(&Config{GitProvider: gitProviderGitHub})
	if !errors.As(err, &missing) || missing.Var != "GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("github with no secret: err = %v; want MissingVarError(GITHUB_WEBHOOK_SECRET)", err)
	}
	if err := requireProviderSecret(&Config{GitProvider: gitProviderGitHub, GitHubWebhookSecret: Secret("shh")}); err != nil {
		t.Errorf("github with secret: err = %v; want nil", err)
	}
	if err := requireProviderSecret(&Config{GitProvider: gitProviderGitHub, GitHubWebhookSecret: Secret("shh"), BitbucketWebhookSecret: ""}); err != nil {
		t.Errorf("github must not gate on BITBUCKET_WEBHOOK_SECRET: err = %v", err)
	}

	// bitbucket: requires BITBUCKET_WEBHOOK_SECRET, ignores the github secret.
	err = requireProviderSecret(&Config{GitProvider: gitProviderBitbucket})
	if !errors.As(err, &missing) || missing.Var != "BITBUCKET_WEBHOOK_SECRET" {
		t.Fatalf("bitbucket with no secret: err = %v; want MissingVarError(BITBUCKET_WEBHOOK_SECRET)", err)
	}
	if err := requireProviderSecret(&Config{GitProvider: gitProviderBitbucket, BitbucketWebhookSecret: Secret("bb")}); err != nil {
		t.Errorf("bitbucket with secret: err = %v; want nil", err)
	}
	if err := requireProviderSecret(&Config{GitProvider: gitProviderBitbucket, BitbucketWebhookSecret: Secret("bb"), GitHubWebhookSecret: ""}); err != nil {
		t.Errorf("bitbucket must not gate on GITHUB_WEBHOOK_SECRET: err = %v", err)
	}
}
