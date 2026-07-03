package application

import (
	"sort"
	"strings"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// HasPathRules reports whether any repo tier in the mappings configures a
// `paths:` block. Used to gate the "path routing needs GITHUB_TOKEN" warnings:
// without paths there is nothing to warn about.
func (p *Provider) HasPathRules() bool {
	for _, org := range p.file.Mappings {
		for _, reportConfig := range org {
			if len(reportConfig.Paths) > 0 {
				return true
			}
		}
	}
	return false
}

// RepoHasPathRules reports whether the specific repository's tier configures a
// `paths:` block. The runtime uses it to decide, per webhook, whether fetching
// the PR's changed files is worthwhile — repos without path rules skip the
// GitHub call entirely.
func (p *Provider) RepoHasPathRules(repository string) bool {
	_, repoCfg := p.lookup(repository)
	return repoCfg != nil && len(repoCfg.Paths) > 0
}

// PathChannels returns the distinct channels explicitly set on the repository's
// path rules (those that override the base channel), in sorted order. The base
// channel is validated separately; these are the extra Slack channels path
// routing can post to, so validation must confirm the bot is in each.
func (p *Provider) PathChannels(repository string) []string {
	_, repoCfg := p.lookup(repository)
	if repoCfg == nil {
		return nil
	}
	return pathChannels(repoCfg.Paths)
}

// pathChannels returns the distinct, sorted channels explicitly set on a set of
// path rules. Rules that omit a channel (they inherit the base) contribute
// nothing.
func pathChannels(paths []domain.PathRule) []string {
	seen := map[string]bool{}
	var channels []string
	for _, rule := range paths {
		if rule.Channel != "" && !seen[rule.Channel] {
			seen[rule.Channel] = true
			channels = append(channels, rule.Channel)
		}
	}
	sort.Strings(channels)
	return channels
}

// TargetsForFiles returns the fan-out destinations for a PR touching files: one
// Target per distinct matched channel, mentions unioned within each channel.
// With no path rules, no files, or no match it returns a single base target.
func (p *Provider) TargetsForFiles(repository string, files []string) []domain.Target {
	starPtr, repoPtr := p.lookup(repository)
	base := resolveRouting(starPtr, repoPtr)
	baseTarget := []domain.Target{{Channel: base.Channel, Mentions: base.Mentions}}
	if repoPtr == nil || len(repoPtr.Paths) == 0 {
		return baseTarget
	}
	winners := matchedRules(repoPtr.Paths, files)
	if len(winners) == 0 {
		return baseTarget
	}

	// Group matched rules by resolved channel, preserving first-seen order, and
	// union each channel's mentions (a rule with no mentions inherits base).
	order := []string{}
	byChannel := map[string][]domain.PathRule{}
	for _, rule := range winners {
		channel := rule.Channel
		if channel == "" {
			channel = base.Channel
		}
		if _, seen := byChannel[channel]; !seen {
			order = append(order, channel)
		}
		byChannel[channel] = append(byChannel[channel], rule)
	}

	targets := make([]domain.Target, 0, len(order))
	for _, channel := range order {
		targets = append(targets, domain.Target{
			Channel:  channel,
			Mentions: unionMentions(byChannel[channel], base.Mentions),
		})
	}
	return targets
}

// matchedRules returns the distinct path rules that win at least one file, in
// declaration order. A file's winner is the longest matching directory; that
// winner is unique because all rules matching one file are nested prefixes of
// it (and normalized dirs are distinct, so no two share a length).
func matchedRules(paths []domain.PathRule, files []string) []domain.PathRule {
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
	out := make([]domain.PathRule, 0)
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

// unionMentions unions the winners' effective mentions, deduped, in declaration
// order. A winner with no mentions key inherits base mentions (hazard M2); an
// explicit empty list contributes nothing.
func unionMentions(winners []domain.PathRule, baseMentions []string) []string {
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
