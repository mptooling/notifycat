package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
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

func TestStartupError_MissingMappingsFile(t *testing.T) {
	inner := &mappings.FileNotFoundError{Path: "/app/mappings.yaml", Err: os.ErrNotExist}
	err := startupError(fmt.Errorf("app: load mappings: %w", inner))
	msg := err.Error()
	if !strings.Contains(msg, "/app/mappings.yaml") {
		t.Errorf("message %q does not contain the path", msg)
	}
	if !strings.Contains(msg, "mappings.example.yaml") {
		t.Errorf("message %q does not suggest the example file", msg)
	}
}

func TestStartupError_UnreadableMappingsFile(t *testing.T) {
	inner := &mappings.FileNotFoundError{Path: "/app/mappings.yaml", Err: os.ErrPermission}
	err := startupError(fmt.Errorf("app: load mappings: %w", inner))
	msg := err.Error()
	if !strings.Contains(msg, "/app/mappings.yaml") {
		t.Errorf("message %q does not contain the path", msg)
	}
	if !strings.Contains(msg, "permission") {
		t.Errorf("message %q does not mention permissions", msg)
	}
}

func TestStartupError_MalformedMappings(t *testing.T) {
	yamlErr := errors.New("yaml: line 3: could not find expected ':'")
	inner := &mappings.ParseError{
		Path: "/app/mappings.yaml",
		Err:  fmt.Errorf("mappings: parse: %w", yamlErr),
	}
	err := startupError(fmt.Errorf("app: load mappings: %w", inner))
	msg := err.Error()
	if !strings.Contains(msg, "/app/mappings.yaml") {
		t.Errorf("message %q does not contain the path", msg)
	}
	if !strings.Contains(msg, "notifycat-doctor") {
		t.Errorf("message %q does not mention notifycat-doctor", msg)
	}
	if !strings.Contains(msg, "yaml: line 3") {
		t.Errorf("message %q does not include the parser detail", msg)
	}
}

func TestStartupError_InternalError_Passthrough(t *testing.T) {
	orig := errors.New("database: unexpected error")
	got := startupError(orig)
	if got != orig {
		t.Errorf("startupError(%v) = %v; want original error unchanged", orig, got)
	}
}
