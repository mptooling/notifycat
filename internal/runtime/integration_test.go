package runtime_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
)

// mappingSeed describes one explicit org/repo entry the integration fixture
// should bake into the YAML (and the lock) before Wire runs. Mentions may
// be nil.
type mappingSeed struct {
	repository string
	channel    string
	mentions   []string
}

// slackFake records every API call made by the wired notifycat server. The
// integration tests assert on the recorded calls rather than mocking individual
// pieces.
type slackFake struct {
	*httptest.Server
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Method string
	Path   string
	Body   map[string]any
	Query  map[string][]string
}

func newSlackFake(t *testing.T) *slackFake {
	t.Helper()

	fake := &slackFake{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		fake.mu.Lock()
		fake.calls = append(fake.calls, fakeCall{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
			Query:  r.URL.Query(),
		})
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/chat.postMessage":
			_, _ = io.WriteString(w, `{"ok":true,"ts":"ts-1700000001"}`)
		case "/api/reactions.get":
			_, _ = io.WriteString(w, `{"ok":true,"message":{"reactions":[]}}`)
		case "/api/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOTTEST"}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// paths lists the API paths the server called, in order.
func (f *slackFake) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, len(f.calls))
	for i, call := range f.calls {
		paths[i] = call.Path
	}
	return paths
}

func (f *slackFake) findCall(path string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call.Path == path {
			return call, true
		}
	}
	return fakeCall{}, false
}

// integrationFixture is the wired app + a Slack fake + a pre-seeded mapping.
type integrationFixture struct {
	server *httptest.Server
	cfg    config.Config
	slack  *slackFake
}

func newIntegrationFixture(t *testing.T, seeds ...mappingSeed) *integrationFixture {
	return newIntegrationFixtureCfg(t, nil, seeds...)
}

// newIntegrationFixtureCfg is the same as newIntegrationFixture but invokes
// mutate(cfg) just before buildTestServer — used by tests that need to flip a flag
// (e.g. IgnoreAIReviews) without duplicating fixture setup.
func newIntegrationFixtureCfg(t *testing.T, mutate func(*config.Config), seeds ...mappingSeed) *integrationFixture {
	t.Helper()

	slack := newSlackFake(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	cfg := config.Config{
		Addr:                ":0",
		LogLevel:            "error",
		LogFormat:           "text",
		DatabaseURL:         "file:" + filepath.Join(dir, "int.db"),
		ConfigFile:          configPath,
		Mappings:            seedsToMappings(t, seeds),
		MessageTTLDays:      30,
		DependabotFormat:    true,
		GitHubWebhookSecret: config.Secret("itsecret"),
		SlackBotToken:       config.Secret("xoxb-int"),
		SlackBaseURL:        slack.URL,
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
	primeLock(t, configPath, routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest))

	if mutate != nil {
		mutate(&cfg)
	}

	server := httptest.NewServer(buildTestServer(t, cfg).Handler)
	t.Cleanup(server.Close)

	return &integrationFixture{server: server, cfg: cfg, slack: slack}
}

// seedsToMappings converts the seed slice into a map[string]routingdomain.Org
// suitable for cfg.Mappings. Seeds sharing an org are merged; if two seeds in
// the same org have different channels the second wins (write your tests
// accordingly).
func seedsToMappings(t *testing.T, seeds []mappingSeed) map[string]routingdomain.Org {
	t.Helper()

	mappings := map[string]routingdomain.Org{}
	for _, seed := range seeds {
		org, repo, ok := splitRepository(seed.repository)
		require.Truef(t, ok, "seed repository %q must be org/repo", seed.repository)

		orgTiers := mappings[org]
		if orgTiers == nil {
			orgTiers = make(routingdomain.Org)
		}
		repoConfig := routingdomain.RepoConfig{Channel: seed.channel}
		if seed.mentions != nil {
			repoConfig.Mentions = seed.mentions
			repoConfig.MentionsPresent = true
		}
		orgTiers[repo] = repoConfig
		mappings[org] = orgTiers
	}
	return mappings
}

func splitRepository(repository string) (org, repo string, ok bool) {
	slash := strings.IndexByte(repository, '/')
	if slash < 1 || slash == len(repository)-1 {
		return "", "", false
	}
	return repository[:slash], repository[slash+1:], true
}

// primeLock writes a lock file whose hashes match the provider's entries, so
// startup validation finds nothing to revalidate. The integration suite is
// testing post-startup behavior, not validation.
func primeLock(t *testing.T, configPath string, provider *routingapp.Provider) {
	t.Helper()

	now := time.Now()
	lock := routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{}}
	for _, entry := range provider.Entries() {
		lock.Entries[entry.Key()] = routinginfra.LockEntry{SHA256: entry.Hash(), ValidatedAt: now}
	}
	require.NoError(t, routinginfra.WriteLock(routinginfra.LockPath(configPath), lock))
}

