package app_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/app"
	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/store"
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
	f := &slackFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
			Query:  r.URL.Query(),
		})
		f.mu.Unlock()

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
	t.Cleanup(f.Close)
	return f
}

func (f *slackFake) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Path
	}
	return out
}

func (f *slackFake) findCall(path string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Path == path {
			return c, true
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
// mutate(cfg) just before app.Wire — used by tests that need to flip a flag
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
	primeLock(t, configPath, mappings.NewProvider(mappings.Defaults{}, cfg.Mappings, cfg.Digest))

	if mutate != nil {
		mutate(&cfg)
	}

	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	t.Cleanup(cleanup)

	ts := httptest.NewServer(server.Handler)
	t.Cleanup(ts.Close)

	return &integrationFixture{server: ts, cfg: cfg, slack: slack}
}

// seedsToMappings converts the seed slice into a map[string]mappings.Org
// suitable for cfg.Mappings. Seeds sharing an org are merged; if two seeds in
// the same org have different channels the second wins (write your tests
// accordingly).
func seedsToMappings(t *testing.T, seeds []mappingSeed) map[string]mappings.Org {
	t.Helper()
	m := map[string]mappings.Org{}
	for _, s := range seeds {
		org, repo, ok := splitRepository(s.repository)
		if !ok {
			t.Fatalf("seed repository %q must be org/repo", s.repository)
		}
		orgTiers := m[org]
		if orgTiers == nil {
			orgTiers = make(mappings.Org)
		}
		repoConfig := mappings.RepoConfig{Channel: s.channel}
		if s.mentions != nil {
			repoConfig.Mentions = s.mentions
			repoConfig.MentionsPresent = true
		}
		orgTiers[repo] = repoConfig
		m[org] = orgTiers
	}
	return m
}

func splitRepository(s string) (org, repo string, ok bool) {
	i := strings.IndexByte(s, '/')
	if i < 1 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// primeLock writes a lock file whose hashes match the provider's entries, so
// startup validation finds nothing to revalidate. The integration suite is
// testing post-startup behavior, not validation.
func primeLock(t *testing.T, configPath string, p *mappings.Provider) {
	t.Helper()
	now := time.Now()
	lock := mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	for _, e := range p.Entries() {
		lock.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: now}
	}
	if err := mappings.WriteLock(mappings.LockPath(configPath), lock); err != nil {
		t.Fatalf("prime: write lock: %v", err)
	}
}

// seedMessage seeds one stored message for a PR.
func (f *integrationFixture) seedMessage(t *testing.T, repository string, prNumber int, ts string) {
	t.Helper()
	db, err := store.Open(f.cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()
	// Seed one stored message in the repo's mapped channel so the
	// close/draft/review handlers (which read stored messages) have something
	// to act on. All integration seeds target octo/widget → C123ABCDE.
	if err := store.NewPullRequests(db).AddMessage(context.Background(), repository, prNumber, "C123ABCDE", ts); err != nil {
		t.Fatalf("seed AddMessage: %v", err)
	}
}

// post sends a JSON payload to /webhook/github with a valid HMAC signature
// and returns the HTTP status. The response body is drained and closed inline
// so callers never have to track it.
func (f *integrationFixture) post(t *testing.T, payload string) int {
	return f.postEvent(t, "", payload)
}

func (f *integrationFixture) postEvent(t *testing.T, event, payload string) int {
	t.Helper()
	body := []byte(payload)
	mac := hmac.New(sha256.New, []byte("itsecret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/webhook/github", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

// loadMessage returns the first stored message for a PR, or ErrNotFound when
// the PR has no stored messages — used to verify the stored message post-flow.
func (f *integrationFixture) loadMessage(t *testing.T, repository string, prNumber int) (store.Message, error) {
	t.Helper()
	db, err := store.Open(f.cfg.DatabaseURL)
	if err != nil {
		return store.Message{}, err
	}
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()
	msgs, err := store.NewPullRequests(db).Messages(context.Background(), repository, prNumber)
	if err != nil {
		return store.Message{}, err
	}
	return msgs[0], nil
}

// ---------- the 6 event-type tests ----------

func TestIntegration_OpenedPR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := f.post(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "https://gh/octo/widget/pull/42",
			"user": {"login": "bob"}, "draft": false,
			"created_at": "2026-06-05T14:04:00Z"
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.postMessage") {
		t.Errorf("chat.postMessage not called; calls = %v", f.slack.methods())
	}

	// Block Kit shape, threaded end-to-end through the real HTTP pipeline:
	// a headline section keeps the mention and linked title; a context line
	// carries repo, author, and the localized open-time token; and a top-level
	// text fallback is sent alongside the blocks.
	body := f.postedBody(t)
	section := blockText(body, "section")
	if !strings.Contains(section, ":rocket:") || !strings.Contains(section, "@alice, please review") ||
		!strings.Contains(section, "<https://gh/octo/widget/pull/42|PR #42: fix>") {
		t.Errorf("headline section wrong: %q", section)
	}
	ctx := blockText(body, "context")
	if !strings.Contains(ctx, "octo/widget · bob · opened ") || !strings.Contains(ctx, "<!date^") {
		t.Errorf("context line did not carry repo/author/localized time: %q", ctx)
	}
	if fallback, _ := body["text"].(string); fallback == "" {
		t.Error("posted message has no top-level text fallback")
	}

	saved, err := f.loadMessage(t, "octo/widget", 42)
	if err != nil {
		t.Fatalf("loadMessage: %v", err)
	}
	if saved.MessageID == "" {
		t.Errorf("saved message id is empty")
	}
}

func TestIntegration_ClosedMerged(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "closed",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u",
			"user": {"login": "bob"}, "merged": true
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.update") {
		t.Errorf("chat.update not called; calls = %v", f.slack.methods())
	}
	if !contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add not called (reactions enabled); calls = %v", f.slack.methods())
	}
	if call, ok := f.slack.findCall("/api/reactions.add"); ok {
		if call.Body["name"] != "twisted_rightwards_arrows" {
			t.Errorf("reactions.add name = %v; want twisted_rightwards_arrows", call.Body["name"])
		}
	}
}

func TestIntegration_ConvertedToDraft(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "converted_to_draft",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "wip", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.delete") {
		t.Errorf("chat.delete not called; calls = %v", f.slack.methods())
	}
	if _, err := f.loadMessage(t, "octo/widget", 42); err == nil {
		t.Errorf("slack_messages row should be deleted")
	}
}

func TestIntegration_ReviewApproved(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
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

func TestIntegration_ReviewCommented(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "commented"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "speech_balloon" {
		t.Errorf("reaction name = %v; want speech_balloon", call.Body["name"])
	}
}

func TestIntegration_PullRequestReviewLineComment(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.postEvent(t, "pull_request_review_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}},
		"comment": {"body": "line comment"}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "speech_balloon" {
		t.Errorf("reaction name = %v; want speech_balloon", call.Body["name"])
	}
}

func TestIntegration_IssueCommentOnPR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.postEvent(t, "issue_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 42, "pull_request": {"url": "https://api.github.com/repos/octo/widget/pulls/42"}},
		"comment": {"body": "conversation comment"}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "speech_balloon" {
		t.Errorf("reaction name = %v; want speech_balloon", call.Body["name"])
	}
}

func TestIntegration_IssueCommentOnPlainIssueIsIgnored(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.postEvent(t, "issue_comment", `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 42},
		"comment": {"body": "plain issue comment"}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add called for plain-issue comment; methods = %v", f.slack.methods())
	}
}

func TestIntegration_ReviewRequestChange(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "changes_requested"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	call, ok := f.slack.findCall("/api/reactions.add")
	if !ok {
		t.Fatalf("reactions.add not called; methods = %v", f.slack.methods())
	}
	if call.Body["name"] != "exclamation" {
		t.Errorf("reaction name = %v; want exclamation", call.Body["name"])
	}
}

// ---------- Dependabot / Renovate compact format ----------

// postedBody returns the JSON body of the first chat.postMessage call.
func (f *integrationFixture) postedBody(t *testing.T) map[string]any {
	t.Helper()
	call, ok := f.slack.findCall("/api/chat.postMessage")
	if !ok {
		t.Fatalf("chat.postMessage not called; methods = %v", f.slack.methods())
	}
	return call.Body
}

// postedText returns the rendered headline (the first section block's text) of
// the first chat.postMessage call — the visible message line carrying the
// leading emoji and the linked title.
func (f *integrationFixture) postedText(t *testing.T) string {
	t.Helper()
	return blockText(f.postedBody(t), "section")
}

// blockText returns the text of the first block of the given type ("section"
// or "context") in a posted Slack message body, or "" if absent.
func blockText(body map[string]any, blockType string) string {
	blocks, _ := body["blocks"].([]any)
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok || bm["type"] != blockType {
			continue
		}
		if txt, ok := bm["text"].(map[string]any); ok { // section
			s, _ := txt["text"].(string)
			return s
		}
		if els, ok := bm["elements"].([]any); ok && len(els) > 0 { // context
			if em, ok := els[0].(map[string]any); ok {
				s, _ := em["text"].(string)
				return s
			}
		}
	}
	return ""
}

func TestIntegration_DependabotRoutinePR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := f.post(t, `{
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

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	text := f.postedText(t)
	for _, want := range []string{":package:", "dependabot bumped", "bump acme/lib from 1.2.0 to 1.2.1"} {
		if !strings.Contains(text, want) {
			t.Errorf("posted text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "please review") {
		t.Errorf("routine bot PR should not say 'please review': %q", text)
	}
}

func TestIntegration_DependabotSecurityPR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})

	status := f.post(t, `{
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

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	text := f.postedText(t)
	for _, want := range []string{":rotating_light:", "dependabot security update"} {
		if !strings.Contains(text, want) {
			t.Errorf("posted text missing %q: %q", want, text)
		}
	}
}

func TestIntegration_RenovateRoutinePR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})

	status := f.post(t, `{
		"action": "opened",
		"sender": {"login": "renovate[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "Update acme/lib to v2", "html_url": "u",
			"user": {"login": "renovate[bot]"}, "draft": false
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	text := f.postedText(t)
	if !strings.Contains(text, ":package:") || !strings.Contains(text, "renovate bumped") {
		t.Errorf("renovate routine PR text wrong: %q", text)
	}
}

func TestIntegration_DependabotFormatDisabled(t *testing.T) {
	f := newIntegrationFixtureCfg(t,
		func(c *config.Config) { c.DependabotFormat = false },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil},
	)

	status := f.post(t, `{
		"action": "opened",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump acme/lib", "html_url": "u",
			"user": {"login": "dependabot[bot]"}, "draft": false
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	text := f.postedText(t)
	if !strings.Contains(text, "please review") {
		t.Errorf("with format disabled, dependabot PR should use standard format: %q", text)
	}
	if strings.Contains(text, ":package:") {
		t.Errorf("with format disabled, compact format should not appear: %q", text)
	}
}

// ---------- bot-reviewer suppression ----------

func TestIntegration_IgnoreAIReviews_BotReviewSuppressesReaction(t *testing.T) {
	f := newIntegrationFixtureCfg(t,
		func(c *config.Config) { c.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil},
	)
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "copilot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add called for bot reviewer; methods = %v", f.slack.methods())
	}
}

func TestIntegration_IgnoreAIReviews_BotLineCommentSuppressesReaction(t *testing.T) {
	f := newIntegrationFixtureCfg(t,
		func(c *config.Config) { c.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil},
	)
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.postEvent(t, "pull_request_review_comment", `{
		"action": "created",
		"sender": {"login": "dependabot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add called for bot line-commenter; methods = %v", f.slack.methods())
	}
}

func TestIntegration_IgnoreAIReviews_HumanReviewerStillReacts(t *testing.T) {
	f := newIntegrationFixtureCfg(t,
		func(c *config.Config) { c.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil},
	)
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "alice", "type": "User"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add not called for human reviewer; methods = %v", f.slack.methods())
	}
}

func TestIntegration_IgnoreAIReviewsDisabled_BotReviewerStillReacts(t *testing.T) {
	// Flag defaults to false — bot reviewers behave exactly like humans.
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil})
	f.seedMessage(t, "octo/widget", 42, "prev-ts")

	status := f.post(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "github-actions[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/reactions.add") {
		t.Errorf("reactions.add not called for bot reviewer with flag off; methods = %v", f.slack.methods())
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
