package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// ScheduledSender posts the scheduled strategy's list messages.
type ScheduledSender struct {
	queue    domain.ScheduledQueue
	behavior domain.RepoBehavior
	poster   domain.ScheduledPoster
	logger   *slog.Logger
	now      func() time.Time
}

// NewScheduledSender builds a ScheduledSender.
func NewScheduledSender(params domain.ScheduledSenderParams) *ScheduledSender {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	return &ScheduledSender{
		queue:    params.Queue,
		behavior: params.Behavior,
		poster:   params.Poster,
		logger:   params.Logger,
		now:      now,
	}
}

// SendSchedule posts one list per channel. Each entry's repo is checked
// against its current config, so a schedule change applies to already queued
// PRs too.
func (s *ScheduledSender) SendSchedule(ctx context.Context, spec string) error {
	entries, err := s.queue.Pending(ctx)
	if err != nil {
		return fmt.Errorf("notification: pending scheduled notifications: %w", err)
	}
	groups, err := s.groupByChannel(ctx, entries, spec)
	if err != nil {
		return err
	}
	for _, group := range groups {
		s.send(ctx, spec, group)
	}
	return nil
}

func (s *ScheduledSender) send(ctx context.Context, spec string, group scheduledGroup) {
	request := domain.ScheduledListRequest{Mentions: group.mentions, Entries: group.entries}
	if err := s.poster.PostScheduledList(ctx, group.channel, request); err != nil {
		s.logger.Error("scheduled notification post failed",
			slog.String("schedule", spec),
			slog.String("channel", group.channel),
			slog.Int("prs", len(group.entries)),
			slog.Any("err", err))
		return
	}
	ids := make([]uint, 0, len(group.entries))
	for _, entry := range group.entries {
		ids = append(ids, entry.ID)
	}
	if err := s.queue.MarkSent(ctx, ids, s.now()); err != nil {
		s.logger.Error("scheduled notification mark sent failed",
			slog.String("schedule", spec),
			slog.String("channel", group.channel),
			slog.Any("err", err))
		return
	}
	s.logger.Info("scheduled notifications posted",
		slog.String("schedule", spec),
		slog.String("channel", group.channel),
		slog.Int("prs", len(group.entries)))
}

type scheduledGroup struct {
	channel  string
	mentions []string
	entries  []domain.ScheduledEntry
}

// groupByChannel keeps the entries whose repo sends on spec and buckets them
// by channel in first-seen order, with each channel's mentions unioned.
func (s *ScheduledSender) groupByChannel(ctx context.Context, entries []domain.ScheduledEntry, spec string) ([]scheduledGroup, error) {
	sendsOnSpec := map[string]bool{}
	var order []string
	byChannel := map[string]*scheduledGroup{}
	for _, entry := range entries {
		included, known := sendsOnSpec[entry.Repository]
		if !known {
			var err error
			included, err = s.repositorySendsOn(ctx, entry.Repository, spec)
			if err != nil {
				return nil, err
			}
			sendsOnSpec[entry.Repository] = included
		}
		if !included {
			continue
		}
		group := byChannel[entry.Channel]
		if group == nil {
			group = &scheduledGroup{channel: entry.Channel}
			byChannel[entry.Channel] = group
			order = append(order, entry.Channel)
		}
		for _, mention := range entry.Mentions {
			if !slices.Contains(group.mentions, mention) {
				group.mentions = append(group.mentions, mention)
			}
		}
		group.entries = append(group.entries, entry)
	}

	groups := make([]scheduledGroup, 0, len(order))
	for _, channel := range order {
		groups = append(groups, *byChannel[channel])
	}
	return groups, nil
}

func (s *ScheduledSender) repositorySendsOn(ctx context.Context, repository, spec string) (bool, error) {
	mapping, err := s.behavior.Get(ctx, repository)
	if errors.Is(err, routingdomain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("notification: scheduled behavior for %s: %w", repository, err)
	}
	return slices.Contains(mapping.Notifications.Specs(), spec), nil
}

var _ domain.ScheduledSender = (*ScheduledSender)(nil)
