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

var defaultPartySettings = domain.PartySettings{Reviewers: 1, Deadline: time.Hour}

type recorderFixture struct {
	store    *fakePullRequestStore
	calendar *fakeWorkCalendar
	settings *fakePartySettings
	recorder *application.Recorder
}

func newRecorderFixture(partySettings domain.PartySettings) recorderFixture {
	fixture := recorderFixture{
		store:    &fakePullRequestStore{},
		calendar: &fakeWorkCalendar{},
		settings: &fakePartySettings{settings: partySettings},
	}
	fixture.recorder = application.NewRecorder(domain.RecorderParams{
		PullRequests:  fixture.store,
		Catalog:       &fakeCatalog{repositories: []string{"acme/api"}},
		Clock:         &fakeClock{now: now, tick: time.Minute},
		Calendar:      fixture.calendar,
		PartySettings: fixture.settings,
	})
	return fixture
}

func newRecorder(store *fakePullRequestStore, catalog *fakeCatalog) *application.Recorder {
	return application.NewRecorder(domain.RecorderParams{
		PullRequests:  store,
		Catalog:       catalog,
		Clock:         &fakeClock{now: now},
		Calendar:      &fakeWorkCalendar{},
		PartySettings: &fakePartySettings{settings: defaultPartySettings},
	})
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
		Party: domain.PartyState{Reviewers: 1, RecordedAt: now, DeadlineAt: now.Add(time.Hour)},
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

var openEvent = kernel.Event{Kind: kernel.KindOpened, Repository: "Acme/API", PR: kernel.PR{Number: 42}}

func TestRecorder_StoresThePartyState(t *testing.T) {
	fixture := newRecorderFixture(domain.PartySettings{Reviewers: 2, Deadline: 30 * time.Minute})
	deadline := now.Add(3 * time.Hour)
	fixture.calendar.deadline = deadline

	err := fixture.recorder.Record(t.Context(), openEvent)

	require.NoError(t, err)
	require.Len(t, fixture.store.upserts, 1)
	party := fixture.store.upserts[0].Party
	assert.Equal(t, 2, party.Reviewers)
	assert.WithinDuration(t, now, party.RecordedAt, 0)
	assert.WithinDuration(t, fixture.store.upserts[0].UpdatedAt, party.RecordedAt, 0, "a first record reads the clock once")
	assert.WithinDuration(t, deadline, party.DeadlineAt, 0)
	wantKey := domain.PullRequestKey{Repository: "acme/api", Number: 42}
	assert.Equal(t, []domain.PullRequestKey{wantKey}, fixture.store.findKeys)
	assert.Equal(t, []domain.PullRequestKey{wantKey}, fixture.settings.keys)
	require.Len(t, fixture.calendar.addCalls, 1)
	assert.WithinDuration(t, now, fixture.calendar.addCalls[0].start, 0)
	assert.Equal(t, 30*time.Minute, fixture.calendar.addCalls[0].workingTime)
}

func TestRecorder_KeepsTheFirstRecordTime(t *testing.T) {
	fixture := newRecorderFixture(defaultPartySettings)
	firstRecordedAt := now.Add(-48 * time.Hour)
	fixture.store.stored = &domain.PullRequest{Party: domain.PartyState{RecordedAt: firstRecordedAt}}

	err := fixture.recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindUpdated, Repository: "acme/api", PR: kernel.PR{Number: 42}})

	require.NoError(t, err)
	require.Len(t, fixture.store.upserts, 1)
	assert.WithinDuration(t, firstRecordedAt, fixture.store.upserts[0].Party.RecordedAt, 0)
	require.Len(t, fixture.calendar.addCalls, 1)
	assert.WithinDuration(t, firstRecordedAt, fixture.calendar.addCalls[0].start, 0)
}

func TestRecorder_StoredRowWithoutRecordTimeStartsNow(t *testing.T) {
	fixture := newRecorderFixture(defaultPartySettings)
	fixture.store.stored = &domain.PullRequest{}

	err := fixture.recorder.Record(t.Context(), openEvent)

	require.NoError(t, err)
	require.Len(t, fixture.store.upserts, 1)
	assert.WithinDuration(t, now, fixture.store.upserts[0].Party.RecordedAt, 0)
	require.Len(t, fixture.calendar.addCalls, 1)
	assert.WithinDuration(t, now, fixture.calendar.addCalls[0].start, 0)
}

func TestRecorder_ReturnsFindError(t *testing.T) {
	findErr := errors.New("db locked")
	fixture := newRecorderFixture(defaultPartySettings)
	fixture.store.findErr = findErr

	err := fixture.recorder.Record(t.Context(), openEvent)

	require.ErrorIs(t, err, findErr)
	assert.Zero(t, fixture.settings.calls, "a failed lookup never resolves settings")
	assert.Empty(t, fixture.store.upserts)
}

func TestRecorder_ReturnsResolverError(t *testing.T) {
	resolveErr := errors.New("no settings")
	fixture := newRecorderFixture(defaultPartySettings)
	fixture.settings.err = resolveErr

	err := fixture.recorder.Record(t.Context(), openEvent)

	require.ErrorIs(t, err, resolveErr)
	assert.Empty(t, fixture.store.upserts)
}

func TestRecorder_CloseEventNeitherFindsNorResolves(t *testing.T) {
	fixture := newRecorderFixture(defaultPartySettings)

	err := fixture.recorder.Record(t.Context(), kernel.Event{Kind: kernel.KindClosed, Repository: "acme/api", PR: kernel.PR{Number: 42}})

	require.NoError(t, err)
	assert.Zero(t, fixture.store.findCalls)
	assert.Zero(t, fixture.settings.calls)
}
