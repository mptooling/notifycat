// Package smoke drives an end-to-end delivery test against a running notifycat
// stack. It forges a correctly-signed `pull_request: opened` webhook, POSTs it
// to the live /webhook/github endpoint — exercising the real signature
// middleware, dispatcher, and Slack client — and then reads the resulting Slack
// message timestamp back from the database so the operator can confirm a real
// message landed in the mapped channel.
//
// With reactions requested (the --reactions flag), it additionally replays the
// review lifecycle for the same synthetic PR — a comment, an approval, and a
// merge — and verifies, via reactions.get, that the server added the configured
// emoji to the message. The CLI entry point lives in cmd/notifycat-smoke; the
// wrapper exposes it as `./notifycat smoke owner/repo`.
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

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/slack"
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

// ReactionReader reads the reactions attached to a Slack message, so the smoke
// test can confirm the server actually reacted. *slack.Client satisfies it.
type ReactionReader interface {
	GetReactions(ctx context.Context, channel, ts string) ([]slack.Reaction, error)
}

// Doer is the slice of *http.Client the runner needs.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Smoke runs the delivery test. Construct once via New; Run is safe to call
// repeatedly (each call uses a fresh PR number, so the open handler posts anew
// rather than treating it as an already-sent duplicate).
type Smoke struct {
	mappings        RepoMappings
	store           MessageStore
	reactions       ReactionReader
	http            Doer
	secret          string
	url             string
	rxCfg           config.Reactions
	ignoreAIReviews bool
	now             func() time.Time
}

// New wires a Smoke. url is the full webhook endpoint to POST to (e.g.
// http://notifycat:8080/webhook/github); secret is GITHUB_WEBHOOK_SECRET;
// rxCfg mirrors the server's reaction config so the verifier knows which emoji
// to expect; ignoreAIReviews mirrors NOTIFYCAT_IGNORE_AI_REVIEWS so the runner
// only replays a bot review when the server would actually mark it; now
// supplies the clock used to derive a unique PR number per run.
func New(mappings RepoMappings, messages MessageStore, reactions ReactionReader, httpClient Doer, secret, url string, rxCfg config.Reactions, ignoreAIReviews bool, now func() time.Time) *Smoke {
	return &Smoke{
		mappings:        mappings,
		store:           messages,
		reactions:       reactions,
		http:            httpClient,
		secret:          secret,
		url:             url,
		rxCfg:           rxCfg,
		ignoreAIReviews: ignoreAIReviews,
		now:             now,
	}
}

// ReactionCheck is the outcome of one lifecycle step: the event that was
// replayed, the emoji the server was expected to add, and whether reactions.get
// confirmed it. VerifyErr is set when the reaction could not be read back at all
// (e.g. the bot token lacks reactions:read) — distinct from a confirmed absence.
type ReactionCheck struct {
	Step      string
	Emoji     string
	Present   bool
	VerifyErr error
}

// Result describes a successful delivery: which channel received the message,
// the PR number used, the Slack timestamp the server stored, and — when
// reactions were requested — the per-step reaction verifications.
type Result struct {
	Repository string
	Channel    string
	PRNumber   int
	Title      string
	Timestamp  string
	URL        string

	// ReactionsRequested is true when the caller asked for the lifecycle pass.
	ReactionsRequested bool
	// ReactionsEnabled mirrors the server's SLACK_REACTIONS_ENABLED. When a
	// caller requests reactions but this is false, the lifecycle is skipped.
	ReactionsEnabled bool
	Reactions        []ReactionCheck

	// IgnoreAIReviews and BotReviewMarker let the CLI explain why the bot-review
	// step was skipped: either bot reviews are muted, or the marker is disabled.
	IgnoreAIReviews bool
	BotReviewMarker string
}

// ghEvent describes one synthetic webhook to replay.
type ghEvent struct {
	event       string // X-GitHub-Event header
	action      string
	reviewState string // review.state, empty when there is no review object
	merged      bool
	senderType  string // sender.type; empty defaults to "User" in buildPayload
}

// lifecycleStep pairs a synthetic event with the emoji the server is expected
// to add for it, plus a label for the report.
type lifecycleStep struct {
	name  string
	emoji string
	ev    ghEvent
}

