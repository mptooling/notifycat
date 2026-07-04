package runtime_test

import (
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/runtime"
)

// TestModule_StartsAndStops builds the whole runtime graph from a minimal valid
// config and exercises the real lifecycle: openAndMigrate, the startup gate
// (passing with empty mappings so no network is touched), and the HTTP server +
// scheduler OnStart/OnStop hooks. Mappings are empty, so the startup gate is a
// no-op and the digest scheduler is nil; the server binds an ephemeral port on
// loopback to avoid conflicts.
func TestModule_StartsAndStops(t *testing.T) {
	cfg := config.Config{
		ConfigFile:     t.TempDir() + "/config.yaml",
		Addr:           "127.0.0.1:0",
		LogLevel:       "error",
		LogFormat:      "text",
		DatabaseURL:    "file:" + t.TempDir() + "/test.db",
		SlackBaseURL:   "https://slack.com",
		GitHubBaseURL:  "https://api.github.com",
		MessageTTLDays: 30,
		DigestTimezone: time.UTC,
		// Mappings left nil (empty): the startup gate is a no-op and no
		// digest scheduler is built, so RequireStart touches no network.
		GitHubWebhookSecret: config.Secret("dummy-webhook-secret"),
		SlackBotToken:       config.Secret("xoxb-dummy-token"),
		// GitHubToken and SlackSigningSecret intentionally empty: no GitHub
		// fetcher is built and the Slack interactivity route is skipped.
	}

	app := fxtest.New(t, runtime.Module, fx.Supply(cfg), fx.NopLogger)
	app.RequireStart()
	app.RequireStop()
}
