package infrastructure_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

func TestHTTPWebhookSender_PassesThroughStatusCode(t *testing.T) {
	for _, wantStatus := range []int{200, 401, 500} {
		wantStatus := wantStatus
		t.Run(http.StatusText(wantStatus), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(wantStatus)
			}))
			defer srv.Close()

			sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)
			status, err := sender.Send(context.Background(), srv.URL, []byte(`{}`), map[string]string{
				"Content-Type": "application/json",
			})
			if err != nil {
				t.Fatalf("Send returned error %v; want nil", err)
			}
			if status != wantStatus {
				t.Errorf("status = %d; want %d", status, wantStatus)
			}
		})
	}
}

func TestHTTPWebhookSender_SetsHeaders(t *testing.T) {
	var capturedEvent, capturedSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedEvent = r.Header.Get("X-GitHub-Event")
		capturedSig = r.Header.Get("X-Hub-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)
	_, _ = sender.Send(context.Background(), srv.URL, []byte(`{}`), map[string]string{
		"X-GitHub-Event":      "pull_request",
		"X-Hub-Signature-256": "sha256=abc",
	})

	if capturedEvent != "pull_request" {
		t.Errorf("X-GitHub-Event = %q; want pull_request", capturedEvent)
	}
	if capturedSig != "sha256=abc" {
		t.Errorf("X-Hub-Signature-256 = %q; want sha256=abc", capturedSig)
	}
}

func TestHTTPWebhookSender_SendsBody(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := []byte(`{"action":"opened"}`)
	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)
	_, _ = sender.Send(context.Background(), srv.URL, body, nil)

	if string(capturedBody) != string(body) {
		t.Errorf("body = %q; want %q", capturedBody, body)
	}
}

func TestHTTPWebhookSender_TransportError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)
	status, err := sender.Send(context.Background(), url, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("Send returned nil error for unreachable server; want non-nil")
	}
	if status != 0 {
		t.Errorf("status = %d; want 0 on transport error", status)
	}
}
