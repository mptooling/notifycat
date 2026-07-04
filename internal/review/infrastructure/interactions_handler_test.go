package infrastructure

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestInteractionsHandler_ParsesAndForwardsToSink(t *testing.T) {
	var got Interaction
	sink := func(_ context.Context, in Interaction) error {
		got = in
		return nil
	}
	handler := NewInteractionsHandler(sink, discardLogger())

	body := formEncode(`{
		"type": "block_actions",
		"user": {"id": "U1"},
		"actions": [{"action_id": "start_review", "value": "octo/widget#42"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if got.Type != "block_actions" || len(got.Actions) != 1 || got.Actions[0].ActionID != "start_review" {
		t.Errorf("sink received %+v", got)
	}
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
	handler := NewInteractionsHandler(sink, discardLogger())

	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", strings.NewReader("payload=not-json"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if called {
		t.Error("sink was called for a malformed payload; want skipped")
	}
}

func TestInteractionsHandler_NilSinkStillReturns200(t *testing.T) {
	handler := NewInteractionsHandler(nil, discardLogger())

	body := formEncode(`{"type": "block_actions"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}
