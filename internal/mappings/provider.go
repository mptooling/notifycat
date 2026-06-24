package mappings

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/mptooling/notifycat/internal/store"
)

// Provider serves repository → mapping lookups from a parsed mappings.yaml.
// Construct with Load; safe for concurrent reads (no mutation after Load).
type Provider struct {
	file File
}

// Load reads and validates the file at path.
func Load(path string) (*Provider, error) {
	f, err := os.Open(path) //nolint:gosec // path is operator-supplied configuration
	if err != nil {
		return nil, &FileNotFoundError{Path: path, Err: err}
	}
	defer func() { _ = f.Close() }()

	file, err := Parse(f)
	if err != nil {
		return nil, &ParseError{Path: path, Err: err}
	}
	return &Provider{file: file}, nil
}

// NewProvider builds a Provider from already-decoded sections (config.yaml's
// `mappings:` map and `digest:` block), the in-memory counterpart to Load.
// A nil digest leaves the feature on by default (see Digest).
func NewProvider(m map[string]Org, digest *DigestConfig) *Provider {
	return &Provider{file: File{Mappings: m, Digest: digest}}
}

// DefaultDigestSchedule is the cron spec used when the digest section is
// absent or omits `schedule`: 9am every morning, server-local time.
const DefaultDigestSchedule = "0 9 * * *"

// Digest returns the effective stuck-PR digest configuration. The feature is
// enabled by default, so an absent `digest:` section yields {Enabled: true,
// Schedule: DefaultDigestSchedule}. An explicit section may disable it or
// override the schedule.
func (p *Provider) Digest() DigestConfig {
	cfg := DigestConfig{Enabled: true, Schedule: DefaultDigestSchedule}
	if p.file.Digest != nil {
		cfg.Enabled = p.file.Digest.Enabled
		if s := strings.TrimSpace(p.file.Digest.Schedule); s != "" {
			cfg.Schedule = s
		}
	}
	return cfg
}

// Get returns the mapping for "org/repo": exact match first, then wildcard
// on the org. Returns store.ErrNotFound when nothing matches.
func (p *Provider) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	org, repo, ok := splitRepo(repository)
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	o, ok := p.file.Mappings[org]
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	if !o.Repositories.All {
		matched := false
		for _, r := range o.Repositories.List {
			if r == repo {
				matched = true
				break
			}
		}
		if !matched {
			return store.RepoMapping{}, store.ErrNotFound
		}
	}
	return store.RepoMapping{
		Repository:   repository,
		SlackChannel: o.Channel,
		Mentions:     resolveMentions(o),
	}, nil
}

// resolveMentions materializes the absent-mentions case as @channel so
// downstream consumers (composer, list CLI) don't need to know about
// MentionsPresent. Returns a fresh slice to keep the parsed file immutable.
func resolveMentions(o Org) []string {
	if !o.MentionsPresent {
		return []string{ChannelMention}
	}
	return append([]string(nil), o.Mentions...)
}

// Entries returns validation units in deterministic order: orgs sorted A→Z,
// explicit repos within each org sorted A→Z, wildcard entries last per org.
func (p *Provider) Entries() []Entry {
	orgs := make([]string, 0, len(p.file.Mappings))
	for org := range p.file.Mappings {
		orgs = append(orgs, org)
	}
	sort.Strings(orgs)

	var out []Entry
	for _, org := range orgs {
		o := p.file.Mappings[org]
		mentions := resolveMentions(o)
		if o.Repositories.All {
			out = append(out, Entry{
				Org: org, Wildcard: true,
				Channel: o.Channel, Mentions: mentions,
			})
			continue
		}
		repos := append([]string(nil), o.Repositories.List...)
		sort.Strings(repos)
		for _, r := range repos {
			out = append(out, Entry{
				Org: org, Repo: r,
				Channel: o.Channel, Mentions: mentions,
			})
		}
	}
	return out
}

func splitRepo(s string) (org, repo string, ok bool) {
	i := strings.IndexByte(s, '/')
	if i < 1 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
