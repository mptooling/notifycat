package bitbucket_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

// newTestClient serves handler over httptest and returns a client pointed at it.
// Assertions inside a handler must use assert, never require: it runs on the
// server goroutine, where FailNow is illegal.
func newTestClient(t *testing.T, handler http.HandlerFunc) *bitbucket.Client {
	t.Helper()

	return newTestClientAs(t, "", handler)
}

func newTestClientAs(t *testing.T, username string, handler http.HandlerFunc) *bitbucket.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return bitbucket.NewClient(server.Client(), "tok", username, bitbucket.WithBaseURL(server.URL))
}

// errorHandler replies with a Bitbucket-shaped error envelope.
func errorHandler(status int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"`+message+`"}}`)
	}
}

func TestGetRepository_HappyPath(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repositories/acme/web", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":true}`)
	})

	repo, err := client.GetRepository(context.Background(), "acme", "web")

	require.NoError(t, err)
	assert.Equal(t, "acme/web", repo.FullName)
	assert.Equal(t, "web", repo.Slug)
	assert.True(t, repo.IsPrivate)
}

func TestGetRepository_NotFoundIsAPIError(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusNotFound, "No such repository"))

	_, err := client.GetRepository(context.Background(), "acme", "web")

	var apiErr *bitbucket.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
	assert.Equal(t, "No such repository", apiErr.Message)
}

func TestGetRepository_BearerAuth(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":false}`)
	})

	_, err := client.GetRepository(context.Background(), "acme", "web")

	require.NoError(t, err)
}

func TestGetRepository_BasicAuth(t *testing.T) {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:tok"))
	client := newTestClientAs(t, "user@example.com", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, want, r.Header.Get("Authorization"), "a username switches the scheme to basic auth")
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":false}`)
	})

	_, err := client.GetRepository(context.Background(), "acme", "web")

	require.NoError(t, err)
}

func TestGetRepository_RateLimitIsAPIError(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusTooManyRequests, "rate limited"))

	_, err := client.GetRepository(context.Background(), "acme", "web")

	var apiErr *bitbucket.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.Status)
}

func TestGetRepository_ResponseCapTruncation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		var body strings.Builder
		body.WriteString(`{"full_name":"acme/web","slug":"web","is_private":false,"pad":"`)
		body.WriteString(strings.Repeat("x", (1<<20)+1024))
		body.WriteString(`"}`)
		_, _ = io.WriteString(w, body.String())
	})

	_, err := client.GetRepository(context.Background(), "acme", "web")

	require.Error(t, err, "an oversized body is truncated, so decoding must fail")
	var apiErr *bitbucket.APIError
	assert.NotErrorAs(t, err, &apiErr, "truncation is a decode error, not an API error")
}

func TestListWorkspaceRepos_SinglePage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repositories/acme", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("pagelen"))
		_, _ = io.WriteString(w, `{"values":[{"slug":"api"},{"slug":"web"}]}`)
	})

	got, err := client.ListWorkspaceRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, got)
}

func TestListWorkspaceRepos_FollowsNext(t *testing.T) {
	var page atomic.Int32
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"values":[{"slug":"api"}],"next":"`+baseURL+`/repositories/acme?page=2"}`)
		default:
			_, _ = io.WriteString(w, `{"values":[{"slug":"web"}]}`)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client := bitbucket.NewClient(server.Client(), "tok", "", bitbucket.WithBaseURL(server.URL))

	got, err := client.ListWorkspaceRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, got)
}

func TestListWorkspaceRepos_EmptyIsNoError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[]}`)
	})

	got, err := client.ListWorkspaceRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListHookEvents_FiltersBySuffixAndUnionsEvents(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repositories/acme/web/hooks", r.URL.Path)
		_, _ = io.WriteString(w, `{"values":[
			{"uuid":"{1}","url":"https://notifycat.example/webhook/bitbucket","active":true,"events":["pullrequest:created","pullrequest:updated"]},
			{"uuid":"{2}","url":"https://other.example/hook","active":true,"events":["repo:push"]},
			{"uuid":"{3}","url":"https://notifycat.example/webhook/bitbucket","active":false,"events":["pullrequest:approved"]},
			{"uuid":"{4}","url":"https://notifycat.example/webhook/bitbucket","active":true,"events":["pullrequest:approved"]}
		]}`)
	})

	got, err := client.ListHookEvents(context.Background(), "acme", "web", "/webhook/bitbucket")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"pullrequest:approved", "pullrequest:created", "pullrequest:updated"}, got,
		"only active hooks whose URL carries our suffix count, and their events union")
}

