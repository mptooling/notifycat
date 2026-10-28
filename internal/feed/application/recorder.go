package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/kernel"
)

// Recorder implements domain.Recorder.
type Recorder struct {
	pullRequests domain.PullRequestStore
	catalog      domain.RepositoryCatalog
	clock        domain.Clock
}

var _ domain.Recorder = (*Recorder)(nil)

// NewRecorder builds a Recorder.
func NewRecorder(pullRequests domain.PullRequestStore, catalog domain.RepositoryCatalog, clock domain.Clock) *Recorder {
	return &Recorder{pullRequests: pullRequests, catalog: catalog, clock: clock}
}

// Record implements domain.Recorder.
func (recorder *Recorder) Record(ctx context.Context, event kernel.Event) error {
	repository := strings.ToLower(event.Repository)
	if !recorder.catalog.IsConfigured(repository) {
		return nil
	}
	switch event.Kind {
	case kernel.KindOpened, kernel.KindReadyForReview, kernel.KindUpdated, kernel.KindReopened:
		if err := recorder.pullRequests.Upsert(ctx, recorder.feedPullRequest(repository, event.PR)); err != nil {
			return fmt.Errorf("upsert feed pull request: %w", err)
		}
	case kernel.KindClosed, kernel.KindMerged, kernel.KindConvertedToDraft:
		if err := recorder.pullRequests.Delete(ctx, repository, event.PR.Number); err != nil {
			return fmt.Errorf("delete feed pull request: %w", err)
		}
	default:
	}
	return nil
}

func (recorder *Recorder) feedPullRequest(repository string, pullRequest kernel.PR) domain.PullRequest {
	return domain.PullRequest{
		Repository:   repository,
		Number:       pullRequest.Number,
		Title:        pullRequest.Title,
		Description:  pullRequest.Body,
		URL:          pullRequest.URL,
		AuthorID:     pullRequest.AuthorID,
		SourceBranch: pullRequest.SourceBranch,
		TargetBranch: pullRequest.TargetBranch,
		HeadSHA:      pullRequest.HeadSHA,
		CreatedAt:    pullRequest.CreatedAt.UTC(),
		UpdatedAt:    recorder.clock.Now().UTC(),
	}
}
