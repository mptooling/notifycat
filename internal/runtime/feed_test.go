package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	feeddomain "github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/kernel"
	notificationapp "github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/platform/config"
)

var (
	errSlack  = errors.New("slack unavailable")
	errRecord = errors.New("database is locked")
)

type callLog struct {
	calls []string
}

func (log *callLog) add(call string) {
	log.calls = append(log.calls, call)
}

type fakeEventDispatcher struct {
	err error
	log *callLog
}

func (dispatcher *fakeEventDispatcher) Dispatch(context.Context, kernel.Event) error {
	if dispatcher.log != nil {
		dispatcher.log.add("dispatch")
	}
	return dispatcher.err
}

type fakeRecorder struct {
	err           error
	log           *callLog
	recorded      []kernel.Event
	contextErrors []error
	deadlines     []time.Time
}

func (recorder *fakeRecorder) Record(ctx context.Context, event kernel.Event) error {
	if recorder.log != nil {
		recorder.log.add("record")
	}
	recorder.recorded = append(recorder.recorded, event)
	recorder.contextErrors = append(recorder.contextErrors, ctx.Err())
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		recorder.deadlines = append(recorder.deadlines, deadline)
	}
	return recorder.err
}

type fakeCleaner struct {
	removed int
	err     error
	calls   []string
}

func (cleaner *fakeCleaner) RemoveAll(context.Context) (int, error) {
	cleaner.calls = append(cleaner.calls, "remove_all")
	return cleaner.removed, cleaner.err
}

func (cleaner *fakeCleaner) RemoveUnconfigured(context.Context) (int, error) {
	cleaner.calls = append(cleaner.calls, "remove_unconfigured")
	return cleaner.removed, cleaner.err
}

type fakeSubscriptionCleaner struct {
	removed feeddomain.RemovedSubscriptions
	err     error
	calls   int
}

func (cleaner *fakeSubscriptionCleaner) RemoveUnconfigured(context.Context) (feeddomain.RemovedSubscriptions, error) {
	cleaner.calls++
	return cleaner.removed, cleaner.err
}

func updatedEvent() kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindUpdated,
		Repository: "acme/api",
		PR:         kernel.PR{Number: 42, HeadSHA: "bbb"},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordingDispatcher_RecordErrorKeepsDispatchResult(t *testing.T) {
	var logged bytes.Buffer
	calls := &callLog{}
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{log: calls},
		recorder: &fakeRecorder{err: errRecord, log: calls},
		logger:   slog.New(slog.NewTextHandler(&logged, nil)),
	}

	err := dispatcher.Dispatch(context.Background(), updatedEvent())

	require.NoError(t, err, "a record failure never changes the webhook response")
	assert.Equal(t, []string{"dispatch", "record"}, calls.calls, "the notification runs before the record")
	assert.Contains(t, logged.String(), `level=WARN msg="feed pull request not recorded"`)
	assert.Contains(t, logged.String(), "repository=acme/api")
	assert.Contains(t, logged.String(), "pr=42")
	assert.Contains(t, logged.String(), "kind=updated")
	assert.Contains(t, logged.String(), `err="database is locked"`)
}

func TestRecordingDispatcher_RecordsWhenDispatchFails(t *testing.T) {
	calls := &callLog{}
	recorder := &fakeRecorder{log: calls}
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{err: errSlack, log: calls},
		recorder: recorder,
		logger:   discardLogger(),
	}

	err := dispatcher.Dispatch(context.Background(), updatedEvent())

	require.ErrorIs(t, err, errSlack)
	assert.Equal(t, []kernel.Event{updatedEvent()}, recorder.recorded)
	assert.Equal(t, []string{"dispatch", "record"}, calls.calls)
}

func TestRecordingDispatcher_DispatchAndRecordBothFailReturnsDispatchError(t *testing.T) {
	calls := &callLog{}
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{err: errSlack, log: calls},
		recorder: &fakeRecorder{err: errRecord, log: calls},
		logger:   discardLogger(),
	}

	err := dispatcher.Dispatch(context.Background(), updatedEvent())

	require.ErrorIs(t, err, errSlack)
	assert.NotErrorIs(t, err, errRecord, "a record failure never replaces the notification error")
	assert.Equal(t, []string{"dispatch", "record"}, calls.calls)
}

func TestRecordingDispatcher_RecordsWithoutCancel(t *testing.T) {
	recorder := &fakeRecorder{}
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{},
		recorder: recorder,
		logger:   discardLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dispatchedAt := time.Now()

	err := dispatcher.Dispatch(ctx, updatedEvent())

	require.NoError(t, err)
	assert.Equal(t, []error{nil}, recorder.contextErrors, "a client that hangs up must not abort the record")
	require.Len(t, recorder.deadlines, 1, "a slow store must not hold the webhook response forever")
	assert.WithinDuration(t, dispatchedAt.Add(feeddomain.RecordTimeout), recorder.deadlines[0], time.Second)
}

