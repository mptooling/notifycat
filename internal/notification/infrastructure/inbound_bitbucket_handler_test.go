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
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

const bitbucketOpenedBody = `{
	"actor": {"type": "user", "display_name": "Jane"},
	"pullrequest": {"id": 42, "title": "Fix", "state": "OPEN", "draft": false,
		"links": {"html": {"href": "https://bitbucket.org/ws/repo/pull-requests/42"}},
		"author": {"display_name": "Bob", "type": "user"}},
	"repository": {"full_name": "workspace/repo"}
}`

// postBitbucketWebhook drives the Bitbucket handler with body and returns the recorder.
func postBitbucketWebhook(handler http.Handler, eventKey, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", strings.NewReader(body))
	if eventKey != "" {
		request.Header.Set("X-Event-Key", eventKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestBitbucketHandler_HappyPath(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), "pullrequest:created", bitbucketOpenedBody)

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, dispatcher.called)
	assert.Equal(t, kernel.ProviderBitbucket, dispatcher.event.Provider)
	assert.Equal(t, kernel.KindOpened, dispatcher.event.Kind)
	assert.Equal(t, 42, dispatcher.event.PR.Number)
	assert.Equal(t, "workspace/repo", dispatcher.event.Repository)
}

func TestBitbucketHandler_MissingIDReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), "pullrequest:created",
		`{"repository":{"full_name":"w/r"},"pullrequest":{}}`)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.False(t, dispatcher.called)
}

func TestBitbucketHandler_InvalidJSONReturns400(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), "pullrequest:created", "not-json")

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.False(t, dispatcher.called)
}

func TestBitbucketHandler_DispatchErrorReturns500(t *testing.T) {
	dispatcher := &fakeDispatcher{err: context.DeadlineExceeded}

	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), "pullrequest:created", bitbucketOpenedBody)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestBitbucketHandler_XEventKeyHeaderMapped(t *testing.T) {
	dispatcher := &fakeDispatcher{}

	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), "pullrequest:approved", bitbucketOpenedBody)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, kernel.KindApproved, dispatcher.event.Kind,
		"without the header the adapter cannot tell this is an approval")
}
