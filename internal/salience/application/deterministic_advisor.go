// Package application holds the salience use cases: the deterministic,
// model-backed, and resilient advisors plus the pure guard-pipeline stages
// (signals, minimize, guard, sanitize, clamp).
package application

import (
	"context"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// DeterministicAdvisor repackages today's config-driven behavior as
// decisions: every candidate posts, loud, standard format, configured
// mentions and emoji, no notes, digest in given order. It performs zero I/O
// and always succeeds — every fallback path lands here, and with
// ai.enabled: false it is the bound Advisor, keeping Slack output
// byte-identical to pre-salience notifycat.
type DeterministicAdvisor struct{}

// NewDeterministicAdvisor builds a DeterministicAdvisor.
func NewDeterministicAdvisor() *DeterministicAdvisor { return &DeterministicAdvisor{} }

// DecideOpen implements domain.Advisor.
func (a *DeterministicAdvisor) DecideOpen(_ context.Context, request domain.OpenDecisionRequest) domain.OpenDecision {
	targets := make([]domain.TargetDecision, len(request.Candidates))
	for i, candidate := range request.Candidates {
		targets[i] = deterministicTarget(candidate, request.DefaultEmoji)
	}
	return domain.OpenDecision{Targets: targets}
}

// deterministicTarget is the per-channel decision today's behavior maps to.
// The clamp stage reuses it to repair an invalid model decision per channel.
func deterministicTarget(candidate domain.CandidateTarget, defaultEmoji string) domain.TargetDecision {
	return domain.TargetDecision{
		Channel:      candidate.Channel,
		Loudness:     domain.LoudnessPing,
		Mentions:     candidate.Mentions,
		LeadingEmoji: defaultEmoji,
		Format:       domain.FormatStandard,
		Emphasis:     domain.EmphasisNone,
	}
}

// DecideUpdated implements domain.Advisor.
func (a *DeterministicAdvisor) DecideUpdated(_ context.Context, request domain.UpdatedDecisionRequest) domain.UpdatedDecision {
	return domain.UpdatedDecision{Emoji: request.DefaultEmoji}
}

// DecideDigest implements domain.Advisor.
func (a *DeterministicAdvisor) DecideDigest(_ context.Context, request domain.DigestDecisionRequest) domain.DigestDecision {
	order := make([]int, len(request.PRs))
	highlights := make([]domain.Highlight, len(request.PRs))
	notes := make([]string, len(request.PRs))
	for i := range request.PRs {
		order[i] = i
		highlights[i] = domain.HighlightNormal
	}
	return domain.DigestDecision{
		Order:          order,
		Highlights:     highlights,
		Notes:          notes,
		ParentLoudness: domain.LoudnessPing,
	}
}

var _ domain.Advisor = (*DeterministicAdvisor)(nil)
