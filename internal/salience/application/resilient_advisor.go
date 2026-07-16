package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// ResilientAdvisor is the Advisor bound when ai.enabled is true. It wraps the
// model advisor with the per-tier opt-out, the decision cache, the circuit
// breaker, and the per-decision timeout, and emits one `ai decision` log line
// per consultation (mirroring the ignored-webhook-event contract). Every skip
// lands on the deterministic advisor — zero I/O, always succeeds.
type ResilientAdvisor struct {
	config        domain.Config
	model         *ModelAdvisor
	deterministic *DeterministicAdvisor
	cache         *decisionCache
	circuit       *circuitBreaker
	logger        *slog.Logger
	now           func() time.Time
}

// NewResilientAdvisor builds the resilient advisor from its params. Now
// defaults to time.Now and Logger to slog.Default when nil.
func NewResilientAdvisor(params domain.AdvisorParams) *ResilientAdvisor {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	logger := params.Logger
	if logger == nil {
		logger = slog.Default()
	}
	deterministic := NewDeterministicAdvisor()
	return &ResilientAdvisor{
		config:        params.Config,
		model:         NewModelAdvisor(params.Gateway, deterministic),
		deterministic: deterministic,
		cache:         newDecisionCache(domain.CacheSize, domain.CacheTTL),
		circuit:       newCircuitBreaker(domain.CircuitFailureThreshold, domain.CircuitOpenDuration),
		logger:        logger,
		now:           now,
	}
}

// DecideOpen implements domain.Advisor.
func (a *ResilientAdvisor) DecideOpen(ctx context.Context, request domain.OpenDecisionRequest) domain.OpenDecision {
	started := a.now()
	if !request.TierEnabled {
		decision := a.deterministic.DecideOpen(ctx, request)
		decision.FallbackReason = domain.FallbackDisabled
		a.log(domain.SurfaceOpen, decision.DecisionTrace, started)
		return decision
	}
	key := cacheKey(domain.SurfaceOpen, request)
	if cached, ok := a.cache.get(key, started); ok {
		decision := cached.(domain.OpenDecision)
		decision.CacheHit = true
		a.log(domain.SurfaceOpen, decision.DecisionTrace, started)
		return decision
	}
	if a.circuit.open(started) {
		decision := a.deterministic.DecideOpen(ctx, request)
		decision.FallbackReason = domain.FallbackCircuitOpen
		a.log(domain.SurfaceOpen, decision.DecisionTrace, started)
		return decision
	}
	request.Signals = ComputeSignals(request.PR.Title, request.PR.Body, request.ChangedFiles)
	decideCtx, cancel := context.WithTimeout(ctx, domain.DecisionTimeout)
	defer cancel()
	decision := a.model.DecideOpen(decideCtx, request)
	a.circuit.record(isGatewayFailure(decision.FallbackReason), a.now())
	if modelDecisionApplied(decision.FallbackReason) {
		a.cache.put(key, decision, a.now())
	}
	a.log(domain.SurfaceOpen, decision.DecisionTrace, started)
	return decision
}

// DecideUpdated implements domain.Advisor.
func (a *ResilientAdvisor) DecideUpdated(ctx context.Context, request domain.UpdatedDecisionRequest) domain.UpdatedDecision {
	started := a.now()
	if !request.TierEnabled {
		decision := a.deterministic.DecideUpdated(ctx, request)
		decision.FallbackReason = domain.FallbackDisabled
		a.log(domain.SurfaceUpdated, decision.DecisionTrace, started)
		return decision
	}
	key := cacheKey(domain.SurfaceUpdated, request)
	if cached, ok := a.cache.get(key, started); ok {
		decision := cached.(domain.UpdatedDecision)
		decision.CacheHit = true
		a.log(domain.SurfaceUpdated, decision.DecisionTrace, started)
		return decision
	}
	if a.circuit.open(started) {
		decision := a.deterministic.DecideUpdated(ctx, request)
		decision.FallbackReason = domain.FallbackCircuitOpen
		a.log(domain.SurfaceUpdated, decision.DecisionTrace, started)
		return decision
	}
	decideCtx, cancel := context.WithTimeout(ctx, domain.DecisionTimeout)
	defer cancel()
	decision := a.model.DecideUpdated(decideCtx, request)
	a.circuit.record(isGatewayFailure(decision.FallbackReason), a.now())
	if modelDecisionApplied(decision.FallbackReason) {
		a.cache.put(key, decision, a.now())
	}
	a.log(domain.SurfaceUpdated, decision.DecisionTrace, started)
	return decision
}

// DecideDigest implements domain.Advisor. The digest is a cron (no
// redeliveries), so it skips the cache; it fills the global operator
// instructions itself because digest groups span repo tiers.
func (a *ResilientAdvisor) DecideDigest(ctx context.Context, request domain.DigestDecisionRequest) domain.DigestDecision {
	started := a.now()
	if a.circuit.open(started) {
		decision := a.deterministic.DecideDigest(ctx, request)
		decision.FallbackReason = domain.FallbackCircuitOpen
		a.log(domain.SurfaceDigest, decision.DecisionTrace, started)
		return decision
	}
	request.Instructions = a.config.Instructions
	decideCtx, cancel := context.WithTimeout(ctx, domain.DigestDecisionTimeout)
	defer cancel()
	decision := a.model.DecideDigest(decideCtx, request)
	a.circuit.record(isGatewayFailure(decision.FallbackReason), a.now())
	a.log(domain.SurfaceDigest, decision.DecisionTrace, started)
	return decision
}

// isGatewayFailure reports whether the reason indicates the provider itself
// failed — the classes the circuit breaker counts. Content-level failures
// (malformed, clamp, guard) do not open the circuit.
func isGatewayFailure(reason domain.FallbackReason) bool {
	return reason == domain.FallbackTimeout || reason == domain.FallbackTransportError || reason == domain.FallbackRateLimited
}

// modelDecisionApplied reports whether the decision content came from the
// model (fully, or clamped per field) — the only decisions worth caching.
func modelDecisionApplied(reason domain.FallbackReason) bool {
	return reason == domain.FallbackNone || reason == domain.FallbackClampViolation
}

// cacheKey fingerprints a request: surface plus a hash of the full request
// payload, so redeliveries hit and any content change misses.
func cacheKey(surface domain.Surface, request any) string {
	payload, _ := json.Marshal(request)
	sum := sha256.Sum256(payload)
	return string(surface) + ":" + hex.EncodeToString(sum[:])
}

// log emits the one structured line per consultation.
func (a *ResilientAdvisor) log(surface domain.Surface, trace domain.DecisionTrace, started time.Time) {
	a.logger.Info("ai decision",
		slog.String("surface", string(surface)),
		slog.String("provider", string(a.config.Provider)),
		slog.String("model", a.config.Model),
		slog.Int64("latency_ms", a.now().Sub(started).Milliseconds()),
		slog.Int("tokens_in", trace.TokensIn),
		slog.Int("tokens_out", trace.TokensOut),
		slog.Bool("cache_hit", trace.CacheHit),
		slog.String("fallback_reason", string(trace.FallbackReason)),
		slog.String("rationale", trace.Rationale),
	)
}

var _ domain.Advisor = (*ResilientAdvisor)(nil)
