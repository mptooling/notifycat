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
	// filesMaxRespMiB is a roomier per-page cap for the pull-request files
	// endpoint, whose rows embed the file patch and so run far larger than the
	// other endpoints' JSON. Oversized responses are truncated (then fail to
	// decode), which path routing treats as a soft failure and falls back to the
	// repo tier.
	filesMaxRespMiB = 16
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
	if err != nil {
		return nil, fmt.Errorf("unable to create a request to get a list of hooks: %w", err)
	}

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

// PullRequestState is the subset of a PR's status the stuck-PR digest needs to
// decide whether the PR is still worth nagging about.
type PullRequestState struct {
	State string `json:"state"` // "open" | "closed"
	Draft bool   `json:"draft"`
}

// GetPullRequest fetches a single PR's open/closed and draft state. A non-2xx
// response is returned as a typed *APIError (e.g. Status 404 for a missing or
// inaccessible PR) so callers can decide how to treat it.
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequestState, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	req, err := c.createRequest(ctx, path)
	if err != nil {
		return PullRequestState{}, fmt.Errorf("github: get-pull-request: %w", err)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL operator-controlled
	if err != nil {
		return PullRequestState{}, fmt.Errorf("github: get-pull-request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBytes int64 = defaultMaxRespMiB << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return PullRequestState{}, fmt.Errorf("github: get-pull-request: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PullRequestState{}, &APIError{Method: "get-pull-request", Status: resp.StatusCode, Message: extractMessage(body)}
	}

	var pr PullRequestState
	if err := json.Unmarshal(body, &pr); err != nil {
		return PullRequestState{}, fmt.Errorf("github: get-pull-request: decode: %w", err)
	}
	return pr, nil
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

// ListOrgRepos returns the names of every repository in the org, following
// GitHub's Link header for pagination. Empty result is a normal outcome
// (org with no repos or all repos filtered by token scope).
func (c *Client) ListOrgRepos(ctx context.Context, org string) ([]string, error) {
	next := fmt.Sprintf("/orgs/%s/repos?per_page=100", url.PathEscape(org))
	var names []string
	for next != "" {
		page, nextURL, err := c.listOrgReposPage(ctx, next)
		if err != nil {
			return nil, err
		}
		names = append(names, page...)
		next = nextURL
	}
	return names, nil
}

func (c *Client) listOrgReposPage(ctx context.Context, target string) ([]string, string, error) {
	body, next, err := c.getPaginated(ctx, target, "list-org-repos", defaultMaxRespMiB<<20)
	if err != nil {
		return nil, "", err
	}
	var page []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("github: list-org-repos: decode: %w", err)
	}
	out := make([]string, 0, len(page))
	for _, r := range page {
		out = append(out, r.Name)
	}
	return out, next, nil
}

// ListPullRequestFiles returns the repo-relative paths of every file changed in
// a PR, following GitHub's Link header for pagination. Path routing uses it to
// decide which directory rules a PR touches. An empty result is possible (a PR
// with no file changes).
func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	next := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), number)
	var files []string
	for next != "" {
		page, nextURL, err := c.listPullRequestFilesPage(ctx, next)
		if err != nil {
			return nil, err
		}
		files = append(files, page...)
		next = nextURL
	}
	return files, nil
}

func (c *Client) listPullRequestFilesPage(ctx context.Context, target string) ([]string, string, error) {
	body, next, err := c.getPaginated(ctx, target, "list-pull-request-files", filesMaxRespMiB<<20)
	if err != nil {
		return nil, "", err
	}
	var page []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("github: list-pull-request-files: decode: %w", err)
	}
	out := make([]string, 0, len(page))
	for _, file := range page {
		out = append(out, file.Filename)
	}
	return out, next, nil
}

// getPaginated issues a GET against a paginated list endpoint and returns the
// raw body plus the rel="next" link from the Link header. target is either an
// absolute next-page URL (from a prior Link header) or a baseURL-relative path
// beginning with "/". method names the operation for error messages, and
// maxBytes caps the response read (the files endpoint embeds patches and needs
// a roomier cap than the others).
func (c *Client) getPaginated(ctx context.Context, target, method string, maxBytes int64) ([]byte, string, error) {
	reqURL := target
	if strings.HasPrefix(target, "/") {
		reqURL = c.baseURL + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("github: build %s request: %w", method, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL operator-controlled
	if err != nil {
		return nil, "", fmt.Errorf("github: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", fmt.Errorf("github: %s: read body: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &APIError{Method: method, Status: resp.StatusCode, Message: extractMessage(body)}
	}
	return body, parseNextLink(resp.Header.Get("Link")), nil
}

// parseNextLink extracts the rel="next" URL from a GitHub Link header.
// Returns "" when no next page is advertised.
func parseNextLink(header string) string {
	for _, segment := range strings.Split(header, ",") {
		segment = strings.TrimSpace(segment)
		if !strings.Contains(segment, `rel="next"`) {
			continue
		}
		open := strings.IndexByte(segment, '<')
		closeIdx := strings.IndexByte(segment, '>')
		if open >= 0 && closeIdx > open {
			return segment[open+1 : closeIdx]
		}
	}
	return ""
}
