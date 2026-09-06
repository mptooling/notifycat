package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptions_Valid(t *testing.T) {
	opts, err := parseOptions([]string{"-from", "C0OLD", "-to", "C0NEW", "-repo", "acme/api", "-dry-run"})

	require.NoError(t, err)
	assert.Equal(t, options{from: "C0OLD", to: "C0NEW", repository: "acme/api", dryRun: true}, opts)
}

// An empty -to is the drop mode: remove the messages with no replacement.
func TestParseOptions_EmptyDestinationIsAllowed(t *testing.T) {
	opts, err := parseOptions([]string{"-from", "C0OLD"})

	require.NoError(t, err)
	assert.Empty(t, opts.to)
}

func TestParseOptions_AuditNeedsNoChannels(t *testing.T) {
	opts, err := parseOptions([]string{"-audit"})

	require.NoError(t, err)
	assert.True(t, opts.audit)
}

func TestParseOptions_Rejected(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		want string
	}{
		{name: "no source", args: []string{"-to", "C0NEW"}, want: "-from is required"},
		{name: "source is a channel name", args: []string{"-from", "#engineering"}, want: "not a Slack channel id"},
		{name: "destination is a channel name", args: []string{"-from", "C0OLD", "-to", "#eng"}, want: "not a Slack channel id"},
		{name: "same channel", args: []string{"-from", "C0OLD", "-to", "C0OLD"}, want: "the same channel"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseOptions(testCase.args)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestMissingScopes(t *testing.T) {
	granted := []string{"chat:write", "reactions:write"}

	missing := missingScopes(granted, requiredScopes)

	assert.Equal(t, []string{"reactions:read", "channels:history", "groups:history"}, missing)
}
