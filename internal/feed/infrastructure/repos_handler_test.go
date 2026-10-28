package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

func TestRepos_List(t *testing.T) {
	fixture := newFeedFixture()
	fixture.subscriptions.available = domain.ConfiguredRepositories{
		Repositories:  []string{"acme/api", "beta/svc"},
		Organizations: []string{"gamma"},
	}

	recorder := fixture.do(http.MethodGet, "/api/v1/repos", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"repositories":["acme/api","beta/svc"],"organizations":["gamma"]}`, recorder.Body.String())
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestRepos_List_EmptyIsEmptyArrays(t *testing.T) {
	fixture := newFeedFixture()

	recorder := fixture.do(http.MethodGet, "/api/v1/repos", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"repositories":[],"organizations":[]}`, recorder.Body.String())
}

func TestRepos_NoPrincipalIs401(t *testing.T) {
	fixture := newFeedFixture()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/repos", nil)
	recorder := httptest.NewRecorder()

	fixture.mux.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
	assert.Zero(t, fixture.subscriptions.availableCalls)
}
