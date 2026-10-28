package infrastructure_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeSubscriptions struct {
	subscriptions   []domain.Subscription
	available       domain.ConfiguredRepositories
	err             error
	listCalls       int
	addCalls        int
	removeCalls     int
	availableCalls  int
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

func (fake *fakeSubscriptions) Available() domain.ConfiguredRepositories {
	fake.availableCalls++
	return fake.available
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

type feedFixture struct {
	subscriptions *fakeSubscriptions
	pullRequests  *fakePullRequests
	logs          *bytes.Buffer
	mux           *http.ServeMux
}

func newFeedFixture() feedFixture {
	fixture := feedFixture{
		subscriptions: &fakeSubscriptions{},
		pullRequests:  &fakePullRequests{},
		logs:          &bytes.Buffer{},
		mux:           http.NewServeMux(),
	}
	logger := slog.New(slog.NewTextHandler(fixture.logs, nil))
	for _, route := range infrastructure.NewRoutes(fixture.subscriptions, fixture.pullRequests, logger) {
		fixture.mux.Handle(route.Pattern, route.Handler)
	}
	return fixture
}

func (fixture feedFixture) do(method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	request = request.WithContext(accessdomain.WithPrincipal(request.Context(), accessdomain.Principal{TokenID: "t1", User: "alice"}))
	recorder := httptest.NewRecorder()
	fixture.mux.ServeHTTP(recorder, request)
	return recorder
}

var aliceSubscriber = domain.Subscriber{TokenID: "t1", User: "alice"}
