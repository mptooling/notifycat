package github_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/mptooling/notifycat/internal/github"
)

func TestListHookEvents_FiltersBySuffixAndUnionsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repos/acme/widgets/hooks" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":1,"active":true,"events":["pull_request","pull_request_review"],"config":{"url":"https://notifycat.example/webhook/github"}},
			{"id":2,"active":true,"events":["push"],"config":{"url":"https://other.example/hook"}},
			{"id":3,"active":false,"events":["pull_request_review_comment"],"config":{"url":"https://notifycat.example/webhook/github"}},
			{"id":4,"active":true,"events":["pull_request_review_comment"],"config":{"url":"https://notifycat.example/webhook/github"}}
		]`)
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	got, err := c.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")
	if err != nil {
		t.Fatalf("ListHookEvents: %v", err)
	}
	sort.Strings(got)
	want := []string{"pull_request", "pull_request_review", "pull_request_review_comment"}
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
		_, _ = io.WriteString(w, `[{"id":1,"active":true,"events":["push"],"config":{"url":"https://other.example/hook"}}]`)
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	got, err := c.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")
	if err != nil {
		t.Fatalf("ListHookEvents: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("events = %v; want empty", got)
	}
}

func TestListHookEvents_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found","documentation_url":"..."}`)
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	_, err := c.ListHookEvents(context.Background(), "acme", "widgets", "/webhook/github")
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v; want *github.APIError", err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != "Not Found" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestListOrgRepos_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"name":"api"},{"name":"web"}]`))
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	got, err := c.ListOrgRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("got %v", got)
	}
}

func TestListOrgRepos_FollowsLinkHeader(t *testing.T) {
	var page atomic.Int32
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch page.Add(1) {
		case 1:
			w.Header().Set("Link", `<`+base+`/orgs/acme/repos?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"api"}]`))
		default:
			_, _ = w.Write([]byte(`[{"name":"web"}]`))
		}
	}))
	defer srv.Close()
	base = srv.URL

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	got, err := c.ListOrgRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("expected [api, web]; got %v", got)
	}
}

func TestListOrgRepos_Non2xxIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	_, err := c.ListOrgRepos(context.Background(), "acme")
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("want APIError 404; got %T %v", err, err)
	}
}

func TestGetPullRequest_OpenAndDraft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/web/pulls/42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"state":"open","draft":true,"title":"x"}`)
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	pr, err := c.GetPullRequest(context.Background(), "acme", "web", 42)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.State != "open" || !pr.Draft {
		t.Fatalf("pr = %+v; want open+draft", pr)
	}
}

func TestGetPullRequest_NotFoundIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	c := github.NewClient(srv.Client(), "tok", github.WithBaseURL(srv.URL))
	_, err := c.GetPullRequest(context.Background(), "acme", "web", 99)
	var apiErr *github.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("want APIError 404; got %T %v", err, err)
	}
}