// openFixtureDB opens the fixture's database and closes it when the test ends.
func (f *integrationFixture) openFixtureDB(t *testing.T) *persistence.PullRequests {
	t.Helper()

	db, err := persistence.Open(f.cfg.DatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, closeErr := persistence.SQLDB(db); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return persistence.NewPullRequests(db)
}

// seedMessage seeds one stored message for a PR in the repo's mapped channel so
// the close/draft/review handlers have something to act on. All integration
// seeds target octo/widget → C123ABCDE.
func (f *integrationFixture) seedMessage(t *testing.T, repository string, prNumber int, timestamp string) {
	t.Helper()

	err := f.openFixtureDB(t).AddMessage(context.Background(), repository, prNumber, "C123ABCDE", timestamp)
	require.NoError(t, err)
}

// post sends a JSON payload to /webhook/github with a valid HMAC signature and
// returns the HTTP status.
func (f *integrationFixture) post(t *testing.T, payload string) int {
	t.Helper()

	return f.postEvent(t, "", payload)
}

func (f *integrationFixture) postEvent(t *testing.T, event, payload string) int {
	t.Helper()

	headers := map[string]string{
		"Content-Type":        "application/json",
		"X-Hub-Signature-256": githubSignature("itsecret", payload),
	}
	if event != "" {
		headers["X-GitHub-Event"] = event
	}
	status, _ := request(t, f.server, http.MethodPost, "/webhook/github", payload, headers)
	return status
}

// loadMessage returns the first stored message for a PR, or ErrNotFound when
// the PR has no stored messages — used to verify the stored message post-flow.
func (f *integrationFixture) loadMessage(t *testing.T, repository string, prNumber int) (persistence.Message, error) {
	t.Helper()

	messages, err := f.openFixtureDB(t).Messages(context.Background(), repository, prNumber)
	if err != nil {
		return persistence.Message{}, err
	}
	if len(messages) == 0 {
		return persistence.Message{}, persistence.ErrNotFound
	}
	return messages[0], nil
}

// postedBody returns the JSON body of the first chat.postMessage call.
func (f *integrationFixture) postedBody(t *testing.T) map[string]any {
	t.Helper()

	return f.requireCall(t, "/api/chat.postMessage").Body
}

// postedText returns the rendered headline (the first section block's text) of
// the first chat.postMessage call — the visible message line carrying the
// leading emoji and the linked title.
func (f *integrationFixture) postedText(t *testing.T) string {
	t.Helper()

	return blockText(f.postedBody(t), "section")
}

// requireCall returns the first call to path, failing the test when absent.
func (f *integrationFixture) requireCall(t *testing.T, path string) fakeCall {
	t.Helper()

	call, ok := f.slack.findCall(path)
	require.Truef(t, ok, "%s was never called; calls = %v", path, f.slack.paths())
	return call
}

// requireReaction returns the emoji of the first reactions.add call.
func (f *integrationFixture) requireReaction(t *testing.T) any {
	t.Helper()

	return f.requireCall(t, "/api/reactions.add").Body["name"]
}

// blockText returns the text of the first block of the given type ("section"
// or "context") in a posted Slack message body, or "" if absent.
func blockText(body map[string]any, blockType string) string {
	blocks, _ := body["blocks"].([]any)
	for _, block := range blocks {
		fields, ok := block.(map[string]any)
		if !ok || fields["type"] != blockType {
			continue
		}
		if text, ok := fields["text"].(map[string]any); ok { // section
			rendered, _ := text["text"].(string)
			return rendered
		}
		if elements, ok := fields["elements"].([]any); ok && len(elements) > 0 { // context
			if element, ok := elements[0].(map[string]any); ok {
				rendered, _ := element["text"].(string)
				return rendered
			}
		}
	}
	return ""
}

func TestIntegration_OpenedPR(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := fixture.post(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "https://gh/octo/widget/pull/42",
			"user": {"login": "bob"}, "draft": false,
			"created_at": "2026-06-05T14:04:00Z"
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.postMessage")

	// Block Kit shape, threaded end-to-end through the real HTTP pipeline:
	// a headline section keeps the mention and linked title; a context line
	// carries repo, author, and the localized open-time token; and a top-level
	// text fallback is sent alongside the blocks.
	body := fixture.postedBody(t)
	section := blockText(body, "section")
	assert.Contains(t, section, ":rocket:")
	assert.Contains(t, section, "@alice, please review")
	assert.Contains(t, section, "<https://gh/octo/widget/pull/42|PR #42: fix>")

	context := blockText(body, "context")
	assert.Contains(t, context, "octo/widget · bob · opened ")
	assert.Contains(t, context, "<!date^")
	assert.NotEmpty(t, body["text"], "the posted message carries a text fallback")

	saved, err := fixture.loadMessage(t, "octo/widget", 42)
	require.NoError(t, err)
	assert.NotEmpty(t, saved.MessageID)
}

func TestIntegration_ClosedMerged(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "closed",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u",
			"user": {"login": "bob"}, "merged": true
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.update")
	assert.Equal(t, "twisted_rightwards_arrows", fixture.requireReaction(t))
}

func TestIntegration_ConvertedToDraft(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "converted_to_draft",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "wip", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/chat.delete")
	_, err := fixture.loadMessage(t, "octo/widget", 42)
	assert.Error(t, err, "the stored message row is deleted along with the Slack message")
}

func TestIntegration_ReviewApproved(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "white_check_mark", fixture.requireReaction(t))
}

func TestIntegration_ReviewCommented(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "commented"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "speech_balloon", fixture.requireReaction(t))
}