func TestListHookEvents_NoMatchingHook(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[{"uuid":"{1}","url":"https://other.example/hook","active":true,"events":["repo:push"]}]}`)
	})

	got, err := client.ListHookEvents(context.Background(), "acme", "web", "/webhook/bitbucket")

	require.NoError(t, err)
	assert.NotNil(t, got, "callers distinguish 'no hook' from nil, so the slice stays non-nil")
	assert.Empty(t, got)
}

func TestGetPullRequest_OpenAndDraft(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repositories/acme/web/pullrequests/42", r.URL.Path)
		_, _ = io.WriteString(w, `{"state":"OPEN","draft":true,"title":"x"}`)
	})

	pullRequest, err := client.GetPullRequest(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, "OPEN", pullRequest.State)
	assert.True(t, pullRequest.Draft)
}

func TestGetPullRequest_NotFoundIsAPIError(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusNotFound, "Not Found"))

	_, err := client.GetPullRequest(context.Background(), "acme", "web", 99)

	var apiErr *bitbucket.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestGetPullRequest_ServerErrorIsAPIError(t *testing.T) {
	client := newTestClient(t, errorHandler(http.StatusInternalServerError, "boom"))

	_, err := client.GetPullRequest(context.Background(), "acme", "web", 42)

	var apiErr *bitbucket.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Status)
}

func TestListPullRequestFiles_RedirectWithAuthReplay(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repositories/acme/web/pullrequests/42/diffstat":
			http.Redirect(w, r,
				baseURL+"/repositories/acme/web/pullrequests/42/diffstat/abc123..def456?from_pullrequest_id=42",
				http.StatusFound)
		case "/repositories/acme/web/pullrequests/42/diffstat/abc123..def456":
			assert.NotEmpty(t, r.Header.Get("Authorization"), "auth must be replayed on a same-host redirect")
			_, _ = io.WriteString(w, `{"values":[
				{"status":"modified","new":{"path":"src/a.go"},"old":{"path":"src/a.go"}},
				{"status":"removed","new":null,"old":{"path":"src/gone.go"}},
				{"status":"modified","new":{"path":"src/a.go"},"old":{"path":"src/a.go"}}
			]}`)
		default:
			assert.Failf(t, "unexpected request", "path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client := bitbucket.NewClient(server.Client(), "tok", "", bitbucket.WithBaseURL(server.URL))

	got, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a.go", "src/gone.go"}, got, "a removed file keeps its old path and duplicates collapse")
}

func TestListPullRequestFiles_SpecNoneSoftFail(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/acme/web/pullrequests/42/diffstat" {
			http.Redirect(w, r,
				baseURL+"/repositories/acme/web/pullrequests/42/diffstat/None?from_pullrequest_id=42",
				http.StatusFound)
			return
		}
		assert.Failf(t, "redirect to a spec=None target must not be followed", "path %q", r.URL.Path)
	}))
	defer server.Close()
	baseURL = server.URL
	client := bitbucket.NewClient(server.Client(), "tok", "", bitbucket.WithBaseURL(server.URL))

	_, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	assert.ErrorIs(t, err, bitbucket.ErrDiffstatUnavailable)
}

func TestListPullRequestFiles_RefusesCrossHostRedirect(t *testing.T) {
	var attackerReached atomic.Bool
	attacker := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		attackerReached.Store(true)
		assert.Empty(t, r.Header.Get("Authorization"), "the credential must never leave our host")
	}))
	defer attacker.Close()
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/steal?from_pullrequest_id=42", http.StatusFound)
	})

	_, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	require.Error(t, err)
	require.NotErrorIs(t, err, bitbucket.ErrDiffstatUnavailable, "a cross-host redirect is a hard error, not the soft-fail")
	assert.False(t, attackerReached.Load(), "the cross-host target must never be contacted")
}

func TestListPullRequestFiles_FollowsNext(t *testing.T) {
	var page atomic.Int32
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"values":[{"status":"added","new":{"path":"a.go"},"old":null}],"next":"`+baseURL+`/repositories/acme/web/pullrequests/42/diffstat?page=2"}`)
		default:
			_, _ = io.WriteString(w, `{"values":[{"status":"modified","new":{"path":"b.go"},"old":{"path":"b.go"}}]}`)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client := bitbucket.NewClient(server.Client(), "tok", "", bitbucket.WithBaseURL(server.URL))

	got, err := client.ListPullRequestFiles(context.Background(), "acme", "web", 42)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "b.go"}, got)
}
