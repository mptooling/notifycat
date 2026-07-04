package domain

import (
	"context"
	"errors"
	"time"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// Sentinel errors let callers render clear remediation messages and pick exit
// codes without parsing strings or leaking a stack trace.
var (
	// ErrNoMapping means the repository is absent from mappings. Returned
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

// SmokeConfig carries the parameters that drive a smoke run. All are derived
// from config / environment at wiring time; the application layer stays free of
// config imports.
type SmokeConfig struct {
	WebhookURL      string
	WebhookSecret   string
	IgnoreAIReviews bool

	// Reactions mirrors the server's reaction configuration.
	Reactions SmokeReactionsConfig
	// Now supplies the clock used to derive a unique PR number per run.
	Now func() time.Time
}

// SmokeReactionsConfig mirrors config.Reactions without importing the config
// package.
type SmokeReactionsConfig struct {
	Enabled       bool
	NewPR         string
	MergedPR      string
	Approved      string
	Commented     string
	RequestChange string
	BotReview     string
}

// SmokeMessage is the stored-message view smoke reads after delivery.
type SmokeMessage struct {
	Channel   string
	MessageID string
}

// SmokeReactionCheck is the outcome of one lifecycle step: the event that was
// replayed, the emoji the server was expected to add, and whether the Slack
// readback confirmed it. VerifyErr is set when the reaction could not be read
// back at all (e.g. the bot token lacks reactions:read) — distinct from a
// confirmed absence.
type SmokeReactionCheck struct {
	Step      string
	Emoji     string
	Present   bool
	VerifyErr error
}

// SmokeResult describes a completed delivery run.
type SmokeResult struct {
	Repository string
	Channel    string
	PRNumber   int
	Title      string
	Timestamp  string
	URL        string

	// ReactionsRequested is true when the caller asked for the lifecycle pass.
	ReactionsRequested bool
	// ReactionsEnabled mirrors the server's reactions.enabled. When a caller
	// requests reactions but this is false, the lifecycle is skipped.
	ReactionsEnabled bool
	Reactions        []SmokeReactionCheck

	// IgnoreAIReviews and BotReviewMarker let the CLI explain why the bot-review
	// step was skipped.
	IgnoreAIReviews bool
	BotReviewMarker string
}

// Signer signs a webhook body, returning the HTTP header name and value.
type Signer interface {
	Sign(secret string, body []byte) (header, value string)
}

// WebhookSender POSTs a signed webhook body and returns the HTTP status code.
// A transport error is returned as a non-nil err with status 0.
type WebhookSender interface {
	Send(ctx context.Context, url string, body []byte, headers map[string]string) (status int, err error)
}

// SmokeMappings resolves a repository to its routing mapping.
type SmokeMappings interface {
	Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
}

// SmokeMessages reads the stored messages for a PR.
type SmokeMessages interface {
	Messages(ctx context.Context, repository string, prNumber int) ([]SmokeMessage, error)
}

// SmokeReactions reads the reaction emoji names on a Slack message.
type SmokeReactions interface {
	Reactions(ctx context.Context, channel, ts string) ([]string, error)
}

// SmokeCleanup deletes the synthetic pull_requests row the smoke run causes
// the server to create. It is called deferred so cleanup runs on every exit
// path once the PR number is known.
type SmokeCleanup interface {
	DeletePR(ctx context.Context, repository string, prNumber int) error
}

// Smoke runs a synthetic webhook delivery (optionally replaying review
// reactions) against the live endpoint and reports what it observed.
type Smoke interface {
	Run(ctx context.Context, target string, withReactions bool) (SmokeResult, error)
}
