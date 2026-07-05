package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// OpenHandler reacts to a PR being opened (non-draft) or marked
// ready_for_review. It fans out one notification per resolved target channel and
// records each for later updates.
type OpenHandler struct {
	store     domain.MessageStore
	resolver  domain.TargetResolver
	messenger domain.Messenger
	logger    *slog.Logger
}

// NewOpenHandler builds an OpenHandler.
func NewOpenHandler(store domain.MessageStore, resolver domain.TargetResolver, messenger domain.Messenger, logger *slog.Logger) *OpenHandler {
	return &OpenHandler{store: store, resolver: resolver, messenger: messenger, logger: logger}
}

// Applicable returns true for a freshly opened or ready-for-review PR. The
// inbound adapter does the draft gating (a draft open never yields KindOpened),
// so no handler branches on PR.Draft.
func (h *OpenHandler) Applicable(event kernel.Event) bool {
	return event.Kind == kernel.KindOpened || event.Kind == kernel.KindReadyForReview
}

// Handle posts one notification per resolved target channel and records each. It
// is idempotent per channel: an existing message for a channel is skipped, so a
// redelivery or a partial-failure retry only posts the missing channels.
func (h *OpenHandler) Handle(ctx context.Context, event kernel.Event) error {
	behavior, targets, err := h.resolver.ResolveTargets(ctx, event.Repository, event.PR.Number)
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

	for _, target := range targets {
		if already[target.Channel] {
			continue
		}
		messageID, err := h.messenger.PostOpen(ctx, target.Channel, h.openRequest(event, behavior, target.Mentions))
		if err != nil {
			return err // successful channels are already saved; retry skips them
		}
		if err := h.store.AddMessage(ctx, event.Repository, event.PR.Number, target.Channel, messageID); err != nil {
			return err
		}
	}
	return nil
}

// openRequest builds the post intent for one channel, deciding the compact
// dependency-bot template when the repo enables it and the PR author is a known
// bot. Detection keys off the PR author, not the webhook sender: on a
// ready_for_review event the sender is the human who marked a bot's draft ready,
// while the author stays the bot.
func (h *OpenHandler) openRequest(event kernel.Event, behavior routingdomain.RepoMapping, mentions []string) domain.OpenRequest {
	request := domain.OpenRequest{
		Repository: event.Repository,
		PR:         event.PR,
		Mentions:   mentions,
		NewPREmoji: behavior.Reactions.NewPR,
	}
	if behavior.DependabotFormat {
		if kind := DetectBot(event.PR.Author); kind != domain.BotKindNone {
			request.Bot = &domain.BotFormat{Name: kind.Name(), Security: IsSecurityAdvisory(event.PR.Body)}
		}
	}
	return request
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
