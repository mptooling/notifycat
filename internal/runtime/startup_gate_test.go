package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/platform/config"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	"github.com/mptooling/notifycat/internal/runtime"
)

const memberChannel = `{"ok":true,"channel":{"id":"C0123ABCDE","name":"general","is_member":true}}`

// slackValidationFake answers the two read-only calls the validator makes,
// including the scope header the Slack client reads scopes from.
func slackValidationFake(t *testing.T, channelBody string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("X-OAuth-Scopes", "chat:write,reactions:write,channels:read")
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOTTEST"}`)
		case "/api/conversations.info":
			_, _ = io.WriteString(w, channelBody)
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// gitHubHooksFake answers the hooks listing with a canned status and body.
func gitHubHooksFake(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// gateConfig wires one explicit mapping against the two fakes, with a read
// token set (so the webhook check runs) and no primed lock (so the gate
// actually validates).
func gateConfig(t *testing.T, slackURL, gitHubURL string) config.Config {
	t.Helper()

	dir := t.TempDir()
	return config.Config{
		Addr:                ":0",
		LogLevel:            "error",
		LogFormat:           "text",
		DatabaseURL:         "file:" + filepath.Join(dir, "gate.db"),
		ConfigFile:          filepath.Join(dir, "config.yaml"),
		Mappings:            seedsToMappings(t, []mappingSeed{{repository: "acme/api", channel: "C0123ABCDE"}}),
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret"),
		SlackBotToken:       config.Secret("xoxb-test"),
		SlackBaseURL:        slackURL,
		GitHubBaseURL:       gitHubURL,
		GitHubToken:         config.Secret("gh-read-only"),
	}
}

func readGateLock(t *testing.T, configPath string) routinginfra.Lock {
	t.Helper()

	lock, err := routinginfra.ReadLock(routinginfra.LockPath(configPath))
	require.NoError(t, err)
	return lock
}

// Issue #172's headline case: a read token whose identity may not list hooks
// used to abort boot for every repository. It must now boot, and the entry must
// stay out of the lock so the next boot re-probes it.
func TestStartupGate_HooksListingForbidden_BootsWithoutCaching(t *testing.T) {
	slack := slackValidationFake(t, memberChannel)
	gitHub := gitHubHooksFake(t, http.StatusForbidden,
		`{"message":"Access denied. You must have write or admin access."}`)
	cfg := gateConfig(t, slack.URL, gitHub.URL)

	buildTestServer(t, cfg)

	assert.NotContains(t, readGateLock(t, cfg.ConfigFile).Entries, "acme/api", "a warned entry is never cached")
}

func TestStartupGate_NoWebhookOnRepository_Boots(t *testing.T) {
	slack := slackValidationFake(t, memberChannel)
	gitHub := gitHubHooksFake(t, http.StatusOK, `[]`)
	cfg := gateConfig(t, slack.URL, gitHub.URL)

	buildTestServer(t, cfg)

	assert.NotContains(t, readGateLock(t, cfg.ConfigFile).Entries, "acme/api")
}

// The control for the two cases above: with the webhook in place the entry is
// cached, so their exclusion is about the warning and not about the gate having
// stopped writing the lock altogether.
func TestStartupGate_FullCoverage_CachesEntry(t *testing.T) {
	slack := slackValidationFake(t, memberChannel)
	gitHub := gitHubHooksFake(t, http.StatusOK, `[{"id":1,"active":true,
		"config":{"url":"https://notifycat.example.com/webhook/github"},
		"events":["pull_request","pull_request_review","pull_request_review_comment","issue_comment"]}]`)
	cfg := gateConfig(t, slack.URL, gitHub.URL)

	buildTestServer(t, cfg)

	assert.Contains(t, readGateLock(t, cfg.ConfigFile).Entries, "acme/api")
}

// The fatal path stays unregressed: a broken Slack install still refuses to start.
func TestStartupGate_SlackChannelFailure_AbortsStartup(t *testing.T) {
	slack := slackValidationFake(t, `{"ok":false,"error":"channel_not_found"}`)
	gitHub := gitHubHooksFake(t, http.StatusOK, `[]`)
	cfg := gateConfig(t, slack.URL, gitHub.URL)

	err := fx.New(fx.Supply(cfg), runtime.Module, fx.NopLogger).Err()

	require.Error(t, err)
	assert.ErrorContains(t, err, "startup validation failed for 1 entries")
}
