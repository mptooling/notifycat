package infrastructure_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/maintenance/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

type fakeBitbucketPRGetter struct {
	state        string
	draft        bool
	err          error
	gotWorkspace string
	gotRepoSlug  string
	gotID        int
}

func (f *fakeBitbucketPRGetter) GetPullRequest(_ context.Context, workspace, repoSlug string, id int) (bitbucket.PullRequestState, error) {
	f.gotWorkspace, f.gotRepoSlug, f.gotID = workspace, repoSlug, id
	if f.err != nil {
		return bitbucket.PullRequestState{}, f.err
	}
	return bitbucket.PullRequestState{State: f.state, Draft: f.draft}, nil
}

func TestBitbucketChecker_SplitsRepoAndMapsState(t *testing.T) {
	getter := &fakeBitbucketPRGetter{state: "MERGED"}
	checker := infrastructure.NewBitbucketChecker(getter)

	open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 42)
	if err != nil {
		t.Fatalf("IsOpen: %v", err)
	}
	if open {
		t.Errorf("MERGED PR reported open")
	}
	if getter.gotWorkspace != "workspace" || getter.gotRepoSlug != "repo-slug" || getter.gotID != 42 {
		t.Errorf("split = %q/%q#%d; want workspace/repo-slug#42", getter.gotWorkspace, getter.gotRepoSlug, getter.gotID)
	}
}

func TestBitbucketChecker_OPENState(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN"})
	open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
	if err != nil || !open {
		t.Fatalf("open=%v err=%v; want open,nil", open, err)
	}
}

func TestBitbucketChecker_ClosedStates(t *testing.T) {
	tests := []struct {
		name  string
		state string
	}{
		{"MERGED", "MERGED"},
		{"DECLINED", "DECLINED"},
		{"SUPERSEDED", "SUPERSEDED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: tt.state})
			open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
			if err != nil {
				t.Fatalf("IsOpen: %v", err)
			}
			if open {
				t.Errorf("%s reported open; want closed", tt.state)
			}
		})
	}
}

func TestBitbucketChecker_PropagatesError(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: errors.New("boom")})
	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
	if err == nil {
		t.Fatal("expected error to propagate (so the row is left untouched)")
	}
	if errors.Is(err, domain.ErrPRNotFound) {
		t.Fatal("a plain (non-404) error must not be treated as not-found")
	}
}

func TestBitbucketChecker_NotFoundMapsToErrPRNotFound(t *testing.T) {
	apiErr := &bitbucket.APIError{Method: "get-pull-request", Status: http.StatusNotFound, Message: "Not Found"}
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
	if !errors.Is(err, domain.ErrPRNotFound) {
		t.Fatalf("err = %v; want it to match ErrPRNotFound", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %q; want it to preserve the underlying 404 detail", err)
	}
}

func TestBitbucketChecker_Non404APIErrorPropagates(t *testing.T) {
	apiErr := &bitbucket.APIError{Method: "get-pull-request", Status: http.StatusInternalServerError, Message: "boom"}
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if errors.Is(err, domain.ErrPRNotFound) {
		t.Fatal("a non-404 API error must not be treated as not-found")
	}
}

func TestBitbucketChecker_OpenDraftMapsToErrPRDraft(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN", draft: true})

	open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)
	if open {
		t.Error("a draft PR must not be reported open")
	}
	if !errors.Is(err, domain.ErrPRDraft) {
		t.Fatalf("err = %v; want it to match ErrPRDraft", err)
	}
}

func TestBitbucketChecker_MergedDraftStillMapsToErrPRDraft(t *testing.T) {
	// A draft must never stay in the database even when Bitbucket also reports it
	// merged — the draft flag wins over the merged disposition.
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "MERGED", draft: true})

	if _, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1); !errors.Is(err, domain.ErrPRDraft) {
		t.Fatalf("err = %v; want it to match ErrPRDraft", err)
	}
}

func TestBitbucketChecker_RejectsBadRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository string
	}{
		{"no slash", "no-slash"},
		{"empty", ""},
		{"too many parts", "a/b/c"},
		{"empty workspace", "/repo-slug"},
		{"empty slug", "workspace/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN"})
			_, err := checker.IsOpen(context.Background(), tt.repository, 1)
			if err == nil {
				t.Fatal("expected error for malformed repository")
			}
			if errors.Is(err, domain.ErrPRNotFound) || errors.Is(err, domain.ErrPRDraft) {
				t.Fatalf("err = %v; want a validation error, not a sentinel", err)
			}
		})
	}
}
