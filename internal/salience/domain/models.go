package domain

import (
	"encoding/json"
	"log/slog"
	"time"
)

// Config is the parsed ai: block of config.yaml. The API key deliberately
// lives outside this DTO — platform/config keeps it Secret-typed and gateway
// constructors receive it only at the composition root.
type Config struct {
	Enabled      bool
	Provider     ProviderName
	Model        string
	BaseURL      string
	Instructions string
}

// PRSummary is the advisor's view of a PR. Title, Body, and Author are
// attacker-influenced: they cross the guard pipeline before reaching a model
// and never reach Slack from here.
type PRSummary struct {
	Number      int
	Title       string
	Body        string
	Author      string
	AuthorIsBot bool
}

// Signals are the rule-sufficient facts pre-computed by signals.go — never
// asked of the model, always fed to it.
type Signals struct {
	Breaking      bool
	Revert        bool
	DocsOnly      bool
	DepsOnly      bool
	GeneratedOnly bool
}

// CandidateTarget is one mapping-declared fan-out destination the advisor may
// select and decorate. Mentions is the full configured set for that channel;
// a decision may only ping a subset of it.
type CandidateTarget struct {
	Channel  string
	Mentions []string
}

// OpenDecisionRequest carries everything the advisor may consider for an
// opened/ready PR. TierEnabled is the resolved per-tier ai.enabled;
// Instructions is the concatenated global→org→repo operator guidance.
// Signals is filled by the advisor itself (resilient path), not the caller.
type OpenDecisionRequest struct {
	Repository     string
	PR             PRSummary
	Signals        Signals
	ChangedFiles   []string
	Candidates     []CandidateTarget
	DefaultEmoji   string
	EmojiAllowlist []string
	Instructions   string
	TierEnabled    bool
}

// TargetDecision is the per-channel open decision; every field is clamped
// (enums, subsets of configured values, bounded sanitized text).
type TargetDecision struct {
	Channel      string
	Loudness     Loudness
	Mentions     []string
	LeadingEmoji string
	Format       Format
	Emphasis     Emphasis
	ContextBlock string
	ThreadNote   string
}

// DecisionTrace carries the observability fields every decision records for
// the ai-decision log line. Rationale is logged, never posted.
type DecisionTrace struct {
	Rationale      string
	FallbackReason FallbackReason
	TokensIn       int
	TokensOut      int
	CacheHit       bool
}

// OpenDecision is the advisor's answer for an opened/ready PR: one decision
// per selected candidate channel. Implementations never return an empty
// Targets list for a non-empty Candidates list — salience can never drop a PR.
type OpenDecision struct {
	Targets []TargetDecision
	DecisionTrace
}

// UpdatedDecisionRequest describes a review/merge/close event. Kind is the
// kernel event-kind token (e.g. "approved", "merged"); DefaultEmoji is the
// emoji the event would use today.
type UpdatedDecisionRequest struct {
	Repository     string
	PR             PRSummary
	Kind           string
	SenderLogin    string
	SenderIsBot    bool
	DefaultEmoji   string
	EmojiAllowlist []string
	Instructions   string
	TierEnabled    bool
}

// UpdatedDecision picks the emoji substituted wherever the configured one
// would appear for the event — the reaction and, on merge/close, the updated
// message's leading emoji.
type UpdatedDecision struct {
	Emoji string
	DecisionTrace
}

// DigestPRSummary is one stuck PR as the digest advisor sees it. The store
// keeps no PR title, so there is none here.
type DigestPRSummary struct {
	Repository string
	Number     int
	IdleDays   int
}

// DigestDecisionRequest describes one channel's stuck-PR report. Instructions
// is filled by the advisor from the global config (digest spans repos, so
// per-tier guidance does not apply).
type DigestDecisionRequest struct {
	Channel      string
	PRs          []DigestPRSummary
	Mentions     []string
	Instructions string
}

// DigestDecision reorders and decorates one channel report. Order is a
// permutation of the request's PR indices; Highlights and Notes are parallel
// to the request's PRs (indexed by input position, not output position). The
// digest always posts — ParentLoudness only modulates the parent's mentions.
type DigestDecision struct {
	Order          []int
	Highlights     []Highlight
	Notes          []string
	ParentLoudness Loudness
	DecisionTrace
}

// ModelRequest is one structured-output generation call: a trusted system
// prompt, a user payload whose untrusted parts are enveloped, a JSON Schema
// the response must match, and an output-token cap. One turn, zero tools.
type ModelRequest struct {
	System          string
	User            string
	Schema          json.RawMessage
	MaxOutputTokens int
}

// ModelResponse is the provider-neutral generation result. RateLimit is
// best-effort header data (nil when the provider exposes none).
type ModelResponse struct {
	Text      string
	TokensIn  int
	TokensOut int
	RateLimit *RateLimitInfo
}

// RateLimitInfo is best-effort rate-limit headroom parsed from provider
// response headers (x-ratelimit-*). Unknown numeric fields are -1.
type RateLimitInfo struct {
	RequestsRemaining int
	RequestsLimit     int
	TokensRemaining   int
	TokensLimit       int
	Reset             string
}

// GatewayConfig is the constructor input for a provider client. APIKey is the
// revealed AI_API_KEY (empty for keyless openai_compatible endpoints); it is
// excluded from marshaling so the key cannot leave the process by accident.
type GatewayConfig struct {
	APIKey  string `json:"-"`
	Model   string
	BaseURL string
}

// AdvisorParams is the NewAdvisor factory input. Gateway is nil when the
// feature is disabled; Now defaults to time.Now when nil.
type AdvisorParams struct {
	Config  Config
	Gateway ModelGateway
	Logger  *slog.Logger
	Now     func() time.Time
}
