package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// errInjected is a sentinel used by tests that inject failures.
var errInjected = errors.New("injected failure")

func draftEvent(repository string, prNumber int) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindConvertedToDraft,
		Repository: repository,
		PR:         kernel.PR{Number: prNumber},
	}
}

func TestDraftHandler_Applicable(t *testing.T) {
	handler := application.NewDraftHandler(newFakeMessageStore(), &fakeMessenger{}, discardLogger())

	assert.True(t, handler.Applicable(kernel.Event{Kind: kernel.KindConvertedToDraft}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindOpened}))
}

func TestDraftHandler_Handle_DeletesMessageAndRow(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	messenger := &fakeMessenger{}
	handler := application.NewDraftHandler(store, messenger, discardLogger())

	err := handler.Handle(context.Background(), draftEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.NotEmpty(t, messenger.deletes)
	_, err = store.Messages(context.Background(), "octo/widget", 42)
	assert.ErrorIs(t, err, routingdomain.ErrNotFound)
}

func TestDraftHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	store := newFakeMessageStore()
	messenger := &fakeMessenger{}
	logger, logged := captureLogger()
	handler := application.NewDraftHandler(store, messenger, logger)

	err := handler.Handle(context.Background(), draftEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.Empty(t, messenger.deletes, "nothing stored means nothing to delete")
	assertLogFields(t, logged.Bytes(), map[string]any{
		"level":      "INFO",
		"msg":        "ignored webhook event",
		"reason":     "no_stored_message",
		"handler":    "draft",
		"provider":   "github",
		"kind":       "converted_to_draft",
		"repository": "octo/widget",
		"pr":         float64(42),
	})
}

func TestDraftHandler_DeletesEveryMessageAndRow(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("acme/web", 7,
		domain.Message{Channel: "C0A", MessageID: "100.1"},
		domain.Message{Channel: "C0B", MessageID: "200.1"},
	)
	messenger := &fakeMessenger{}
	handler := application.NewDraftHandler(store, messenger, discardLogger())

	err := handler.Handle(context.Background(), draftEvent("acme/web", 7))

	require.NoError(t, err)
	assert.Len(t, messenger.deletes, 2, "every channel's message is deleted")
	_, err = store.Messages(context.Background(), "acme/web", 7)
	assert.ErrorIs(t, err, routingdomain.ErrNotFound)
}
