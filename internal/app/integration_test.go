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

	"github.com/mptooling/notifycat/internal/app"
	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/store"
)

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

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	slack := newSlackFake(t)

	cfg := config.Config{
		Addr:                ":0",
		LogLevel:            "error",
		LogFormat:           "text",
		DatabaseURL:         "file:" + filepath.Join(t.TempDir(), "int.db"),
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

	server, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	t.Cleanup(cleanup)

	ts := httptest.NewServer(server.Handler)
	t.Cleanup(ts.Close)

	return &integrationFixture{server: ts, cfg: cfg, slack: slack}
}

// seedMapping inserts a repo→channel mapping directly via the store.
func (f *integrationFixture) seedMapping(t *testing.T, repository, channel string, mentions []string) {
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
	repo := store.NewRepoMappings(db)
	if _, err := repo.Upsert(context.Background(), store.RepoMapping{
		Repository:   repository,
		SlackChannel: channel,
		Mentions:     mentions,
	}); err != nil {
		t.Fatalf("seed Upsert: %v", err)
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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", []string{"@alice"})

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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", []string{"@alice"})
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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", []string{"@alice"})
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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", nil)
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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", nil)
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
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", nil)
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

func TestIntegration_ReviewRequestChange(t *testing.T) {
	f := newIntegrationFixture(t)
	f.seedMapping(t, "octo/widget", "C123ABCDE", nil)
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

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
