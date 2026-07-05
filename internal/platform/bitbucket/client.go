// Package bitbucket is a minimal Bitbucket Cloud REST API client covering only
// the endpoints notifycat needs for validation and path routing. It is
// intentionally narrow — adding a new endpoint means adding one method, not
// pulling in a full SDK.
package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultBaseURL    = "https://api.bitbucket.org/2.0"
	defaultMaxRespMiB = 1
	// diffstatMaxRespMiB is a roomier per-page cap for the pull-request diffstat
	// endpoint, whose rows can run far larger than the other endpoints' JSON.
	// Oversized responses are truncated (then fail to decode), which path routing
	// treats as a soft failure and falls back to the repo tier.
	diffstatMaxRespMiB = 16
	// diffstatPageLen is the maximum page size Bitbucket accepts for diffstat.
	diffstatPageLen = 500
	// workspaceReposPageLen is the maximum page size for the workspace repos list.
	workspaceReposPageLen = 100
)

// ErrDiffstatUnavailable is returned by ListPullRequestFiles when Bitbucket
// redirects the diffstat request to a stale source ref (spec=None), meaning the
// changed-file set cannot be resolved. Callers treat it as a soft failure.
var ErrDiffstatUnavailable = errors.New("bitbucket: diffstat unavailable (stale source ref)")

// errStaleSourceRef is the private sentinel returned from CheckRedirect when a
// diffstat redirect targets a spec=None ref; ListPullRequestFiles maps it to the
// exported ErrDiffstatUnavailable.
var errStaleSourceRef = errors.New("bitbucket: stale source ref")

// Client talks to the Bitbucket Cloud REST API.
type Client struct {
	httpClient *http.Client
	token      string
	email      string
	baseURL    string
}

// Option configures Client construction.
type Option func(*Client)

// WithBaseURL overrides the Bitbucket API base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// NewClient builds a Client. The httpClient is used as-is; callers should
// configure timeouts on it. token is the Bitbucket access token; email, when
// non-empty, switches auth to HTTP Basic (base64 of email:token) instead of a
// bearer token.
func NewClient(hc *http.Client, token, email string, opts ...Option) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	c := &Client{httpClient: hc, token: token, email: email, baseURL: defaultBaseURL}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError represents a non-2xx Bitbucket response.
type APIError struct {
	Method  string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bitbucket: %s: %d %s", e.Method, e.Status, e.Message)
}