func TestBuildWebhookDispatcher_DisabledReturnsPlainDispatcher(t *testing.T) {
	plain := notificationapp.NewDispatcher(discardLogger(), nil)

	built := buildWebhookDispatcher(config.Config{AgentAPIEnabled: false}, plain, &fakeRecorder{}, discardLogger())

	assert.Same(t, plain, built)
}

func TestCleanFeed_PullRequests(t *testing.T) {
	testCases := []struct {
		name            string
		agentAPIEnabled bool
		removed         int
		err             error
		wantCall        string
		wantLog         string
	}{
		{
			name: "agent API off removes every pull request", removed: 2, wantCall: "remove_all",
			wantLog: `level=INFO msg="feed pull requests removed" count=2 reason=agent_api_off`,
		},
		{
			name: "agent API on removes unmapped repositories", agentAPIEnabled: true, removed: 1, wantCall: "remove_unconfigured",
			wantLog: `level=INFO msg="feed pull requests removed" count=1 reason=unmapped`,
		},
		{
			name: "a failure only warns", agentAPIEnabled: true, err: errRecord, wantCall: "remove_unconfigured",
			wantLog: `level=WARN msg="feed cleanup failed" err="database is locked"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logged bytes.Buffer
			cleaner := &fakeCleaner{removed: testCase.removed, err: testCase.err}

			cleanFeed(config.Config{AgentAPIEnabled: testCase.agentAPIEnabled}, cleaner, &fakeSubscriptionCleaner{}, slog.New(slog.NewTextHandler(&logged, nil)))

			assert.Equal(t, []string{testCase.wantCall}, cleaner.calls)
			assert.Contains(t, logged.String(), testCase.wantLog)
		})
	}
}

func TestCleanFeed_NothingRemovedLogsNothing(t *testing.T) {
	var logged bytes.Buffer
	cleaner := &fakeCleaner{}

	cleanFeed(config.Config{AgentAPIEnabled: true}, cleaner, &fakeSubscriptionCleaner{}, slog.New(slog.NewTextHandler(&logged, nil)))

	assert.Equal(t, []string{"remove_unconfigured"}, cleaner.calls)
	assert.Empty(t, logged.String())
}

func TestCleanFeed_Subscriptions(t *testing.T) {
	testCases := []struct {
		name            string
		agentAPIEnabled bool
		removed         feeddomain.RemovedSubscriptions
		err             error
		wantLog         string
	}{
		{name: "agent API on", agentAPIEnabled: true, removed: feeddomain.RemovedSubscriptions{Count: 2, Repositories: []string{"a/b", "c/d"}}, wantLog: `level=INFO msg="feed subscriptions removed" count=2 reason=unmapped repositories="[a/b c/d]"`},
		{name: "agent API off", removed: feeddomain.RemovedSubscriptions{Count: 3, Repositories: []string{"e/f"}}, wantLog: `level=INFO msg="feed subscriptions removed" count=3 reason=unmapped repositories=[e/f]`},
		{name: "a failure only warns", err: errRecord, wantLog: `level=WARN msg="feed cleanup failed" err="database is locked"`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logged bytes.Buffer
			subscriptions := &fakeSubscriptionCleaner{removed: testCase.removed, err: testCase.err}

			cleanFeed(config.Config{AgentAPIEnabled: testCase.agentAPIEnabled}, &fakeCleaner{}, subscriptions, slog.New(slog.NewTextHandler(&logged, nil)))

			assert.Equal(t, 1, subscriptions.calls, "subscriptions are cleaned on every start")
			assert.Contains(t, logged.String(), testCase.wantLog)
		})
	}
}

func TestCleanFeed_PullRequestFailureDoesNotSkipSubscriptions(t *testing.T) {
	subscriptions := &fakeSubscriptionCleaner{}

	cleanFeed(config.Config{AgentAPIEnabled: true}, &fakeCleaner{err: errRecord}, subscriptions, discardLogger())

	assert.Equal(t, 1, subscriptions.calls)
}

func TestCleanFeed_SubscriptionFailureDoesNotSkipPullRequests(t *testing.T) {
	pullRequests := &fakeCleaner{}

	cleanFeed(config.Config{AgentAPIEnabled: true}, pullRequests, &fakeSubscriptionCleaner{err: errRecord}, discardLogger())

	assert.Equal(t, []string{"remove_unconfigured"}, pullRequests.calls)
}
