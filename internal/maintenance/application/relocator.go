package application

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

// Relocator is the message relocate use case; see domain.Relocator.
type Relocator struct {
	lister     domain.TrackedLister
	rows       domain.MessageRows
	courier    domain.MessageCourier
	reactions  domain.ReactionPolicy
	channels   domain.ChannelConfig
	logger     *slog.Logger
	from       string
	to         string
	repository string
	dryRun     bool
}

// NewRelocator constructs the relocate use case from its domain params.
func NewRelocator(params domain.RelocatorParams) *Relocator {
	return &Relocator{
		lister:     params.Lister,
		rows:       params.Rows,
		courier:    params.Courier,
		reactions:  params.Reactions,
		channels:   params.Channels,
		logger:     params.Logger,
		from:       params.From,
		to:         params.To,
		repository: params.Repository,
		dryRun:     params.DryRun,
	}
}

// Run implements domain.Relocator.
func (r *Relocator) Run(ctx context.Context) (domain.RelocateSummary, error) {
	prs, err := r.lister.ListOpenWithMessages(ctx)
	if err != nil {
		return domain.RelocateSummary{}, err
	}

	var summary domain.RelocateSummary
	for _, pullRequest := range prs {
		if r.repository != "" && pullRequest.Repository != r.repository {
			continue
		}
		source, found := messageIn(pullRequest, r.from)
		if !found {
			continue
		}
		summary.Scanned++
		r.relocate(ctx, pullRequest, source, &summary)
	}
	return summary, nil
}

// Audit implements domain.Relocator.
func (r *Relocator) Audit(ctx context.Context) ([]domain.StaleMessage, error) {
	prs, err := r.lister.ListOpenWithMessages(ctx)
	if err != nil {
		return nil, err
	}

	var stale []domain.StaleMessage
	for _, pullRequest := range prs {
		configured := r.channels.ConfiguredChannels(pullRequest.Repository)
		for _, message := range pullRequest.Messages {
			if slices.Contains(configured, message.Channel) {
				continue
			}
			stale = append(stale, domain.StaleMessage{
				Repository: pullRequest.Repository,
				PRNumber:   pullRequest.PRNumber,
				Channel:    message.Channel,
			})
		}
	}
	return stale, nil
}

// relocate resolves one PR's source message: forgotten when there is no
// destination, merged when the destination already holds a message for this PR,
// moved otherwise.
func (r *Relocator) relocate(ctx context.Context, pullRequest domain.TrackedPR, source domain.TrackedMessage, summary *domain.RelocateSummary) {
	if r.to == "" {
		r.forget(ctx, pullRequest, source, summary, "relocate: row dropped; the message is left where it is")
		return
	}
	if _, exists := messageIn(pullRequest, r.to); exists {
		r.merge(ctx, pullRequest, source, summary)
		return
	}
	r.move(ctx, pullRequest, source, summary)
}

// move reposts the message in the destination, retargets the row, carries the
// reactions over and leaves the original behind as a pointer. The row is
// retargeted immediately after the repost so a crash can only ever leave an
// unmarked message in the source channel — never a row pointing at a message
// that was never posted.
func (r *Relocator) move(ctx context.Context, pullRequest domain.TrackedPR, source domain.TrackedMessage, summary *domain.RelocateSummary) {
	if r.dryRun {
		summary.Moved++
		r.log("relocate: would move message (dry-run)", pullRequest, source)
		return
	}

	messageID, err := r.courier.Repost(ctx, source, r.to)
	if errors.Is(err, domain.ErrMessageGone) {
		if r.forgetRow(ctx, pullRequest, source, summary) {
			summary.Forgotten++
			r.log("relocate: original message is gone; dropping its row", pullRequest, source)
		}
		return
	}
	if err != nil {
		summary.Errors++
		r.logFailure("relocate: repost failed; leaving message in place", pullRequest, source, err)
		return
	}

	if err := r.rows.MoveMessage(ctx, pullRequest.Repository, pullRequest.PRNumber, r.from, r.to, messageID); err != nil {
		summary.Errors++
		r.logFailure("relocate: retargeting the row failed; the original is left in place", pullRequest, source, err)
		return
	}

	posted := domain.TrackedMessage{Channel: r.to, MessageID: messageID}
	r.carryReactions(ctx, pullRequest, source, posted)
	if err := r.courier.MarkMoved(ctx, source, r.to); err != nil {
		summary.Errors++
		r.logFailure("relocate: message moved but the original could not be marked", pullRequest, source, err)
		return
	}
	summary.Moved++
	r.log("relocate: message moved", pullRequest, source)
}

