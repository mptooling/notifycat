package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

type fakeDispatcher struct {
	event  kernel.Event
	called bool
	err    error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, e kernel.Event) error {
	f.event = e
	f.called = true
	return f.err
}

var _ domain.EventDispatcher = (*fakeDispatcher)(nil)

func TestGitHubHandler_HappyPath(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewGitHubHandler(dispatcher)

	body := strings.NewReader(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 7, "title": "x", "html_url": "u", "user": {"login": "a"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher not called")
	}
	if dispatcher.event.GitHubEvent != "pull_request" {
		t.Errorf("GitHubEvent = %q; want %q", dispatcher.event.GitHubEvent, "pull_request")
	}
	if dispatcher.event.Action != "opened" {
		t.Errorf("Action = %q; want %q", dispatcher.event.Action, "opened")
	}
	if dispatcher.event.PR.Number != 7 {
		t.Errorf("PR.Number = %d; want 7", dispatcher.event.PR.Number)
	}
	if dispatcher.event.Repository != "octo/widget" {
		t.Errorf("Repository = %q; want %q", dispatcher.event.Repository, "octo/widget")
	}
}

func TestGitHubHandler_MissingPRReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewGitHubHandler(dispatcher)

	body := strings.NewReader(`{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
	if dispatcher.called {
		t.Error("dispatcher invoked despite missing PR")
	}
}

func TestGitHubHandler_InvalidJSONReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewGitHubHandler(dispatcher)

	body := strings.NewReader("not-json")
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
	if dispatcher.called {
		t.Error("dispatcher invoked despite invalid JSON")
	}
}

func TestGitHubHandler_DispatchErrorReturns500(t *testing.T) {
	dispatcher := &fakeDispatcher{err: context.DeadlineExceeded}
	h := infrastructure.NewGitHubHandler(dispatcher)

	body := strings.NewReader(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 7, "title": "x", "html_url": "u", "user": {"login": "a"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500", rec.Code)
	}
}

func TestGitHubHandler_XGitHubEventHeaderMapped(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewGitHubHandler(dispatcher)

	body := strings.NewReader(`{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 3, "title": "feat", "html_url": "u", "user": {"login": "alice"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	req.Header.Set("X-GitHub-Event", "pull_request_review")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if dispatcher.event.GitHubEvent != "pull_request_review" {
		t.Errorf("GitHubEvent = %q; want %q", dispatcher.event.GitHubEvent, "pull_request_review")
	}
}
