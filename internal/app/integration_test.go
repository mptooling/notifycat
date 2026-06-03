package app_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	mappingsPath := filepath.Join(dir, "mappings.yaml")
	writeSeedYAML(t, mappingsPath, seeds)
	primeLock(t, mappingsPath)

	cfg := config.Config{
		Addr:                ":0",
		LogLevel:            "error",
		LogFormat:           "text",
		DatabaseURL:         "file:" + filepath.Join(dir, "int.db"),
		MappingsFile:        mappingsPath,
		MessageTTLDays:      30,
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

	if mutate != nil {
		mutate(&cfg)
	}

	server, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	t.Cleanup(cleanup)

	ts := httptest.NewServer(server.Handler)
	t.Cleanup(ts.Close)

	return &integrationFixture{server: ts, cfg: cfg, slack: slack}
}

// writeSeedYAML renders the seed slice into a valid mappings.yaml. Seeds
// sharing an org are merged channel/mentions are not de-duplicated; if you
// pass two seeds in the same org with different channels the second wins
// (write your tests accordingly).
func writeSeedYAML(t *testing.T, path string, seeds []mappingSeed) {
	t.Helper()
	orgs := map[string]struct {
		channel  string
		mentions []string
		repos    []string
	}{}
	var order []string
	for _, s := range seeds {
		org, repo, ok := splitRepository(s.repository)
		if !ok {
			t.Fatalf("seed repository %q must be org/repo", s.repository)
		}
		entry, exists := orgs[org]
		if !exists {
			order = append(order, org)
		}
		entry.channel = s.channel
		entry.mentions = s.mentions
		entry.repos = append(entry.repos, repo)
		orgs[org] = entry
	}

	var sb strings.Builder
	sb.WriteString("mappings:\n")
	if len(order) == 0 {
		sb.WriteString("  {}\n")
	}
	for _, org := range order {
		e := orgs[org]
		fmt.Fprintf(&sb, "  %s:\n", org)
		fmt.Fprintf(&sb, "    channel: %s\n", e.channel)
		fmt.Fprintf(&sb, "    mentions: [%s]\n", quoteMentions(e.mentions))
		fmt.Fprintf(&sb, "    repositories: [%s]\n", quoteRepos(e.repos))
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("write mappings.yaml: %v", err)
	}
}

func splitRepository(s string) (org, repo string, ok bool) {
	i := strings.IndexByte(s, '/')
	if i < 1 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func quoteMentions(ms []string) string {
	if len(ms) == 0 {
		return ""
	}
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = fmt.Sprintf("%q", m)
	}
	return strings.Join(parts, ", ")
}

func quoteRepos(rs []string) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = fmt.Sprintf("%q", r)
	}
	return strings.Join(parts, ", ")
}

// primeLock writes a lock file whose hashes match the parsed entries, so
// startup validation finds nothing to revalidate. The integration suite is
// testing post-startup behavior, not validation.
func primeLock(t *testing.T, mappingsPath string) {
	t.Helper()
	p, err := mappings.Load(mappingsPath)
	if err != nil {
		t.Fatalf("prime: load mappings: %v", err)
	}
	now := time.Now()
	lock := mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	for _, e := range p.Entries() {
		lock.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: now}
	}
	if err := mappings.WriteLock(mappings.LockPath(mappingsPath), lock); err != nil {
		t.Fatalf("prime: write lock: %v", err)
	}
}

// seedSlackMessage inserts a SlackMessage row directly.
func (f *integrationFixture) seedSlackMessage(t *testing.T, repository string, prNumber int, ts string) {
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
	repo := store.NewSlackMessages(db)
	if err := repo.Save(context.Background(), store.SlackMessage{
		PRNumber: prNumber, Repository: repository, TS: ts,
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
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

// loadMessage helper used to verify the stored TS post-flow.
func (f *integrationFixture) loadMessage(t *testing.T, repository string, prNumber int) (store.SlackMessage, error) {
	t.Helper()
	db, err := store.Open(f.cfg.DatabaseURL)
	if err != nil {
		return store.SlackMessage{}, err
	}
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()
	return store.NewSlackMessages(db).Get(context.Background(), repository, prNumber)
}

// ---------- the 6 event-type tests ----------

func TestIntegration_OpenedPR(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})

	status := f.post(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "https://gh/octo/widget/pull/42",
			"user": {"login": "bob"}, "draft": false
		}
	}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !contains(f.slack.methods(), "/api/chat.postMessage") {
		t.Errorf("chat.postMessage not called; calls = %v", f.slack.methods())
	}
	saved, err := f.loadMessage(t, "octo/widget", 42)
	if err != nil {
		t.Fatalf("loadMessage: %v", err)
	}
	if saved.TS == "" {
		t.Errorf("saved TS is empty")
	}
}

func TestIntegration_ClosedMerged(t *testing.T) {
	f := newIntegrationFixture(t, mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: []string{"@alice"}})
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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

// ---------- bot-reviewer suppression ----------

func TestIntegration_IgnoreAIReviews_BotReviewSuppressesReaction(t *testing.T) {
	f := newIntegrationFixtureCfg(t,
		func(c *config.Config) { c.IgnoreAIReviews = true },
		mappingSeed{repository: "octo/widget", channel: "C123ABCDE", mentions: nil},
	)
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
	f.seedSlackMessage(t, "octo/widget", 42, "prev-ts")

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
