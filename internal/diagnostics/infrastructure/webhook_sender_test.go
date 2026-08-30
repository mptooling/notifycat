package infrastructure_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

func TestHTTPWebhookSender_PassesThroughStatusCode(t *testing.T) {
	for _, wantStatus := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(wantStatus), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(wantStatus)
			}))
			defer server.Close()
			sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)

			status, err := sender.Send(context.Background(), server.URL, []byte(`{}`), map[string]string{
				"Content-Type": "application/json",
			})

			require.NoError(t, err)
			assert.Equal(t, wantStatus, status)
		})
	}
}

func TestHTTPWebhookSender_SetsHeaders(t *testing.T) {
	var gotEvent, gotSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEvent = r.Header.Get("X-GitHub-Event")
		gotSignature = r.Header.Get("X-Hub-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)

	_, err := sender.Send(context.Background(), server.URL, []byte(`{}`), map[string]string{
		"X-GitHub-Event":      "pull_request",
		"X-Hub-Signature-256": "sha256=abc",
	})

	require.NoError(t, err)
	assert.Equal(t, "pull_request", gotEvent)
	assert.Equal(t, "sha256=abc", gotSignature)
}

func TestHTTPWebhookSender_SendsBody(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	body := []byte(`{"action":"opened"}`)
	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)

	_, err := sender.Send(context.Background(), server.URL, body, nil)

	require.NoError(t, err)
	assert.Equal(t, body, gotBody)
}

func TestHTTPWebhookSender_TransportError_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close() // nothing is listening now
	sender := infrastructure.NewHTTPWebhookSender(http.DefaultClient)

	status, err := sender.Send(context.Background(), url, []byte(`{}`), nil)

	require.Error(t, err)
	assert.Zero(t, status)
}
