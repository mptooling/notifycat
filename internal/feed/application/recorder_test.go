package application_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/kernel"
)

func newRecorder(store *fakePullRequestStore, catalog *fakeCatalog) *application.Recorder {
	return application.NewRecorder(store, catalog, &fakeClock{now: now})
}

func TestRecorder_Record(t *testing.T) {
	testCases := []struct {
		name        string
		kind        kernel.EventKind
		wantUpserts int
		wantDeletes int
	}{
		{name: "opened", kind: kernel.KindOpened, wantUpserts: 1},
		{name: "ready_for_review", kind: kernel.KindReadyForReview, wantUpserts: 1},
		{name: "updated", kind: kernel.KindUpdated, wantUpserts: 1},
		{name: "reopened", kind: kernel.KindReopened, wantUpserts: 1},
		{name: "closed", kind: kernel.KindClosed, wantDeletes: 1},
		{name: "merged", kind: kernel.KindMerged, wantDeletes: 1},
		{name: "converted_to_draft", kind: kernel.KindConvertedToDraft, wantDeletes: 1},
		{name: "approved", kind: kernel.KindApproved},
		{name: "commented", kind: kernel.KindCommented},
		{name: "unknown", kind: kernel.KindUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakePullRequestStore{}
			recorder := newRecorder(store, &fakeCatalog{repositories: []string{"acme/api"}})
			event := kernel.Event{Kind: testCase.kind, Repository: "acme/api", PR: kernel.PR{Number: 42}}

			err := recorder.Record(t.Context(), event)

			require.NoError(t, err)
			assert.Len(t, store.upserts, testCase.wantUpserts)
			require.Len(t, store.deletes, testCase.wantDeletes)
			if testCase.wantDeletes == 1 {
				assert.Equal(t, deleteCall{repository: "acme/api", number: 42}, store.deletes[0])
			}
		})
	}
}

func TestRecorder_SkipsUnconfiguredRepository(t *testing.T) {
	store := &fakePullRequestStore{}
	recorder := newRecorder(store, &fakeCatalog{})

	openedErr := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindOpened, Repository: "acme/api", PR: kernel.PR{Number: 1}})
	closedErr := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindClosed, Repository: "acme/api", PR: kernel.PR{Number: 1}})

	require.NoError(t, openedErr)
	require.NoError(t, closedErr)
	assert.Empty(t, store.upserts)
	assert.Empty(t, store.deletes)
}

func TestRecorder_StoresFeedFields(t *testing.T) {
	store := &fakePullRequestStore{}
	recorder := newRecorder(store, &fakeCatalog{repositories: []string{"acme/api"}})
	createdAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "Acme/API",
		PR: kernel.PR{
			Number: 42, Title: "Add feed", URL: "https://example.test/pr/42", Body: "the body",
			HeadSHA: "abc123", AuthorID: "9001", SourceBranch: "feat/x", TargetBranch: "main",
			CreatedAt: createdAt, Author: "octocat",
		},
		Sender: kernel.Sender{Login: "octocat"},
	}

	err := recorder.Record(t.Context(), event)

	require.NoError(t, err)
	require.Len(t, store.upserts, 1)
	stored := store.upserts[0]
	assert.Equal(t, domain.PullRequest{
		Repository: "acme/api", Number: 42, Title: "Add feed", Description: "the body",
		URL: "https://example.test/pr/42", AuthorID: "9001", SourceBranch: "feat/x", TargetBranch: "main",
		HeadSHA: "abc123", CreatedAt: createdAt.UTC(), UpdatedAt: now,
	}, stored)
	assert.Equal(t, time.UTC, stored.CreatedAt.Location())
	assert.NotContains(t, fmt.Sprintf("%+v", stored), "octocat", "the feed never stores an author name")
}

func TestRecorder_ReadyForReviewRefreshesHeadSHA(t *testing.T) {
	store := &fakePullRequestStore{}
	recorder := newRecorder(store, &fakeCatalog{repositories: []string{"acme/api"}})

	firstErr := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindReadyForReview, Repository: "acme/api", PR: kernel.PR{Number: 1, HeadSHA: "old"}})
	secondErr := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindReadyForReview, Repository: "acme/api", PR: kernel.PR{Number: 1, HeadSHA: "new"}})

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Len(t, store.upserts, 2)
	assert.Equal(t, "new", store.upserts[1].HeadSHA)
}

func TestRecorder_ReturnsStoreError(t *testing.T) {
	storeErr := errors.New("disk full")
	store := &fakePullRequestStore{upsertErr: storeErr}
	recorder := newRecorder(store, &fakeCatalog{repositories: []string{"acme/api"}})

	err := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindOpened, Repository: "acme/api", PR: kernel.PR{Number: 1}})

	require.ErrorIs(t, err, storeErr)
}

func TestRecorder_ReturnsDeleteError(t *testing.T) {
	storeErr := errors.New("disk full")
	store := &fakePullRequestStore{deleteErr: storeErr}
	recorder := newRecorder(store, &fakeCatalog{repositories: []string{"acme/api"}})

	err := recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindClosed, Repository: "acme/api", PR: kernel.PR{Number: 1}})

	require.ErrorIs(t, err, storeErr)
}
