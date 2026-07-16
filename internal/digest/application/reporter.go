package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// Reporter builds and posts the stuck-PR digest for every channel that owns at
// least one stuck PR. It is the application's DigestReporter and, via
// ReportSchedule, its ScheduleJob.
type Reporter struct {
	finder   domain.StuckFinder
	mappings domain.MappingLookup
	poster   domain.DigestPoster
	composer domain.DigestComposer
	digests  domain.DigestResolver
	advisor  saliencedomain.Advisor
	now      func() time.Time
	tz       *time.Location
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
		mappings: params.Mappings,
		poster:   params.Poster,
		composer: params.Composer,
		digests:  params.Digests,
		advisor:  params.Advisor,
		now:      now,
		tz:       tz,
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

	for _, group := range r.groupByChannel(ctx, prs, now, include) {
		decision := r.advisor.DecideDigest(ctx, digestDecisionRequest(group))
		decidedPRs := applyDigestDecision(group.prs, decision)
		mentions := group.mentions
		if decision.ParentLoudness == saliencedomain.LoudnessQuiet {
			mentions = nil
		}
		ts, err := r.poster.PostMessage(ctx, group.channel, r.composer.StuckDigestParent(mentions, len(decidedPRs)))
		if err != nil {
			r.logger.Error("stuck-pr digest: parent post failed",
				slog.String("channel", group.channel),
				slog.Int("prs", len(decidedPRs)),
				slog.Any("err", err))
			continue
		}
		if _, err := r.poster.PostReply(ctx, group.channel, ts, r.composer.StuckDigestList(decidedPRs)); err != nil {
			r.logger.Error("stuck-pr digest: list reply failed",
				slog.String("channel", group.channel),
				slog.Int("prs", len(decidedPRs)),
				slog.Any("err", err))
			continue
		}
		r.logger.Info("stuck-pr digest posted",
			slog.String("channel", group.channel),
			slog.Int("prs", len(decidedPRs)))
	}
	return nil
}

type channelGroup struct {
	channel  string
	mentions []string
	prs      []domain.StuckPR
}

// groupByChannel buckets stuck PRs by their stored message channels, preserving
// first-seen channel order for stable output. Base mentions are added to a
// channel group only when that channel equals the repo's base SlackChannel;
// messages living in path (fan-out) channels get no @-ping. PRs whose repo has
// no mapping, or for which include returns false, are skipped.
func (r *Reporter) groupByChannel(ctx context.Context, prs []domain.PullRequest, now time.Time, include func(repo string) bool) []channelGroup {
	var order []string
	byChannel := map[string]*channelGroup{}
	mentionSeen := map[string]map[string]bool{}

	for _, pr := range prs {
		if !include(pr.Repository) {
			r.logger.Debug("stuck-pr digest: skipping repo by schedule filter",
				slog.String("repository", pr.Repository),
				slog.Int("pr", pr.PRNumber))
			continue
		}
		mapping, err := r.mappings.Get(ctx, pr.Repository)
		if errors.Is(err, routingdomain.ErrNotFound) {
			continue
		}
		if err != nil {
			r.logger.Error("stuck-pr digest: mapping lookup failed",
				slog.String("repository", pr.Repository), slog.Any("err", err))
			continue
		}
		for _, message := range pr.Messages {
			group := byChannel[message.Channel]
			if group == nil {
				group = &channelGroup{channel: message.Channel}
				byChannel[message.Channel] = group
				mentionSeen[message.Channel] = map[string]bool{}
				order = append(order, message.Channel)
			}
			// Base mentions only when the stored channel is the repo's base channel.
			if message.Channel == mapping.SlackChannel {
				for _, mention := range mapping.Mentions {
					if !mentionSeen[message.Channel][mention] {
						mentionSeen[message.Channel][mention] = true
						group.mentions = append(group.mentions, mention)
					}
				}
			}
			group.prs = append(group.prs, domain.StuckPR{
				Repository: pr.Repository,
				Number:     pr.PRNumber,
				URL:        prURL(pr.Repository, pr.PRNumber),
				IdleDays:   idleDays(now, pr.UpdatedAt),
			})
		}
	}

	out := make([]channelGroup, 0, len(order))
	for _, channel := range order {
		out = append(out, *byChannel[channel])
	}
	return out
}

// prURL builds the github.com web URL for a PR. The store keeps no URL, so it is
// reconstructed from repo + number; this assumes github.com (GitHub Enterprise
// hosts are not handled here).
func prURL(repository string, number int) string {
	return domain.GitHubPRURLPrefix + repository + domain.PullPathSegment + strconv.Itoa(number)
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

// digestDecisionRequest maps one channel group to the advisor's request. The
// store keeps no PR titles, so summaries carry repo, number, and idle days
// only; operator instructions are filled by the advisor from global config
// (digest groups span repos, so per-tier guidance does not apply).
func digestDecisionRequest(group channelGroup) saliencedomain.DigestDecisionRequest {
	summaries := make([]saliencedomain.DigestPRSummary, len(group.prs))
	for i, pr := range group.prs {
		summaries[i] = saliencedomain.DigestPRSummary{Repository: pr.Repository, Number: pr.Number, IdleDays: pr.IdleDays}
	}
	return saliencedomain.DigestDecisionRequest{Channel: group.channel, PRs: summaries, Mentions: group.mentions}
}

// applyDigestDecision reorders the list per the decision and applies the
// per-PR decorations. The advisor contract guarantees Order is a permutation
// and the slices are parallel to the input; the guards keep a buggy advisor
// from panicking the cron — on any shape mismatch the input passes through
// untouched.
func applyDigestDecision(prs []domain.StuckPR, decision saliencedomain.DigestDecision) []domain.StuckPR {
	if len(decision.Order) != len(prs) || len(decision.Highlights) != len(prs) || len(decision.Notes) != len(prs) {
		return prs
	}
	out := make([]domain.StuckPR, 0, len(prs))
	for _, index := range decision.Order {
		if index < 0 || index >= len(prs) {
			return prs
		}
		pr := prs[index]
		pr.Attention = decision.Highlights[index] == saliencedomain.HighlightAttention
		pr.Note = decision.Notes[index]
		out = append(out, pr)
	}
	return out
}

var (
	_ domain.DigestReporter = (*Reporter)(nil)
	_ domain.ScheduleJob    = (*Reporter)(nil)
)
