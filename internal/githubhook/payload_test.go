package githubhook_test

import (
	"testing"

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