// applyAuth attaches Bitbucket credentials to a request. With an email it uses
// HTTP Basic (email:token); otherwise it sends the token as a bearer credential.
// A missing token leaves the request unauthenticated, mirroring the GitHub
// client's optional-token behavior.
func (c *Client) applyAuth(req *http.Request) {
	if c.token == "" {
		return
	}
	if c.email != "" {
		req.SetBasicAuth(c.email, c.token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}

// Repository is the subset of a repository record notifycat validates against.
type Repository struct {
	FullName  string `json:"full_name"`
	Slug      string `json:"slug"`
	IsPrivate bool   `json:"is_private"`
}

// GetRepository fetches a single repository. A non-2xx response is returned as a
// typed *APIError (e.g. Status 404 for a missing or inaccessible repo).
func (c *Client) GetRepository(ctx context.Context, workspace, repoSlug string) (Repository, error) {
	path := fmt.Sprintf("/repositories/%s/%s", url.PathEscape(workspace), url.PathEscape(repoSlug))
	body, err := c.get(ctx, path, "get-repository", defaultMaxRespMiB<<20)
	if err != nil {
		return Repository{}, err
	}
	var repo Repository
	if err := json.Unmarshal(body, &repo); err != nil {
		return Repository{}, fmt.Errorf("bitbucket: get-repository: decode: %w", err)
	}
	return repo, nil
}

// PullRequestState is the subset of a PR's status the stuck-PR digest needs. The
// State field is Bitbucket's raw value (OPEN, MERGED, DECLINED, SUPERSEDED) and
// is not normalized.
type PullRequestState struct {
	State string `json:"state"`
	Draft bool   `json:"draft"`
}

// GetPullRequest fetches a single PR's state and draft flag. A non-2xx response
// is returned as a typed *APIError.
func (c *Client) GetPullRequest(ctx context.Context, workspace, repoSlug string, id int) (PullRequestState, error) {
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d",
		url.PathEscape(workspace), url.PathEscape(repoSlug), id)
	body, err := c.get(ctx, path, "get-pull-request", defaultMaxRespMiB<<20)
	if err != nil {
		return PullRequestState{}, err
	}
	var pr PullRequestState
	if err := json.Unmarshal(body, &pr); err != nil {
		return PullRequestState{}, fmt.Errorf("bitbucket: get-pull-request: decode: %w", err)
	}
	return pr, nil
}

// ListWorkspaceRepos returns the slugs of every repository in the workspace,
// following Bitbucket's pagination envelope. An empty result is a normal
// outcome (empty workspace or all repos filtered by token scope).
func (c *Client) ListWorkspaceRepos(ctx context.Context, workspace string) ([]string, error) {
	next := fmt.Sprintf("/repositories/%s?pagelen=%d", url.PathEscape(workspace), workspaceReposPageLen)
	var slugs []string
	for next != "" {
		values, nextURL, err := c.getPaginated(ctx, next, "list-workspace-repos", defaultMaxRespMiB<<20)
		if err != nil {
			return nil, err
		}
		var page []struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(values, &page); err != nil {
			return nil, fmt.Errorf("bitbucket: list-workspace-repos: decode: %w", err)
		}
		for _, repo := range page {
			slugs = append(slugs, repo.Slug)
		}
		next = nextURL
	}
	return slugs, nil
}

// Hook is the subset of a repository webhook record we care about.
type Hook struct {
	UUID   string   `json:"uuid"`
	URL    string   `json:"url"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
}

// ListHookEvents returns the union of events configured across active hooks
// pointing at notifycat (matched by `urlSuffix` against hook.url). Returns an
// empty slice when no matching hook exists, so callers can distinguish "no
// notifycat hook" from "hook misconfigured".
func (c *Client) ListHookEvents(ctx context.Context, workspace, repoSlug, urlSuffix string) ([]string, error) {
	hooks, err := c.listHooks(ctx, workspace, repoSlug)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, hook := range hooks {
		if !hook.Active {
			continue
		}
		if urlSuffix != "" && !strings.HasSuffix(hook.URL, urlSuffix) {
			continue
		}
		for _, event := range hook.Events {
			if _, ok := seen[event]; ok {
				continue
			}
			seen[event] = struct{}{}
			out = append(out, event)
		}
	}
	return out, nil
}

func (c *Client) listHooks(ctx context.Context, workspace, repoSlug string) ([]Hook, error) {
	next := fmt.Sprintf("/repositories/%s/%s/hooks",
		url.PathEscape(workspace), url.PathEscape(repoSlug))
	var hooks []Hook
	for next != "" {
		values, nextURL, err := c.getPaginated(ctx, next, "list-hooks", defaultMaxRespMiB<<20)
		if err != nil {
			return nil, err
		}
		var page []Hook
		if err := json.Unmarshal(values, &page); err != nil {
			return nil, fmt.Errorf("bitbucket: list-hooks: decode: %w", err)
		}
		hooks = append(hooks, page...)
		next = nextURL
	}
	return hooks, nil
}

// ListPullRequestFiles returns the repo-relative paths of every file changed in
// a PR, following Bitbucket's diffstat pagination. Bitbucket 302-redirects the
// diffstat request to a spec-pinned URL; this call follows the redirect while
// replaying auth. A redirect to a stale source ref (spec=None) surfaces as
// ErrDiffstatUnavailable so path routing can soft-fail to the repo tier.
func (c *Client) ListPullRequestFiles(ctx context.Context, workspace, repoSlug string, id int) ([]string, error) {
	next := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/diffstat?pagelen=%d",
		url.PathEscape(workspace), url.PathEscape(repoSlug), id, diffstatPageLen)

	// Clone the injected client for this call only so the redirect policy that
	// replays auth and detects spec=None never leaks onto c.httpClient.
	redirectClient := *c.httpClient
	redirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("bitbucket: stopped after 10 redirects")
		}
		if isStaleSourceRef(req.URL) {
			return errStaleSourceRef
		}
		c.applyAuth(req)
		return nil
	}

	seen := make(map[string]struct{})
	var files []string
	for next != "" {
		values, nextURL, err := c.getPaginatedWith(ctx, &redirectClient, next, "list-pull-request-files", diffstatMaxRespMiB<<20)
		if err != nil {
			if errors.Is(err, errStaleSourceRef) {
				return nil, ErrDiffstatUnavailable
			}
			return nil, err
		}
		var page []struct {
			New *struct {
				Path string `json:"path"`
			} `json:"new"`
			Old *struct {
				Path string `json:"path"`
			} `json:"old"`
		}
		if err := json.Unmarshal(values, &page); err != nil {
			return nil, fmt.Errorf("bitbucket: list-pull-request-files: decode: %w", err)
		}
		for _, entry := range page {
			path := ""
			if entry.New != nil {
				path = entry.New.Path
			} else if entry.Old != nil {
				path = entry.Old.Path
			}
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			files = append(files, path)
		}
		next = nextURL
	}
	return files, nil
}

// isStaleSourceRef reports whether a diffstat redirect targets a stale source
// ref, which Bitbucket signals with a spec of "None" — either as the final path
// segment (.../diffstat/None) or as a spec=None query value. The "None" token is
// matched case-insensitively.
func isStaleSourceRef(u *url.URL) bool {
	base := u.Path
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	if strings.EqualFold(base, "None") {
		return true
	}
	return strings.EqualFold(u.Query().Get("spec"), "None")
}

// get issues a GET against a single-object endpoint and returns the raw body.
// path is a baseURL-relative path beginning with "/". method names the operation
// for error messages, and maxBytes caps the response read.
func (c *Client) get(ctx context.Context, path, method string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: build %s request: %w", method, err)
	}
	req.Header.Set("Accept", "application/json")
	c.applyAuth(req)

	// baseURL is operator-controlled and path is internally composed from
	// validated workspace/repo strings; gosec G107 does not apply.
	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL operator-controlled
	if err != nil {
		return nil, fmt.Errorf("bitbucket: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("bitbucket: %s: read body: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Method: method, Status: resp.StatusCode, Message: extractMessage(body)}
	}
	return body, nil
}

// getPaginated issues a GET against a paginated list endpoint using the client's
// default http.Client. See getPaginatedWith for semantics.
func (c *Client) getPaginated(ctx context.Context, target, method string, maxBytes int64) (json.RawMessage, string, error) {
	return c.getPaginatedWith(ctx, c.httpClient, target, method, maxBytes)
}

// getPaginatedWith issues a GET against a paginated list endpoint and returns the
// raw `values` array plus the `next` page URL from Bitbucket's pagination
// envelope. target is either an absolute next-page URL (from a prior response) or
// a baseURL-relative path beginning with "/". doer lets diffstat supply a
// redirect-following client; the other endpoints pass c.httpClient.
func (c *Client) getPaginatedWith(ctx context.Context, doer *http.Client, target, method string, maxBytes int64) (json.RawMessage, string, error) {
	reqURL := target
	if strings.HasPrefix(target, "/") {
		reqURL = c.baseURL + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("bitbucket: build %s request: %w", method, err)
	}
	req.Header.Set("Accept", "application/json")
	c.applyAuth(req)

	resp, err := doer.Do(req) //nolint:gosec // baseURL operator-controlled
	if err != nil {
		return nil, "", fmt.Errorf("bitbucket: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", fmt.Errorf("bitbucket: %s: read body: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &APIError{Method: method, Status: resp.StatusCode, Message: extractMessage(body)}
	}

	var envelope struct {
		Values json.RawMessage `json:"values"`
		Next   string          `json:"next"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", fmt.Errorf("bitbucket: %s: decode: %w (body=%q)", method, err, string(body))
	}
	return envelope.Values, envelope.Next, nil
}

// extractMessage pulls the message out of a Bitbucket error envelope
// ({"type":"error","error":{"message":"..."}}) if the body is JSON, falling back
// to the raw body trimmed for log readability.
func extractMessage(body []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
