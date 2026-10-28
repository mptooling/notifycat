package infrastructure_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
)

type feedFixture struct {
	subscriptions *fakeSubscriptions
	pullRequests  *fakePullRequests
	logs          *bytes.Buffer
	mux           *http.ServeMux
}

func newFeedFixture() feedFixture {
	fixture := feedFixture{
		subscriptions: &fakeSubscriptions{},
		pullRequests:  &fakePullRequests{},
		logs:          &bytes.Buffer{},
		mux:           http.NewServeMux(),
	}
	logger := slog.New(slog.NewTextHandler(fixture.logs, nil))
	for _, route := range infrastructure.NewRoutes(fixture.subscriptions, fixture.pullRequests, logger) {
		fixture.mux.Handle(route.Pattern, route.Handler)
	}
	return fixture
}

func (fixture feedFixture) do(method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	request = request.WithContext(accessdomain.WithPrincipal(request.Context(), accessdomain.Principal{TokenID: "t1", User: "alice"}))
	recorder := httptest.NewRecorder()
	fixture.mux.ServeHTTP(recorder, request)
	return recorder
}

var aliceSubscriber = domain.Subscriber{TokenID: "t1", User: "alice"}

func sampleSubscriptions() []domain.Subscription {
	return []domain.Subscription{{ID: 17, TokenID: "t1", Repository: "acme/api", CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}}
}

const sampleSubscriptionsJSON = `{"subscriptions":[{"id":17,"repository":"acme/api","created_at":"2026-10-28T09:00:00Z"}]}`

func TestSubs_List(t *testing.T) {
	fixture := newFeedFixture()
	fixture.subscriptions.subscriptions = sampleSubscriptions()

	recorder := fixture.do(http.MethodGet, "/api/v1/subs", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, sampleSubscriptionsJSON, recorder.Body.String())
	assert.Equal(t, aliceSubscriber, fixture.subscriptions.gotSubscriber)
}

func TestSubs_List_EmptyIsEmptyArray(t *testing.T) {
	fixture := newFeedFixture()

	recorder := fixture.do(http.MethodGet, "/api/v1/subs", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"subscriptions":[]}`, recorder.Body.String())
}

func TestSubs_Add(t *testing.T) {
	fixture := newFeedFixture()
	fixture.subscriptions.subscriptions = sampleSubscriptions()

	recorder := fixture.do(http.MethodPost, "/api/v1/subs", `{"repositories":["acme/api","acme/web"]}`)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, sampleSubscriptionsJSON, recorder.Body.String())
	assert.Equal(t, []string{"acme/api", "acme/web"}, fixture.subscriptions.gotRepositories)
	assert.Equal(t, aliceSubscriber, fixture.subscriptions.gotSubscriber)
}

func TestSubs_Add_BadRequests(t *testing.T) {
	testCases := []struct {
		name       string
		body       string
		useCaseErr error
		wantBody   string
	}{
		{name: "not json", body: "not json", wantBody: `{"error":"invalid_body"}`},
		{name: "unknown field", body: `{"repositories":["acme/api"],"extra":1}`, wantBody: `{"error":"invalid_body"}`},
		{name: "trailing garbage", body: `{"repositories":["acme/api"]} x`, wantBody: `{"error":"invalid_body"}`},
		{name: "too large", body: `{"repositories":["` + strings.Repeat("a", domain.MaxRequestBodyBytes) + `"]}`, wantBody: `{"error":"invalid_body"}`},
		{name: "no repositories", body: `{"repositories":[]}`, useCaseErr: domain.ErrNoRepositories, wantBody: `{"error":"invalid_body"}`},
		{
			name:       "unknown repository",
			body:       `{"repositories":["acme/nope"]}`,
			useCaseErr: &domain.UnknownRepositoriesError{Repositories: []string{"acme/nope"}},
			wantBody:   `{"error":"unknown_repository","repositories":["acme/nope"]}`,
		},
		{name: "too many", body: `{"repositories":["acme/api"]}`, useCaseErr: domain.ErrTooManySubscriptions, wantBody: `{"error":"too_many_subscriptions"}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()
			fixture.subscriptions.err = testCase.useCaseErr

			recorder := fixture.do(http.MethodPost, "/api/v1/subs", testCase.body)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, testCase.wantBody, recorder.Body.String())
			assert.Empty(t, fixture.logs.String(), "client errors are not logged")
		})
	}
}

func TestSubs_Remove(t *testing.T) {
	fixture := newFeedFixture()

	recorder := fixture.do(http.MethodDelete, "/api/v1/subs/17", "")

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, int64(17), fixture.subscriptions.gotID)
	assert.Equal(t, aliceSubscriber, fixture.subscriptions.gotSubscriber)
}

func TestSubs_Remove_UnknownIs404(t *testing.T) {
	fixture := newFeedFixture()
	fixture.subscriptions.err = domain.ErrSubscriptionNotFound

	recorder := fixture.do(http.MethodDelete, "/api/v1/subs/17", "")

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.JSONEq(t, `{"error":"not_found"}`, recorder.Body.String())
}

func TestSubs_Remove_InvalidIDIs404WithoutUseCase(t *testing.T) {
	for _, rawID := range []string{"abc", "0", "-1"} {
		t.Run(rawID, func(t *testing.T) {
			fixture := newFeedFixture()

			recorder := fixture.do(http.MethodDelete, "/api/v1/subs/"+rawID, "")

			assert.Equal(t, http.StatusNotFound, recorder.Code)
			assert.JSONEq(t, `{"error":"not_found"}`, recorder.Body.String())
			assert.Zero(t, fixture.subscriptions.removeCalls)
		})
	}
}

func TestSubs_UseCaseFailureIs500AndLogged(t *testing.T) {
	requests := []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/subs", ""},
		{http.MethodPost, "/api/v1/subs", `{"repositories":["acme/api"]}`},
		{http.MethodDelete, "/api/v1/subs/17", ""},
	}
	for _, request := range requests {
		t.Run(request.method, func(t *testing.T) {
			fixture := newFeedFixture()
			fixture.subscriptions.err = assert.AnError

			recorder := fixture.do(request.method, request.target, request.body)

			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
			assert.Contains(t, fixture.logs.String(), "agent api request failed")
		})
	}
}

func TestSubs_NoPrincipalIs401(t *testing.T) {
	fixture := newFeedFixture()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/subs", nil)
	recorder := httptest.NewRecorder()

	fixture.mux.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
	assert.Zero(t, fixture.subscriptions.listCalls)
}

func TestRoutes_Patterns(t *testing.T) {
	routes := infrastructure.NewRoutes(&fakeSubscriptions{}, &fakePullRequests{}, discardLogger())

	patterns := make([]string, 0, len(routes))
	for _, route := range routes {
		patterns = append(patterns, route.Pattern)
	}

	assert.ElementsMatch(t, []string{
		domain.ListSubscriptionsPattern,
		domain.AddSubscriptionsPattern,
		domain.RemoveSubscriptionPattern,
		domain.PullRequestsPattern,
	}, patterns)
}
