// Package digest posts the scheduled stuck-PR reminder: a per-channel summary
// of open PRs that nobody has touched since before today. It rides the same
// slack_messages rows and repo→channel mappings the webhook flow already uses,
// adding only a cron-driven read pass — no new persistence.
package digest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// StuckFinder returns open PRs (with their messages) idle since before cutoff.
type StuckFinder interface {
	FindStuck(ctx context.Context, cutoff time.Time) ([]store.PullRequest, error)
}

// MappingLookup resolves a repository to its Slack channel and mentions.
type MappingLookup interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// Resolver looks up the effective digest configuration for a repository.
type Resolver interface {
	DigestFor(repository string) mappings.DigestConfig
}

// Poster posts a composed message to a Slack channel, either as a top-level
// post (PostMessage, returning its ts) or as a reply threaded under one
// (PostReply).
type Poster interface {
	PostMessage(ctx context.Context, channel string, msg slack.Message) (string, error)
	PostReply(ctx context.Context, channel, threadTS string, msg slack.Message) (string, error)
}

// Reporter builds and posts the stuck-PR digest for every channel that owns at
// least one stuck PR.
type Reporter struct {
	finder   StuckFinder
	mappings MappingLookup
	digests  Resolver
	slack    Poster
	composer *slack.Composer
	now      func() time.Time
	tz       *time.Location
	logger   *slog.Logger
}

// NewReporter constructs a Reporter. now defaults to time.Now; tz is the
// timezone the "start of day" cutoff is computed in (nil defaults to UTC) and
// must match the scheduler's timezone so a digest fires and cuts off in the
// same zone.
func NewReporter(finder StuckFinder, mappings MappingLookup, poster Poster, composer *slack.Composer, digests Resolver, logger *slog.Logger, tz *time.Location) *Reporter {
	if tz == nil {
		tz = time.UTC
	}
	return &Reporter{
		finder:   finder,
		mappings: mappings,
		slack:    poster,
		composer: composer,
		digests:  digests,
		now:      time.Now,
		tz:       tz,
		logger:   logger,
	}
}

// Report runs one digest pass including all enabled repos: find open PRs idle
// since the start of today, group them by Slack channel, and post one reminder
// per channel. A failed post for one channel is logged and skipped so the
// others still go out.
func (r *Reporter) Report(ctx context.Context) error {
	return r.report(ctx, func(repo string) bool {
		d := r.digests.DigestFor(repo)
		return d.Enabled
	})
}

// ReportSchedule runs one digest pass for a single cron spec: it includes only
// stuck PRs whose repo's effective digest is enabled and scheduled at spec.
func (r *Reporter) ReportSchedule(ctx context.Context, spec string) error {
	return r.report(ctx, func(repo string) bool {
		d := r.digests.DigestFor(repo)
		return d.Enabled && d.Schedule == spec
	})
}

// report runs one digest pass with a custom inclusion filter: find open PRs idle
// since the start of today, group them by Slack channel (including only rows
// where include returns true), and post one reminder per channel. A failed
// post for one channel is logged and skipped so the others still go out.
func (r *Reporter) report(ctx context.Context, include func(repo string) bool) error {
	// Evaluate in the configured zone so the firing time and the cutoff agree;
	// r.tz — not the clock's own location — drives the day boundary.
	now := r.now().In(r.tz)
	cutoff := startOfDay(now)

	prs, err := r.finder.FindStuck(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("digest: find stuck: %w", err)
	}
	if len(prs) == 0 {
		r.logger.Debug("stuck-pr digest: nothing to report")
		return nil
	}

	for _, g := range r.groupByChannel(ctx, prs, now, include) {
		ts, err := r.slack.PostMessage(ctx, g.channel, r.composer.StuckDigestParent(g.mentions, len(g.prs)))
		if err != nil {
			r.logger.Error("stuck-pr digest: parent post failed",
				slog.String("channel", g.channel),
				slog.Int("prs", len(g.prs)),
				slog.Any("err", err))
			continue
		}
		if _, err := r.slack.PostReply(ctx, g.channel, ts, r.composer.StuckDigestList(g.prs)); err != nil {
			r.logger.Error("stuck-pr digest: list reply failed",
				slog.String("channel", g.channel),
				slog.Int("prs", len(g.prs)),
				slog.Any("err", err))
			continue
		}
		r.logger.Info("stuck-pr digest posted",
			slog.String("channel", g.channel),
			slog.Int("prs", len(g.prs)))
	}
	return nil
}

type channelGroup struct {
	channel  string
	mentions []string
	prs      []slack.StuckPR
}

// groupByChannel buckets stuck PRs by their stored message channels, preserving
// first-seen channel order for stable output. Base mentions are added to a
// channel group only when that channel equals the repo's base SlackChannel;
// messages living in path (fan-out) channels get no @-ping. PRs whose repo has
// no mapping, or for which include returns false, are skipped.
func (r *Reporter) groupByChannel(ctx context.Context, prs []store.PullRequest, now time.Time, include func(repo string) bool) []channelGroup {
	var order []string
	byChannel := map[string]*channelGroup{}
	mentionSeen := map[string]map[string]bool{}

	for _, pr := range prs {
		if !include(pr.Repository) {
			continue
		}
		mapping, err := r.mappings.Get(ctx, pr.Repository)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			r.logger.Error("stuck-pr digest: mapping lookup failed",
				slog.String("repository", pr.Repository), slog.Any("err", err))
			continue
		}
		for _, m := range pr.Messages {
			g := byChannel[m.Channel]
			if g == nil {
				g = &channelGroup{channel: m.Channel}
				byChannel[m.Channel] = g
				mentionSeen[m.Channel] = map[string]bool{}
				order = append(order, m.Channel)
			}
			// Base mentions only when the stored channel is the repo's base channel.
			if m.Channel == mapping.SlackChannel {
				for _, mention := range mapping.Mentions {
					if !mentionSeen[m.Channel][mention] {
						mentionSeen[m.Channel][mention] = true
						g.mentions = append(g.mentions, mention)
					}
				}
			}
			g.prs = append(g.prs, slack.StuckPR{
				Repository: pr.Repository,
				Number:     pr.PRNumber,
				URL:        prURL(pr.Repository, pr.PRNumber),
				IdleDays:   idleDays(now, pr.UpdatedAt),
			})
		}
	}

	out := make([]channelGroup, 0, len(order))
	for _, ch := range order {
		out = append(out, *byChannel[ch])
	}
	return out
}

// prURL builds the github.com web URL for a PR. The store keeps no URL, so it
// is reconstructed from repo + number; this assumes github.com (GitHub
// Enterprise hosts are not handled here).
func prURL(repository string, number int) string {
	return "https://github.com/" + repository + "/pull/" + strconv.Itoa(number)
}

// startOfDay returns local midnight for t's own location.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// idleDays counts whole calendar days between updatedAt and now, evaluated in
// now's location so a row stored in UTC and a local "now" agree near midnight.
// Rounded to absorb DST drift; floored at 1 (FindStuck only yields rows from
// before today).
func idleDays(now, updatedAt time.Time) int {
	loc := now.Location()
	today := startOfDay(now.In(loc))
	day := startOfDay(updatedAt.In(loc))
	days := int(today.Sub(day).Round(24*time.Hour) / (24 * time.Hour))
	if days < 1 {
		return 1
	}
	return days
}
