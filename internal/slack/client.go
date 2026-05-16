package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Defaults.
const (
	defaultBaseURL    = "https://slack.com"
	defaultMaxRespMiB = 1 // we never expect a large Slack response
)

// Client is a thin Slack Web API client covering only the endpoints needed by
// the notifier. It is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string
}

// Option configures Client construction.
type Option func(*Client)

// WithBaseURL overrides the Slack API base URL — used in tests against an
// httptest.Server. The URL must NOT have a trailing slash.
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

// APIError represents a non-ok Slack API response.
type APIError struct {
	Method string
	Code   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("slack: %s: %s", e.Method, e.Code)
}

// PostMessage posts a new message to channel and returns its ts.
func (c *Client) PostMessage(ctx context.Context, channel, text string) (string, error) {
	var resp struct {
		TS string `json:"ts"`
	}
	if err := c.postJSON(ctx, "chat.postMessage", map[string]any{
		"channel": channel,
		"text":    text,
	}, &resp, nil); err != nil {
		return "", err
	}
	return resp.TS, nil
}

// UpdateMessage edits an existing message by ts.
func (c *Client) UpdateMessage(ctx context.Context, channel, ts, text string) error {
	return c.postJSON(ctx, "chat.update", map[string]any{
		"channel": channel,
		"ts":      ts,
		"text":    text,
	}, nil, nil)
}

// DeleteMessage removes an existing message by ts.
func (c *Client) DeleteMessage(ctx context.Context, channel, ts string) error {
	return c.postJSON(ctx, "chat.delete", map[string]any{
		"channel": channel,
		"ts":      ts,
	}, nil, nil)
}

// AddReaction adds a reaction emoji to a message. "already_reacted" is
// treated as success — it means the reaction we wanted is already there.
func (c *Client) AddReaction(ctx context.Context, channel, ts, name string) error {
	return c.postJSON(ctx, "reactions.add", map[string]any{
		"channel":   channel,
		"timestamp": ts,
		"name":      name,
	}, nil, []string{"already_reacted"})
}

// Reaction is one entry from reactions.get.
type Reaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// GetReactions returns the reactions attached to a message, or an empty slice
// if none are present.
func (c *Client) GetReactions(ctx context.Context, channel, ts string) ([]Reaction, error) {
	var resp struct {
		Message struct {
			Reactions []Reaction `json:"reactions"`
		} `json:"message"`
	}
	q := url.Values{
		"channel":   {channel},
		"timestamp": {ts},
	}
	if err := c.getJSON(ctx, "reactions.get", q, &resp, nil); err != nil {
		return nil, err
	}
	return resp.Message.Reactions, nil
}

// AuthTest returns the bot's user_id and the OAuth scopes Slack reports as
// granted to the token. Scopes are read from the X-OAuth-Scopes response
// header (comma-separated) and used by validation to verify required scopes
// are present.
func (c *Client) AuthTest(ctx context.Context) (userID string, scopes []string, err error) {
	var resp struct {
		UserID string `json:"user_id"`
	}
	var hdr http.Header
	if err := c.getJSON(ctx, "auth.test", nil, &resp, &hdr); err != nil {
		return "", nil, err
	}
	return resp.UserID, parseScopes(hdr.Get("X-OAuth-Scopes")), nil
}

// ChannelInfo is the subset of conversations.info we need for validation.
type ChannelInfo struct {
	ID         string
	Name       string
	IsMember   bool
	IsArchived bool
}

// ConversationsInfo returns metadata about a channel, including whether the
// bot is a member. The channel argument must be a Slack channel ID.
func (c *Client) ConversationsInfo(ctx context.Context, channel string) (ChannelInfo, error) {
	var resp struct {
		Channel struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			IsMember   bool   `json:"is_member"`
			IsArchived bool   `json:"is_archived"`
		} `json:"channel"`
	}
	q := url.Values{"channel": {channel}}
	if err := c.getJSON(ctx, "conversations.info", q, &resp, nil); err != nil {
		return ChannelInfo{}, err
	}
	return ChannelInfo{
		ID:         resp.Channel.ID,
		Name:       resp.Channel.Name,
		IsMember:   resp.Channel.IsMember,
		IsArchived: resp.Channel.IsArchived,
	}, nil
}

// parseScopes splits a comma-separated OAuth scopes header value into trimmed
// non-empty entries.
func parseScopes(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ----- internals -----

func (c *Client) postJSON(
	ctx context.Context,
	method string,
	payload any,
	out any,
	allowErrCodes []string,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal %s payload: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build %s request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return c.do(req, method, out, allowErrCodes, nil)
}

// getJSON issues a GET against the Slack API. When outHeader is non-nil the
// response Header is copied into it before the call returns — used by
// AuthTest to read X-OAuth-Scopes.
func (c *Client) getJSON(
	ctx context.Context,
	method string,
	query url.Values,
	out any,
	outHeader *http.Header,
) error {
	u := c.baseURL + "/api/" + method
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("slack: build %s request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return c.do(req, method, out, nil, outHeader)
}

func (c *Client) do(req *http.Request, method string, out any, allowErrCodes []string, outHeader *http.Header) error {
	// The URL is composed from c.baseURL (operator-configured) and a hard-coded
	// method name; there is no user-controlled taint, so gosec G107/G704 do
	// not apply here.
	resp, err := c.httpClient.Do(req) //nolint:gosec // baseURL is operator-controlled, method is internal constant
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if outHeader != nil {
		*outHeader = resp.Header.Clone()
	}

	const maxBytes int64 = defaultMaxRespMiB << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return fmt.Errorf("slack: %s: read body: %w", method, err)
	}

	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("slack: %s: decode envelope: %w (body=%q, status=%d)", method, err, string(body), resp.StatusCode)
	}
	if !envelope.OK {
		for _, allowed := range allowErrCodes {
			if envelope.Error == allowed {
				return nil
			}
		}
		return &APIError{Method: method, Code: envelope.Error}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("slack: %s: decode payload: %w", method, err)
	}
	return nil
}
