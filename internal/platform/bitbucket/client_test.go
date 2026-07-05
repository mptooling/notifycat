package bitbucket_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

func TestGetRepository_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repositories/acme/web" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept = %q", got)
		}
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":true}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	repo, err := c.GetRepository(context.Background(), "acme", "web")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.FullName != "acme/web" || repo.Slug != "web" || !repo.IsPrivate {
		t.Fatalf("repo = %+v", repo)
	}
}

func TestGetRepository_NotFoundIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"No such repository"}}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.GetRepository(context.Background(), "acme", "web")
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v; want *bitbucket.APIError", err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != "No such repository" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestGetRepository_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth = %q; want Bearer tok", got)
		}
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":false}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	if _, err := c.GetRepository(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
}

func TestGetRepository_BasicAuth(t *testing.T) {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:tok"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("auth = %q; want %q", got, want)
		}
		_, _ = io.WriteString(w, `{"full_name":"acme/web","slug":"web","is_private":false}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "user@example.com", bitbucket.WithBaseURL(srv.URL))
	if _, err := c.GetRepository(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
}

func TestListWorkspaceRepos_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/acme" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("pagelen"); got != "100" {
			t.Errorf("pagelen = %q; want 100", got)
		}
		_, _ = io.WriteString(w, `{"values":[{"slug":"api"},{"slug":"web"}]}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListWorkspaceRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("got %v; want [api web]", got)
	}
}

func TestListWorkspaceRepos_FollowsNext(t *testing.T) {
	var page atomic.Int32
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"values":[{"slug":"api"}],"next":"`+base+`/repositories/acme?page=2"}`)
		default:
			_, _ = io.WriteString(w, `{"values":[{"slug":"web"}]}`)
		}
	}))
	defer srv.Close()
	base = srv.URL

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListWorkspaceRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("got %v; want [api web]", got)
	}
}

func TestListWorkspaceRepos_EmptyIsNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[]}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListWorkspaceRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v; want empty", got)
	}
}

func TestListHookEvents_FiltersBySuffixAndUnionsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repositories/acme/web/hooks" {
			t.Errorf("path = %q", got)
		}
		_, _ = io.WriteString(w, `{"values":[
			{"uuid":"{1}","url":"https://notifycat.example/webhook/bitbucket","active":true,"events":["pullrequest:created","pullrequest:updated"]},
			{"uuid":"{2}","url":"https://other.example/hook","active":true,"events":["repo:push"]},
			{"uuid":"{3}","url":"https://notifycat.example/webhook/bitbucket","active":false,"events":["pullrequest:approved"]},
			{"uuid":"{4}","url":"https://notifycat.example/webhook/bitbucket","active":true,"events":["pullrequest:approved"]}
		]}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListHookEvents(context.Background(), "acme", "web", "/webhook/bitbucket")
	if err != nil {
		t.Fatalf("ListHookEvents: %v", err)
	}
	sort.Strings(got)
	want := []string{"pullrequest:approved", "pullrequest:created", "pullrequest:updated"}
	if len(got) != len(want) {
		t.Fatalf("events = %v; want %v", got, want)
	}
	for i, ev := range want {
		if got[i] != ev {
			t.Fatalf("events[%d] = %q; want %q", i, got[i], ev)
		}
	}
}

func TestListHookEvents_NoMatchingHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[{"uuid":"{1}","url":"https://other.example/hook","active":true,"events":["repo:push"]}]}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListHookEvents(context.Background(), "acme", "web", "/webhook/bitbucket")
	if err != nil {
		t.Fatalf("ListHookEvents: %v", err)
	}
	if got == nil {
		t.Fatalf("events = nil; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("events = %v; want empty", got)
	}
}

func TestGetPullRequest_OpenAndDraft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/acme/web/pullrequests/42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"state":"OPEN","draft":true,"title":"x"}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	pr, err := c.GetPullRequest(context.Background(), "acme", "web", 42)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.State != "OPEN" || !pr.Draft {
		t.Fatalf("pr = %+v; want OPEN+draft", pr)
	}
}

func TestGetPullRequest_NotFoundIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"Not Found"}}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.GetPullRequest(context.Background(), "acme", "web", 99)
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("want APIError 404; got %T %v", err, err)
	}
}

