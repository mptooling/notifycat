package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/kernel"
)

// Recorder implements domain.Recorder.
type Recorder struct {
	pullRequests  domain.PullRequestStore
	catalog       domain.RepositoryCatalog
	clock         domain.Clock
	calendar      domain.WorkCalendar
	partySettings domain.PartySettingsResolver
}

var _ domain.Recorder = (*Recorder)(nil)

// NewRecorder builds a Recorder.
func NewRecorder(params domain.RecorderParams) *Recorder {
	return &Recorder{
		pullRequests:  params.PullRequests,
		catalog:       params.Catalog,
		clock:         params.Clock,
		calendar:      params.Calendar,
		partySettings: params.PartySettings,
	}
}

// Record implements domain.Recorder.
func (recorder *Recorder) Record(ctx context.Context, event kernel.Event) error {
	repository := strings.ToLower(event.Repository)
	if !recorder.catalog.IsConfigured(repository) {
		return nil
	}
	switch event.Kind {
	case kernel.KindOpened, kernel.KindReadyForReview, kernel.KindUpdated, kernel.KindReopened:
		moment := recorder.clock.Now().UTC()
		feedPullRequest := recorder.feedPullRequest(repository, event.PR, moment)
		party, err := recorder.partyState(ctx, domain.PullRequestKey{Repository: repository, Number: event.PR.Number}, moment)
		if err != nil {
			return err
		}
		feedPullRequest.Party = party
		if err := recorder.pullRequests.Upsert(ctx, feedPullRequest); err != nil {
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

func (recorder *Recorder) partyState(ctx context.Context, key domain.PullRequestKey, now time.Time) (domain.PartyState, error) {
	stored, found, err := recorder.pullRequests.Find(ctx, key)
	if err != nil {
		return domain.PartyState{}, fmt.Errorf("find feed pull request: %w", err)
	}
	hasRecordTime := found && !stored.Party.RecordedAt.IsZero()
	resolution := recorder.partySettings.Resolve(ctx, key)
	if resolution.Fallback && hasRecordTime {
		return stored.Party, nil
	}
	recordedAt := now
	if hasRecordTime {
		recordedAt = stored.Party.RecordedAt
	}
	return domain.PartyState{
		Reviewers:  resolution.Settings.Reviewers,
		RecordedAt: recordedAt,
		DeadlineAt: recorder.calendar.AddWorkingTime(recordedAt, resolution.Settings.Deadline),
	}, nil
}

func (recorder *Recorder) feedPullRequest(repository string, pullRequest kernel.PR, now time.Time) domain.PullRequest {
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
		UpdatedAt:    now,
	}
}
