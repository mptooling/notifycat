package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// NewAdvisor picks the Advisor binding for the deployment: the deterministic
// advisor when the feature is off (or no gateway was built), the resilient
// model-backed advisor when it is on. Consumers never know which they got.
func NewAdvisor(params domain.AdvisorParams) domain.Advisor {
	if !params.Config.Enabled || params.Gateway == nil {
		return NewDeterministicAdvisor()
	}
	return NewResilientAdvisor(params)
}

// ModelAdvisor asks the model gateway for a structured decision through the
// guard pipeline: tripwire → minimize+envelope → gateway → strict parse →
// clamp. Every failure returns the deterministic decision with a classifying
// FallbackReason; it never errors and never retries — systemic failure is the
// circuit breaker's job (resilient advisor).
type ModelAdvisor struct {
	gateway       domain.ModelGateway
	deterministic domain.Advisor
}

// NewModelAdvisor builds a ModelAdvisor over a provider gateway.
func NewModelAdvisor(gateway domain.ModelGateway, deterministic domain.Advisor) *ModelAdvisor {
	return &ModelAdvisor{gateway: gateway, deterministic: deterministic}
}

type targetDecisionWire struct {
	Channel      string   `json:"channel"`
	Loudness     string   `json:"loudness"`
	Mentions     []string `json:"mentions"`
	LeadingEmoji string   `json:"leading_emoji"`
	Format       string   `json:"format"`
	Emphasis     string   `json:"emphasis"`
	ContextBlock string   `json:"context_block"`
}

type openDecisionWire struct {
	Targets   []targetDecisionWire `json:"targets"`
	Rationale string               `json:"rationale"`
}

type updatedDecisionWire struct {
	Emoji     string `json:"emoji"`
	Rationale string `json:"rationale"`
}

type digestDecisionWire struct {
	Order          []int    `json:"order"`
	Highlights     []string `json:"highlights"`
	Notes          []string `json:"notes"`
	ParentLoudness string   `json:"parent_loudness"`
	Rationale      string   `json:"rationale"`
}

// DecideOpen implements domain.Advisor.
func (a *ModelAdvisor) DecideOpen(ctx context.Context, request domain.OpenDecisionRequest) domain.OpenDecision {
	fallback := a.deterministic.DecideOpen(ctx, request)
	// Build the minimized envelope first: minimizeBody removes HTML comments by
	// deleting them, which concatenates adjacent text and can reassemble injection
	// phrases not present in the raw body. The guard must see the same text the
	// model will see — the minimized title, body, files, and author, all of which
	// are placed inside the untrusted envelope.
	envelope := newMinimizedOpenEnvelope(request)
	if guardTripped(envelope.title, envelope.body, strings.Join(envelope.files, "\n"), envelope.author) {
		fallback.FallbackReason = domain.FallbackGuardTripped
		return fallback
	}
	response, failure := a.generate(ctx, domain.ModelRequest{
		System:          systemPrompt(openTask, request.Instructions),
		User:            openUserPrompt(envelope, request),
		Schema:          openDecisionSchema(),
		MaxOutputTokens: domain.MaxOutputTokens,
	})
	if failure != domain.FallbackNone {
		fallback.FallbackReason = failure
		return fallback
	}
	var wire openDecisionWire
	if err := strictUnmarshal(response.Text, &wire); err != nil {
		fallback.FallbackReason = domain.FallbackMalformedOutput
		fallback.TokensIn, fallback.TokensOut = response.TokensIn, response.TokensOut
		return fallback
	}
	decision := domain.OpenDecision{Targets: make([]domain.TargetDecision, len(wire.Targets))}
	for i, target := range wire.Targets {
		decision.Targets[i] = domain.TargetDecision{
			Channel:      target.Channel,
			Loudness:     domain.Loudness(target.Loudness),
			Mentions:     target.Mentions,
			LeadingEmoji: target.LeadingEmoji,
			Format:       domain.Format(target.Format),
			Emphasis:     domain.Emphasis(target.Emphasis),
			ContextBlock: target.ContextBlock,
		}
	}
	clamped, violated := clampOpen(decision, request)
	if violated {
		clamped.FallbackReason = domain.FallbackClampViolation
	}
	clamped.TokensIn, clamped.TokensOut = response.TokensIn, response.TokensOut
	clamped.Rationale = truncateRunes(wire.Rationale, domain.MaxRationaleChars)
	return clamped
}

