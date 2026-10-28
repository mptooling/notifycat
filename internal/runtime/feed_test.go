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

type fakeEventDispatcher struct {
	err error
}

func (dispatcher *fakeEventDispatcher) Dispatch(context.Context, kernel.Event) error {
	return dispatcher.err
}

type fakeRecorder struct {
	err           error
	recorded      []kernel.Event
	contextErrors []error
}

func (recorder *fakeRecorder) Record(ctx context.Context, event kernel.Event) error {
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
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{},
		recorder: &fakeRecorder{err: errRecord},
		logger:   slog.New(slog.NewTextHandler(&logged, nil)),
	}

	err := dispatcher.Dispatch(context.Background(), updatedEvent())

	require.NoError(t, err, "a record failure never changes the webhook response")
	assert.Contains(t, logged.String(), `level=WARN msg="feed pull request not recorded"`)
	assert.Contains(t, logged.String(), "repository=acme/api")
	assert.Contains(t, logged.String(), "pr=42")
	assert.Contains(t, logged.String(), "kind=updated")
	assert.Contains(t, logged.String(), `err="database is locked"`)
}

func TestRecordingDispatcher_RecordsWhenDispatchFails(t *testing.T) {
	recorder := &fakeRecorder{}
	dispatcher := &recordingDispatcher{
		next:     &fakeEventDispatcher{err: errSlack},
		recorder: recorder,
		logger:   discardLogger(),
	}

	err := dispatcher.Dispatch(context.Background(), updatedEvent())

	require.ErrorIs(t, err, errSlack)
	assert.Equal(t, []kernel.Event{updatedEvent()}, recorder.recorded)
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
