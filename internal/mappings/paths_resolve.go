package mappings

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mptooling/notifycat/internal/store"
)

// maxMatchedPathOwners caps how many distinct directory rules a single PR may
// match before path routing gives up and posts to the repo base channel with
// the base mentions (hazard M5). A PR sprawling across more directories than
// this would union too many teams into one ping to be useful, so the base tier
// (one channel, one audience) is the safer destination.
const maxMatchedPathOwners = 5

// HasPathRules reports whether any repo tier in the mappings configures a
// `paths:` block. Used to gate the "path routing needs GITHUB_TOKEN" warnings:
// without paths there is nothing to warn about.
func (p *Provider) HasPathRules() bool {
	for _, o := range p.file.Mappings {
		for _, rc := range o {
			if len(rc.Paths) > 0 {
				return true
			}
		}
	}
	return false
}

// GetForFiles resolves routing for a PR whose changed files are `files`
// (repo-relative paths, as GitHub reports them), applying the repo tier's
// `paths:` rules on top of the base org/repo resolution from Get. With no path
// rules, no files, or no path match, the result is identical to Get's — the PR
// routes to the repo/org tier exactly as today.
//
// It logs (via logger, if non-nil) two operator-relevant outcomes: the M5
// safety valve firing, and mentions bottoming out at @channel (M3). The caller
// supplies the changed files — GetForFiles makes no GitHub API call.
func (p *Provider) GetForFiles(ctx context.Context, logger *slog.Logger, repository string, files []string) (store.RepoMapping, error) {
	m, err := p.Get(ctx, repository)
	if err != nil {
		return store.RepoMapping{}, err
	}
	_, repoPtr := p.lookup(repository)
	if repoPtr == nil || len(repoPtr.Paths) == 0 {
		return m, nil
	}

	base := Resolved{Channel: m.SlackChannel, Mentions: m.Mentions}
	res, out := resolvePaths(base, repoPtr.Paths, files)
	if !out.matched {
		return m, nil
	}
	if out.valveTripped && logger != nil {
		logger.Warn("path routing: too many matched directories; routing to the repo base channel",
			slog.String("repository", repository),
			slog.Int("matched", out.owners),
			slog.Int("limit", maxMatchedPathOwners))
	}
	if out.fellBackToChannel && logger != nil {
		logger.Warn("path routing resolved to @channel; matched directories set no team mentions",
			slog.String("repository", repository),
			slog.String("channel", res.Channel))
	}
	m.SlackChannel = res.Channel
	m.Mentions = res.Mentions
	return m, nil
}

// pathOutcome carries the non-routing facts the caller logs about.
type pathOutcome struct {
	matched           bool // at least one path rule matched a changed file
	owners            int  // distinct matched directory rules
	fellBackToChannel bool // resolved mentions are exactly @channel (M3)
	valveTripped      bool // owners exceeded the cap; fell back to base (M5)
}

// resolvePaths layers path rules over base for a PR touching files. Each file
// picks its most-specific matching rule; across the PR the most-specific
// winning rule owns the channel and the winners' mentions are unioned. No match
// (or no rules/files) returns base unchanged.
func resolvePaths(base Resolved, paths []PathRule, files []string) (Resolved, pathOutcome) {
	if len(paths) == 0 || len(files) == 0 {
		return base, pathOutcome{}
	}
	winners := matchedRules(paths, files)
	if len(winners) == 0 {
		return base, pathOutcome{}
	}
	if len(winners) > maxMatchedPathOwners {
		return base, pathOutcome{matched: true, owners: len(winners), valveTripped: true}
	}
	res := Resolved{
		Channel:  channelWinner(winners, base.Channel),
		Mentions: unionMentions(winners, base.Mentions),
	}
	fellBack := len(res.Mentions) == 1 && res.Mentions[0] == ChannelMention
	return res, pathOutcome{matched: true, owners: len(winners), fellBackToChannel: fellBack}
}

// matchedRules returns the distinct path rules that win at least one file, in
// declaration order. A file's winner is the longest matching directory; that
// winner is unique because all rules matching one file are nested prefixes of
// it (and normalized dirs are distinct, so no two share a length).
func matchedRules(paths []PathRule, files []string) []PathRule {
	chosen := make([]bool, len(paths))
	for _, f := range files {
		f = strings.TrimPrefix(strings.TrimSpace(f), "/")
		best := -1
		for i := range paths {
			if fileUnder(f, paths[i].Dir) && (best == -1 || len(paths[i].Dir) > len(paths[best].Dir)) {
				best = i
			}
		}
		if best >= 0 {
			chosen[best] = true
		}
	}
	out := make([]PathRule, 0)
	for i := range paths {
		if chosen[i] {
			out = append(out, paths[i])
		}
	}
	return out
}

// fileUnder reports whether file lives inside dir (segment-aware): "modules/acme"
// matches "modules/acme/x.go" but not "modules/acmexyz/x.go".
func fileUnder(file, dir string) bool {
	return strings.HasPrefix(file, dir+"/")
}

// channelWinner picks the channel from the most-specific winning rule:
// longest directory → fewest path segments → declaration order (winners is
// already in declaration order, so equal-on-both keeps the earlier rule; a
// lexical fallback is documented but unreachable for distinct dirs). A winner
// with no channel of its own inherits the base channel.
func channelWinner(winners []PathRule, baseChannel string) string {
	best := 0
	for i := 1; i < len(winners); i++ {
		switch {
		case len(winners[i].Dir) > len(winners[best].Dir):
			best = i
		case len(winners[i].Dir) == len(winners[best].Dir) &&
			segments(winners[i].Dir) < segments(winners[best].Dir):
			best = i
		}
	}
	if winners[best].Channel != "" {
		return winners[best].Channel
	}
	return baseChannel
}

// unionMentions unions the winners' effective mentions, deduped, in declaration
// order. A winner with no mentions key inherits base mentions (hazard M2); an
// explicit empty list contributes nothing.
func unionMentions(winners []PathRule, baseMentions []string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(ms []string) {
		for _, m := range ms {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	for _, w := range winners {
		if w.MentionsPresent {
			add(w.Mentions)
		} else {
			add(baseMentions)
		}
	}
	return out
}

// segments counts the path components in a normalized (no leading/trailing
// slash, non-empty) directory.
func segments(dir string) int {
	return strings.Count(dir, "/") + 1
}
