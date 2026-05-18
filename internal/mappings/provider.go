package mappings

import (
	"context"
	"fmt"
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
		return nil, fmt.Errorf("mappings: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	file, err := Parse(f)
	if err != nil {
		return nil, err
	}
	return &Provider{file: file}, nil
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
		Mentions:     append([]string(nil), o.Mentions...),
	}, nil
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
		if o.Repositories.All {
			out = append(out, Entry{
				Org: org, Wildcard: true,
				Channel: o.Channel, Mentions: o.Mentions,
			})
			continue
		}
		repos := append([]string(nil), o.Repositories.List...)
		sort.Strings(repos)
		for _, r := range repos {
			out = append(out, Entry{
				Org: org, Repo: r,
				Channel: o.Channel, Mentions: o.Mentions,
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
