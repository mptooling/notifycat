package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat/internal/salience/domain"
	"github.com/mptooling/notifycat/internal/salience/infrastructure/gemini"
)

func modelRequest() domain.ModelRequest {
	return domain.ModelRequest{
		System:          "system prompt",
		User:            "user payload",
		Schema:          json.RawMessage(`{"type":"object"}`),
		MaxOutputTokens: 1024,
	}
}

func TestGenerateRequestShape(t *testing.T) {
	var captured struct {
		path   string
		apiKey string
		body   map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.apiKey = r.Header.Get("x-goog-api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":3}}`))
	}))
	defer server.Close()

	client := gemini.NewClient(server.Client(), domain.GatewayConfig{APIKey: "test-key", Model: "gemini-2.5-flash", BaseURL: server.URL})
	response, err := client.Generate(context.Background(), modelRequest())
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if captured.path != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Errorf("path = %q", captured.path)
	}
	if captured.apiKey != "test-key" {
		t.Errorf("x-goog-api-key = %q", captured.apiKey)
	}
	generationConfig := captured.body["generationConfig"].(map[string]any)
	if generationConfig["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v", generationConfig["responseMimeType"])
	}
	if generationConfig["responseJsonSchema"] == nil {
		t.Error("responseJsonSchema missing")
	}
	if generationConfig["temperature"] != float64(0) {
		t.Errorf("temperature = %v; want 0", generationConfig["temperature"])
	}
	if response.Text != `{"ok":true}` || response.TokensIn != 11 || response.TokensOut != 3 {
		t.Errorf("response = %+v", response)
	}
}

func TestGenerateRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Quota exceeded for quota metric 'GenerateContent requests'"}}`))
	}))
	defer server.Close()

	client := gemini.NewClient(server.Client(), domain.GatewayConfig{APIKey: "k", Model: "m", BaseURL: server.URL})
	_, err := client.Generate(context.Background(), modelRequest())

	var rateLimited *domain.RateLimitedError
	if !errors.As(err, &rateLimited) {
		t.Fatalf("error = %v; want *RateLimitedError", err)
	}
	if rateLimited.RetryAfter != "30" || rateLimited.Detail == "" {
		t.Errorf("rate limit detail lost: %+v", rateLimited)
	}
}

func TestGenerateServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"backend unavailable"}}`))
	}))
	defer server.Close()

	client := gemini.NewClient(server.Client(), domain.GatewayConfig{APIKey: "k", Model: "m", BaseURL: server.URL})
	if _, err := client.Generate(context.Background(), modelRequest()); err == nil {
		t.Fatal("want an error for a 500")
	}
}

func TestGenerateEmptyCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer server.Close()

	client := gemini.NewClient(server.Client(), domain.GatewayConfig{APIKey: "k", Model: "m", BaseURL: server.URL})
	if _, err := client.Generate(context.Background(), modelRequest()); err == nil {
		t.Fatal("want an error for a response without candidates")
	}
}
