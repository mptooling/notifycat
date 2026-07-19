// Package gemini is the hand-rolled Gemini generateContent adapter for the
// salience ModelGateway port — no SDK, matching the platform client style,
// keeping the govulncheck surface flat.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// DefaultBaseURL is the public Gemini API host; ai.base_url overrides it for
// proxies and tests.
const DefaultBaseURL = "https://generativelanguage.googleapis.com"

// maxResponseBytes bounds a decision response read — decisions are tiny.
const maxResponseBytes = 1 << 20

// Client implements domain.ModelGateway over the Gemini REST API. Safe for
// concurrent use.
type Client struct {
	httpClient *http.Client
	apiKey     string
	model      string
	baseURL    string
}

// NewClient builds a Client. An empty BaseURL uses DefaultBaseURL.
func NewClient(httpClient *http.Client, config domain.GatewayConfig) *Client {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{httpClient: httpClient, apiKey: config.APIKey, model: config.Model, baseURL: baseURL}
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	ResponseMIMEType   string          `json:"responseMimeType"`
	ResponseJSONSchema json.RawMessage `json:"responseJsonSchema,omitempty"`
	MaxOutputTokens    int             `json:"maxOutputTokens"`
	Temperature        float64         `json:"temperature"`
}

type generateRequest struct {
	SystemInstruction content          `json:"systemInstruction"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// Generate implements domain.ModelGateway.
func (c *Client) Generate(ctx context.Context, request domain.ModelRequest) (domain.ModelResponse, error) {
	payload, err := json.Marshal(generateRequest{
		SystemInstruction: content{Parts: []part{{Text: request.System}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: request.User}}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType:   "application/json",
			ResponseJSONSchema: request.Schema,
			MaxOutputTokens:    request.MaxOutputTokens,
			Temperature:        0,
		},
	})
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("gemini: marshal request: %w", err)
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, c.model)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("x-goog-api-key", c.apiKey)

	httpResponse, err := c.httpClient.Do(httpRequest) //nolint:gosec // baseURL is config-controlled, model is constant
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("gemini: %w", err)
	}
	defer func() { _ = httpResponse.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
	if err != nil {
		return domain.ModelResponse{}, fmt.Errorf("gemini: read response: %w", err)
	}
	if httpResponse.StatusCode == http.StatusTooManyRequests {
		return domain.ModelResponse{}, &domain.RateLimitedError{
			Detail:     errorDetail(body),
			RetryAfter: httpResponse.Header.Get("Retry-After"),
		}
	}
	if httpResponse.StatusCode != http.StatusOK {
		return domain.ModelResponse{}, fmt.Errorf("gemini: status %d: %s", httpResponse.StatusCode, errorDetail(body))
	}
	var decoded generateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return domain.ModelResponse{}, fmt.Errorf("gemini: decode response: %w", err)
	}
	if len(decoded.Candidates) == 0 || len(decoded.Candidates[0].Content.Parts) == 0 {
		return domain.ModelResponse{}, fmt.Errorf("gemini: response has no candidates")
	}
	return domain.ModelResponse{
		Text:      decoded.Candidates[0].Content.Parts[0].Text,
		TokensIn:  decoded.UsageMetadata.PromptTokenCount,
		TokensOut: decoded.UsageMetadata.CandidatesTokenCount,
	}, nil
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
