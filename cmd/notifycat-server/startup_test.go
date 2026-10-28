package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/config"
)

func TestStartupError_MissingSecret(t *testing.T) {
	for _, varName := range []string{"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN"} {
		t.Run(varName, func(t *testing.T) {
			err := startupError(fmt.Errorf("config: %w", &config.MissingVarError{Var: varName}))

			require.ErrorContains(t, err, varName)
			assert.ErrorContains(t, err, ".env.example", "the operator is pointed at the template")
		})
	}
}

func TestStartupError_MissingAdminKeyHashPointsAtKeygen(t *testing.T) {
	err := startupError(fmt.Errorf("config: %w", &config.MissingVarError{
		Var:    "NOTIFYCAT_ADMIN_KEY_HASH",
		Remedy: "run notifycat-keygen",
	}))

	require.ErrorContains(t, err, "NOTIFYCAT_ADMIN_KEY_HASH is not set")
	assert.ErrorContains(t, err, "run notifycat-keygen")
	assert.NotContains(t, err.Error(), ".env.example", "the hash comes from keygen, not from the template")
}

func TestStartupError_InternalError_Passthrough(t *testing.T) {
	original := errors.New("database: unexpected error")

	got := startupError(original)

	assert.Same(t, original, got, "an internal error is returned unwrapped")
}
