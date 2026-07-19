package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// OpenHandler reacts to a PR being opened (non-draft) or marked
// ready_for_review. It resolves the fan-out targets, consults the salience
// advisor for the per-channel presentation, and posts one notification per
// decided target, recording each for later updates. The dependency-bot
// compact policy is rule-sufficient and short-circuits the advisor.
type OpenHandler struct {
	store     domain.MessageStore
	resolver  domain.TargetResolver
	messenger domain.Messenger
	advisor   saliencedomain.Advisor
	logger    *slog.Logger
}

// NewOpenHandler builds an OpenHandler from its params.
func NewOpenHandler(params domain.OpenHandlerParams) *OpenHandler {
	return &OpenHandler{
		store:     params.Store,
		resolver:  params.Resolver,
		messenger: params.Messenger,
		advisor:   params.Advisor,
		logger:    params.Logger,
	}
}

// Applicable returns true for a freshly opened or ready-for-review PR. The
// inbound adapter does the draft gating (a draft open never yields KindOpened),
// so no handler branches on PR.Draft.
func (h *OpenHandler) Applicable(event kernel.Event) bool {
	return event.Kind == kernel.KindOpened || event.Kind == kernel.KindReadyForReview
}

// Handle posts one notification per decided target channel and records each.
// It is idempotent per channel: an existing message for a channel is skipped,
// so a redelivery or a partial-failure retry only posts the missing channels.
func (h *OpenHandler) Handle(ctx context.Context, event kernel.Event) error {
	resolved, err := h.resolver.ResolveTargets(ctx, event.Repository, event.PR.Number)
	if errors.Is(err, routingdomain.ErrNotFound) {
		h.logIgnored(event, domain.ReasonNoMapping)
		return nil
	}
	if err != nil {
		return err
	}

	existing, err := h.store.Messages(ctx, event.Repository, event.PR.Number)
	if err != nil && !errors.Is(err, routingdomain.ErrNotFound) {
		return err
	}
	already := map[string]bool{}
	for _, message := range existing {
		already[message.Channel] = true
	}

	if bot := h.botFormat(event, resolved.Mapping); bot != nil {
		return h.postBotFormat(ctx, event, resolved, already, bot)
	}
	decision := h.advisor.DecideOpen(ctx, openDecisionRequest(event, resolved))
	return h.postDecision(ctx, event, decision, already)
}

// botFormat returns the compact dependency-bot template inputs when the repo
// enables the format and the PR author is a known bot; nil otherwise.
// Detection keys off the PR author, not the webhook sender: on a
// ready_for_review event the sender is the human who marked a bot's draft
// ready, while the author stays the bot. The policy is rule-sufficient, so it
// deliberately short-circuits the advisor — policy outranks AI.
func (h *OpenHandler) botFormat(event kernel.Event, mapping routingdomain.RepoMapping) *domain.BotFormat {
	if !mapping.DependabotFormat {
		return nil
	}
	kind := DetectBot(event.PR.Author)
	if kind == domain.BotKindNone {
		return nil
	}
	return &domain.BotFormat{Name: kind.Name(), Security: IsSecurityAdvisory(event.PR.Body)}
}

// postBotFormat posts the compact dependency-bot notification to every
// resolved target, exactly as before the salience layer.
func (h *OpenHandler) postBotFormat(ctx context.Context, event kernel.Event, resolved routingdomain.ResolvedTargets, already map[string]bool, bot *domain.BotFormat) error {
	for _, target := range resolved.Targets {
		if already[target.Channel] {
			continue
		}
		request := domain.OpenRequest{
			Repository: event.Repository,
			PR:         event.PR,
			Mentions:   target.Mentions,
			NewPREmoji: resolved.Mapping.Reactions.NewPR,
			Bot:        bot,
		}
		messageID, err := h.messenger.PostOpen(ctx, target.Channel, request)
		if err != nil {
			return err // successful channels are already saved; retry skips them
		}
		if err := h.store.AddMessage(ctx, event.Repository, event.PR.Number, target.Channel, messageID); err != nil {
			return err
		}
	}
	return nil
}

// postDecision posts one notification per decided target and records each.
func (h *OpenHandler) postDecision(ctx context.Context, event kernel.Event, decision saliencedomain.OpenDecision, already map[string]bool) error {
	for _, target := range decision.Targets {
		if already[target.Channel] {
			continue
		}
		mentions := target.Mentions
		if target.Loudness == saliencedomain.LoudnessQuiet {
			mentions = nil
		}
		request := domain.OpenRequest{
			Repository:   event.Repository,
			PR:           event.PR,
			Mentions:     mentions,
			NewPREmoji:   target.LeadingEmoji,
			Compact:      target.Format == saliencedomain.FormatCompact,
			Breaking:     target.Emphasis == saliencedomain.EmphasisBreaking,
			ContextBlock: target.ContextBlock,
		}
		messageID, err := h.messenger.PostOpen(ctx, target.Channel, request)
		if err != nil {
			return err // successful channels are already saved; retry skips them
		}
		if err := h.store.AddMessage(ctx, event.Repository, event.PR.Number, target.Channel, messageID); err != nil {
			return err
		}
	}
	return nil
}

func (h *OpenHandler) logIgnored(event kernel.Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason),
		slog.String("handler", "open"),
		slog.String("provider", event.Provider.String()),
		slog.String("kind", event.Kind.String()),
		slog.String("repository", event.Repository),
		slog.Int("pr", event.PR.Number),
	)
}

var _ domain.Handler = (*OpenHandler)(nil)
