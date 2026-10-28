package application_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

var now = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

const tokenID = "fedcba9876543210fedcba9876543210"

var subscriber = domain.Subscriber{TokenID: tokenID, User: "alice"}

type fakeSubscriptionStore struct {
	subscriptions []domain.Subscription
	addCalls      []domain.NewSubscriptions
	deleteErr     error
	listErr       error
	addErr        error
	nextID        int64
}

func (store *fakeSubscriptionStore) seed(repositories ...string) {
	for _, repository := range repositories {
		store.nextID++
		store.subscriptions = append(store.subscriptions, domain.Subscription{
			ID: store.nextID, TokenID: tokenID, Repository: repository, CreatedAt: now,
		})
	}
}

func (store *fakeSubscriptionStore) List(_ context.Context, token string) ([]domain.Subscription, error) {
	if store.listErr != nil {
		return nil, store.listErr
	}
	var listed []domain.Subscription
	for _, subscription := range store.subscriptions {
		if subscription.TokenID == token {
			listed = append(listed, subscription)
		}
	}
	return listed, nil
}

func (store *fakeSubscriptionStore) Add(_ context.Context, request domain.NewSubscriptions) (int, error) {
	store.addCalls = append(store.addCalls, request)
	if store.addErr != nil {
		return 0, store.addErr
	}
	inserted := 0
	for _, repository := range request.Repositories {
		exists := slices.ContainsFunc(store.subscriptions, func(subscription domain.Subscription) bool {
			return subscription.TokenID == request.TokenID && subscription.Repository == repository
		})
		if exists {
			continue
		}
		store.seed(repository)
		inserted++
	}
	return inserted, nil
}

func (store *fakeSubscriptionStore) Delete(_ context.Context, token string, id int64) (domain.Subscription, error) {
	if store.deleteErr != nil {
		return domain.Subscription{}, store.deleteErr
	}
	for index, subscription := range store.subscriptions {
		if subscription.TokenID == token && subscription.ID == id {
			store.subscriptions = slices.Delete(store.subscriptions, index, index+1)
			return subscription, nil
		}
	}
	return domain.Subscription{}, domain.ErrSubscriptionNotFound
}

type deleteCall struct {
	repository string
	number     int
}

type fakePullRequestStore struct {
	upserts       []domain.PullRequest
	deletes       []deleteCall
	repositories  []string
	changed       []domain.PullRequest
	changedCalls  []changedCall
	upsertErr     error
	deleteErr     error
	repositoryErr error
}

type changedCall struct {
	repositories []string
	since        time.Time
}

func (store *fakePullRequestStore) Upsert(_ context.Context, pullRequest domain.PullRequest) error {
	if store.upsertErr != nil {
		return store.upsertErr
	}
	store.upserts = append(store.upserts, pullRequest)
	return nil
}

func (store *fakePullRequestStore) Delete(_ context.Context, repository string, number int) error {
	if store.deleteErr != nil {
		return store.deleteErr
	}
	store.deletes = append(store.deletes, deleteCall{repository: repository, number: number})
	return nil
}

func (store *fakePullRequestStore) Repositories(context.Context) ([]string, error) {
	return store.repositories, store.repositoryErr
}

func (store *fakePullRequestStore) ChangedSince(_ context.Context, repositories []string, since time.Time) ([]domain.PullRequest, error) {
	store.changedCalls = append(store.changedCalls, changedCall{repositories: repositories, since: since})
	return store.changed, nil
}

type fakeDeliveryStore struct {
	claims  []domain.Claim
	claimed []domain.PullRequest
}

func (store *fakeDeliveryStore) Claim(_ context.Context, claim domain.Claim) ([]domain.PullRequest, error) {
	store.claims = append(store.claims, claim)
	return store.claimed, nil
}

type fakeCatalog struct {
	repositories []string
	calls        int
}

func (catalog *fakeCatalog) IsConfigured(repository string) bool {
	return slices.Contains(catalog.repositories, repository)
}

func (catalog *fakeCatalog) Configured(repositories []string) []string {
	catalog.calls++
	var configured []string
	for _, repository := range repositories {
		if catalog.IsConfigured(repository) {
			configured = append(configured, repository)
		}
	}
	return configured
}

type fakeClock struct {
	now time.Time
}

func (clock *fakeClock) Now() time.Time { return clock.now }

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buffer, nil)), buffer
}
