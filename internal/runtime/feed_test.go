package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
}

func (recorder *fakeRecorder) Record(ctx context.Context, event kernel.Event) error {
	if recorder.log != nil {
		recorder.log.add("record")
	}
	recorder.recorded = append(recorder.recorded, event)
	recorder.contextErrors = append(recorder.contextErrors, ctx.Err())
	return recorder.err
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

	err := dispatcher.Dispatch(ctx, updatedEvent())

	require.NoError(t, err)
	assert.Equal(t, []error{nil}, recorder.contextErrors, "a client that hangs up must not abort the record")
}

func TestBuildWebhookDispatcher_DisabledReturnsPlainDispatcher(t *testing.T) {
	plain := notificationapp.NewDispatcher(discardLogger(), nil)

	built := buildWebhookDispatcher(config.Config{AgentAPIEnabled: false}, plain, &fakeRecorder{}, discardLogger())

	assert.Same(t, plain, built)
}
