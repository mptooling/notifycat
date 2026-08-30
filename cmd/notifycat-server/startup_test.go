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

func TestStartupError_InternalError_Passthrough(t *testing.T) {
	original := errors.New("database: unexpected error")

	got := startupError(original)

	assert.Same(t, original, got, "an internal error is returned unwrapped")
}
