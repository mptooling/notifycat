package infrastructure_test

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeSubscriptions struct {
	subscriptions   []domain.Subscription
	err             error
	listCalls       int
	addCalls        int
	removeCalls     int
	gotSubscriber   domain.Subscriber
	gotRepositories []string
	gotID           int64
}

func (fake *fakeSubscriptions) List(_ context.Context, subscriber domain.Subscriber) ([]domain.Subscription, error) {
	fake.listCalls++
	fake.gotSubscriber = subscriber
	return fake.subscriptions, fake.err
}

func (fake *fakeSubscriptions) Add(_ context.Context, subscriber domain.Subscriber, repositories []string) ([]domain.Subscription, error) {
	fake.addCalls++
	fake.gotSubscriber = subscriber
	fake.gotRepositories = repositories
	return fake.subscriptions, fake.err
}

func (fake *fakeSubscriptions) Remove(_ context.Context, subscriber domain.Subscriber, id int64) error {
	fake.removeCalls++
	fake.gotSubscriber = subscriber
	fake.gotID = id
	return fake.err
}

type fakePullRequests struct {
	pullRequests  []domain.PullRequest
	err           error
	nextCalls     int
	sinceCalls    int
	gotSubscriber domain.Subscriber
	gotSince      time.Time
}

func (fake *fakePullRequests) Next(_ context.Context, subscriber domain.Subscriber) ([]domain.PullRequest, error) {
	fake.nextCalls++
	fake.gotSubscriber = subscriber
	return fake.pullRequests, fake.err
}

func (fake *fakePullRequests) Since(_ context.Context, subscriber domain.Subscriber, since time.Time) ([]domain.PullRequest, error) {
	fake.sinceCalls++
	fake.gotSubscriber = subscriber
	fake.gotSince = since
	return fake.pullRequests, fake.err
}
