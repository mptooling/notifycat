// Package openaicompat is the hand-rolled chat-completions adapter for any
// OpenAI-compatible endpoint (OpenAI, OpenRouter, LiteLLM, Ollama, vLLM…).
// Pointing ai.base_url at a gateway covers the provider long tail with zero
// new machinery — this package is notifycat's out-of-process plugin system.
package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// maxResponseBytes bounds a decision response read — decisions are tiny.
const maxResponseBytes = 1 << 20

// Client implements domain.ModelGateway over the chat-completions API. Safe
// for concurrent use. An empty APIKey sends no Authorization header (keyless
// local endpoints).
type Client struct {
	httpClient *http.Client
	apiKey     string
	model      string
	baseURL    string
}

// NewClient builds a Client. BaseURL is required (config validation enforces
// it) and used verbatim after trailing-slash trimming.
func NewClient(httpClient *http.Client, config domain.GatewayConfig) *Client {
	return &Client{
		httpClient: httpClient,
		apiKey:     config.APIKey,
		model:      config.Model,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaFormat struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type responseFormat struct {
	Type       string           `json:"type"`
	JSONSchema jsonSchemaFormat `json:"json_schema"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat responseFormat `json:"response_format"`
	MaxTokens      int            `json:"max_tokens"`
	Temperature    float64        `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Generate implements domain.ModelGateway.
func (c *Client) Generate(ctx context.Context, request domain.ModelRequest) (domain.ModelResponse, error) {
	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: request.System},
			{Role: "user", Content: request.User},
		},
		ResponseFormat: responseFormat{
			Type:       "json_schema",
			JSONSchema: jsonSchemaFormat{Name: "decision", Schema: request.Schema, Strict: true},
		},
		MaxTokens:   request.MaxOutputTokens,
		Temperature: 0,
	})
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: marshal request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResponse, err := c.httpClient.Do(httpRequest) //nolint:gosec // baseURL is config-controlled, model is constant
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: %w", err)
	}
	defer func() { _ = httpResponse.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: read response: %w", err)
	}
	if httpResponse.StatusCode == http.StatusTooManyRequests {
		return domain.ModelResponse{}, &domain.RateLimitedError{
			Detail:     errorDetail(body),
			RetryAfter: httpResponse.Header.Get("Retry-After"),
		}
	}
	if httpResponse.StatusCode != http.StatusOK {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: status %d: %s", httpResponse.StatusCode, errorDetail(body))
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return domain.ModelResponse{}, fmt.Errorf("openaicompat: response has no choices")
	}
	return domain.ModelResponse{
		Text:      decoded.Choices[0].Message.Content,
		TokensIn:  decoded.Usage.PromptTokens,
		TokensOut: decoded.Usage.CompletionTokens,
		RateLimit: rateLimitInfo(httpResponse.Header),
	}, nil
}

// rateLimitInfo parses best-effort x-ratelimit-* headers (OpenAI, OpenRouter,
// LiteLLM setups that forward them). Nil when the endpoint exposes none;
// unknown numeric fields are -1.
func rateLimitInfo(header http.Header) *domain.RateLimitInfo {
	requestsRemaining := header.Get("x-ratelimit-remaining-requests")
	tokensRemaining := header.Get("x-ratelimit-remaining-tokens")
	if requestsRemaining == "" && tokensRemaining == "" {
		return nil
	}
	return &domain.RateLimitInfo{
		RequestsRemaining: intOrMinusOne(requestsRemaining),
		RequestsLimit:     intOrMinusOne(header.Get("x-ratelimit-limit-requests")),
		TokensRemaining:   intOrMinusOne(tokensRemaining),
		TokensLimit:       intOrMinusOne(header.Get("x-ratelimit-limit-tokens")),
		Reset:             header.Get("x-ratelimit-reset-requests"),
	}
}

func intOrMinusOne(s string) int {
	value, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return value
}

// errorDetail extracts the provider's error message, falling back to a
// truncated raw body.
func errorDetail(body []byte) string {
	var wire struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error.Message != "" {
		return wire.Error.Message
	}
	detail := string(body)
	if len(detail) > 200 {
		detail = detail[:200]
	}
	return detail
}

var _ domain.ModelGateway = (*Client)(nil)
