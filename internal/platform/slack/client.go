package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults.
const (
	defaultBaseURL    = "https://slack.com"
	defaultMaxRespMiB = 1 // we never expect a large Slack response
)

// Rate-limit retry bounds. Slack answers a throttled call with
// error "ratelimited" and a Retry-After header; a bulk run (relocating a
// backlog of messages) hits Tier 3 legitimately, so the call waits and retries
// instead of failing the caller. The wait is capped so a hostile or mistaken
// header cannot stall a run indefinitely.
const (
	maxRateLimitAttempts = 3
	defaultRetryAfter    = time.Second
	maxRetryAfter        = 60 * time.Second
)

// ErrMessageNotFound reports that the channel/ts pair addresses no message —
// it was deleted, or the bot cannot see it.
var ErrMessageNotFound = errors.New("slack: message not found")

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

// APIError represents a non-ok Slack API response. header carries the response
// headers so a rate-limited call can read Retry-After.
type APIError struct {
	Method string
	Code   string
	header http.Header
}

func (e *APIError) Error() string {
	return fmt.Sprintf("slack: %s: %s", e.Method, e.Code)
}

// PostMessage posts a new message to channel and returns its ts. The Block Kit
// blocks render in-channel; msg.Fallback is sent as the top-level text Slack
// uses for the push preview and screen readers.
func (c *Client) PostMessage(ctx context.Context, channel string, msg Message) (string, error) {
	var resp struct {
		TS string `json:"ts"`
	}
	if err := c.postJSON(ctx, "chat.postMessage", map[string]any{
		"channel": channel,
		"text":    msg.Fallback,
		"blocks":  msg.Blocks,
	}, &resp, nil); err != nil {
		return "", err
	}
	return resp.TS, nil
}

// PostReply posts a message as a reply in the thread rooted at threadTS and
// returns its ts. It is PostMessage plus a thread_ts, kept separate so the
// webhook path stays a plain top-level post.
func (c *Client) PostReply(ctx context.Context, channel, threadTS string, msg Message) (string, error) {
	var resp struct {
		TS string `json:"ts"`
	}
	if err := c.postJSON(ctx, "chat.postMessage", map[string]any{
		"channel":   channel,
		"text":      msg.Fallback,
		"blocks":    msg.Blocks,
		"thread_ts": threadTS,
	}, &resp, nil); err != nil {
		return "", err
	}
	return resp.TS, nil
}

// UpdateMessage edits an existing message by ts, replacing both its blocks and
// the top-level text fallback.
func (c *Client) UpdateMessage(ctx context.Context, channel, ts string, msg Message) error {
	return c.postJSON(ctx, "chat.update", map[string]any{
		"channel": channel,
		"ts":      ts,
		"text":    msg.Fallback,
		"blocks":  msg.Blocks,
	}, nil, nil)
}

// UpdateMessageRawBlocks edits a message in place, sending blocks verbatim.
// Callers pass the message's existing blocks (as echoed back by Slack in the
// interaction payload) plus any additions, so the original rendering is
// preserved without re-composing it. fallback is the top-level text.
func (c *Client) UpdateMessageRawBlocks(ctx context.Context, channel, ts string, blocks []json.RawMessage, fallback string) error {
	return c.postJSON(ctx, "chat.update", map[string]any{
		"channel": channel,
		"ts":      ts,
		"text":    fallback,
		"blocks":  blocks,
	}, nil, nil)
}

// RawMessageContent is a message as Slack stored it: its Block Kit blocks
// verbatim, plus the top-level text Slack uses for the push preview.
type RawMessageContent struct {
	Blocks   []json.RawMessage
	Fallback string
}

// MessageContent reads a single message by its ts, returning its blocks
// unparsed so a caller can repost them elsewhere without re-composing. A
// channel/ts pair that addresses nothing yields ErrMessageNotFound.
func (c *Client) MessageContent(ctx context.Context, channel, ts string) (RawMessageContent, error) {
	var resp struct {
		Messages []struct {
			Text   string            `json:"text"`
			Blocks []json.RawMessage `json:"blocks"`
		} `json:"messages"`
	}
	query := url.Values{
		"channel":   {channel},
		"latest":    {ts},
		"oldest":    {ts},
		"inclusive": {"true"},
		"limit":     {"1"},
	}
	if err := c.getJSON(ctx, "conversations.history", query, &resp, nil); err != nil {
		return RawMessageContent{}, err
	}
	if len(resp.Messages) == 0 {
		return RawMessageContent{}, fmt.Errorf("%w: %s/%s", ErrMessageNotFound, channel, ts)
	}
	return RawMessageContent{Blocks: resp.Messages[0].Blocks, Fallback: resp.Messages[0].Text}, nil
}

// PostMessageRawBlocks posts a new message from pre-rendered blocks and returns
// its ts. It is PostMessage for blocks that were not built by the Composer —
// the write counterpart of MessageContent.
func (c *Client) PostMessageRawBlocks(ctx context.Context, channel string, blocks []json.RawMessage, fallback string) (string, error) {
	var resp struct {
		TS string `json:"ts"`
	}
	if err := c.postJSON(ctx, "chat.postMessage", map[string]any{
		"channel": channel,
		"text":    fallback,
		"blocks":  blocks,
	}, &resp, nil); err != nil {
		return "", err
	}
	return resp.TS, nil
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

// do issues the request, retrying a rate-limited call up to
// maxRateLimitAttempts times, honouring Slack's Retry-After.
func (c *Client) do(req *http.Request, method string, out any, allowErrCodes []string, outHeader *http.Header) error {
	for attempt := 1; ; attempt++ {
		throttled, retryAfter, err := c.attempt(req, method, out, allowErrCodes, outHeader)
		if !throttled || attempt == maxRateLimitAttempts {
			return err
		}
		if err := sleepCtx(req.Context(), retryAfter); err != nil {
			return err
		}
		if err := rewind(req); err != nil {
			return err
		}
	}
}

// sleepCtx waits for d unless the context ends first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// rewind restores a request body so the request can be sent again.
func rewind(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("slack: rewind request body: %w", err)
	}
	req.Body = body
	return nil
}

// retryAfterDelay reads Slack's Retry-After header, falling back to
// defaultRetryAfter and capping absurd values.
func retryAfterDelay(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
	if err != nil || seconds < 0 {
		return defaultRetryAfter
	}
	return min(time.Duration(seconds)*time.Second, maxRetryAfter)
}

// attempt performs one request, reporting whether the call was rate-limited and
// how long Slack asked us to wait before trying again.
func (c *Client) attempt(req *http.Request, method string, out any, allowErrCodes []string, outHeader *http.Header) (bool, time.Duration, error) {
	err := c.send(req, method, out, allowErrCodes, outHeader)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == errRateLimited {
		return true, retryAfterDelay(apiErr.header), err
	}
	return false, 0, err
}

// errRateLimited is Slack's error code for a throttled call.
const errRateLimited = "ratelimited"

func (c *Client) send(req *http.Request, method string, out any, allowErrCodes []string, outHeader *http.Header) error {
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
		return &APIError{Method: method, Code: envelope.Error, header: resp.Header.Clone()}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("slack: %s: decode payload: %w", method, err)
	}
	return nil
}
