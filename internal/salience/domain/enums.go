package domain

// ProviderName identifies a model-provider adapter. Exactly one provider is
// wired per deployment, selected by ai.provider — mirroring the git_provider
// stance (no per-tier provider, model, or key).
type ProviderName string

// Recognised providers.
const (
	ProviderGemini           ProviderName = "gemini"
	ProviderOpenAICompatible ProviderName = "openai_compatible"
)

// String returns the config/log token for the provider.
func (p ProviderName) String() string { return string(p) }

// Loudness is how strongly a message pings: ping keeps the decided mentions,
// quiet drops them. A quiet message still posts — never-skip is structural.
type Loudness string

// Loudness values.
const (
	LoudnessPing  Loudness = "ping"
	LoudnessQuiet Loudness = "quiet"
)

// Format selects the open-message template: the standard "please review"
// message or the compact one-liner (the dependency-bot style).
type Format string

// Format values.
const (
	FormatStandard Format = "standard"
	FormatCompact  Format = "compact"
)

// Emphasis marks a PR for extra visual weight. Rendering (emoji, label) stays
// in the deterministic template.
type Emphasis string

// Emphasis values.
const (
	EmphasisNone     Emphasis = "none"
	EmphasisBreaking Emphasis = "breaking"
)

// Highlight is the per-PR digest decoration.
type Highlight string

// Highlight values.
const (
	HighlightNormal    Highlight = "normal"
	HighlightAttention Highlight = "attention"
)

// Surface names a consultation point, used in cache keys and the ai-decision
// log line.
type Surface string

// Surface values.
const (
	SurfaceOpen    Surface = "open"
	SurfaceUpdated Surface = "updated"
	SurfaceDigest  Surface = "digest"
)

// FallbackReason records why a decision fell back to deterministic values.
// Empty means the model decision was applied. The operations doc carries the
// matching reason table (like the ignored-webhook-event contract).
type FallbackReason string

// Fallback reasons, one per failure class.
const (
	FallbackNone            FallbackReason = ""
	FallbackTimeout         FallbackReason = "timeout"
	FallbackTransportError  FallbackReason = "transport_error"
	FallbackRateLimited     FallbackReason = "rate_limited"
	FallbackMalformedOutput FallbackReason = "malformed_output"
	FallbackGuardTripped    FallbackReason = "guard_tripped"
	FallbackClampViolation  FallbackReason = "clamp_violation"
	FallbackCircuitOpen     FallbackReason = "circuit_open"
	FallbackDisabled        FallbackReason = "disabled"
)
