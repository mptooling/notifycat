package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

// The adapter fills the "org" slot with a Bitbucket workspace's repositories by
// delegating to ListWorkspaceRepos.
func TestBitbucketRepoLister_ListOrgReposDelegates(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[{"slug":"api"},{"slug":"web"}],"next":""}`))
	}))
	defer server.Close()
	client := bitbucket.NewClient(http.DefaultClient, "token", "", bitbucket.WithBaseURL(server.URL))
	lister := NewBitbucketRepoLister(client)

	repos, err := lister.ListOrgRepos(context.Background(), "acme")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, repos)
	assert.Equal(t, "/repositories/acme", gotPath)
}
