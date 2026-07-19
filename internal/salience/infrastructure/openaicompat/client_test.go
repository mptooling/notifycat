package openaicompat_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat/internal/salience/domain"
	"github.com/mptooling/notifycat/internal/salience/infrastructure/openaicompat"
)

func modelRequest() domain.ModelRequest {
	return domain.ModelRequest{
		System:          "system prompt",
		User:            "user payload",
		Schema:          json.RawMessage(`{"type":"object"}`),
		MaxOutputTokens: 1024,
	}
}

const chatResponse = `{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":21,"completion_tokens":4}}`

func TestGenerateRequestShape(t *testing.T) {
	var captured struct {
		path          string
		authorization string
		body          map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.authorization = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)
		w.Header().Set("x-ratelimit-remaining-requests", "99")
		w.Header().Set("x-ratelimit-limit-requests", "100")
		_, _ = w.Write([]byte(chatResponse))
	}))
	defer server.Close()

	client := openaicompat.NewClient(server.Client(), domain.GatewayConfig{APIKey: "sk-test", Model: "gpt-4o-mini", BaseURL: server.URL + "/v1"})
	response, err := client.Generate(context.Background(), modelRequest())
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if captured.path != "/v1/chat/completions" {
		t.Errorf("path = %q", captured.path)
	}
	if captured.authorization != "Bearer sk-test" {
		t.Errorf("Authorization = %q", captured.authorization)
	}
	if captured.body["model"] != "gpt-4o-mini" || captured.body["temperature"] != float64(0) {
		t.Errorf("body model/temperature = %v/%v", captured.body["model"], captured.body["temperature"])
	}
	responseFormat := captured.body["response_format"].(map[string]any)
	if responseFormat["type"] != "json_schema" {
		t.Errorf("response_format.type = %v", responseFormat["type"])
	}
	jsonSchema := responseFormat["json_schema"].(map[string]any)
	if jsonSchema["strict"] != true || jsonSchema["schema"] == nil || jsonSchema["name"] == "" {
		t.Errorf("json_schema = %v", jsonSchema)
	}
	if response.Text != `{"ok":true}` || response.TokensIn != 21 || response.TokensOut != 4 {
		t.Errorf("response = %+v", response)
	}
	if response.RateLimit == nil || response.RateLimit.RequestsRemaining != 99 || response.RateLimit.RequestsLimit != 100 {
		t.Errorf("RateLimit = %+v", response.RateLimit)
	}
}

func TestGenerateKeylessSendsNoAuthHeader(t *testing.T) {
	var sawAuthorization bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuthorization = r.Header["Authorization"]
		_, _ = w.Write([]byte(chatResponse))
	}))
	defer server.Close()

	client := openaicompat.NewClient(server.Client(), domain.GatewayConfig{Model: "llama3", BaseURL: server.URL})
	if _, err := client.Generate(context.Background(), modelRequest()); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if sawAuthorization {
		t.Error("keyless mode must not send an Authorization header")
	}
}

func TestGenerateNoRateLimitHeadersMeansNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse))
	}))
	defer server.Close()

	client := openaicompat.NewClient(server.Client(), domain.GatewayConfig{Model: "llama3", BaseURL: server.URL})
	response, err := client.Generate(context.Background(), modelRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.RateLimit != nil {
		t.Errorf("RateLimit = %+v; want nil when the endpoint exposes no headers", response.RateLimit)
	}
}

func TestGenerateRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached for requests"}}`))
	}))
	defer server.Close()

	client := openaicompat.NewClient(server.Client(), domain.GatewayConfig{APIKey: "sk", Model: "m", BaseURL: server.URL})
	_, err := client.Generate(context.Background(), modelRequest())

	var rateLimited *domain.RateLimitedError
	if !errors.As(err, &rateLimited) {
		t.Fatalf("error = %v; want *RateLimitedError", err)
	}
	if rateLimited.RetryAfter != "12" {
		t.Errorf("RetryAfter = %q", rateLimited.RetryAfter)
	}
}
