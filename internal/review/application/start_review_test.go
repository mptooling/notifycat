package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/review/application"
	"github.com/mptooling/notifycat/internal/review/domain"
)

func sampleCommand() domain.StartReviewCommand {
	return domain.StartReviewCommand{
		Repository: "octo/web",
		PRNumber:   42,
		Reviewer:   domain.Reviewer{UserID: "U1", UserName: "alice"},
		Message:    domain.MessageRef{Channel: "C1", TS: "111.222", RawBlocks: []byte(`[{"type":"section"}]`), Fallback: "PR #42"},
	}
}

func newHandler(recorder *fakeRecorder, messages *fakeMessageChecker, decorator *fakeDecorator) *application.Handler {
	return application.NewHandler(domain.HandlerParams{
		Recorder:  recorder,
		Messages:  messages,
		Decorator: decorator,
		Logger:    discardLogger(),
		Now:       func() time.Time { return time.Time{} },
	})
}

func TestHandle_HappyPath_RecordsAndAppendsMarker(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{}
	handler := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	err := handler.Handle(context.Background(), sampleCommand())

	require.NoError(t, err)
	require.Len(t, recorder.started, 1)
	assert.Equal(t, startCall{repository: "octo/web", prNumber: 42, userID: "U1", userName: "alice"}, recorder.started[0])
	require.Len(t, decorator.calls, 1)
	assert.Equal(t, "U1", decorator.calls[0].reviewer.UserID)
	assert.Equal(t, "C1", decorator.calls[0].message.Channel)
	assert.Equal(t, "111.222", decorator.calls[0].message.TS)
}

func TestHandle_DuplicateAppLevel_NoOp(t *testing.T) {
	recorder := &fakeRecorder{active: true}
	decorator := &fakeDecorator{}
	handler := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	err := handler.Handle(context.Background(), sampleCommand())

	require.NoError(t, err)
	assert.Empty(t, recorder.started, "the same reviewer clicking twice records nothing new")
	assert.Empty(t, decorator.calls)
}

func TestHandle_DuplicateDBRace_NoOp(t *testing.T) {
	recorder := &fakeRecorder{startErr: domain.ErrActiveReviewExists}
	decorator := &fakeDecorator{}
	handler := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	err := handler.Handle(context.Background(), sampleCommand())

	require.NoError(t, err)
	assert.Empty(t, decorator.calls, "the unique index caught the race, so the message stays as it is")
}

func TestHandle_UnknownMessage_Ignored(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{}
	handler := newHandler(recorder, &fakeMessageChecker{has: false}, decorator)

	err := handler.Handle(context.Background(), sampleCommand())

	require.NoError(t, err)
	assert.Empty(t, recorder.started, "an untracked PR is never acted on")
	assert.Empty(t, decorator.calls)
}

func TestHandle_UpdateFailure_Swallowed(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{err: errors.New("slack down")}
	handler := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	err := handler.Handle(context.Background(), sampleCommand())

	require.NoError(t, err, "a decorate failure must not fail the interaction")
	assert.Len(t, recorder.started, 1, "the review is still recorded")
}
