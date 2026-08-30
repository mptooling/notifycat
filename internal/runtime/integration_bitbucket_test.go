package runtime_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/platform/config"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// newBitbucketFixture wires the full runtime graph for a git_provider: bitbucket
// deployment against the Slack fake — no GitHub credentials at all. It mirrors
// newIntegrationFixture but posts Bitbucket-signed deliveries to
// /webhook/bitbucket. The lock is primed with the bitbucket provider so startup
// validation finds nothing to revalidate (the provider joins each entry's hash).
func newBitbucketFixture(t *testing.T, seeds ...mappingSeed) *integrationFixture {
	t.Helper()

	slack := newSlackFake(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	cfg := config.Config{
		Addr:                   ":0",
		LogLevel:               "error",
		LogFormat:              "text",
		DatabaseURL:            "file:" + filepath.Join(dir, "bb.db"),
		ConfigFile:             configPath,
		GitProvider:            kernel.ProviderBitbucket,
		Mappings:               seedsToMappings(t, seeds),
		MessageTTLDays:         30,
		DependabotFormat:       true,
		BitbucketWebhookSecret: config.Secret("itsecret"),
		SlackBotToken:          config.Secret("xoxb-int"),
		SlackBaseURL:           slack.URL,
		Reactions: config.Reactions{
			Enabled:       true,
			NewPR:         "rocket",
			MergedPR:      "twisted_rightwards_arrows",
			ClosedPR:      "x",
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
		},
	}
	primeLock(t, configPath, routingapp.NewProvider(
		routingdomain.Defaults{GitProvider: kernel.ProviderBitbucket}, cfg.Mappings, cfg.Digest))

	server := httptest.NewServer(buildTestServer(t, cfg).Handler)
	t.Cleanup(server.Close)

	return &integrationFixture{server: server, cfg: cfg, slack: slack}
}

// postBitbucket sends a Bitbucket-signed delivery (X-Hub-Signature: sha256=<hmac>
// over the raw body, plus the X-Event-Key) to /webhook/bitbucket and returns the
// HTTP status.
func (f *integrationFixture) postBitbucket(t *testing.T, eventKey, payload string) int {
	t.Helper()

	status, _ := request(t, f.server, http.MethodPost, "/webhook/bitbucket", payload, map[string]string{
		"Content-Type":    "application/json",
		"X-Hub-Signature": githubSignature("itsecret", payload),
		"X-Event-Key":     eventKey,
	})
	return status
}

func TestBitbucketIntegration_OpenedPR(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := fixture.postBitbucket(t, "pullrequest:created", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {
			"id": 42, "title": "fix", "state": "OPEN", "draft": false,
			"links": {"html": {"href": "https://bitbucket.org/acme/widget/pull-requests/42"}},
			"author": {"display_name": "Bob", "type": "user"},
			"created_on": "2026-06-05T14:04:00.000000+00:00"
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.postMessage")

	section := blockText(fixture.postedBody(t), "section")
	assert.Contains(t, section, "@alice, please review")
	assert.Contains(t, section, "<https://bitbucket.org/acme/widget/pull-requests/42|PR #42: fix>")

	saved, err := fixture.loadMessage(t, "acme/widget", 42)
	require.NoError(t, err)
	assert.NotEmpty(t, saved.MessageID)
}

func TestBitbucketIntegration_Approved(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := fixture.postBitbucket(t, "pullrequest:approved", `{
		"actor": {"type": "user", "display_name": "Rev"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "fix", "state": "OPEN",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "white_check_mark", fixture.requireReaction(t))
}

func TestBitbucketIntegration_Merged(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := fixture.postBitbucket(t, "pullrequest:fulfilled", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "fix", "state": "MERGED",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.update")
	assert.Equal(t, "twisted_rightwards_arrows", fixture.requireReaction(t))
}

func TestBitbucketIntegration_ConvertedToDraftDeletes(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := fixture.postBitbucket(t, "pullrequest:updated", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "wip", "state": "OPEN", "draft": true,
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.delete")
	_, err := fixture.loadMessage(t, "acme/widget", 42)
	assert.Error(t, err, "converting to draft removes the stored row")
}

func TestBitbucketIntegration_RejectsUnsigned(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})

	status, _ := request(t, fixture.server, http.MethodPost, "/webhook/bitbucket", `{"pullrequest":{"id":1}}`,
		map[string]string{"X-Event-Key": "pullrequest:created"})

	assert.Equal(t, http.StatusUnauthorized, status)
}

// Provider selection: a bitbucket deployment serves /webhook/bitbucket and NOT
// /webhook/github.
func TestBitbucketIntegration_HasNoGitHubRoute(t *testing.T) {
	fixture := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})

	status, _ := request(t, fixture.server, http.MethodPost, "/webhook/github", `{}`, nil)

	assert.Equal(t, http.StatusNotFound, status)
}

// The mirror: a github deployment serves /webhook/github and NOT /webhook/bitbucket.
func TestWire_GitHubHasNoBitbucketRoute(t *testing.T) {
	server := serve(t, newTestConfig(t))

	status, _ := request(t, server, http.MethodPost, "/webhook/bitbucket", `{}`, nil)

	assert.Equal(t, http.StatusNotFound, status)
}
