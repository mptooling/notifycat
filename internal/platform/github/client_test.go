package github_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/github"
)

// newTestClient serves handler over httptest and returns a client pointed at it.
// Assertions inside a handler must use assert, never require: it runs on the
// server goroutine, where FailNow is illegal.
func newTestClient(t *testing.T, handler http.HandlerFunc) *github.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return github.NewClient(server.Client(), "tok", github.WithBaseURL(server.URL))
}

func notFoundHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"message":"Not Found"}`)
}

func TestListHookEvents_FiltersBySuffixAndUnionsEvents(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/widgets/hooks", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":1,"active":true,"events":["pull_request","pull_request_review"],"config":{"url":"https://notifycat.example/webhook/github"}},
			{"id":2,"active":true,"events":["push"],"config":{"url":"https://other.example/hook"}},
			{"id":3,"active":false,"events":["pull_request_review_comment"],"config":{"url":"https://notifycat.example/webhook/github"}},
			{"id":4,"active":true,"events":["pull_request_review_comment"],"config":{"url":"https://notifycat.example/webhook/github"}}
		]`)
	})

	got, err := client.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"pull_request", "pull_request_review", "pull_request_review_comment"}, got,
		"only active hooks whose URL carries our suffix count, and their events union")
}

func TestListHookEvents_NoMatchingHook(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":1,"active":true,"events":["push"],"config":{"url":"https://other.example/hook"}}]`)
	})

	got, err := client.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListHookEvents_APIError(t *testing.T) {
	client := newTestClient(t, notFoundHandler)

	_, err := client.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")

	var apiErr *github.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
	assert.Equal(t, "Not Found", apiErr.Message)
}

func TestListOrgRepos_SinglePage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/acme/repos", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"api"},{"name":"web"}]`)
	})

	got, err := client.ListOrgRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, got)
}

func TestListOrgRepos_FollowsLinkHeader(t *testing.T) {
	var page atomic.Int32
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			w.Header().Set("Link", `<`+baseURL+`/orgs/acme/repos?page=2>; rel="next"`)
			_, _ = io.WriteString(w, `[{"name":"api"}]`)
		default:
			_, _ = io.WriteString(w, `[{"name":"web"}]`)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client := github.NewClient(server.Client(), "tok", github.WithBaseURL(server.URL))

	got, err := client.ListOrgRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, got)
}

func TestListOrgRepos_Non2xxIsAPIError(t *testing.T) {
	client := newTestClient(t, notFoundHandler)

	_, err := client.ListOrgRepos(context.Background(), "acme")

	var apiErr *github.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestGetPullRequest_OpenAndDraft(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/web/pulls/42", r.URL.Path)
		_, _ = io.WriteString(w, `{"state":"open","draft":true,"title":"x"}`)
	})

	pullRequest, err := client.GetPullRequest(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, "open", pullRequest.State)
	assert.True(t, pullRequest.Draft)
}

func TestGetPullRequest_NotFoundIsAPIError(t *testing.T) {
	client := newTestClient(t, notFoundHandler)

	_, err := client.GetPullRequest(context.Background(), "acme", "web", 99)

	var apiErr *github.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestListPullRequestFiles_SinglePage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/web/pulls/42/files", r.URL.Path)
		_, _ = io.WriteString(w, `[{"filename":"modules/acme/x.go"},{"filename":"README.md"}]`)
	})

	got, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, []string{"modules/acme/x.go", "README.md"}, got)
}

func TestListPullRequestFiles_FollowsLinkHeader(t *testing.T) {
	var page atomic.Int32
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			w.Header().Set("Link", `<`+baseURL+`/repos/acme/web/pulls/42/files?page=2>; rel="next"`)
			_, _ = io.WriteString(w, `[{"filename":"a.go"}]`)
		default:
			_, _ = io.WriteString(w, `[{"filename":"b.go"}]`)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client := github.NewClient(server.Client(), "tok", github.WithBaseURL(server.URL))

	got, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "b.go"}, got)
}

func TestListPullRequestFiles_Non2xxIsAPIError(t *testing.T) {
	client := newTestClient(t, notFoundHandler)

	_, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 99)

	var apiErr *github.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}
