package application_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

func newSubscriptionCleaner(store *fakeSubscriptionStore) *application.SubscriptionCleaner {
	return application.NewSubscriptionCleaner(store, &fakeCatalog{repositories: []string{"acme/api", "acme/new"}})
}

func TestSubscriptionCleaner_RemoveUnconfigured(t *testing.T) {
	testCases := []struct {
		name        string
		subscribed  []string
		wantDeleted [][]string
		wantRemoved int
		wantKept    []string
	}{
		{
			name: "unmapped repositories removed", subscribed: []string{"acme/api", "old/gone", "acme/new", "beta/left"},
			wantDeleted: [][]string{{"old/gone", "beta/left"}}, wantRemoved: 2, wantKept: []string{"acme/api", "acme/new"},
		},
		{
			name: "all repositories subscription kept", subscribed: []string{domain.AllRepositories, "old/gone"},
			wantDeleted: [][]string{{"old/gone"}}, wantRemoved: 1, wantKept: []string{domain.AllRepositories},
		},
		{name: "every repository mapped", subscribed: []string{"acme/api", "acme/new"}, wantKept: []string{"acme/api", "acme/new"}},
		{name: "nothing subscribed"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeSubscriptionStore{}
			store.seed(testCase.subscribed...)

			removed, err := newSubscriptionCleaner(store).RemoveUnconfigured(t.Context())

			require.NoError(t, err)
			assert.Equal(t, testCase.wantRemoved, removed)
			assert.Equal(t, testCase.wantDeleted, store.deletedRepositories)
			var kept []string
			for _, subscription := range store.subscriptions {
				kept = append(kept, subscription.Repository)
			}
			assert.Equal(t, testCase.wantKept, kept)
		})
	}
}

func TestSubscriptionCleaner_RemoveUnconfiguredErrors(t *testing.T) {
	testCases := []struct {
		name  string
		store *fakeSubscriptionStore
	}{
		{name: "listing fails", store: &fakeSubscriptionStore{repositoriesErr: errStore}},
		{name: "delete fails", store: &fakeSubscriptionStore{deleteRepositoriesErr: errStore}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.store.seed("old/gone")

			_, err := newSubscriptionCleaner(testCase.store).RemoveUnconfigured(t.Context())

			assert.ErrorIs(t, err, errStore)
		})
	}
}
