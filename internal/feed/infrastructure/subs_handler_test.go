package infrastructure_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

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
	testCases := []struct{ name, rawID string }{
		{name: "not a number", rawID: "abc"},
		{name: "zero", rawID: "0"},
		{name: "negative", rawID: "-1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()

			recorder := fixture.do(http.MethodDelete, "/api/v1/subs/"+testCase.rawID, "")

			assert.Equal(t, http.StatusNotFound, recorder.Code)
			assert.JSONEq(t, `{"error":"not_found"}`, recorder.Body.String())
			assert.Zero(t, fixture.subscriptions.removeCalls)
		})
	}
}

func TestSubs_UseCaseFailureIs500AndLogged(t *testing.T) {
	testCases := []struct{ name, method, target, body string }{
		{name: "list", method: http.MethodGet, target: "/api/v1/subs"},
		{name: "add", method: http.MethodPost, target: "/api/v1/subs", body: `{"repositories":["acme/api"]}`},
		{name: "remove", method: http.MethodDelete, target: "/api/v1/subs/17"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()
			fixture.subscriptions.err = assert.AnError

			recorder := fixture.do(testCase.method, testCase.target, testCase.body)

			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
			assert.Contains(t, fixture.logs.String(), "agent api request failed")
		})
	}
}

func TestSubs_Add_WrappedUnknownRepositoriesIs400WithList(t *testing.T) {
	fixture := newFeedFixture()
	fixture.subscriptions.err = fmt.Errorf("x: %w", &domain.UnknownRepositoriesError{Repositories: []string{"acme/nope"}})

	recorder := fixture.do(http.MethodPost, "/api/v1/subs", `{"repositories":["acme/nope"]}`)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.JSONEq(t, `{"error":"unknown_repository","repositories":["acme/nope"]}`, recorder.Body.String())
}

func TestSubs_NoPrincipalIs401(t *testing.T) {
	testCases := []struct{ name, method, target, body string }{
		{name: "list", method: http.MethodGet, target: "/api/v1/subs"},
		{name: "add", method: http.MethodPost, target: "/api/v1/subs", body: `{"repositories":["acme/api"]}`},
		{name: "remove", method: http.MethodDelete, target: "/api/v1/subs/17"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()
			request := httptest.NewRequestWithContext(context.Background(), testCase.method, testCase.target, strings.NewReader(testCase.body))
			recorder := httptest.NewRecorder()

			fixture.mux.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
			assert.Zero(t, fixture.subscriptions.listCalls+fixture.subscriptions.addCalls+fixture.subscriptions.removeCalls)
		})
	}
}
