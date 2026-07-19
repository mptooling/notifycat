package application

import (
	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// openDecisionRequest maps a resolved open event to the advisor's request:
// candidates mirror the resolved targets, the default emoji is the repo's
// new-PR reaction, and the allowlist is the configured reaction set plus the
// curated extras. Signals are computed inside the advisor, not here.
func openDecisionRequest(event kernel.Event, resolved routingdomain.ResolvedTargets) saliencedomain.OpenDecisionRequest {
	candidates := make([]saliencedomain.CandidateTarget, len(resolved.Targets))
	for i, target := range resolved.Targets {
		candidates[i] = saliencedomain.CandidateTarget{Channel: target.Channel, Mentions: target.Mentions}
	}
	return saliencedomain.OpenDecisionRequest{
		Repository:     event.Repository,
		PR:             prSummary(event),
		ChangedFiles:   resolved.ChangedFiles,
		Candidates:     candidates,
		DefaultEmoji:   resolved.Mapping.Reactions.NewPR,
		EmojiAllowlist: emojiAllowlist(resolved.Mapping.Reactions),
		Instructions:   resolved.Mapping.AIInstructions,
		TierEnabled:    resolved.Mapping.AIEnabled,
	}
}

// updatedDecisionRequest maps a review/close event to the advisor's request.
// defaultEmoji is the configured emoji the event would use today.
func updatedDecisionRequest(event kernel.Event, behavior routingdomain.RepoMapping, defaultEmoji string) saliencedomain.UpdatedDecisionRequest {
	return saliencedomain.UpdatedDecisionRequest{
		Repository:     event.Repository,
		PR:             prSummary(event),
		Kind:           event.Kind.String(),
		SenderLogin:    event.Sender.Login,
		SenderIsBot:    event.Sender.IsBot,
		DefaultEmoji:   defaultEmoji,
		EmojiAllowlist: emojiAllowlist(behavior.Reactions),
		Instructions:   behavior.AIInstructions,
		TierEnabled:    behavior.AIEnabled,
	}
}

func prSummary(event kernel.Event) saliencedomain.PRSummary {
	return saliencedomain.PRSummary{
		Number:      event.PR.Number,
		Title:       event.PR.Title,
		Body:        event.PR.Body,
		Author:      event.PR.Author,
		AuthorIsBot: DetectBot(event.PR.Author) != domain.BotKindNone,
	}
}

// emojiAllowlist is every emoji the advisor may pick: the repo's configured
// reaction set plus the curated extras from the salience domain.
func emojiAllowlist(reactions routingdomain.Reactions) []string {
	configured := []string{
		reactions.NewPR, reactions.MergedPR, reactions.ClosedPR,
		reactions.Approved, reactions.Commented, reactions.RequestChange,
	}
	return append(configured, saliencedomain.CuratedEmojis...)
}