func TestListPullRequestFiles_RedirectWithAuthReplay(t *testing.T) {
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repositories/acme/web/pullrequests/42/diffstat":
			http.Redirect(w, r,
				base+"/repositories/acme/web/pullrequests/42/diffstat/abc123..def456?from_pullrequest_id=42",
				http.StatusFound)
		case "/repositories/acme/web/pullrequests/42/diffstat/abc123..def456":
			if r.Header.Get("Authorization") == "" {
				t.Errorf("redirect target missing Authorization header (auth not replayed)")
			}
			_, _ = io.WriteString(w, `{"values":[
				{"status":"modified","new":{"path":"src/a.go"},"old":{"path":"src/a.go"}},
				{"status":"removed","new":null,"old":{"path":"src/gone.go"}},
				{"status":"modified","new":{"path":"src/a.go"},"old":{"path":"src/a.go"}}
			]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	base = srv.URL

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListPullRequestFiles(context.Background(), "acme", "web", 42)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "src/a.go" || got[1] != "src/gone.go" {
		t.Errorf("got %v; want [src/a.go src/gone.go]", got)
	}
}

func TestListPullRequestFiles_SpecNoneSoftFail(t *testing.T) {
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/acme/web/pullrequests/42/diffstat" {
			http.Redirect(w, r,
				base+"/repositories/acme/web/pullrequests/42/diffstat/None?from_pullrequest_id=42",
				http.StatusFound)
			return
		}
		t.Errorf("redirect to spec=None target should not be followed; got path %q", r.URL.Path)
	}))
	defer srv.Close()
	base = srv.URL

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.ListPullRequestFiles(context.Background(), "acme", "web", 42)
	if !errors.Is(err, bitbucket.ErrDiffstatUnavailable) {
		t.Fatalf("err = %v; want ErrDiffstatUnavailable", err)
	}
}

func TestListPullRequestFiles_RefusesCrossHostRedirect(t *testing.T) {
	var attacker atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		attacker.Store(true)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("credential leaked to cross-host redirect target: %q", r.Header.Get("Authorization"))
		}
	}))
	defer evil.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/steal?from_pullrequest_id=42", http.StatusFound)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.ListPullRequestFiles(context.Background(), "acme", "web", 42)
	if err == nil {
		t.Fatal("expected an error refusing the cross-host redirect, got nil")
	}
	if errors.Is(err, bitbucket.ErrDiffstatUnavailable) {
		t.Fatalf("cross-host redirect should surface as a hard error, not the soft-fail: %v", err)
	}
	if attacker.Load() {
		t.Fatal("cross-host redirect target was contacted; it must never be reached")
	}
}

func TestListPullRequestFiles_FollowsNext(t *testing.T) {
	var page atomic.Int32
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"values":[{"status":"added","new":{"path":"a.go"},"old":null}],"next":"`+base+`/repositories/acme/web/pullrequests/42/diffstat?page=2"}`)
		default:
			_, _ = io.WriteString(w, `{"values":[{"status":"modified","new":{"path":"b.go"},"old":{"path":"b.go"}}]}`)
		}
	}))
	defer srv.Close()
	base = srv.URL

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	got, err := c.ListPullRequestFiles(context.Background(), "acme", "web", 42)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("got %v; want [a.go b.go]", got)
	}
}

func TestGetRepository_RateLimitIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.GetRepository(context.Background(), "acme", "web")
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("want APIError 429; got %T %v", err, err)
	}
}

func TestGetPullRequest_ServerErrorIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.GetPullRequest(context.Background(), "acme", "web", 42)
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("want APIError 500; got %T %v", err, err)
	}
}

func TestGetRepository_ResponseCapTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		b.WriteString(`{"full_name":"acme/web","slug":"web","is_private":false,"pad":"`)
		b.WriteString(strings.Repeat("x", (1<<20)+1024))
		b.WriteString(`"}`)
		_, _ = io.WriteString(w, b.String())
	}))
	defer srv.Close()

	c := bitbucket.NewClient(srv.Client(), "tok", "", bitbucket.WithBaseURL(srv.URL))
	_, err := c.GetRepository(context.Background(), "acme", "web")
	if err == nil {
		t.Fatalf("expected decode error from truncated oversized body, got nil")
	}
	var apiErr *bitbucket.APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("expected decode error, got APIError %+v", apiErr)
	}
}
