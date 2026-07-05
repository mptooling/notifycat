package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

// TestBitbucketRepoLister_ListOrgReposDelegates proves the adapter fills the
// "org" slot with a Bitbucket workspace's repositories by delegating to
// ListWorkspaceRepos.
func TestBitbucketRepoLister_ListOrgReposDelegates(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[{"slug":"api"},{"slug":"web"}],"next":""}`))
	}))
	defer srv.Close()

	client := bitbucket.NewClient(http.DefaultClient, "token", "", bitbucket.WithBaseURL(srv.URL))
	lister := NewBitbucketRepoLister(client)

	repos, err := lister.ListOrgRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 || repos[0] != "api" || repos[1] != "web" {
		t.Fatalf("repos = %v; want [api web]", repos)
	}
	if gotPath != "/repositories/acme" {
		t.Fatalf("delegated to path %q; want /repositories/acme", gotPath)
	}
}