func TestIntegration_PullRequestReviewLineComment(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.postEvent(t, "pull_request_review_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}},
		"comment": {"body": "line comment"}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "speech_balloon", fixture.requireReaction(t))
}

func TestIntegration_IssueCommentOnPR(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.postEvent(t, "issue_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 42, "pull_request": {"url": "https://api.github.com/repos/octo/widget/pulls/42"}},
		"comment": {"body": "conversation comment"}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "speech_balloon", fixture.requireReaction(t))
}

func TestIntegration_IssueCommentOnPlainIssueIsIgnored(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.postEvent(t, "issue_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 42},
		"comment": {"body": "plain issue comment"}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, fixture.slack.paths(), "/api/reactions.add", "a plain issue is not a PR")
}

func TestIntegration_ReviewRequestChange(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "changes_requested"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "exclamation", fixture.requireReaction(t))
}

func TestIntegration_DependabotRoutinePR(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := fixture.post(t, `{
		"action": "opened",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump acme/lib from 1.2.0 to 1.2.1",
			"html_url": "https://gh/octo/widget/pull/42",
			"body": "Bumps acme/lib from 1.2.0 to 1.2.1.\n\n## Release notes\n\n- A change.",
			"user": {"login": "dependabot[bot]"}, "draft": false
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	text := fixture.postedText(t)
	assert.Contains(t, text, ":package:")
	assert.Contains(t, text, "dependabot bumped")
	assert.Contains(t, text, "bump acme/lib from 1.2.0 to 1.2.1")
	assert.NotContains(t, text, "please review", "a routine bot PR does not ask for review")
}

func TestIntegration_DependabotSecurityPR(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})

	status := fixture.post(t, `{
		"action": "opened",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump acme/lib from 1.2.0 to 1.2.1",
			"html_url": "u",
			"body": "Bumps acme/lib.\n\n## Vulnerabilities fixed\n\nSourced from the GitHub Security Advisory Database.\n\nCVE-2026-1234.",
			"user": {"login": "dependabot[bot]"}, "draft": false
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	text := fixture.postedText(t)
	assert.Contains(t, text, ":rotating_light:")
	assert.Contains(t, text, "dependabot security update")
}

func TestIntegration_RenovateRoutinePR(t *testing.T) {
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})

	status := fixture.post(t, `{
		"action": "opened",
		"sender": {"login": "renovate[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "Update acme/lib to v2", "html_url": "u",
			"user": {"login": "renovate[bot]"}, "draft": false
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	text := fixture.postedText(t)
	assert.Contains(t, text, ":package:")
	assert.Contains(t, text, "renovate bumped")
}

func TestIntegration_DependabotFormatDisabled(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		func(cfg *config.Config) { cfg.DependabotFormat = false },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE"},
	)

	status := fixture.post(t, `{
		"action": "opened",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump acme/lib", "html_url": "u",
			"user": {"login": "dependabot[bot]"}, "draft": false
		}
	}`)

	require.Equal(t, http.StatusOK, status)
	text := fixture.postedText(t)
	assert.Contains(t, text, "please review", "with the format off a bot PR posts like any other")
	assert.NotContains(t, text, ":package:")
}

func TestIntegration_IgnoreAIReviews_BotReviewSuppressesReaction(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		func(cfg *config.Config) { cfg.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE"},
	)
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "copilot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, fixture.slack.paths(), "/api/reactions.add")
}

func TestIntegration_IgnoreAIReviews_BotLineCommentSuppressesReaction(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		func(cfg *config.Config) { cfg.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE"},
	)
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.postEvent(t, "pull_request_review_comment", `{
		"action": "created",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.NotContains(t, fixture.slack.paths(), "/api/reactions.add")
}

func TestIntegration_IgnoreAIReviews_HumanReviewerStillReacts(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		func(cfg *config.Config) { cfg.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE"},
	)
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "alice", "type": "User"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/reactions.add")
}

func TestIntegration_IgnoreAIReviewsDisabled_BotReviewerStillReacts(t *testing.T) {
	// The flag defaults to false — bot reviewers behave exactly like humans.
	fixture := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE"})
	fixture.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := fixture.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "github-actions[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, fixture.slack.paths(), "/api/reactions.add")
}
