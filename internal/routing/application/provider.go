package application

import (
	"context"
	"sort"
	"strings"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// Provider serves repository → mapping lookups from a parsed mappings.yaml.
// Construct with NewProvider; safe for concurrent reads (no mutation after construction).
type Provider struct {
	defaults domain.Defaults
	file     domain.File
}

// NewProvider builds a Provider from already-decoded sections (config.yaml's
// `mappings:` map and `digest:` block), the in-memory counterpart to Load.
// A nil digest leaves the feature on by default (see Digest).
func NewProvider(defaults domain.Defaults, m map[string]domain.Org, digest *domain.DigestConfig) *Provider {
	return &Provider{defaults: defaults, file: domain.File{Mappings: m, Digest: digest}}
}

// Digest returns the effective stuck-PR digest configuration. The feature is
// enabled by default, so an absent `digest:` section yields {Enabled: true,
// Schedule: DefaultDigestSchedule}. An explicit section may disable it or
// override the schedule.
func (p *Provider) Digest() domain.DigestConfig {
	cfg := domain.DigestConfig{Enabled: true, Schedule: domain.DefaultDigestSchedule}
	if p.file.Digest != nil {
		cfg.Enabled = p.file.Digest.Enabled
		if s := strings.TrimSpace(p.file.Digest.Schedule); s != "" {
			cfg.Schedule = s
		}
		cfg.Timezone = p.file.Digest.Timezone
	}
	return cfg
}

// lookup returns the org/repo and org/* tiers for repository, either of which
// may be nil. Both nil means the repository resolves to nothing (unmapped org,
// malformed key, or no matching tier).
func (p *Provider) lookup(repository string) (star, repo *domain.RepoConfig) {
	org, r, ok := splitRepo(repository)
	if !ok {
		return nil, nil
	}
	o, ok := p.file.Mappings[org]
	if !ok {
		return nil, nil
	}
	if rc, has := o[r]; has {
		repo = &rc
	}
	if sc, has := o[domain.WildcardKey]; has {
		star = &sc
	}
	return star, repo
}

// Get returns the resolved mapping for "org/repo": the org/repo tier merged
// over the org/* tier. Returns domain.ErrNotFound when the org is unmapped or
// neither an explicit tier nor a wildcard tier matches.
func (p *Provider) Get(_ context.Context, repository string) (domain.RepoMapping, error) {
	starPtr, repoPtr := p.lookup(repository)
	if repoPtr == nil && starPtr == nil {
		return domain.RepoMapping{}, domain.ErrNotFound
	}
	res := resolveRouting(starPtr, repoPtr)
	rx, ignoreAI, dependabot := resolveBehavior(p.defaults, starPtr, repoPtr)
	return domain.RepoMapping{
		Repository:       repository,
		SlackChannel:     res.Channel,
		Mentions:         res.Mentions,
		Reactions:        rx,
		IgnoreAIReviews:  ignoreAI,
		DependabotFormat: dependabot,
	}, nil
}

// Entries returns validation units in deterministic order: orgs A→Z, explicit
// repos within each org A→Z, the wildcard entry last. Each entry's Channel is
// the resolved channel (the tier's own, or inherited from org/*), so the
// validator and lock operate on what a webhook would actually route to.
func (p *Provider) Entries() []domain.Entry {
	orgs := make([]string, 0, len(p.file.Mappings))
	for org := range p.file.Mappings {
		orgs = append(orgs, org)
	}
	sort.Strings(orgs)

	var out []domain.Entry
	for _, org := range orgs {
		o := p.file.Mappings[org]
		var starPtr *domain.RepoConfig
		if sc, has := o[domain.WildcardKey]; has {
			starPtr = &sc
		}
		repos := make([]string, 0, len(o))
		for k := range o {
			if k != domain.WildcardKey {
				repos = append(repos, k)
			}
		}
		sort.Strings(repos)
		for _, r := range repos {
			rc := o[r]
			res := resolveRouting(starPtr, &rc)
			out = append(out, domain.Entry{
				Org:          org,
				Repo:         r,
				Channel:      res.Channel,
				Mentions:     res.Mentions,
				PathChannels: pathChannels(rc.Paths),
			})
		}
		if starPtr != nil {
			res := resolveRouting(starPtr, nil)
			out = append(out, domain.Entry{Org: org, Wildcard: true, Channel: res.Channel, Mentions: res.Mentions})
		}
	}
	return out
}

// DigestFor returns the effective digest config for a repository: the global
// Digest() merged with the org/* and org/repo tiers (most-specific tier that
// sets enabled/schedule wins). An unmapped repo yields the global digest.
func (p *Provider) DigestFor(repository string) domain.DigestConfig {
	digest := p.Digest() // global default (enabled + DefaultDigestSchedule)
	org, repo, ok := splitRepo(repository)
	if !ok {
		return digest
	}
	o, ok := p.file.Mappings[org]
	if !ok {
		return digest
	}
	apply := func(rc domain.RepoConfig, has bool) {
		if has && rc.Digest != nil {
			digest.Enabled = rc.Digest.Enabled
			if s := strings.TrimSpace(rc.Digest.Schedule); s != "" {
				digest.Schedule = s
			}
		}
	}
	star, hasStar := o[domain.WildcardKey]
	apply(star, hasStar)
	rc, hasRepo := o[repo]
	apply(rc, hasRepo)
	return digest
}

// Schedules returns the sorted distinct set of effective digest schedules
// across every mapping entry whose effective digest is enabled. The scheduler
// registers one cron per returned spec.
func (p *Provider) Schedules() []string {
	seen := map[string]struct{}{}
	for _, e := range p.Entries() {
		digestConfig := p.DigestFor(e.Key())
		if !digestConfig.Enabled {
			continue
		}
		seen[digestConfig.Schedule] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func splitRepo(s string) (org, repo string, ok bool) {
	i := strings.IndexByte(s, '/')
	if i < 1 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
