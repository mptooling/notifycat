package reconcile_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/reconcile"
)

type fakePRGetter struct {
	state             string
	err               error
	gotOwner, gotRepo string
	gotNumber         int
}

func (f *fakePRGetter) GetPullRequest(_ context.Context, owner, repo string, number int) (github.PullRequestState, error) {
	f.gotOwner, f.gotRepo, f.gotNumber = owner, repo, number
	if f.err != nil {
		return github.PullRequestState{}, f.err
	}
	return github.PullRequestState{State: f.state}, nil
}

func TestGitHubChecker_SplitsRepoAndMapsState(t *testing.T) {
	g := &fakePRGetter{state: "closed"}
	c := reconcile.NewGitHubChecker(g)

	open, err := c.IsOpen(context.Background(), "acme/web", 42)
	if err != nil {
		t.Fatalf("IsOpen: %v", err)
	}
	if open {
		t.Errorf("closed PR reported open")
	}
	if g.gotOwner != "acme" || g.gotRepo != "web" || g.gotNumber != 42 {
		t.Errorf("split = %q/%q#%d; want acme/web#42", g.gotOwner, g.gotRepo, g.gotNumber)
	}
}

func TestGitHubChecker_OpenState(t *testing.T) {
	c := reconcile.NewGitHubChecker(&fakePRGetter{state: "open"})
	open, err := c.IsOpen(context.Background(), "acme/web", 1)
	if err != nil || !open {
		t.Fatalf("open=%v err=%v; want open,nil", open, err)
	}
}

func TestGitHubChecker_PropagatesError(t *testing.T) {
	c := reconcile.NewGitHubChecker(&fakePRGetter{err: errors.New("404")})
	if _, err := c.IsOpen(context.Background(), "acme/web", 1); err == nil {
		t.Fatal("expected error to propagate (so the row is left untouched)")
	}
}

func TestGitHubChecker_RejectsBadRepository(t *testing.T) {
	c := reconcile.NewGitHubChecker(&fakePRGetter{state: "open"})
	if _, err := c.IsOpen(context.Background(), "no-slash", 1); err == nil {
		t.Fatal("expected error for malformed repository")
	}
}
