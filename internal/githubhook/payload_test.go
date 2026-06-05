package githubhook_test

import (
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/githubhook"
)

func TestParsePayload_PullRequestOpened(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42,
			"title": "fix",
			"html_url": "https://github.com/octo/widget/pull/42",
			"user": {"login": "alice"},
			"merged": false,
			"draft": false
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Action != "opened" || p.Repository != "octo/widget" {
		t.Errorf("action/repo = %q/%q", p.Action, p.Repository)
	}
	if p.PullRequest.Number != 42 || p.PullRequest.Title != "fix" {
		t.Errorf("PR = %+v", p.PullRequest)
	}
	if p.PullRequest.URL != "https://github.com/octo/widget/pull/42" {
		t.Errorf("URL = %q", p.PullRequest.URL)
	}
	if p.PullRequest.Author != "alice" {
		t.Errorf("Author = %q", p.PullRequest.Author)
	}
	if p.PullRequest.Draft {
		t.Error("Draft = true; want false")
	}
	if p.PullRequest.Merged {
		t.Error("Merged = true; want false")
	}
	if p.Review != nil {
		t.Errorf("Review = %+v; want nil for non-review event", p.Review)
	}
}

func TestParsePayload_PullRequestBody(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump", "html_url": "u", "user": {"login": "dependabot[bot]"},
			"body": "## Vulnerabilities fixed\n\nCVE-2026-1234."
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.PullRequest.Body != "## Vulnerabilities fixed\n\nCVE-2026-1234." {
		t.Errorf("Body = %q", p.PullRequest.Body)
	}
}

func TestParsePayload_CreatedAt(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"},
			"created_at": "2026-06-05T14:04:00Z"
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	want := time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)
	if !p.PullRequest.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v; want %v", p.PullRequest.CreatedAt, want)
	}
}

func TestParsePayload_CreatedAtMalformedIsZero(t *testing.T) {
	// A missing or unparseable created_at must not fail the webhook — the
	// notifier only uses it for a cosmetic context line, so it falls back to
	// the zero time.
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"},
			"created_at": "not-a-time"
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if !p.PullRequest.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v; want zero for a malformed timestamp", p.PullRequest.CreatedAt)
	}
}

func TestParsePayload_ReviewApproved(t *testing.T) {
	body := []byte(`{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Review == nil || p.Review.State != "approved" {
		t.Errorf("Review = %+v", p.Review)
	}
}

func TestParsePayload_Closed_Merged(t *testing.T) {
	body := []byte(`{
		"action": "closed",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u",
			"user": {"login": "alice"}, "merged": true
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Action != "closed" || !p.PullRequest.Merged {
		t.Errorf("payload = %+v", p)
	}
}

func TestParsePayload_DraftConverted(t *testing.T) {
	body := []byte(`{
		"action": "converted_to_draft",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 5, "title": "wip", "html_url": "u",
			"user": {"login": "alice"}
		}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Action != "converted_to_draft" {
		t.Errorf("Action = %q", p.Action)
	}
}

func TestParsePayload_IssueCommentOnPR(t *testing.T) {
	// issue_comment payloads carry the PR number under issue.number, and the
	// presence of issue.pull_request marks the comment as a PR conversation
	// comment rather than a plain-issue comment.
	body := []byte(`{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {
			"number": 42,
			"pull_request": {"url": "https://api.github.com/repos/octo/widget/pulls/42"}
		},
		"sender": {"login": "alice", "type": "User"}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.PullRequest.Number != 42 {
		t.Errorf("PR number = %d; want 42", p.PullRequest.Number)
	}
	if !p.PRComment {
		t.Error("PRComment = false; want true for issue_comment on a PR")
	}
}

func TestParsePayload_IssueCommentOnPlainIssue(t *testing.T) {
	// A comment on a plain issue (no issue.pull_request) must parse without
	// error so the dispatcher can ignore it with reason: no_handler, rather
	// than 400-ing every issue comment in the repo.
	body := []byte(`{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 99},
		"sender": {"login": "alice", "type": "User"}
	}`)

	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.PullRequest.Number != 0 {
		t.Errorf("PR number = %d; want 0 for a plain-issue comment", p.PullRequest.Number)
	}
	if p.PRComment {
		t.Error("PRComment = true; want false for a plain-issue comment")
	}
}

func TestParsePayload_MissingPRNumberIsError(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {}
	}`)

	_, err := githubhook.ParsePayload(body)
	if err == nil {
		t.Fatal("ParsePayload(missing PR number) returned nil; want error")
	}
}

func TestParsePayload_InvalidJSONIsError(t *testing.T) {
	_, err := githubhook.ParsePayload([]byte("not-json"))
	if err == nil {
		t.Fatal("ParsePayload(invalid) returned nil; want error")
	}
}

func TestParsePayload_SenderBot(t *testing.T) {
	body := []byte(`{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "copilot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)
	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Sender.Type != "Bot" {
		t.Errorf("Sender.Type = %q; want %q", p.Sender.Type, "Bot")
	}
	if p.Sender.Login != "copilot[bot]" {
		t.Errorf("Sender.Login = %q; want %q", p.Sender.Login, "copilot[bot]")
	}
}

func TestParsePayload_SenderUser(t *testing.T) {
	body := []byte(`{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "alice", "type": "User"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)
	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Sender.Type != "User" || p.Sender.Login != "alice" {
		t.Errorf("Sender = %+v; want {Login: alice, Type: User}", p.Sender)
	}
}

func TestParsePayload_SenderAbsentIsZeroValue(t *testing.T) {
	// Pre-existing tests omit sender; the field must remain optional and
	// parse to the zero value rather than failing.
	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"}
		}
	}`)
	p, err := githubhook.ParsePayload(body)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.Sender.Type != "" || p.Sender.Login != "" {
		t.Errorf("Sender = %+v; want zero value when omitted", p.Sender)
	}
}
