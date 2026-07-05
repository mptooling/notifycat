package infrastructure

import (
	"errors"
	"testing"
	"time"
)

func TestParseBitbucketPayload_AllFields(t *testing.T) {
	body := []byte(`{
		"actor": {"type": "user", "display_name": "Jane", "nickname": "jane"},
		"pullrequest": {
			"id": 42, "title": "Fix", "description": "body text",
			"state": "OPEN", "draft": false,
			"created_on": "2026-06-05T14:04:00.000000+00:00",
			"links": {"html": {"href": "https://bitbucket.org/ws/repo/pull-requests/42"}},
			"author": {"display_name": "Bob", "type": "user"}
		},
		"repository": {"full_name": "workspace/repo"}
	}`)

	p, err := parseBitbucketPayload(body)
	if err != nil {
		t.Fatalf("parseBitbucketPayload: %v", err)
	}
	if p.Repository != "workspace/repo" {
		t.Errorf("Repository = %q; want workspace/repo", p.Repository)
	}
	if p.PullRequest.ID != 42 {
		t.Errorf("ID = %d; want 42", p.PullRequest.ID)
	}
	if p.PullRequest.Title != "Fix" {
		t.Errorf("Title = %q; want Fix", p.PullRequest.Title)
	}
	if p.PullRequest.URL != "https://bitbucket.org/ws/repo/pull-requests/42" {
		t.Errorf("URL = %q", p.PullRequest.URL)
	}
	if p.PullRequest.Author != "Bob" {
		t.Errorf("Author = %q; want Bob", p.PullRequest.Author)
	}
	if p.PullRequest.State != "OPEN" {
		t.Errorf("State = %q; want OPEN", p.PullRequest.State)
	}
	if p.PullRequest.Draft {
		t.Error("Draft = true; want false")
	}
	if p.PullRequest.Description != "body text" {
		t.Errorf("Description = %q; want body text", p.PullRequest.Description)
	}
	want := time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)
	if !p.PullRequest.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v; want %v", p.PullRequest.CreatedAt, want)
	}
	if p.Actor.DisplayName != "Jane" || p.Actor.Type != "user" {
		t.Errorf("Actor = %+v; want {Jane user}", p.Actor)
	}
}

func TestParseBitbucketPayload_CreatedOnMalformedIsZero(t *testing.T) {
	body := []byte(`{
		"pullrequest": {"id": 42, "title": "x", "state": "OPEN", "created_on": "not-a-time"},
		"repository": {"full_name": "w/r"}
	}`)

	p, err := parseBitbucketPayload(body)
	if err != nil {
		t.Fatalf("parseBitbucketPayload: %v", err)
	}
	if !p.PullRequest.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v; want zero for a malformed timestamp", p.PullRequest.CreatedAt)
	}
}

func TestParseBitbucketPayload_MissingIDIsError(t *testing.T) {
	body := []byte(`{"repository":{"full_name":"w/r"},"pullrequest":{"title":"x"}}`)

	_, err := parseBitbucketPayload(body)
	if err == nil {
		t.Fatal("parseBitbucketPayload(missing id) returned nil; want error")
	}
	if !errors.Is(err, ErrMissingPRNumber) {
		t.Errorf("err = %v; want errors.Is(err, ErrMissingPRNumber)", err)
	}
}

func TestParseBitbucketPayload_InvalidJSONIsError(t *testing.T) {
	_, err := parseBitbucketPayload([]byte("not-json"))
	if err == nil {
		t.Fatal("parseBitbucketPayload(invalid) returned nil; want error")
	}
}

func TestToBitbucketEvent_MergedFromState(t *testing.T) {
	p, err := parseBitbucketPayload([]byte(`{
		"pullrequest": {"id": 7, "state": "MERGED"},
		"repository": {"full_name": "w/r"}
	}`))
	if err != nil {
		t.Fatalf("parseBitbucketPayload: %v", err)
	}
	event := toBitbucketEvent("pullrequest:fulfilled", p)
	if !event.PR.Merged {
		t.Error("PR.Merged = false; want true for state MERGED")
	}
}
