package application_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

var now = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

const tokenID = "fedcba9876543210fedcba9876543210"

var subscriber = domain.Subscriber{TokenID: tokenID, User: "alice"}

type fakeSubscriptionStore struct {
	subscriptions []domain.Subscription
	addCalls      []domain.NewSubscriptions
	setTypeCalls  []domain.TypeChange
	setTypeErr    error
	deleteErr     error
	listErr       error
	addErr        error
	nextID        int64

	deletedRepositories   [][]string
	repositoriesErr       error
	deleteRepositoriesErr error
}

func (store *fakeSubscriptionStore) Repositories(context.Context) ([]string, error) {
	if store.repositoriesErr != nil {
		return nil, store.repositoriesErr
	}
	var repositories []string
	for _, subscription := range store.subscriptions {
		if !slices.Contains(repositories, subscription.Repository) {
			repositories = append(repositories, subscription.Repository)
		}
	}
	return repositories, nil
}

func (store *fakeSubscriptionStore) DeleteRepositories(_ context.Context, repositories []string) (int, error) {
	store.deletedRepositories = append(store.deletedRepositories, repositories)
	if store.deleteRepositoriesErr != nil {
		return 0, store.deleteRepositoriesErr
	}
	before := len(store.subscriptions)
	store.subscriptions = slices.DeleteFunc(store.subscriptions, func(subscription domain.Subscription) bool {
		return slices.Contains(repositories, subscription.Repository)
	})
	return before - len(store.subscriptions), nil
}

func (store *fakeSubscriptionStore) seed(repositories ...string) {
	store.seedOfType(domain.SubscriptionTypeSolo, repositories...)
}

