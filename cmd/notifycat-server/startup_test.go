package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
)

func TestStartupError_MissingSecret(t *testing.T) {
	cases := []struct {
		varName string
	}{
		{"GITHUB_WEBHOOK_SECRET"},
		{"SLACK_BOT_TOKEN"},
	}
	for _, tc := range cases {
		t.Run(tc.varName, func(t *testing.T) {
			err := startupError(fmt.Errorf("config: %w", &config.MissingVarError{Var: tc.varName}))
			msg := err.Error()
			if !strings.Contains(msg, tc.varName) {
				t.Errorf("message %q does not name the missing variable %q", msg, tc.varName)
			}
			if !strings.Contains(msg, ".env.example") {
				t.Errorf("message %q does not mention .env.example", msg)
			}
		})
	}
}

func TestStartupError_InternalError_Passthrough(t *testing.T) {
	orig := errors.New("database: unexpected error")
	got := startupError(orig)
	if got != orig {
		t.Errorf("startupError(%v) = %v; want original error unchanged", orig, got)
	}
}
