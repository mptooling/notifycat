package application

import (
	"sort"
	"strings"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

var _ domain.PartyRules = (*Provider)(nil)

type partyTier struct {
	override domain.PartyOverride
	paths    []domain.PathRule
}

func buildPartyIndex(mappings map[string]domain.Org) map[string]partyTier {
	index := make(map[string]partyTier)
	orgs := make([]string, 0, len(mappings))
	for org := range mappings {
		orgs = append(orgs, org)
	}
	sort.Strings(orgs)
	for _, org := range orgs {
		repos := make([]string, 0, len(mappings[org]))
		for repo := range mappings[org] {
			repos = append(repos, repo)
		}
		sort.Strings(repos)
		for _, repo := range repos {
			key := strings.ToLower(org + "/" + repo)
			if _, taken := index[key]; taken {
				continue
			}
			tier := mappings[org][repo]
			index[key] = partyTier{override: tier.Party, paths: partyPathRules(tier.Paths)}
		}
	}
	return index
}

func partyPathRules(paths []domain.PathRule) []domain.PathRule {
	var rules []domain.PathRule
	for _, rule := range paths {
		if rule.Party.IsSet() {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (p *Provider) partyTiers(repository string) (star, repo partyTier) {
	org, name, ok := splitRepo(strings.ToLower(repository))
	if !ok {
		return partyTier{}, partyTier{}
	}
	return p.party[org+"/"+domain.WildcardKey], p.party[org+"/"+name]
}

// HasPartyPathRules reports whether a path rule of the repository sets a party key.
func (p *Provider) HasPartyPathRules(repository string) bool {
	_, repo := p.partyTiers(repository)
	return len(repo.paths) > 0
}

// PartySettings returns the highest cap and the shortest timer over the files.
func (p *Provider) PartySettings(repository string, files []string) domain.PartySettings {
	star, repo := p.partyTiers(repository)
	base := p.repositoryParty(star, repo)
	if len(files) == 0 || len(repo.paths) == 0 {
		return base
	}
	var result domain.PartySettings
	for index, file := range files {
		fileSettings := fileParty(repo.paths, base, file)
		if index == 0 || fileSettings.Reviewers > result.Reviewers {
			result.Reviewers = fileSettings.Reviewers
		}
		if index == 0 || fileSettings.Deadline < result.Deadline {
			result.Deadline = fileSettings.Deadline
		}
	}
	return result
}

func (p *Provider) repositoryParty(star, repo partyTier) domain.PartySettings {
	settings := p.defaults.Party
	if settings.Reviewers < domain.MinPartyReviewers {
		settings.Reviewers = domain.DefaultPartyReviewers
	}
	if settings.Deadline < domain.MinPartyDeadline {
		settings.Deadline = domain.DefaultPartyDeadline
	}
	for _, override := range []domain.PartyOverride{star.override, repo.override} {
		if override.Reviewers != nil {
			settings.Reviewers = *override.Reviewers
		}
		if override.Deadline != nil {
			settings.Deadline = *override.Deadline
		}
	}
	return settings
}

func fileParty(rules []domain.PathRule, base domain.PartySettings, file string) domain.PartySettings {
	file = strings.TrimPrefix(strings.TrimSpace(file), "/")
	settings := base
	var reviewersDir, deadlineDir string
	reviewersFound, deadlineFound := false, false
	for _, rule := range rules {
		if !fileUnder(file, rule.Dir) {
			continue
		}
		if rule.Party.Reviewers != nil && (!reviewersFound || len(rule.Dir) > len(reviewersDir)) {
			settings.Reviewers, reviewersDir, reviewersFound = *rule.Party.Reviewers, rule.Dir, true
		}
		if rule.Party.Deadline != nil && (!deadlineFound || len(rule.Dir) > len(deadlineDir)) {
			settings.Deadline, deadlineDir, deadlineFound = *rule.Party.Deadline, rule.Dir, true
		}
	}
	return settings
}
