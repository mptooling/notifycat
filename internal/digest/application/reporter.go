package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/kernel"
)

// Reporter builds and posts the stuck-PR digest for every channel that owns at
// least one stuck PR. It is the application's DigestReporter and, via
// ReportSchedule, its ScheduleJob.
type Reporter struct {
	finder   domain.StuckFinder
	targets  domain.DigestTargets
	poster   domain.DigestPoster
	composer domain.DigestComposer
	digests  domain.DigestResolver
	now      func() time.Time
	tz       *time.Location
	provider kernel.Provider
	logger   *slog.Logger
}

// NewReporter constructs a Reporter from its params. Now defaults to time.Now
// and TZ to UTC when unset.
func NewReporter(params domain.ReporterParams) *Reporter {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	tz := params.TZ
	if tz == nil {
		tz = time.UTC
	}
	return &Reporter{
		finder:   params.Finder,
		targets:  params.Targets,
		poster:   params.Poster,
		composer: params.Composer,
		digests:  params.Digests,
		now:      now,
		tz:       tz,
		provider: params.Provider,
		logger:   params.Logger,
	}
}

// Report runs one digest pass including all enabled repos: find open PRs idle
// since the start of today, group them by channel, and post one reminder per
// channel. A failed post for one channel is logged and skipped so the others
// still go out.
func (r *Reporter) Report(ctx context.Context) error {
	return r.report(ctx, func(repo string) bool {
		return r.digests.DigestFor(repo).Enabled
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
// since the start of today, group them by channel (including only rows where
// include returns true), and post one reminder per channel. A failed post for
// one channel is logged and skipped so the others still go out.
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

	for _, group := range r.groupByChannel(prs, now, include) {
		ts, err := r.poster.PostMessage(ctx, group.channel, r.composer.StuckDigestParent(group.mentions, len(group.prs)))
		if err != nil {
			r.logger.Error("stuck-pr digest: parent post failed",
				slog.String("channel", group.channel),
				slog.Int("prs", len(group.prs)),
				slog.Any("err", err))
			continue
		}
		if _, err := r.poster.PostReply(ctx, group.channel, ts, r.composer.StuckDigestList(group.prs)); err != nil {
			r.logger.Error("stuck-pr digest: list reply failed",
				slog.String("channel", group.channel),
				slog.Int("prs", len(group.prs)),
				slog.Any("err", err))
			continue
		}
		r.logger.Info("stuck-pr digest posted",
			slog.String("channel", group.channel),
			slog.Int("prs", len(group.prs)))
	}
	return nil
}

type channelGroup struct {
	channel  string
	mentions []string
	prs      []domain.StuckPR
}

// groupByChannel buckets stuck PRs by the channels their repository is
// configured to post to, preserving first-seen channel order for stable output.
// Each channel carries its own configured mentions, so a `channels:` fan-out
// pings every channel's own group. PRs whose repo resolves to no channel, or for
// which include returns false, are skipped.
//
// Destinations come from config, never from the PR's stored messages: after an
// operator repoints a repo at a new channel the reminder has to follow the
// config, not the channel the original message happens to live in.
func (r *Reporter) groupByChannel(prs []domain.PullRequest, now time.Time, include func(repo string) bool) []channelGroup {
	var order []string
	byChannel := map[string]*channelGroup{}
	mentionSeen := map[string]map[string]bool{}

	for _, pullRequest := range prs {
		if !include(pullRequest.Repository) {
			r.logger.Debug("stuck-pr digest: skipping repo by schedule filter",
				slog.String("repository", pullRequest.Repository),
				slog.Int("pr", pullRequest.PRNumber))
			continue
		}
		for _, target := range r.targets.BaseTargets(pullRequest.Repository) {
			// An unmapped repo resolves to one target with an empty channel.
			if target.Channel == "" {
				continue
			}
			group := byChannel[target.Channel]
			if group == nil {
				group = &channelGroup{channel: target.Channel}
				byChannel[target.Channel] = group
				mentionSeen[target.Channel] = map[string]bool{}
				order = append(order, target.Channel)
			}
			for _, mention := range target.Mentions {
				if !mentionSeen[target.Channel][mention] {
					mentionSeen[target.Channel][mention] = true
					group.mentions = append(group.mentions, mention)
				}
			}
			group.prs = append(group.prs, domain.StuckPR{
				Repository: pullRequest.Repository,
				Number:     pullRequest.PRNumber,
				URL:        r.provider.PullRequestWebURL(pullRequest.Repository, pullRequest.PRNumber),
				IdleDays:   idleDays(now, pullRequest.UpdatedAt),
			})
		}
	}

	out := make([]channelGroup, 0, len(order))
	for _, channel := range order {
		out = append(out, *byChannel[channel])
	}
	return out
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

var (
	_ domain.DigestReporter = (*Reporter)(nil)
	_ domain.ScheduleJob    = (*Reporter)(nil)
)
