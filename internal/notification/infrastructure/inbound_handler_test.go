package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

type fakeDispatcher struct {
	event  kernel.Event
	called bool
	err    error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, event kernel.Event) error {
	f.event = event
	f.called = true
	return f.err
}

var _ domain.EventDispatcher = (*fakeDispatcher)(nil)

const openedPRBody = `{
	"action": "opened",
	"repository": {"full_name": "octo/widget"},
	"pull_request": {"number": 7, "title": "x", "html_url": "u", "user": {"login": "a"}}
}`

// postGitHubWebhook drives the GitHub handler with body and returns the recorder.
func postGitHubWebhook(handler http.Handler, githubEvent, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(body))
	if githubEvent != "" {
		request.Header.Set("X-GitHub-Event", githubEvent)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestGitHubHandler_HappyPath(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), "pull_request", openedPRBody)

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, dispatcher.called)
	assert.Equal(t, kernel.ProviderGitHub, dispatcher.event.Provider)
	assert.Equal(t, kernel.KindOpened, dispatcher.event.Kind)
	assert.Equal(t, 7, dispatcher.event.PR.Number)
	assert.Equal(t, "octo/widget", dispatcher.event.Repository)
}

func TestGitHubHandler_MissingPRReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), "",
		`{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{}}`)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.False(t, dispatcher.called)
}

func TestGitHubHandler_InvalidJSONReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), "", "not-json")

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.False(t, dispatcher.called)
}

func TestGitHubHandler_DispatchErrorReturns500(t *testing.T) {
	dispatcher := &fakeDispatcher{err: context.DeadlineExceeded}

	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), "", openedPRBody)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestGitHubHandler_XGitHubEventHeaderMapped(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), "pull_request_review", `{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {"number": 3, "title": "feat", "html_url": "u", "user": {"login": "alice"}}
	}`)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, kernel.KindApproved, dispatcher.event.Kind,
		"without the header the adapter cannot tell this is a review")
}
