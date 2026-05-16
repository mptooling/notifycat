// Package github is a minimal GitHub API client covering only the endpoints
// notifycat needs for validation. It is intentionally narrow — adding a new
// endpoint means adding one method, not pulling in go-github.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultBaseURL    = "https://api.github.com"
	defaultMaxRespMiB = 1
)

// Client talks to the GitHub REST API.
type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string
}

// Option configures Client construction.
type Option func(*Client)

// WithBaseURL overrides the GitHub API base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// NewClient builds a Client. The httpClient is used as-is; callers should
// configure timeouts on it.
func NewClient(hc *http.Client, token string, opts ...Option) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	c := &Client{httpClient: hc, token: token, baseURL: defaultBaseURL}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError represents a non-2xx GitHub response.
type APIError struct {
	Method  string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: %s: %d %s", e.Method, e.Status, e.Message)
}

// Hook is the subset of a repository webhook record we care about.
type Hook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// ListHookEvents returns the union of events configured across active hooks
// pointing at notifycat (matched by `urlSuffix` against hook.config.url).
// Returns an empty slice when no matching hook exists, so callers can
// distinguish "no notifycat hook" from "hook misconfigured".
func (c *Client) ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error) {
	hooks, err := c.listHooks(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, h := range hooks {
		if !h.Active {
			continue
		}
		if urlSuffix != "" && !strings.HasSuffix(h.Config.URL, urlSuffix) {
			continue
		}
		for _, ev := range h.Events {
			if _, ok := seen[ev]; ok {
				continue
			}
			seen[ev] = struct{}{}
			out = append(out, ev)
		}
	}
	return out, nil
}

func (c *Client) listHooks(ctx context.Context, owner, repo string) ([]Hook, error) {
	path := fmt.Sprintf("/repos/%s/%s/hooks", url.PathEscape(owner), url.PathEscape(repo))
	req, err := c.createRequest(ctx, path)

	// baseURL is operator-controlled and path is internally composed from
	// validated owner/repo strings; gosec G107 does not apply.
	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL operator-controlled
	if err != nil {
		return nil, fmt.Errorf("github: list-hooks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBytes int64 = defaultMaxRespMiB << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("github: list-hooks: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Method: "list-hooks", Status: resp.StatusCode, Message: extractMessage(body)}
	}

	var hooks []Hook
	if err := json.Unmarshal(body, &hooks); err != nil {
		return nil, fmt.Errorf("github: list-hooks: decode: %w (body=%q)", err, string(body))
	}
	return hooks, nil
}

func (c *Client) createRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build list-hooks request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return req, nil
}

// extractMessage pulls the "message" field from a GitHub error envelope if it
// is JSON, falling back to the raw body trimmed for log readability.
func extractMessage(body []byte) string {
	var env struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Message != "" {
		return env.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
