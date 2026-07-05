package runtime_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

	server := buildTestServer(t, cfg)
	ts := httptest.NewServer(server.Handler)
	t.Cleanup(ts.Close)

	return &integrationFixture{server: ts, cfg: cfg, slack: slack}
}

// postBitbucket sends a Bitbucket-signed delivery (X-Hub-Signature: sha256=<hmac>
// over the raw body, plus the X-Event-Key) to /webhook/bitbucket and returns the
// HTTP status.
func (f *integrationFixture) postBitbucket(t *testing.T, eventKey, payload string) int {
	t.Helper()
	body := []byte(payload)
	mac := hmac.New(sha256.New, []byte("itsecret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/webhook/bitbucket", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", sig)
	req.Header.Set("X-Event-Key", eventKey)
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestBitbucketIntegration_OpenedPR(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := f.postBitbucket(t, "pullrequest:created", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {
			"id": 42, "title": "fix", "state": "OPEN", "draft": false,
			"links": {"html": {"href": "https://bitbucket.org/acme/widget/pull-requests/42"}},
			"author": {"display_name": "Bob", "type": "user"},
			"created_on": "2026-06-05T14:04:00.000000+00:00"
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.postMessage") {
		t.Errorf("chat.postMessage not called; calls = %v", f.slack.methods())
	}
	section := blockText(f.postedBody(t), "section")
	if !strings.Contains(section, "@alice, please review") ||
		!strings.Contains(section, "<https://bitbucket.org/acme/widget/pull-requests/42|PR #42: fix>") {
		t.Errorf("headline section wrong: %q", section)
	}
	saved, err := f.loadMessage(t, "acme/widget", 42)
	if err != nil || saved.MessageID == "" {
		t.Fatalf("stored message missing after open: %v", err)
	}
}

func TestBitbucketIntegration_Approved(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	f.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := f.postBitbucket(t, "pullrequest:approved", `{
		"actor": {"type": "user", "display_name": "Rev"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "fix", "state": "OPEN",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "white_check_mark" {
		t.Errorf("reaction name = %v; want white_check_mark", call.Body["name"])
	}
}

func TestBitbucketIntegration_Merged(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	f.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := f.postBitbucket(t, "pullrequest:fulfilled", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "fix", "state": "MERGED",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.update") {
		t.Errorf("chat.update not called; calls = %v", f.slack.methods())
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "twisted_rightwards_arrows" {
		t.Errorf("reaction name = %v; want twisted_rightwards_arrows", call.Body["name"])
	}
}

func TestBitbucketIntegration_ConvertedToDraftDeletes(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})
	f.seedMessage(t, "acme/widget", 42, "prev-ts")

	status := f.postBitbucket(t, "pullrequest:updated", `{
		"actor": {"type": "user", "display_name": "Bob"},
		"repository": {"full_name": "acme/widget"},
		"pullrequest": {"id": 42, "title": "wip", "state": "OPEN", "draft": true,
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.delete") {
		t.Errorf("chat.delete not called; calls = %v", f.slack.methods())
	}
	if _, err := f.loadMessage(t, "acme/widget", 42); err == nil {
		t.Errorf("stored message row should be deleted after convert-to-draft")
	}
}

// TestBitbucketIntegration_RejectsUnsigned confirms an unsigned Bitbucket
// delivery (no X-Hub-Signature) is rejected 401 by the middleware.
func TestBitbucketIntegration_RejectsUnsigned(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/webhook/bitbucket", strings.NewReader(`{"pullrequest":{"id":1}}`))
	req.Header.Set("X-Event-Key", "pullrequest:created")
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 for an unsigned delivery", resp.StatusCode)
	}
}

// TestBitbucketIntegration_HasNoGitHubRoute confirms provider selection: a
// bitbucket deployment serves /webhook/bitbucket and NOT /webhook/github.
func TestBitbucketIntegration_HasNoGitHubRoute(t *testing.T) {
	f := newBitbucketFixture(t, mappingSeed{repository: "acme/widget", channel: "C123ABCDE"})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/webhook/github", strings.NewReader(`{}`))
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d; want 404 (github route must be absent on a bitbucket deployment)", resp.StatusCode)
	}
}

// TestWire_GitHubHasNoBitbucketRoute is the mirror: a github deployment serves
// /webhook/github and NOT /webhook/bitbucket.
func TestWire_GitHubHasNoBitbucketRoute(t *testing.T) {
	cfg := newTestConfig(t) // github (default)
	server := buildTestServer(t, cfg)
	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/bitbucket", strings.NewReader(`{}`))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d; want 404 (bitbucket route must be absent on a github deployment)", resp.StatusCode)
	}
}
