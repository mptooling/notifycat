package application

import domain "github.com/mptooling/notifycat/internal/routing/domain"

func slackMappings(mappings map[string]domain.Org) map[string]domain.Org {
	slackView := make(map[string]domain.Org, len(mappings))
	for orgName, org := range mappings {
		slackOrg := make(domain.Org, len(org))
		for repoName, tier := range org {
			if tier.PartyOnly() {
				continue
			}
			tier.Paths = slackPathRules(tier.Paths)
			slackOrg[repoName] = tier
		}
		if len(slackOrg) > 0 {
			slackView[orgName] = slackOrg
		}
	}
	return slackView
}

func slackPathRules(rules []domain.PathRule) []domain.PathRule {
	var kept []domain.PathRule
	for _, rule := range rules {
		if !rule.PartyOnly() {
			kept = append(kept, rule)
		}
	}
	return kept
}
