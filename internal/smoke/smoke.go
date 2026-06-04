// Package smoke drives an end-to-end delivery test against a running notifycat
// stack. It forges a correctly-signed `pull_request: opened` webhook, POSTs it
// to the live /webhook/github endpoint — exercising the real signature
// middleware, dispatcher, and Slack client — and then reads the resulting Slack
// message timestamp back from the database so the operator can confirm a real
// message landed in the mapped channel. The CLI entry point lives in
// cmd/notifycat-smoke; the wrapper exposes it as `./notifycat smoke owner/repo`.
package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/store"
)

// smokeTitlePrefix marks the synthetic PR so an operator can recognise and
// delete the Slack message it produces. It is part of the acceptance contract.
const smokeTitlePrefix = "[notifycat smoke]"

// Sentinel errors let the CLI render a clear remediation message — and pick an
// exit code — without parsing strings or leaking a stack trace.
var (
	// ErrNoMapping means the repository is absent from mappings.yaml. Returned
	// before any network call so the operator fixes config first.
	ErrNoMapping = errors.New("smoke: repository not present in mappings")
	// ErrSignatureRejected means the server answered 401 — the secret this
	// command signed with does not match the one the server runs with.
	ErrSignatureRejected = errors.New("smoke: server rejected the signature")
	// ErrUnreachable means the POST never reached a server.
	ErrUnreachable = errors.New("smoke: could not reach the server")
	// ErrUnexpectedStatus means the server answered with a non-200, non-401 code.
	ErrUnexpectedStatus = errors.New("smoke: unexpected response status")
)

// RepoMappings looks up the Slack routing for a repository. *mappings.Provider
// satisfies it; declared here so the consumer owns its interface.
type RepoMappings interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// MessageStore reads back the stored Slack message timestamp for a PR.
// *store.SlackMessages satisfies it.
type MessageStore interface {
	Get(ctx context.Context, repository string, prNumber int) (store.SlackMessage, error)
}

// Doer is the slice of *http.Client the runner needs.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Smoke runs the delivery test. Construct once via New; Run is safe to call
// repeatedly (each call uses a fresh PR number, so the open handler posts anew
// rather than treating it as an already-sent duplicate).
type Smoke struct {
	mappings RepoMappings
	store    MessageStore
	http     Doer
	secret   string
	url      string
	now      func() time.Time
}

// New wires a Smoke. url is the full webhook endpoint to POST to (e.g.
// http://notifycat:8080/webhook/github); secret is GITHUB_WEBHOOK_SECRET; now
// supplies the clock used to derive a unique PR number per run.
func New(mappings RepoMappings, messages MessageStore, httpClient Doer, secret, url string, now func() time.Time) *Smoke {
	return &Smoke{
		mappings: mappings,
		store:    messages,
		http:     httpClient,
		secret:   secret,
		url:      url,
		now:      now,
	}
}

// Result describes a successful delivery: which channel received the message,
// the PR number used, and the Slack timestamp the server stored.
type Result struct {
	Repository string
	Channel    string
	PRNumber   int
	Title      string
	Timestamp  string
	URL        string
}

// Run validates that target is mapped, posts a signed synthetic
// `pull_request: opened` to the live endpoint, and reports the channel and the
// Slack timestamp read back from the store. Mapping is checked first so an
// unmapped repo fails (ErrNoMapping) without any network traffic.
func (s *Smoke) Run(ctx context.Context, target string) (Result, error) {
	mapping, err := s.mappings.Get(ctx, target)
	if errors.Is(err, store.ErrNotFound) {
		return Result{}, fmt.Errorf("%w: %s", ErrNoMapping, target)
	}
	if err != nil {
		return Result{}, fmt.Errorf("smoke: look up mapping for %s: %w", target, err)
	}

	prNumber := int(s.now().Unix())
	title := fmt.Sprintf("%s delivery test — safe to delete (PR #%d)", smokeTitlePrefix, prNumber)
	body, err := buildPayload(target, prNumber, title)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("smoke: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set(githubhook.SignatureHeader, githubhook.Sign(s.secret, body))

	resp, err := s.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w at %s: %v", ErrUnreachable, s.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to read back the stored timestamp.
	case http.StatusUnauthorized:
		return Result{}, ErrSignatureRejected
	default:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return Result{}, fmt.Errorf("%w: %d: %s", ErrUnexpectedStatus, resp.StatusCode, bytes.TrimSpace(snippet))
	}

	msg, err := s.store.Get(ctx, target, prNumber)
	if err != nil {
		return Result{}, fmt.Errorf("smoke: server returned 200 but the Slack message timestamp was not stored "+
			"(was the repo mapped to a channel the bot can post to?): %w", err)
	}

	return Result{
		Repository: target,
		Channel:    mapping.SlackChannel,
		PRNumber:   prNumber,
		Title:      title,
		Timestamp:  msg.TS,
		URL:        s.url,
	}, nil
}

// buildPayload renders a minimal `pull_request: opened` body carrying only the
// fields githubhook.ParsePayload and the open handler read. draft is false so
// the open handler acts rather than ignoring it as a draft.
func buildPayload(repository string, number int, title string) ([]byte, error) {
	type user struct {
		Login string `json:"login"`
	}
	payload := struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			User    user   `json:"user"`
			Draft   bool   `json:"draft"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
	}{Action: "opened"}
	payload.Repository.FullName = repository
	payload.PullRequest.Number = number
	payload.PullRequest.Title = title
	payload.PullRequest.HTMLURL = fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
	payload.PullRequest.User = user{Login: "notifycat-smoke"}
	payload.Sender.Login = "notifycat-smoke"
	payload.Sender.Type = "User"

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("smoke: marshal payload: %w", err)
	}
	return body, nil
}
