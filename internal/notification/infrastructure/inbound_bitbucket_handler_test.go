package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

func TestBitbucketHandler_HappyPath(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewBitbucketHandler(dispatcher)

	body := strings.NewReader(`{
		"actor": {"type": "user", "display_name": "Jane"},
		"pullrequest": {"id": 42, "title": "Fix", "state": "OPEN", "draft": false,
			"links": {"html": {"href": "https://bitbucket.org/ws/repo/pull-requests/42"}},
			"author": {"display_name": "Bob", "type": "user"}},
		"repository": {"full_name": "workspace/repo"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", body)
	req.Header.Set("X-Event-Key", "pullrequest:created")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher not called")
	}
	if dispatcher.event.Provider != kernel.ProviderBitbucket {
		t.Errorf("Provider = %q; want %q", dispatcher.event.Provider, kernel.ProviderBitbucket)
	}
	if dispatcher.event.Kind != kernel.KindOpened {
		t.Errorf("Kind = %v; want KindOpened", dispatcher.event.Kind)
	}
	if dispatcher.event.PR.Number != 42 {
		t.Errorf("PR.Number = %d; want 42", dispatcher.event.PR.Number)
	}
	if dispatcher.event.Repository != "workspace/repo" {
		t.Errorf("Repository = %q; want %q", dispatcher.event.Repository, "workspace/repo")
	}
}

func TestBitbucketHandler_MissingIDReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewBitbucketHandler(dispatcher)

	body := strings.NewReader(`{"repository":{"full_name":"w/r"},"pullrequest":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", body)
	req.Header.Set("X-Event-Key", "pullrequest:created")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
	if dispatcher.called {
		t.Error("dispatcher invoked despite missing PR id")
	}
}

func TestBitbucketHandler_InvalidJSONReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewBitbucketHandler(dispatcher)

	body := strings.NewReader("not-json")
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", body)
	req.Header.Set("X-Event-Key", "pullrequest:created")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
	if dispatcher.called {
		t.Error("dispatcher invoked despite invalid JSON")
	}
}

func TestBitbucketHandler_DispatchErrorReturns500(t *testing.T) {
	dispatcher := &fakeDispatcher{err: context.DeadlineExceeded}
	h := infrastructure.NewBitbucketHandler(dispatcher)

	body := strings.NewReader(`{
		"actor": {"type": "user", "display_name": "Jane"},
		"pullrequest": {"id": 42, "title": "Fix", "state": "OPEN",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}},
		"repository": {"full_name": "workspace/repo"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", body)
	req.Header.Set("X-Event-Key", "pullrequest:created")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500", rec.Code)
	}
}

func TestBitbucketHandler_XEventKeyHeaderMapped(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	h := infrastructure.NewBitbucketHandler(dispatcher)

	body := strings.NewReader(`{
		"actor": {"type": "user", "display_name": "Jane"},
		"pullrequest": {"id": 3, "title": "feat", "state": "OPEN",
			"links": {"html": {"href": "u"}}, "author": {"display_name": "Bob", "type": "user"}},
		"repository": {"full_name": "workspace/repo"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", body)
	req.Header.Set("X-Event-Key", "pullrequest:approved")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	// The X-Event-Key header drives the kind mapping: without it the adapter
	// cannot tell this is an approval and would fall through to KindUnknown.
	if dispatcher.event.Kind != kernel.KindApproved {
		t.Errorf("Kind = %v; want KindApproved", dispatcher.event.Kind)
	}
}
