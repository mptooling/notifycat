package application

import "github.com/mptooling/notifycat/internal/salience/domain"

// clampOpen repairs a model open decision field by field against the request.
// Unknown or duplicate channels are dropped; an empty or fully-invalid target
// list falls back to every candidate deterministically — salience can never
// drop a PR. Any repair reports violated=true (logged as clamp_violation)
// while surviving valid fields keep the model's choice.
func clampOpen(decision domain.OpenDecision, request domain.OpenDecisionRequest) (domain.OpenDecision, bool) {
	candidatesByChannel := make(map[string]domain.CandidateTarget, len(request.Candidates))
	for _, candidate := range request.Candidates {
		candidatesByChannel[candidate.Channel] = candidate
	}
	violated := false
	clampedTargets := make([]domain.TargetDecision, 0, len(decision.Targets))
	seen := map[string]bool{}
	for _, target := range decision.Targets {
		candidate, known := candidatesByChannel[target.Channel]
		if !known || seen[target.Channel] {
			violated = true
			continue
		}
		seen[target.Channel] = true
		clampedTarget, targetViolated := clampTarget(target, candidate, request)
		if targetViolated {
			violated = true
		}
		clampedTargets = append(clampedTargets, clampedTarget)
	}
	if len(clampedTargets) == 0 {
		for _, candidate := range request.Candidates {
			clampedTargets = append(clampedTargets, deterministicTarget(candidate, request.DefaultEmoji))
		}
		decision.Targets = clampedTargets
		return decision, true
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
	clamped.ThreadNote = sanitizeLine(target.ThreadNote, domain.MaxThreadNoteChars)
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
