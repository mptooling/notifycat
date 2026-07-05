package config

import (
	"errors"
	"testing"
)

// TestRequireProviderSecret_ScopedToGitHub proves the webhook-secret requirement
// follows the selected provider (D8): github requires GITHUB_WEBHOOK_SECRET, and
// any other provider does not gate on it. Behavior is identical while github is
// the only wired value; this pins the seam so a second provider drops in cleanly.
func TestRequireProviderSecret_ScopedToGitHub(t *testing.T) {
	github := &Config{GitProvider: gitProviderGitHub} // no GitHubWebhookSecret
	err := requireProviderSecret(github)
	var missing *MissingVarError
	if !errors.As(err, &missing) || missing.Var != "GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("github with no secret: err = %v; want MissingVarError(GITHUB_WEBHOOK_SECRET)", err)
	}

	withSecret := &Config{GitProvider: gitProviderGitHub, GitHubWebhookSecret: Secret("shh")}
	if err := requireProviderSecret(withSecret); err != nil {
		t.Errorf("github with secret: err = %v; want nil", err)
	}

	other := &Config{GitProvider: gitProviderBitbucket} // no GitHubWebhookSecret
	if err := requireProviderSecret(other); err != nil {
		t.Errorf("non-github provider must not require GITHUB_WEBHOOK_SECRET; err = %v", err)
	}
}
