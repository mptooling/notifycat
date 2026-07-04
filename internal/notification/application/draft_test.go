package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// errInjected is a sentinel used by tests that inject failures.
var errInjected = errors.New("injected failure")

func decodeLog(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	rec := map[string]any{}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode log: %v (raw=%q)", err, raw)
	}
	return rec
}

func wantFields(t *testing.T, rec map[string]any, fields map[string]any) {
	t.Helper()
	for k, v := range fields {
		if rec[k] != v {
			t.Errorf("log[%q] = %v (%T); want %v (%T)", k, rec[k], rec[k], v, v)
		}
	}
}

func draftEvent(repo string, prNumber int) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindConvertedToDraft,
		Repository: repo,
		PR:         kernel.PR{Number: prNumber},
	}
}

func TestDraftHandler_Applicable(t *testing.T) {
	h := application.NewDraftHandler(newFakeMessageStore(), &fakeMessenger{}, discardLogger())

	if !h.Applicable(kernel.Event{Kind: kernel.KindConvertedToDraft}) {
		t.Error("converted_to_draft should be applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindOpened}) {
		t.Error("opened should not be applicable")
	}
}

func TestDraftHandler_Handle_DeletesMessageAndRow(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	messenger := &fakeMessenger{}
	h := application.NewDraftHandler(store, messenger, discardLogger())

	if err := h.Handle(context.Background(), draftEvent("octo/widget", 42)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.deletes) == 0 {
		t.Errorf("Delete not called")
	}
	if _, err := store.Messages(context.Background(), "octo/widget", 42); err != routingdomain.ErrNotFound {
		t.Errorf("store row not deleted: err = %v", err)
	}
}

func TestDraftHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	store := newFakeMessageStore()
	messenger := &fakeMessenger{}

	logger, buf := captureLogger()
	h := application.NewDraftHandler(store, messenger, logger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindConvertedToDraft,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.deletes) != 0 {
		t.Errorf("Delete called when no message stored: %d calls", len(messenger.deletes))
	}

	rec := decodeLog(t, buf.Bytes())
	wantFields(t, rec, map[string]any{
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
	store.seed("acme/web", 7, domain.Message{Channel: "C0A", MessageID: "100.1"})
	store.seed("acme/web", 7, domain.Message{Channel: "C0B", MessageID: "200.1"})
	messenger := &fakeMessenger{}
	h := application.NewDraftHandler(store, messenger, discardLogger())

	if err := h.Handle(context.Background(), draftEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(messenger.deletes) != 2 {
		t.Fatalf("want 2 deletes; got %d", len(messenger.deletes))
	}
	if _, err := store.Messages(context.Background(), "acme/web", 7); err != routingdomain.ErrNotFound {
		t.Fatalf("PR row should be deleted; got %v", err)
	}
}
