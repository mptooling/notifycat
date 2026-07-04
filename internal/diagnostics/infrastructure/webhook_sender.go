package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// HTTPWebhookSender implements diagnosticsdomain.WebhookSender over *http.Client.
type HTTPWebhookSender struct {
	client *http.Client
}

// NewHTTPWebhookSender returns a sender that uses the given HTTP client.
func NewHTTPWebhookSender(client *http.Client) *HTTPWebhookSender {
	return &HTTPWebhookSender{client: client}
}

// Send POSTs body to url with the provided headers and returns the HTTP status
// code. A transport error returns status 0 and a non-nil err.
func (s *HTTPWebhookSender) Send(ctx context.Context, url string, body []byte, headers map[string]string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("smoke: build request: %w", err)
	}
	for key, val := range headers {
		req.Header.Set(key, val)
	}

	resp, err := s.client.Do(req) //nolint:gosec // url is operator-controlled (webhook endpoint from config)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
