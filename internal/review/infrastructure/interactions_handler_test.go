package infrastructure

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// postInteraction drives the interactions handler with an already-encoded body.
func postInteraction(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestInteractionsHandler_ParsesAndForwardsToSink(t *testing.T) {
	var got Interaction
	sink := func(_ context.Context, interaction Interaction) error {
		got = interaction
		return nil
	}
	body := formEncode(`{
		"type": "block_actions",
		"user": {"id": "U1"},
		"actions": [{"action_id": "start_review", "value": "octo/widget#42"}]
	}`)

	recorder := postInteraction(NewInteractionsHandler(sink, discardLogger()), string(body))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "block_actions", got.Type)
	require.Len(t, got.Actions, 1)
	assert.Equal(t, "start_review", got.Actions[0].ActionID)
}

func TestInteractionsHandler_MalformedPayloadReturns200(t *testing.T) {
	// After a valid signature (enforced by the middleware), an unparseable
	// payload is ignored with a 200 — Slack retries on any non-200, and there
	// is nothing for it to retry into.
	called := false
	sink := func(_ context.Context, _ Interaction) error {
		called = true
		return nil
	}

	recorder := postInteraction(NewInteractionsHandler(sink, discardLogger()), "payload=not-json")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.False(t, called, "a malformed payload never reaches the sink")
}

func TestInteractionsHandler_NilSinkStillReturns200(t *testing.T) {
	body := formEncode(`{"type": "block_actions"}`)

	recorder := postInteraction(NewInteractionsHandler(nil, discardLogger()), string(body))

	assert.Equal(t, http.StatusOK, recorder.Code)
}