func (store *fakeSubscriptionStore) seedOfType(subscriptionType domain.SubscriptionType, repositories ...string) {
	for _, repository := range repositories {
		store.nextID++
		store.subscriptions = append(store.subscriptions, domain.Subscription{
			ID: store.nextID, TokenID: tokenID, Repository: repository, Type: subscriptionType, CreatedAt: now,
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

func (store *fakeSubscriptionStore) SetType(_ context.Context, token string, change domain.TypeChange) (int, error) {
	store.setTypeCalls = append(store.setTypeCalls, change)
	if store.setTypeErr != nil {
		return 0, store.setTypeErr
	}
	changed := 0
	for index, subscription := range store.subscriptions {
		matches := subscription.TokenID == token && (change.Repositories == nil || slices.Contains(change.Repositories, subscription.Repository))
		if matches && subscription.Type != change.Type {
			store.subscriptions[index].Type = change.Type
			changed++
		}
	}
	return changed, nil
}

type deleteCall struct {
	repository string
	number     int
}

type fakePullRequestStore struct {
	upserts               []domain.PullRequest
	deletes               []deleteCall
	repositories          []string
	changed               []domain.PullRequest
	changedCalls          []domain.SinceQuery
	received              []domain.PullRequest
	receivedCalls         []domain.SinceQuery
	deleteAllCalls        int
	deletedRepositories   [][]string
	removedCount          int
	upsertErr             error
	deleteErr             error
	repositoryErr         error
	deleteAllErr          error
	deleteRepositoriesErr error
	stored                *domain.PullRequest
	findErr               error
	findCalls             int
	findKeys              []domain.PullRequestKey
}

func (store *fakePullRequestStore) Find(_ context.Context, key domain.PullRequestKey) (domain.PullRequest, bool, error) {
	store.findCalls++
	store.findKeys = append(store.findKeys, key)
	if store.findErr != nil {
		return domain.PullRequest{}, false, store.findErr
	}
	if store.stored == nil {
		return domain.PullRequest{}, false, nil
	}
	return *store.stored, true, nil
}

func (store *fakePullRequestStore) ReceivedSince(_ context.Context, query domain.SinceQuery) ([]domain.PullRequest, error) {
	store.receivedCalls = append(store.receivedCalls, query)
	return store.received, nil
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

func (store *fakePullRequestStore) DeleteAll(context.Context) (int, error) {
	store.deleteAllCalls++
	if store.deleteAllErr != nil {
		return 0, store.deleteAllErr
	}
	return store.removedCount, nil
}

func (store *fakePullRequestStore) DeleteRepositories(_ context.Context, repositories []string) (int, error) {
	store.deletedRepositories = append(store.deletedRepositories, repositories)
	if store.deleteRepositoriesErr != nil {
		return 0, store.deleteRepositoriesErr
	}
	return store.removedCount, nil
}

func (store *fakePullRequestStore) Repositories(context.Context) ([]string, error) {
	return store.repositories, store.repositoryErr
}

func (store *fakePullRequestStore) ChangedSince(_ context.Context, query domain.SinceQuery) ([]domain.PullRequest, error) {
	store.changedCalls = append(store.changedCalls, query)
	return store.changed, nil
}

type callLog struct {
	calls []string
}

func (recorded *callLog) add(call string) {
	if recorded != nil {
		recorded.calls = append(recorded.calls, call)
	}
}

type fakeDeliveryStore struct {
	claims      []domain.Claim
	claimed     []domain.PullRequest
	claimErr    error
	partyClaims []domain.PartyClaim
	partyResult domain.PartyClaimResult
	partyErr    error
	calls       *callLog
}

func (store *fakeDeliveryStore) Claim(_ context.Context, claim domain.Claim) ([]domain.PullRequest, error) {
	store.calls.add("claim")
	store.claims = append(store.claims, claim)
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	return store.claimed, nil
}

func (store *fakeDeliveryStore) ClaimParty(_ context.Context, claim domain.PartyClaim) (domain.PartyClaimResult, error) {
	store.calls.add("claim party")
	store.partyClaims = append(store.partyClaims, claim)
	if store.partyErr != nil {
		return domain.PartyClaimResult{}, store.partyErr
	}
	return store.partyResult, nil
}

type pollRecord struct {
	tokenID  string
	polledAt time.Time
}

type fakePollStore struct {
	records []pollRecord
	err     error
	calls   *callLog
}

func (store *fakePollStore) Record(_ context.Context, token string, polledAt time.Time) error {
	store.calls.add("record poll")
	if store.err != nil {
		return store.err
	}
	store.records = append(store.records, pollRecord{tokenID: token, polledAt: polledAt})
	return nil
}

type fakeWorkCalendar struct {
	dayOff       bool
	day          string
	dayOffChecks []time.Time
	dayMoments   []time.Time
	deadline     time.Time
	addCalls     []addWorkingTimeCall
}

type addWorkingTimeCall struct {
	start       time.Time
	workingTime time.Duration
}

func (calendar *fakeWorkCalendar) IsDayOff(moment time.Time) bool {
	calendar.dayOffChecks = append(calendar.dayOffChecks, moment)
	return calendar.dayOff
}

func (calendar *fakeWorkCalendar) Day(moment time.Time) string {
	calendar.dayMoments = append(calendar.dayMoments, moment)
	return calendar.day
}

func (calendar *fakeWorkCalendar) AddWorkingTime(start time.Time, workingTime time.Duration) time.Time {
	calendar.addCalls = append(calendar.addCalls, addWorkingTimeCall{start: start, workingTime: workingTime})
	if !calendar.deadline.IsZero() {
		return calendar.deadline
	}
	return start.Add(workingTime)
}

type fakeCatalog struct {
	repositories []string
	calls        int
}

func (catalog *fakeCatalog) IsConfigured(repository string) bool {
	return slices.Contains(catalog.repositories, repository)
}

func (catalog *fakeCatalog) HasRepositories() bool { return len(catalog.repositories) > 0 }

func (catalog *fakeCatalog) List() domain.ConfiguredRepositories {
	return domain.ConfiguredRepositories{Repositories: catalog.repositories}
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

type fakePartySettings struct {
	settings domain.PartySettings
	err      error
	calls    int
	keys     []domain.PullRequestKey
}

func (resolver *fakePartySettings) Resolve(_ context.Context, key domain.PullRequestKey) (domain.PartySettings, error) {
	resolver.calls++
	resolver.keys = append(resolver.keys, key)
	return resolver.settings, resolver.err
}

type fakeClock struct {
	now  time.Time
	tick time.Duration
}

func (clock *fakeClock) Now() time.Time {
	current := clock.now
	clock.now = clock.now.Add(clock.tick)
	return current
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buffer, nil)), buffer
}

func captureDebugLogger() (*slog.Logger, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug})), buffer
}
