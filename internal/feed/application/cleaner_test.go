package application_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
)

var errStore = errors.New("database is locked")

func newCleaner(store *fakePullRequestStore) *application.PullRequestCleaner {
	return application.NewPullRequestCleaner(store, &fakeCatalog{repositories: []string{"acme/api", "acme/new"}})
}

func TestPullRequestCleaner_RemoveAll(t *testing.T) {
	store := &fakePullRequestStore{removedCount: 4}

	removed, err := newCleaner(store).RemoveAll(t.Context())

	require.NoError(t, err)
	assert.Equal(t, 4, removed)
	assert.Equal(t, 1, store.deleteAllCalls)
}

func TestPullRequestCleaner_RemoveAllError(t *testing.T) {
	store := &fakePullRequestStore{deleteAllErr: errStore}

	_, err := newCleaner(store).RemoveAll(t.Context())

	assert.ErrorIs(t, err, errStore)
}

func TestPullRequestCleaner_RemoveUnconfigured(t *testing.T) {
	testCases := []struct {
		name        string
		stored      []string
		wantDeleted [][]string
		wantRemoved int
	}{
		{name: "unmapped repositories removed", stored: []string{"acme/api", "old/gone", "acme/new", "beta/left"}, wantDeleted: [][]string{{"old/gone", "beta/left"}}, wantRemoved: 3},
		{name: "every repository mapped", stored: []string{"acme/api", "acme/new"}},
		{name: "nothing stored"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakePullRequestStore{repositories: testCase.stored, removedCount: 3}

			removed, err := newCleaner(store).RemoveUnconfigured(t.Context())

			require.NoError(t, err)
			assert.Equal(t, testCase.wantRemoved, removed)
			assert.Equal(t, testCase.wantDeleted, store.deletedRepositories)
			assert.Zero(t, store.deleteAllCalls)
		})
	}
}

func TestPullRequestCleaner_RemoveUnconfiguredErrors(t *testing.T) {
	testCases := []struct {
		name  string
		store *fakePullRequestStore
	}{
		{name: "listing fails", store: &fakePullRequestStore{repositoryErr: errStore}},
		{name: "delete fails", store: &fakePullRequestStore{repositories: []string{"old/gone"}, deleteRepositoriesErr: errStore}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := newCleaner(testCase.store).RemoveUnconfigured(t.Context())

			assert.ErrorIs(t, err, errStore)
		})
	}
}
