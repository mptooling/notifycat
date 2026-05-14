package githubhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/githubhook"
)

func TestHandler_HappyPath(t *testing.T) {
	var got githubhook.Payload
	h := githubhook.NewHandler(func(_ context.Context, p githubhook.Payload) error {
		got = p
		return nil
	})

	body := strings.NewReader(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 7, "title": "x", "html_url": "u", "user": {"login": "a"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if got.Action != "opened" || got.Repository != "octo/widget" || got.PullRequest.Number != 7 {
		t.Errorf("payload = %+v", got)
	}
}

func TestHandler_MissingPRReturns400(t *testing.T) {
	h := githubhook.NewHandler(func(context.Context, githubhook.Payload) error {
		t.Fatal("sink invoked despite missing PR")
		return nil
	})

	body := strings.NewReader(`{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestHandler_InvalidJSONReturns400(t *testing.T) {
	h := githubhook.NewHandler(func(context.Context, githubhook.Payload) error {
		t.Fatal("sink invoked despite invalid JSON")
		return nil
	})

	body := strings.NewReader("not-json")
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}