// Run validates that target is mapped, posts a signed synthetic
// `pull_request: opened` to the live endpoint, and reports the channel and the
// Slack timestamp read back from the store. Mapping is checked first so an
// unmapped repo fails (ErrNoMapping) without any network traffic.
//
// When withReactions is set and the server has reactions enabled, Run then
// replays a comment, an approval, and a merge for the same PR and verifies the
// configured emoji appeared on the message. A missing emoji is recorded in the
// Result (not returned as an error) so the CLI can report every step.
func (s *Smoke) Run(ctx context.Context, target string, withReactions bool) (Result, error) {
	mapping, err := s.mappings.Get(ctx, target)
	if errors.Is(err, store.ErrNotFound) {
		return Result{}, fmt.Errorf("%w: %s", ErrNoMapping, target)
	}
	if err != nil {
		return Result{}, fmt.Errorf("smoke: look up mapping for %s: %w", target, err)
	}

	prNumber := int(s.now().Unix())
	title := fmt.Sprintf("%s delivery test — safe to delete (PR #%d)", smokeTitlePrefix, prNumber)
	res := Result{
		Repository:         target,
		Channel:            mapping.SlackChannel,
		PRNumber:           prNumber,
		Title:              title,
		URL:                s.url,
		ReactionsRequested: withReactions,
		ReactionsEnabled:   s.rxCfg.Enabled,
		IgnoreAIReviews:    s.ignoreAIReviews,
		BotReviewMarker:    s.rxCfg.BotReview,
	}

	if err := s.deliver(ctx, target, prNumber, title, ghEvent{event: "pull_request", action: "opened"}); err != nil {
		return Result{}, err
	}

	msg, err := s.store.Get(ctx, target, prNumber)
	if err != nil {
		return Result{}, fmt.Errorf("smoke: server returned 200 but the Slack message timestamp was not stored "+
			"(was the repo mapped to a channel the bot can post to?): %w", err)
	}
	res.Timestamp = msg.TS

	if !withReactions || !s.rxCfg.Enabled {
		return res, nil
	}

	steps := []lifecycleStep{
		{"comment", s.rxCfg.Commented, ghEvent{event: "pull_request_review", action: "submitted", reviewState: "commented"}},
	}
	// Replay a bot review only when the server would actually mark it: bot
	// reviews aren't muted and a marker emoji is configured. Otherwise the
	// readback would find nothing and report a spurious miss — so we skip it
	// here and the CLI prints why (see renderReactions).
	if !s.ignoreAIReviews && s.rxCfg.BotReview != "" {
		steps = append(steps, lifecycleStep{"bot", s.rxCfg.BotReview, ghEvent{
			event: "pull_request_review", action: "submitted", reviewState: "commented", senderType: "Bot",
		}})
	}
	steps = append(steps,
		lifecycleStep{"approve", s.rxCfg.Approved, ghEvent{event: "pull_request_review", action: "submitted", reviewState: "approved"}},
		lifecycleStep{"merge", s.rxCfg.MergedPR, ghEvent{event: "pull_request", action: "closed", merged: true}},
	)
	for _, step := range steps {
		if err := s.deliver(ctx, target, prNumber, title, step.ev); err != nil {
			return res, err
		}
		check := ReactionCheck{Step: step.name, Emoji: step.emoji}
		reactions, gerr := s.reactions.GetReactions(ctx, mapping.SlackChannel, msg.TS)
		if gerr != nil {
			check.VerifyErr = gerr
		} else {
			check.Present = containsReaction(reactions, step.emoji)
		}
		res.Reactions = append(res.Reactions, check)
	}
	return res, nil
}

// deliver signs and POSTs one synthetic event, mapping the response to a
// sentinel error. A 200 is success; 401 is a secret mismatch; anything else is
// unexpected.
func (s *Smoke) deliver(ctx context.Context, repository string, number int, title string, ev ghEvent) error {
	body, err := buildPayload(repository, number, title, ev)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("smoke: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", ev.event)
	req.Header.Set(githubhook.SignatureHeader, githubhook.Sign(s.secret, body))

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w at %s: %v", ErrUnreachable, s.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrSignatureRejected
	default:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%w: %d: %s", ErrUnexpectedStatus, resp.StatusCode, bytes.TrimSpace(snippet))
	}
}

func containsReaction(reactions []slack.Reaction, name string) bool {
	for _, r := range reactions {
		if r.Name == name {
			return true
		}
	}
	return false
}

// buildPayload renders a minimal webhook body carrying only the fields
// githubhook.ParsePayload and the handlers read for ev. The sender defaults to
// a User (so bot-reviewer logic never fires) unless ev.senderType overrides it
// — the bot-review marker step sets it to "Bot"; draft is false so the open
// handler acts rather than ignoring it.
func buildPayload(repository string, number int, title string, ev ghEvent) ([]byte, error) {
	type user struct {
		Login string `json:"login"`
	}
	type review struct {
		State string `json:"state"`
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
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
		} `json:"pull_request"`
		Review *review `json:"review,omitempty"`
		Sender struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
	}{Action: ev.action}
	payload.Repository.FullName = repository
	payload.PullRequest.Number = number
	payload.PullRequest.Title = title
	payload.PullRequest.HTMLURL = fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
	payload.PullRequest.User = user{Login: "notifycat-smoke"}
	payload.PullRequest.Merged = ev.merged
	if ev.reviewState != "" {
		payload.Review = &review{State: ev.reviewState}
	}
	payload.Sender.Login = "notifycat-smoke"
	payload.Sender.Type = "User"
	if ev.senderType != "" {
		payload.Sender.Type = ev.senderType
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("smoke: marshal payload: %w", err)
	}
	return body, nil
}
