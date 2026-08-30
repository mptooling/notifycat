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
	"github.com/mptooling/notifycat/internal/platform/github"
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
	getter := &fakePRGetter{state: "closed"}
	checker := infrastructure.NewGitHubChecker(getter)

	open, err := checker.IsOpen(context.Background(), "acme/web", 42)

	require.NoError(t, err)
	assert.False(t, open)
	assert.Equal(t, "acme", getter.gotOwner)
	assert.Equal(t, "web", getter.gotRepo)
	assert.Equal(t, 42, getter.gotNumber)
}

func TestGitHubChecker_OpenState(t *testing.T) {
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{state: "open"})

	open, err := checker.IsOpen(context.Background(), "acme/web", 1)

	require.NoError(t, err)
	assert.True(t, open)
}

func TestGitHubChecker_PropagatesError(t *testing.T) {
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{err: errors.New("boom")})

	_, err := checker.IsOpen(context.Background(), "acme/web", 1)

	require.Error(t, err, "the row is left untouched when the check fails")
	assert.NotErrorIs(t, err, domain.ErrPRNotFound)
}

func TestGitHubChecker_NotFoundMapsToErrPRNotFound(t *testing.T) {
	apiErr := &github.APIError{Method: "get-pull-request", Status: http.StatusNotFound, Message: "Not Found"}
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "acme/web", 1)

	assert.ErrorIs(t, err, domain.ErrPRNotFound)
	assert.ErrorContains(t, err, "404", "the underlying detail survives the wrap")
}

func TestGitHubChecker_Non404APIErrorPropagates(t *testing.T) {
	apiErr := &github.APIError{Method: "get-pull-request", Status: http.StatusInternalServerError, Message: "boom"}
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{err: apiErr})

	_, err := checker.IsOpen(context.Background(), "acme/web", 1)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrPRNotFound)
}

func TestGitHubChecker_OpenDraftMapsToErrPRDraft(t *testing.T) {
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{state: "open", draft: true})

	open, err := checker.IsOpen(context.Background(), "acme/web", 1)

	assert.False(t, open)
	assert.ErrorIs(t, err, domain.ErrPRDraft)
}

func TestGitHubChecker_ClosedDraftStillMapsToErrPRDraft(t *testing.T) {
	// A draft must never stay in the database even when GitHub also reports it
	// closed — the draft flag wins over the closed disposition.
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{state: "closed", draft: true})

	_, err := checker.IsOpen(context.Background(), "acme/web", 1)

	assert.ErrorIs(t, err, domain.ErrPRDraft)
}

func TestGitHubChecker_RejectsBadRepository(t *testing.T) {
	checker := infrastructure.NewGitHubChecker(&fakePRGetter{state: "open"})

	_, err := checker.IsOpen(context.Background(), "no-slash", 1)

	assert.Error(t, err)
}