// DecideUpdated implements domain.Advisor.
func (a *ModelAdvisor) DecideUpdated(ctx context.Context, request domain.UpdatedDecisionRequest) domain.UpdatedDecision {
	fallback := a.deterministic.DecideUpdated(ctx, request)
	if guardTripped(request.PR.Title, request.SenderLogin) {
		fallback.FallbackReason = domain.FallbackGuardTripped
		return fallback
	}
	response, failure := a.generate(ctx, domain.ModelRequest{
		System:          systemPrompt(updatedTask, request.Instructions),
		User:            updatedUserPrompt(request),
		Schema:          updatedDecisionSchema(),
		MaxOutputTokens: domain.MaxOutputTokens,
	})
	if failure != domain.FallbackNone {
		fallback.FallbackReason = failure
		return fallback
	}
	var wire updatedDecisionWire
	if err := strictUnmarshal(response.Text, &wire); err != nil {
		fallback.FallbackReason = domain.FallbackMalformedOutput
		fallback.TokensIn, fallback.TokensOut = response.TokensIn, response.TokensOut
		return fallback
	}
	clamped, violated := clampUpdated(domain.UpdatedDecision{Emoji: wire.Emoji}, request)
	if violated {
		clamped.FallbackReason = domain.FallbackClampViolation
	}
	clamped.TokensIn, clamped.TokensOut = response.TokensIn, response.TokensOut
	clamped.Rationale = truncateRunes(wire.Rationale, domain.MaxRationaleChars)
	return clamped
}

// DecideDigest implements domain.Advisor. Digest summaries carry no
// attacker-authored text (the store keeps no titles), so there is no
// tripwire stage; the prompt caps at MaxDigestPRs and the clamp pads the
// tail back deterministically.
func (a *ModelAdvisor) DecideDigest(ctx context.Context, request domain.DigestDecisionRequest) domain.DigestDecision {
	fallback := a.deterministic.DecideDigest(ctx, request)
	decidedCount := len(request.PRs)
	if decidedCount > domain.MaxDigestPRs {
		decidedCount = domain.MaxDigestPRs
	}
	response, failure := a.generate(ctx, domain.ModelRequest{
		System:          systemPrompt(digestTask, request.Instructions),
		User:            digestUserPrompt(request, decidedCount),
		Schema:          digestDecisionSchema(),
		MaxOutputTokens: domain.MaxOutputTokens,
	})
	if failure != domain.FallbackNone {
		fallback.FallbackReason = failure
		return fallback
	}
	var wire digestDecisionWire
	if err := strictUnmarshal(response.Text, &wire); err != nil {
		fallback.FallbackReason = domain.FallbackMalformedOutput
		fallback.TokensIn, fallback.TokensOut = response.TokensIn, response.TokensOut
		return fallback
	}
	highlights := make([]domain.Highlight, len(wire.Highlights))
	for i, highlight := range wire.Highlights {
		highlights[i] = domain.Highlight(highlight)
	}
	clamped, violated := clampDigest(domain.DigestDecision{
		Order:          wire.Order,
		Highlights:     highlights,
		Notes:          wire.Notes,
		ParentLoudness: domain.Loudness(wire.ParentLoudness),
	}, request)
	if violated {
		clamped.FallbackReason = domain.FallbackClampViolation
	}
	clamped.TokensIn, clamped.TokensOut = response.TokensIn, response.TokensOut
	clamped.Rationale = truncateRunes(wire.Rationale, domain.MaxRationaleChars)
	return clamped
}

// generate performs one gateway call and classifies its failure.
func (a *ModelAdvisor) generate(ctx context.Context, request domain.ModelRequest) (domain.ModelResponse, domain.FallbackReason) {
	response, err := a.gateway.Generate(ctx, request)
	switch {
	case err == nil:
		return response, domain.FallbackNone
	case errors.Is(err, context.DeadlineExceeded):
		return domain.ModelResponse{}, domain.FallbackTimeout
	default:
		var rateLimited *domain.RateLimitedError
		if errors.As(err, &rateLimited) {
			return domain.ModelResponse{}, domain.FallbackRateLimited
		}
		return domain.ModelResponse{}, domain.FallbackTransportError
	}
}

// strictUnmarshal parses the model text with unknown fields rejected. No
// lenient repair, no retry — a malformed response is a fallback. Exactly one
// JSON object must be present; any trailing content is an error.
func strictUnmarshal(text string, value any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected trailing content")
	}
	return nil
}

var _ domain.Advisor = (*ModelAdvisor)(nil)
