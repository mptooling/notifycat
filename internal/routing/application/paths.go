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

// AdditionalChannels returns the channels the repository can post to beyond its
// primary base channel (extra base-list channels plus per-path channels),
// sorted and deduped. The validator checks bot membership for each; the doctor
// and notifycat-config validate share this via the validation port.
func (p *Provider) AdditionalChannels(repository string) []string {
	starPtr, repoPtr := p.lookup(repository)
	return additionalChannels(starPtr, repoPtr)
}

// pathChannels returns the distinct, sorted channels explicitly set on a set of
// path rules, in either form. Rules that omit channels (they inherit the base)
// contribute nothing.
func pathChannels(paths []domain.PathRule) []string {
	seen := map[string]bool{}
	var channels []string
	add := func(channel string) {
		if channel != "" && !seen[channel] {
			seen[channel] = true
			channels = append(channels, channel)
		}
	}
	for _, rule := range paths {
		add(rule.Channel)
		for _, spec := range rule.Channels {
			add(spec.Channel)
		}
	}
	sort.Strings(channels)
	return channels
}

// additionalChannels returns the sorted distinct channels a repository can post
// to beyond its primary base channel: extra base-list channels plus every path
// channel. These are the channels validation must confirm bot membership for,
// on top of the primary, and they feed the lock entry hash.
func additionalChannels(star, repo *domain.RepoConfig) []string {
	base := resolveBaseTargets(star, repo)
	seen := map[string]bool{}
	var out []string
	add := func(channel string) {
		if channel != "" && !seen[channel] {
			seen[channel] = true
			out = append(out, channel)
		}
	}
	for _, target := range base[1:] { // skip the primary
		add(target.Channel)
	}
	if repo != nil {
		for _, channel := range pathChannels(repo.Paths) {
			add(channel)
		}
	}
	sort.Strings(out)
	return out
}

// BaseTargets returns the repository's unconditional base fan-out targets (the
// channels every PR is announced to before any path rules apply). The router
// uses it when the repo has no path rules or no changed-files reader.
func (p *Provider) BaseTargets(repository string) []domain.Target {
	starPtr, repoPtr := p.lookup(repository)
	return resolveBaseTargets(starPtr, repoPtr)
}

// TargetsForFiles returns the fan-out destinations for a PR touching files. With
// no path rules, no files, or no match it returns the full base target set.
// Matched path rules (single or list) replace the base set; a matched rule that
// omits channels inherits the primary base channel. Targets are grouped by
// channel with mentions unioned; a list entry's absent mentions default to
// @channel, a single-form rule's absent mentions inherit the primary base.
func (p *Provider) TargetsForFiles(repository string, files []string) []domain.Target {
	starPtr, repoPtr := p.lookup(repository)
	base := resolveBaseTargets(starPtr, repoPtr)
	if repoPtr == nil || len(repoPtr.Paths) == 0 {
		return base
	}
	winners := matchedRules(repoPtr.Paths, files)
	if len(winners) == 0 {
		return base
	}

	primaryChannel := base[0].Channel
	primaryMentions := base[0].Mentions

	order := []string{}
	byChannel := map[string][]mentionContribution{}
	add := func(channel string, mentions []string, present bool) {
		if channel == "" {
			channel = primaryChannel
		}
		if _, seen := byChannel[channel]; !seen {
			order = append(order, channel)
		}
		byChannel[channel] = append(byChannel[channel], mentionContribution{mentions: mentions, present: present})
	}
	for _, rule := range winners {
		if len(rule.Channels) > 0 {
			for _, spec := range rule.Channels {
				add(spec.Channel, specMentions(spec), true) // list form: mentions already resolved, no base inherit
			}
			continue
		}
		add(rule.Channel, rule.Mentions, rule.MentionsPresent)
	}

	targets := make([]domain.Target, 0, len(order))
	for _, channel := range order {
		targets = append(targets, domain.Target{
			Channel:  channel,
			Mentions: unionContributions(byChannel[channel], primaryMentions),
		})
	}
	return targets
}

// mentionContribution is one matched rule's mentions for a channel: present=true
// means use mentions as-is; present=false means inherit the base mentions.
type mentionContribution struct {
	mentions []string
	present  bool
}

// unionContributions unions contributions' effective mentions, deduped, in order.
// An absent (present=false) contribution inherits baseMentions.
func unionContributions(contribs []mentionContribution, baseMentions []string) []string {
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
	for _, c := range contribs {
		if c.present {
			add(c.mentions)
		} else {
			add(baseMentions)
		}
	}
	return out
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

