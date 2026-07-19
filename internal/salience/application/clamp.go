package application

import "github.com/mptooling/notifycat/internal/salience/domain"

// clampOpen repairs a model open decision against the request. Every candidate
// channel is present in the result — the model tunes each channel's loudness,
// mentions, emoji, format, and emphasis, but never drops a channel (never-skip
// is structural; quiet, not omission, is the noise lever). Unknown or duplicate
// channels the model returns are ignored; a candidate the model omitted, and any
// invalid field, is repaired to its deterministic default. Any repair reports
// violated=true (logged as clamp_violation); surviving valid fields keep the
// model's choice.
func clampOpen(decision domain.OpenDecision, request domain.OpenDecisionRequest) (domain.OpenDecision, bool) {
	candidateChannels := make(map[string]bool, len(request.Candidates))
	candidatesByChannel := make(map[string]domain.CandidateTarget, len(request.Candidates))
	for _, candidate := range request.Candidates {
		candidateChannels[candidate.Channel] = true
		candidatesByChannel[candidate.Channel] = candidate
	}
	violated := false
	modelByChannel := make(map[string]domain.TargetDecision, len(decision.Targets))
	for _, target := range decision.Targets {
		_, alreadySeen := modelByChannel[target.Channel]
		if !candidateChannels[target.Channel] || alreadySeen {
			violated = true // unknown or duplicate channel — ignored
			continue
		}
		modelByChannel[target.Channel] = target
	}
	clampedTargets := make([]domain.TargetDecision, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		target, decided := modelByChannel[candidate.Channel]
		if !decided {
			// never skip a channel: the model omitted it, so post it deterministically
			clampedTargets = append(clampedTargets, deterministicTarget(candidate, request.DefaultEmoji))
			violated = true
			continue
		}
		clampedTarget, targetViolated := clampTarget(target, candidatesByChannel[candidate.Channel], request)
		if targetViolated {
			violated = true
		}
		clampedTargets = append(clampedTargets, clampedTarget)
	}
	decision.Targets = clampedTargets
	return decision, violated
}

// clampTarget repairs one per-channel decision. Each invalid field falls back
// to that channel's deterministic value independently.
func clampTarget(target domain.TargetDecision, candidate domain.CandidateTarget, request domain.OpenDecisionRequest) (domain.TargetDecision, bool) {
	violated := false
	clamped := deterministicTarget(candidate, request.DefaultEmoji)

	switch target.Loudness {
	case domain.LoudnessPing, domain.LoudnessQuiet:
		clamped.Loudness = target.Loudness
	default:
		violated = true
	}
	switch target.Format {
	case domain.FormatStandard, domain.FormatCompact:
		clamped.Format = target.Format
	default:
		violated = true
	}
	switch target.Emphasis {
	case domain.EmphasisNone, domain.EmphasisBreaking:
		clamped.Emphasis = target.Emphasis
	default:
		violated = true
	}
	if subset, ok := mentionSubset(target.Mentions, candidate.Mentions); ok {
		clamped.Mentions = subset
	} else {
		violated = true
	}
	switch {
	case target.LeadingEmoji == "":
		// keep the default silently — an omitted emoji is not a violation
	case emojiAllowed(target.LeadingEmoji, request.EmojiAllowlist):
		clamped.LeadingEmoji = target.LeadingEmoji
	default:
		violated = true
	}
	clamped.ContextBlock = sanitizeLine(target.ContextBlock, domain.MaxContextBlockChars)
	return clamped, violated
}

// mentionSubset returns the decided mentions when every one is configured for
// the channel (order preserved, duplicates dropped). An empty decided list is
// a valid subset (mention nobody).
func mentionSubset(decided, configured []string) ([]string, bool) {
	allowed := make(map[string]bool, len(configured))
	for _, mention := range configured {
		allowed[mention] = true
	}
	subset := make([]string, 0, len(decided))
	seen := map[string]bool{}
	for _, mention := range decided {
		if !allowed[mention] {
			return nil, false
		}
		if seen[mention] {
			continue
		}
		seen[mention] = true
		subset = append(subset, mention)
	}
	return subset, true
}

func emojiAllowed(emoji string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if emoji == allowed {
			return true
		}
	}
	return false
}

// clampUpdated repairs the updated decision: off-allowlist emoji falls back
// to the configured default; an empty emoji means "keep the default" and is
// not a violation.
func clampUpdated(decision domain.UpdatedDecision, request domain.UpdatedDecisionRequest) (domain.UpdatedDecision, bool) {
	if decision.Emoji == "" {
		decision.Emoji = request.DefaultEmoji
		return decision, false
	}
	if !emojiAllowed(decision.Emoji, request.EmojiAllowlist) {
		decision.Emoji = request.DefaultEmoji
		return decision, true
	}
	return decision, false
}

// clampDigest validates the decision over the decided prefix (the prompt caps
// at MaxDigestPRs) and pads the tail back in original order, undecorated. An
// invalid permutation or parallel-slice mismatch falls back to identity.
func clampDigest(decision domain.DigestDecision, request domain.DigestDecisionRequest) (domain.DigestDecision, bool) {
	total := len(request.PRs)
	decided := total
	if decided > domain.MaxDigestPRs {
		decided = domain.MaxDigestPRs
	}
	violated := false

	if !validPermutation(decision.Order, decided) || len(decision.Highlights) != decided || len(decision.Notes) != decided {
		violated = true
		decision.Order = identityOrder(decided)
		decision.Highlights = make([]domain.Highlight, decided)
		decision.Notes = make([]string, decided)
		for i := range decision.Highlights {
			decision.Highlights[i] = domain.HighlightNormal
		}
	}
	for i := range decision.Highlights {
		if decision.Highlights[i] != domain.HighlightNormal && decision.Highlights[i] != domain.HighlightAttention {
			decision.Highlights[i] = domain.HighlightNormal
			violated = true
		}
		decision.Notes[i] = sanitizeLine(decision.Notes[i], domain.MaxDigestNoteChars)
	}
	for index := decided; index < total; index++ {
		decision.Order = append(decision.Order, index)
		decision.Highlights = append(decision.Highlights, domain.HighlightNormal)
		decision.Notes = append(decision.Notes, "")
	}
	switch decision.ParentLoudness {
	case domain.LoudnessPing, domain.LoudnessQuiet:
	default:
		decision.ParentLoudness = domain.LoudnessPing
		violated = true
	}
	return decision, violated
}

func validPermutation(order []int, length int) bool {
	if len(order) != length {
		return false
	}
	seen := make([]bool, length)
	for _, index := range order {
		if index < 0 || index >= length || seen[index] {
			return false
		}
		seen[index] = true
	}
	return true
}

func identityOrder(length int) []int {
	order := make([]int, length)
	for i := range order {
		order[i] = i
	}
	return order
}
