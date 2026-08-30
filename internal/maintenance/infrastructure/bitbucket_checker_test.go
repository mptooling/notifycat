package infrastructure_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	require.NoError(t, err)
	assert.False(t, open)
	assert.Equal(t, "workspace", getter.gotWorkspace)
	assert.Equal(t, "repo-slug", getter.gotRepoSlug)
	assert.Equal(t, 42, getter.gotID)
}

func TestBitbucketChecker_OPENState(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN"})

	open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	require.NoError(t, err)
	assert.True(t, open)
}

func TestBitbucketChecker_ClosedStates(t *testing.T) {
	for _, state := range []string{"MERGED", "DECLINED", "SUPERSEDED"} {
		t.Run(state, func(t *testing.T) {
			checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: state})

			open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

			require.NoError(t, err)
			assert.False(t, open)
		})
	}
}

func TestBitbucketChecker_PropagatesError(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: errors.New("boom")})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	require.Error(t, err, "the row is left untouched when the check fails")
	assert.NotErrorIs(t, err, domain.ErrPRNotFound)
}

func TestBitbucketChecker_NotFoundMapsToErrPRNotFound(t *testing.T) {
	apiErr := &bitbucket.APIError{Method: "get-pull-request", Status: http.StatusNotFound, Message: "Not Found"}
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	assert.ErrorIs(t, err, domain.ErrPRNotFound)
	assert.ErrorContains(t, err, "404", "the underlying detail survives the wrap")
}

func TestBitbucketChecker_Non404APIErrorPropagates(t *testing.T) {
	apiErr := &bitbucket.APIError{Method: "get-pull-request", Status: http.StatusInternalServerError, Message: "boom"}
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrPRNotFound)
}

func TestBitbucketChecker_OpenDraftMapsToErrPRDraft(t *testing.T) {
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN", draft: true})

	open, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	assert.False(t, open)
	assert.ErrorIs(t, err, domain.ErrPRDraft)
}

func TestBitbucketChecker_MergedDraftStillMapsToErrPRDraft(t *testing.T) {
	// A draft must never stay in the database even when Bitbucket also reports it
	// merged — the draft flag wins over the merged disposition.
	checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "MERGED", draft: true})

	_, err := checker.IsOpen(context.Background(), "workspace/repo-slug", 1)

	assert.ErrorIs(t, err, domain.ErrPRDraft)
}

func TestBitbucketChecker_RejectsBadRepository(t *testing.T) {
	testCases := []struct {
		name       string
		repository string
	}{
		{"no slash", "no-slash"},
		{"empty", ""},
		{"too many parts", "a/b/c"},
		{"empty workspace", "/repo-slug"},
		{"empty slug", "workspace/"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			checker := infrastructure.NewBitbucketChecker(&fakeBitbucketPRGetter{state: "OPEN"})

			_, err := checker.IsOpen(context.Background(), testCase.repository, 1)

			require.Error(t, err)
			assert.NotErrorIs(t, err, domain.ErrPRNotFound, "a malformed repository is a validation error, not a sentinel")
			assert.NotErrorIs(t, err, domain.ErrPRDraft)
		})
	}
}