// merge marks the source message as moved for a PR the destination already
// holds, then forgets its row. Nothing is reposted — the destination has the
// PR already.
func (r *Relocator) merge(ctx context.Context, pullRequest domain.TrackedPR, source domain.TrackedMessage, summary *domain.RelocateSummary) {
	if r.dryRun {
		summary.Merged++
		r.log("relocate: would point the source message at the destination, which already has this PR (dry-run)", pullRequest, source)
		return
	}
	if err := r.courier.MarkMoved(ctx, source, r.to); err != nil {
		summary.Errors++
		r.logFailure("relocate: marking the source message failed", pullRequest, source, err)
		return
	}
	if !r.forgetRow(ctx, pullRequest, source, summary) {
		return
	}
	summary.Merged++
	r.log("relocate: source message pointed at the destination, which already had this PR", pullRequest, source)
}

// forget drops the row and leaves the message alone: with no destination there
// is nothing to point at, and the message itself is never deleted.
func (r *Relocator) forget(ctx context.Context, pullRequest domain.TrackedPR, source domain.TrackedMessage, summary *domain.RelocateSummary, message string) {
	if r.dryRun {
		summary.Forgotten++
		r.log("relocate: would drop the row and leave the message in place (dry-run)", pullRequest, source)
		return
	}
	if !r.forgetRow(ctx, pullRequest, source, summary) {
		return
	}
	summary.Forgotten++
	r.log(message, pullRequest, source)
}

// forgetRow removes the stored row, reporting whether it succeeded.
func (r *Relocator) forgetRow(ctx context.Context, pullRequest domain.TrackedPR, source domain.TrackedMessage, summary *domain.RelocateSummary) bool {
	if err := r.rows.RemoveMessage(ctx, pullRequest.Repository, pullRequest.PRNumber, source.Channel); err != nil {
		summary.Errors++
		r.logFailure("relocate: removing the row failed", pullRequest, source, err)
		return false
	}
	return true
}

// carryReactions re-adds the source message's reactions on the new message.
// A failure is logged and tolerated: the move is already recorded, and a
// reaction is decoration rather than state the notifier reads back.
func (r *Relocator) carryReactions(ctx context.Context, pullRequest domain.TrackedPR, source, posted domain.TrackedMessage) {
	allowed, err := r.reactions.AllowedReactions(ctx, pullRequest.Repository)
	if err != nil {
		r.logFailure("relocate: resolving the repo's reactions failed; the moved message carries none", pullRequest, source, err)
		return
	}
	if err := r.courier.CopyReactions(ctx, source, posted, allowed); err != nil {
		r.logFailure("relocate: carrying reactions over failed", pullRequest, source, err)
	}
}

// messageIn returns the PR's message in channel, if it has one.
func messageIn(pullRequest domain.TrackedPR, channel string) (domain.TrackedMessage, bool) {
	for _, message := range pullRequest.Messages {
		if message.Channel == channel {
			return message, true
		}
	}
	return domain.TrackedMessage{}, false
}

func (r *Relocator) log(message string, pullRequest domain.TrackedPR, source domain.TrackedMessage) {
	r.logger.Info(message,
		slog.String("repository", pullRequest.Repository),
		slog.Int("pr", pullRequest.PRNumber),
		slog.String("from", source.Channel),
		slog.String("to", r.to))
}

func (r *Relocator) logFailure(message string, pullRequest domain.TrackedPR, source domain.TrackedMessage, err error) {
	r.logger.Error(message,
		slog.String("repository", pullRequest.Repository),
		slog.Int("pr", pullRequest.PRNumber),
		slog.String("from", source.Channel),
		slog.String("to", r.to),
		slog.Any("err", err))
}

var _ domain.Relocator = (*Relocator)(nil)
