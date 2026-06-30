package slackhook_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/slackhook"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHandler_ParsesAndForwardsToSink(t *testing.T) {
	var got slackhook.Interaction
	sink := func(_ context.Context, in slackhook.Interaction) error {
		got = in
		return nil
	}
	handler := slackhook.NewHandler(sink, discardLogger())

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

func TestHandler_MalformedPayloadReturns200(t *testing.T) {
	// After a valid signature (enforced by the middleware), an unparseable
	// payload is ignored with a 200 — Slack retries on any non-200, and there
	// is nothing for it to retry into.
	called := false
	sink := func(_ context.Context, _ slackhook.Interaction) error {
		called = true
		return nil
	}
	handler := slackhook.NewHandler(sink, discardLogger())

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

func TestHandler_NilSinkStillReturns200(t *testing.T) {
	handler := slackhook.NewHandler(nil, discardLogger())

	body := formEncode(`{"type": "block_actions"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}
