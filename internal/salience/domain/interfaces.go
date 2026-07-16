package domain

import "context"

// Advisor decides how loudly a notification is presented — never whether it
// exists and never its fundamental content. No method returns an error: the
// advisor cannot fail from a consumer's viewpoint; implementations record a
// FallbackReason instead. Handlers and the digest reporter inject this port
// and never know whether AI is on.
type Advisor interface {
	DecideOpen(ctx context.Context, request OpenDecisionRequest) OpenDecision
	DecideUpdated(ctx context.Context, request UpdatedDecisionRequest) UpdatedDecision
	DecideDigest(ctx context.Context, request DigestDecisionRequest) DigestDecision
}

// ModelGateway is the provider port: one structured-output generation call.
// Implementations are hand-rolled HTTP clients (gemini, openaicompat); a 429
// surfaces as *RateLimitedError, a deadline as the context error.
type ModelGateway interface {
	Generate(ctx context.Context, request ModelRequest) (ModelResponse, error)
}
