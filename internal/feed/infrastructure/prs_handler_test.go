package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

func samplePullRequest() domain.PullRequest {
	return domain.PullRequest{
		Repository:   "acme/api",
		Number:       42,
		Title:        "Add retry to the billing client",
		Description:  "Retries 5xx responses up to three times.",
		URL:          "https://bitbucket.org/acme/api/pull-requests/42",
		AuthorID:     "557058:2f1c",
		SourceBranch: "feature/billing-retry",
		TargetBranch: "main",
		HeadSHA:      "9fceb02d0ae5",
		CreatedAt:    time.Date(2026, 10, 27, 16, 40, 12, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 10, 28, 9, 14, 3, 0, time.UTC),
	}
}

const samplePullRequestJSON = `{"pull_requests":[{
	"repository":"acme/api","number":42,"title":"Add retry to the billing client",
	"description":"Retries 5xx responses up to three times.",
	"url":"https://bitbucket.org/acme/api/pull-requests/42","author_id":"557058:2f1c",
	"source_branch":"feature/billing-retry","target_branch":"main","head_sha":"9fceb02d0ae5",
	"created_at":"2026-10-27T16:40:12Z","updated_at":"2026-10-28T09:14:03Z"}]}`

func TestPrs_Next(t *testing.T) {
	fixture := newFeedFixture()
	fixture.pullRequests.pullRequests = []domain.PullRequest{samplePullRequest()}

	recorder := fixture.do(http.MethodGet, "/api/v1/subs/prs", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, samplePullRequestJSON, recorder.Body.String())
	assert.Equal(t, aliceSubscriber, fixture.pullRequests.gotSubscriber)
	assert.Zero(t, fixture.pullRequests.sinceCalls)
}

func TestPrs_Next_EmptyIsEmptyArray(t *testing.T) {
	fixture := newFeedFixture()

	recorder := fixture.do(http.MethodGet, "/api/v1/subs/prs", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"pull_requests":[]}`, recorder.Body.String())
}

func TestPrs_ZeroCreatedAtIsNull(t *testing.T) {
	fixture := newFeedFixture()
	pullRequest := samplePullRequest()
	pullRequest.CreatedAt = time.Time{}
	fixture.pullRequests.pullRequests = []domain.PullRequest{pullRequest}

	recorder := fixture.do(http.MethodGet, "/api/v1/subs/prs", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"pull_requests":[{
		"repository":"acme/api","number":42,"title":"Add retry to the billing client",
		"description":"Retries 5xx responses up to three times.",
		"url":"https://bitbucket.org/acme/api/pull-requests/42","author_id":"557058:2f1c",
		"source_branch":"feature/billing-retry","target_branch":"main","head_sha":"9fceb02d0ae5",
		"created_at":null,"updated_at":"2026-10-28T09:14:03Z"}]}`, recorder.Body.String())
}

func TestPrs_Since(t *testing.T) {
	fixture := newFeedFixture()
	fixture.pullRequests.pullRequests = []domain.PullRequest{samplePullRequest()}

	recorder := fixture.do(http.MethodGet, "/api/v1/subs/prs?since=2026-10-28T11:00:00%2B02:00", "")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, samplePullRequestJSON, recorder.Body.String())
	assert.WithinDuration(t, time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), fixture.pullRequests.gotSince, 0)
	assert.Equal(t, aliceSubscriber, fixture.pullRequests.gotSubscriber)
	assert.Zero(t, fixture.pullRequests.nextCalls)
}

func TestPrs_InvalidSinceIs400WithoutUseCase(t *testing.T) {
	testCases := []struct{ name, target string }{
		{name: "not a time", target: "/api/v1/subs/prs?since=yesterday"},
		{name: "empty", target: "/api/v1/subs/prs?since="},
		{name: "bad escape", target: "/api/v1/subs/prs?since=%zz"},
		{name: "semicolon", target: "/api/v1/subs/prs?since=2026-10-28T09:00:00Z;x"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()

			recorder := fixture.do(http.MethodGet, testCase.target, "")

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"error":"invalid_since"}`, recorder.Body.String())
			assert.Zero(t, fixture.pullRequests.nextCalls+fixture.pullRequests.sinceCalls)
		})
	}
}

func TestPrs_HeadIs405WithoutUseCase(t *testing.T) {
	fixture := newFeedFixture()

	recorder := fixture.do(http.MethodHead, "/api/v1/subs/prs", "")

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Equal(t, http.MethodGet, recorder.Header().Get("Allow"))
	assert.Zero(t, fixture.pullRequests.nextCalls+fixture.pullRequests.sinceCalls)
}

func TestPrs_UseCaseFailureIs500AndLogged(t *testing.T) {
	testCases := []struct{ name, target string }{
		{name: "next", target: "/api/v1/subs/prs"},
		{name: "since", target: "/api/v1/subs/prs?since=2026-10-28T09:00:00Z"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFeedFixture()
			fixture.pullRequests.err = assert.AnError

			recorder := fixture.do(http.MethodGet, testCase.target, "")

			assert.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
			assert.Contains(t, fixture.logs.String(), "agent api request failed")
		})
	}
}

func TestPrs_NoPrincipalIs401(t *testing.T) {
	fixture := newFeedFixture()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/subs/prs", nil)
	recorder := httptest.NewRecorder()

	fixture.mux.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
	assert.Zero(t, fixture.pullRequests.nextCalls+fixture.pullRequests.sinceCalls)
}
