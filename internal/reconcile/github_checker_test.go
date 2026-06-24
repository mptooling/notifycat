package reconcile_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/reconcile"
)

type fakePRGetter struct {
	state             string
	draft             bool
	err               error
	gotOwner, gotRepo string
	gotNumber         int
}

func (f *fakePRGetter) GetPullRequest(_ context.Context, owner, repo string, number int) (github.PullRequestState, error) {
	f.gotOwner, f.gotRepo, f.gotNumber = owner, repo, number
	if f.err != nil {
		return github.PullRequestState{}, f.err
	}
	return github.PullRequestState{State: f.state, Draft: f.draft}, nil
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
	c := reconcile.NewGitHubChecker(&fakePRGetter{err: errors.New("boom")})
	_, err := c.IsOpen(context.Background(), "acme/web", 1)
	if err == nil {
		t.Fatal("expected error to propagate (so the row is left untouched)")
	}
	if errors.Is(err, reconcile.ErrPRNotFound) {
		t.Fatal("a plain (non-404) error must not be treated as not-found")
	}
}

func TestGitHubChecker_NotFoundMapsToErrPRNotFound(t *testing.T) {
	apiErr := &github.APIError{Method: "get-pull-request", Status: 404, Message: "Not Found"}
	c := reconcile.NewGitHubChecker(&fakePRGetter{err: apiErr})

	_, err := c.IsOpen(context.Background(), "acme/web", 1)
	if !errors.Is(err, reconcile.ErrPRNotFound) {
		t.Fatalf("err = %v; want it to match ErrPRNotFound", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %q; want it to preserve the underlying 404 detail", err)
	}
}

func TestGitHubChecker_Non404APIErrorPropagates(t *testing.T) {
	apiErr := &github.APIError{Method: "get-pull-request", Status: 500, Message: "boom"}
	c := reconcile.NewGitHubChecker(&fakePRGetter{err: apiErr})

	_, err := c.IsOpen(context.Background(), "acme/web", 1)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if errors.Is(err, reconcile.ErrPRNotFound) {
		t.Fatal("a non-404 API error must not be treated as not-found")
	}
}

func TestGitHubChecker_OpenDraftMapsToErrPRDraft(t *testing.T) {
	c := reconcile.NewGitHubChecker(&fakePRGetter{state: "open", draft: true})

	open, err := c.IsOpen(context.Background(), "acme/web", 1)
	if open {
		t.Error("a draft PR must not be reported open")
	}
	if !errors.Is(err, reconcile.ErrPRDraft) {
		t.Fatalf("err = %v; want it to match ErrPRDraft", err)
	}
}

func TestGitHubChecker_ClosedDraftStillMapsToErrPRDraft(t *testing.T) {
	// A draft must never stay in the database even when GitHub also reports it
	// closed — the draft flag wins over the closed disposition.
	c := reconcile.NewGitHubChecker(&fakePRGetter{state: "closed", draft: true})

	if _, err := c.IsOpen(context.Background(), "acme/web", 1); !errors.Is(err, reconcile.ErrPRDraft) {
		t.Fatalf("err = %v; want it to match ErrPRDraft", err)
	}
}

func TestGitHubChecker_RejectsBadRepository(t *testing.T) {
	c := reconcile.NewGitHubChecker(&fakePRGetter{state: "open"})
	if _, err := c.IsOpen(context.Background(), "no-slash", 1); err == nil {
		t.Fatal("expected error for malformed repository")
	}
}
