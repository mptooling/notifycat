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

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// StuckFinder returns open PRs whose last activity predates cutoff.
type StuckFinder interface {
	FindStuck(ctx context.Context, cutoff time.Time) ([]store.SlackMessage, error)
}

// MappingLookup resolves a repository to its Slack channel and mentions.
type MappingLookup interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
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
	slack    Poster
	composer *slack.Composer
	now      func() time.Time
	logger   *slog.Logger
}

// NewReporter constructs a Reporter. now defaults to time.Now.
func NewReporter(finder StuckFinder, mappings MappingLookup, poster Poster, composer *slack.Composer, logger *slog.Logger) *Reporter {
	return &Reporter{
		finder:   finder,
		mappings: mappings,
		slack:    poster,
		composer: composer,
		now:      time.Now,
		logger:   logger,
	}
}

// Report runs one digest pass: find open PRs idle since the start of today,
// group them by Slack channel, and post one reminder per channel. A failed
// post for one channel is logged and skipped so the others still go out.
func (r *Reporter) Report(ctx context.Context) error {
	now := r.now()
	cutoff := startOfDay(now)

	rows, err := r.finder.FindStuck(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("digest: find stuck: %w", err)
	}
	if len(rows) == 0 {
		r.logger.Debug("stuck-pr digest: nothing to report")
		return nil
	}

	for _, g := range r.groupByChannel(ctx, rows, now) {
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

// groupByChannel buckets stuck rows by their mapped Slack channel, preserving
// first-seen channel order for stable output and unioning mentions across the
// entries that feed one channel. Rows whose repo has no mapping are skipped.
func (r *Reporter) groupByChannel(ctx context.Context, rows []store.SlackMessage, now time.Time) []channelGroup {
	var order []string
	byChannel := map[string]*channelGroup{}
	mentionSeen := map[string]map[string]bool{}

	for _, row := range rows {
		mapping, err := r.mappings.Get(ctx, row.Repository)
		if errors.Is(err, store.ErrNotFound) {
			r.logger.Debug("stuck-pr digest: skipping unmapped repo",
				slog.String("repository", row.Repository),
				slog.Int("pr", row.PRNumber))
			continue
		}
		if err != nil {
			r.logger.Error("stuck-pr digest: mapping lookup failed",
				slog.String("repository", row.Repository),
				slog.Any("err", err))
			continue
		}

		g := byChannel[mapping.SlackChannel]
		if g == nil {
			g = &channelGroup{channel: mapping.SlackChannel}
			byChannel[mapping.SlackChannel] = g
			mentionSeen[mapping.SlackChannel] = map[string]bool{}
			order = append(order, mapping.SlackChannel)
		}
		for _, m := range mapping.Mentions {
			if !mentionSeen[g.channel][m] {
				mentionSeen[g.channel][m] = true
				g.mentions = append(g.mentions, m)
			}
		}
		g.prs = append(g.prs, slack.StuckPR{
			Repository: row.Repository,
			Number:     row.PRNumber,
			URL:        prURL(row.Repository, row.PRNumber),
			IdleDays:   idleDays(now, row.UpdatedAt),
		})
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
