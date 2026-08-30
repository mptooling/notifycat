package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
)

type fakeHandler struct {
	applicable func(kernel.Event) bool
	handle     func(context.Context, kernel.Event) error
	called     int
}

func (h *fakeHandler) Applicable(event kernel.Event) bool { return h.applicable(event) }

func (h *fakeHandler) Handle(ctx context.Context, event kernel.Event) error {
	h.called++
	return h.handle(ctx, event)
}

func handlerThatSkips() *fakeHandler {
	return &fakeHandler{
		applicable: func(kernel.Event) bool { return false },
		handle:     func(context.Context, kernel.Event) error { return nil },
	}
}

func handlerThatHandles(err error) *fakeHandler {
	return &fakeHandler{
		applicable: func(kernel.Event) bool { return true },
		handle:     func(context.Context, kernel.Event) error { return err },
	}
}

func TestDispatcher_RunsFirstApplicableHandler(t *testing.T) {
	skipped := handlerThatSkips()
	matched := handlerThatHandles(nil)
	shadowed := handlerThatHandles(nil)
	dispatcher := application.NewDispatcher(discardLogger(), []domain.Handler{skipped, matched, shadowed})

	err := dispatcher.Dispatch(context.Background(), kernel.Event{Kind: kernel.KindOpened})

	require.NoError(t, err)
	assert.Zero(t, skipped.called, "a non-applicable handler never runs")
	assert.Equal(t, 1, matched.called)
	assert.Zero(t, shadowed.called, "only the first applicable handler runs")
}

func TestDispatcher_NoApplicableHandlerIsNotError(t *testing.T) {
	dispatcher := application.NewDispatcher(discardLogger(), []domain.Handler{handlerThatSkips()})

	err := dispatcher.Dispatch(context.Background(), kernel.Event{})

	assert.NoError(t, err)
}

func TestDispatcher_PropagatesHandlerError(t *testing.T) {
	want := errors.New("boom")
	dispatcher := application.NewDispatcher(discardLogger(), []domain.Handler{handlerThatHandles(want)})

	err := dispatcher.Dispatch(context.Background(), kernel.Event{})

	assert.ErrorIs(t, err, want)
}

func TestDispatcher_NoApplicableHandlerLogsContext(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dispatcher := application.NewDispatcher(logger, []domain.Handler{handlerThatSkips()})

	err := dispatcher.Dispatch(context.Background(), kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	})

	require.NoError(t, err)
	assertLogFields(t, logged.Bytes(), map[string]any{
		"level":      "DEBUG",
		"msg":        "ignored webhook event",
		"reason":     "no_handler",
		"handler":    "",
		"provider":   "github",
		"kind":       "unknown",
		"repository": "octo/widget",
		"pr":         float64(42),
	})
}
