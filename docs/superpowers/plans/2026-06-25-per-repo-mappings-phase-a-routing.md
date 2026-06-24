# Per-Repo Mappings — Phase A (routing + schema) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure the `mappings:` section of `config.yaml` from per-org (`org → {channel, mentions, repositories}`) to per-repo tiers (`org → {repo|*: {channel, mentions}}`), resolved by most-specific-wins merge, delivering per-repository routing. Behavioral settings stay global (Phase B adds per-repo behavioral override).

**Architecture:** Each org becomes a map of repo-name (or the literal `*`) to a `RepoConfig` tier. A lookup for `org/repo` resolves the effective channel/mentions by merging the `org/repo` tier over the `org/*` tier (most-specific tier that sets a key wins; absent `mentions` inherits, falling back to `@channel` only when no tier sets it). The existing `Entry`/lock/validate contract is preserved — `Provider.Entries` still emits one `Entry` per `org/repo` and per `org/*` with the *resolved* channel — so validation and the boot-time cache are unchanged. This is a breaking change shipped together with Phase B as a single `0.18` release.

**Tech Stack:** Go 1.25.10, `gopkg.in/yaml.v3`, GORM/SQLite (untouched), `just`.

## Global Constraints

- Go toolchain pinned at **1.25.10**.
- Phase A + Phase B ship as ONE breaking release (`0.17` → `0.18`, pre-1.0 minor bump). Do NOT cut a release between phases.
- **No Claude attribution** in commits or PRs.
- **No hard-wrapped markdown** in repo docs / PR bodies — one long line per paragraph.
- Do **not** commit `config.yaml`, `config.lock`, `.env`, `/data/`.
- **One constructor per type, all deps injected**; never a prod-wiring façade + test-seam pair.
- **Consumer-package interfaces** — declared where consumed.
- **TDD**: RED → verify failure → GREEN. New behavior starts with a failing test.
- **No comments restating code** — document non-obvious *why* only.
- Verify with `just check` before declaring done.
- Schema rules carried verbatim from the design spec (`docs/superpowers/specs/2026-06-24-config-yaml-and-per-repo-mappings-design.md`):
  - Org key matches `^[A-Za-z0-9_.-]+$`; repo key matches `^[A-Za-z0-9_.-]+$` or is the literal `*`.
  - `channel` matches `^[CGD][A-Z0-9]{2,}$`.
  - `mentions` tri-state: absent → inherit (final fallback `<!channel>`); `[]` → ping nobody; `mentions: null`/`~` → rejected. (Behavior change vs 0.17: absent now means *inherit*, not *@channel*.)
  - Every resolvable repo path must yield a non-empty `channel` (from its own tier or `org/*`); otherwise a parse/validate error.
  - Unknown keys inside a tier are rejected (typo safety).

---

### Task 1: `RepoConfig` tier type and strict per-tier decoding

Replace the per-org `Org`/`Repositories` model with `Org = map[string]RepoConfig`, where keys are repo names or `*`. `RepoConfig` carries routing only (channel + mentions tri-state) in Phase A.

**Files:**
- Modify: `internal/mappings/types.go` (replace `Org`, `Repositories`; keep `DigestConfig`, `ChannelMention`, `File`)
- Test: `internal/mappings/types_test.go`

**Interfaces:**
- Consumes: `mappings.ChannelMention` (`"<!channel>"`), `mappings.DigestConfig` (unchanged).
- Produces:
  - `type Org map[string]RepoConfig`
  - `type RepoConfig struct { Channel string; Mentions []string; MentionsPresent bool }` with `func (rc *RepoConfig) UnmarshalYAML(*yaml.Node) error`
  - `type File struct { Digest *DigestConfig; Mappings map[string]Org }` (unchanged shape; `Org`'s underlying type changes)

- [ ] **Step 1: Write the failing test**

Replace `internal/mappings/types_test.go` contents that exercise the old `Org`/`Repositories` shape with tier-based tests. Add:

```go
package mappings_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mptooling/notifycat/internal/mappings"
)

func decodeOrg(t *testing.T, body string) mappings.Org {
	t.Helper()
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return o
}

func TestRepoConfig_ChannelAndMentionsPresent(t *testing.T) {
	o := decodeOrg(t, `
api:
  channel: C0API
  mentions: ["<@U1>"]
"*":
  channel: C0STAR
`)
	api, ok := o["api"]
	if !ok {
		t.Fatal("missing api tier")
	}
	if api.Channel != "C0API" {
		t.Errorf("api.Channel = %q; want C0API", api.Channel)
	}
	if !api.MentionsPresent || len(api.Mentions) != 1 || api.Mentions[0] != "<@U1>" {
		t.Errorf("api mentions = %+v present=%v", api.Mentions, api.MentionsPresent)
	}
	star := o["*"]
	if star.Channel != "C0STAR" || star.MentionsPresent {
		t.Errorf("star = %+v; want channel C0STAR, mentions absent", star)
	}
}

func TestRepoConfig_EmptyMentionsIsPresent(t *testing.T) {
	o := decodeOrg(t, "api:\n  channel: C0API\n  mentions: []\n")
	if !o["api"].MentionsPresent || len(o["api"].Mentions) != 0 {
		t.Errorf("mentions: [] should be present+empty; got %+v", o["api"])
	}
}

func TestRepoConfig_NullMentionsRejected(t *testing.T) {
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  mentions: null\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for mentions: null")
	}
}

func TestRepoConfig_UnknownKeyRejected(t *testing.T) {
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  bogus: x\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for unknown tier key")
	}
}
```

If the old `types_test.go` has `Repositories`/`Org{Channel:...}` literals, delete those tests (they assert the retired shape).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run 'TestRepoConfig'`
Expected: FAIL to compile — `RepoConfig` undefined / `Org` is not a map.

- [ ] **Step 3: Rewrite the types**

In `internal/mappings/types.go`, keep `ChannelMention`, `File`, `DigestConfig` (and its `UnmarshalYAML`). Replace the `Org`, `Repositories` types and their `UnmarshalYAML` methods with:

```go
// Org maps each repo name (or the literal "*") to its tier config. The "*"
// key is the org-level tier: it both supplies defaults that explicit repo
// tiers inherit and matches any repo not named explicitly.
type Org map[string]RepoConfig

// RepoConfig is one tier (org/repo or org/*). Phase A carries routing only.
// An empty Channel means this tier does not set a channel (it inherits).
// MentionsPresent distinguishes an absent mentions key (inherit) from an
// explicit empty list (ping nobody).
type RepoConfig struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool
}

// UnmarshalYAML walks the mapping node by hand so we can keep the mentions
// tri-state (absent vs [] vs null) and reject unknown keys, mirroring the
// 0.17 Org decoder but at the per-repo tier level.
func (rc *RepoConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("repo config: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("repo config: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("repo config: non-scalar key")
		}
		switch keyNode.Value {
		case "channel":
			if err := valNode.Decode(&rc.Channel); err != nil {
				return fmt.Errorf("channel: %w", err)
			}
		case "mentions":
			rc.MentionsPresent = true
			if isNullNode(valNode) {
				return fmt.Errorf("mentions: null is not allowed; omit the key to inherit/@channel or use [] for none")
			}
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("mentions: must be a list (use [] for none, omit the key to inherit)")
			}
			ms := []string{}
			if err := valNode.Decode(&ms); err != nil {
				return fmt.Errorf("mentions: %w", err)
			}
			rc.Mentions = ms
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}
```

Keep the existing `isNullNode` helper (it stays in this file). Ensure `fmt` and `gopkg.in/yaml.v3` remain imported.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run 'TestRepoConfig'`
Expected: PASS. (Other `mappings` tests will not compile yet — Tasks 2-5 fix `parse.go`/`provider.go`. That's expected; do not run the whole package yet.)

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/types.go internal/mappings/types_test.go
git commit -m "feat: per-repo RepoConfig tier type replacing per-org Org shape"
```

---

### Task 2: Tier validation in `parse.go`

Validate org keys, repo keys (or `*`), channel format, the mentions/`*` rules, and the "every resolvable repo yields a channel" invariant.

**Files:**
- Modify: `internal/mappings/parse.go` (`validate` method)
- Test: `internal/mappings/parse_test.go`

**Interfaces:**
- Consumes: `Org`, `RepoConfig` (Task 1).
- Produces: `File.validate() error` rejecting malformed tiers; `Parse(io.Reader) (File, error)` unchanged signature.

- [ ] **Step 1: Write the failing tests**

Add to `internal/mappings/parse_test.go` (and remove any tests asserting the old `repositories:` shape):

```go
func TestParse_PerRepoTiers_OK(t *testing.T) {
	f, err := mappings.Parse(strings.NewReader(`
mappings:
  acme:
    api:
      channel: C0API
    web:
      channel: C0WEB
    "*":
      channel: C0DEFAULT
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Mappings["acme"]["api"].Channel != "C0API" {
		t.Errorf("api channel = %q", f.Mappings["acme"]["api"].Channel)
	}
}

func TestParse_InheritsChannelFromStar(t *testing.T) {
	// api sets no channel but org/* does — valid (api inherits at resolve).
	if _, err := mappings.Parse(strings.NewReader(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
    "*":
      channel: C0DEFAULT
`)); err != nil {
		t.Fatalf("Parse should accept channel inherited from *: %v", err)
	}
}

func TestParse_RepoWithoutChannelAndNoStarRejected(t *testing.T) {
	if _, err := mappings.Parse(strings.NewReader(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
`)); err == nil {
		t.Fatal("expected error: api has no channel and no org/* to inherit from")
	}
}

func TestParse_BadChannelRejected(t *testing.T) {
	if _, err := mappings.Parse(strings.NewReader("mappings:\n  acme:\n    api:\n      channel: not-a-channel\n")); err == nil {
		t.Fatal("expected error for malformed channel")
	}
}

func TestParse_BadRepoKeyRejected(t *testing.T) {
	if _, err := mappings.Parse(strings.NewReader("mappings:\n  acme:\n    \"a/b\":\n      channel: C0API\n")); err == nil {
		t.Fatal("expected error for repo key containing /")
	}
}

func TestParse_EmptyOrgRejected(t *testing.T) {
	if _, err := mappings.Parse(strings.NewReader("mappings:\n  acme: {}\n")); err == nil {
		t.Fatal("expected error for org with no tiers")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestParse`
Expected: FAIL (compile or assertion) — `validate` still references the removed `Repositories`.

- [ ] **Step 3: Rewrite `validate`**

In `internal/mappings/parse.go`, keep the regex vars but replace `validate` with the tier-aware version. Add a `starKey` const:

```go
const starKey = "*"

func (f File) validate() error {
	for org, o := range f.Mappings {
		if !orgPattern.MatchString(org) {
			return fmt.Errorf("mappings: org %q: invalid name (must match %s)", org, orgPattern)
		}
		if len(o) == 0 {
			return fmt.Errorf("mappings: org %q: has no repo entries", org)
		}
		star, hasStar := o[starKey]
		starHasChannel := hasStar && star.Channel != ""
		for repo, rc := range o {
			if repo != starKey && !repoPattern.MatchString(repo) {
				return fmt.Errorf("mappings: org %q: invalid repo key %q (use a bare repo name or \"*\")", org, repo)
			}
			if rc.Channel != "" && !channelPattern.MatchString(rc.Channel) {
				return fmt.Errorf("mappings: org %q repo %q: invalid channel %q", org, repo, rc.Channel)
			}
			// Every resolvable path must yield a channel: this tier sets one,
			// or org/* supplies it.
			if rc.Channel == "" && !starHasChannel {
				return fmt.Errorf("mappings: org %q repo %q: no channel (set channel here or in the org's \"*\" entry)", org, repo)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run TestParse`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/parse.go internal/mappings/parse_test.go
git commit -m "feat: validate per-repo mapping tiers"
```

---

### Task 3: Routing resolver

A pure function that merges the `org/*` tier under the `org/repo` tier for routing.

**Files:**
- Create: `internal/mappings/resolve.go`
- Test: `internal/mappings/resolve_test.go`

**Interfaces:**
- Consumes: `RepoConfig`, `ChannelMention`.
- Produces:
  - `type Resolved struct { Channel string; Mentions []string }`
  - `func resolveRouting(star, repo *RepoConfig) Resolved` — unexported; `repo`/`star` may each be nil but not both. Channel = most-specific non-empty. Mentions = most-specific present, else `[]string{ChannelMention}`.

- [ ] **Step 1: Write the failing test**

Create `internal/mappings/resolve_test.go`:

```go
package mappings

import (
	"reflect"
	"testing"
)

func TestResolveRouting_RepoOverridesStar(t *testing.T) {
	star := &RepoConfig{Channel: "C0STAR", Mentions: []string{"<@S>"}, MentionsPresent: true}
	repo := &RepoConfig{Channel: "C0REPO"}
	got := resolveRouting(star, repo)
	// channel: repo wins; mentions: repo absent → inherit star's
	if got.Channel != "C0REPO" {
		t.Errorf("Channel = %q; want C0REPO", got.Channel)
	}
	if !reflect.DeepEqual(got.Mentions, []string{"<@S>"}) {
		t.Errorf("Mentions = %v; want star's [<@S>]", got.Mentions)
	}
}

func TestResolveRouting_RepoInheritsChannel(t *testing.T) {
	star := &RepoConfig{Channel: "C0STAR"}
	repo := &RepoConfig{Mentions: []string{"<@U>"}, MentionsPresent: true}
	got := resolveRouting(star, repo)
	if got.Channel != "C0STAR" {
		t.Errorf("Channel = %q; want inherited C0STAR", got.Channel)
	}
	if !reflect.DeepEqual(got.Mentions, []string{"<@U>"}) {
		t.Errorf("Mentions = %v; want repo's", got.Mentions)
	}
}

func TestResolveRouting_NoMentionsAnywhere_DefaultsChannelPing(t *testing.T) {
	got := resolveRouting(nil, &RepoConfig{Channel: "C0REPO"})
	if !reflect.DeepEqual(got.Mentions, []string{ChannelMention}) {
		t.Errorf("Mentions = %v; want [%s]", got.Mentions, ChannelMention)
	}
}

func TestResolveRouting_EmptyMentionsPresent_PingsNobody(t *testing.T) {
	repo := &RepoConfig{Channel: "C0REPO", Mentions: []string{}, MentionsPresent: true}
	got := resolveRouting(nil, repo)
	if len(got.Mentions) != 0 {
		t.Errorf("Mentions = %v; want empty (ping nobody)", got.Mentions)
	}
}

func TestResolveRouting_StarOnly(t *testing.T) {
	got := resolveRouting(&RepoConfig{Channel: "C0STAR"}, nil)
	if got.Channel != "C0STAR" || !reflect.DeepEqual(got.Mentions, []string{ChannelMention}) {
		t.Errorf("got %+v; want channel C0STAR + @channel", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestResolveRouting`
Expected: FAIL — `resolveRouting`/`Resolved` undefined.

- [ ] **Step 3: Implement the resolver**

Create `internal/mappings/resolve.go`:

```go
package mappings

// Resolved is the effective routing config for one repository after merging
// the org/repo tier over the org/* tier.
type Resolved struct {
	Channel  string
	Mentions []string
}

// resolveRouting merges the wildcard (org/*) tier under the specific
// (org/repo) tier. At least one of star/repo must be non-nil. For each key,
// the most specific tier that sets it wins; an absent mentions key inherits,
// falling back to @channel only when no tier set mentions.
func resolveRouting(star, repo *RepoConfig) Resolved {
	var r Resolved
	if repo != nil && repo.Channel != "" {
		r.Channel = repo.Channel
	} else if star != nil && star.Channel != "" {
		r.Channel = star.Channel
	}
	switch {
	case repo != nil && repo.MentionsPresent:
		r.Mentions = append([]string(nil), repo.Mentions...)
	case star != nil && star.MentionsPresent:
		r.Mentions = append([]string(nil), star.Mentions...)
	default:
		r.Mentions = []string{ChannelMention}
	}
	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run TestResolveRouting`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/resolve.go internal/mappings/resolve_test.go
git commit -m "feat: per-repo routing resolver (org/repo over org/*)"
```

---

### Task 4: `Provider.Get` over tiers

Rewrite lookup to resolve `org/repo` over `org/*`, returning `store.ErrNotFound` when neither exists.

**Files:**
- Modify: `internal/mappings/provider.go` (`Get`; remove `resolveMentions`)
- Test: `internal/mappings/provider_test.go`

**Interfaces:**
- Consumes: `resolveRouting`, `Org`, `RepoConfig`, `splitRepo` (existing).
- Produces: `func (p *Provider) Get(ctx, repository string) (store.RepoMapping, error)` — unchanged signature; `NewProvider`/`Load`/`Digest` unchanged.

- [ ] **Step 1: Write the failing tests**

Replace the `Get`-related tests in `internal/mappings/provider_test.go` (the ones using `Org{Channel:..., Repositories:...}`) with tier-based ones. Keep `TestNewProvider_BehavesLikeLoad` but update its `Org` literal to the map form:

```go
func tierProvider() *mappings.Provider {
	return mappings.NewProvider(map[string]mappings.Org{
		"acme": {
			"api": {Channel: "C0API", Mentions: []string{"<@U1>"}, MentionsPresent: true},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
}

func TestGet_ExplicitRepo(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0API" || len(got.Mentions) != 1 || got.Mentions[0] != "<@U1>" {
		t.Errorf("got %+v; want C0API + [<@U1>]", got)
	}
}

func TestGet_WildcardFallback(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/unlisted")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0DEFAULT" {
		t.Errorf("channel = %q; want C0DEFAULT", got.SlackChannel)
	}
	if len(got.Mentions) != 1 || got.Mentions[0] != mappings.ChannelMention {
		t.Errorf("mentions = %v; want @channel default", got.Mentions)
	}
}

func TestGet_NoOrgOrNoTier(t *testing.T) {
	p := mappings.NewProvider(map[string]mappings.Org{
		"acme": {"api": {Channel: "C0API"}}, // no "*"
	}, nil)
	if _, err := p.Get(context.Background(), "acme/other"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound (no tier, no wildcard)", err)
	}
	if _, err := p.Get(context.Background(), "ghost/api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound (no org)", err)
	}
}
```

Ensure imports include `errors`, `context`, and `github.com/mptooling/notifycat/internal/store`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestGet`
Expected: FAIL — old `Get` references `o.Repositories`.

- [ ] **Step 3: Rewrite `Get` and drop `resolveMentions`**

In `internal/mappings/provider.go`, replace `Get` and delete the now-unused `resolveMentions`:

```go
// Get returns the resolved mapping for "org/repo": the org/repo tier merged
// over the org/* tier. Returns store.ErrNotFound when the org is unmapped or
// neither an explicit tier nor a wildcard tier matches.
func (p *Provider) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	org, repo, ok := splitRepo(repository)
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	o, ok := p.file.Mappings[org]
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	var repoPtr, starPtr *RepoConfig
	if rc, has := o[repo]; has {
		repoPtr = &rc
	}
	if sc, has := o[starKey]; has {
		starPtr = &sc
	}
	if repoPtr == nil && starPtr == nil {
		return store.RepoMapping{}, store.ErrNotFound
	}
	res := resolveRouting(starPtr, repoPtr)
	return store.RepoMapping{
		Repository:   repository,
		SlackChannel: res.Channel,
		Mentions:     res.Mentions,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run 'TestGet|TestNewProvider'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat: Provider.Get resolves org/repo over org/* tiers"
```

---

### Task 5: `Provider.Entries` over tiers

Emit one `Entry` per `org/repo` and per `org/*`, each carrying its *resolved* channel/mentions, so the existing validate/lock path works unchanged.

**Files:**
- Modify: `internal/mappings/provider.go` (`Entries`)
- Test: `internal/mappings/provider_test.go`

**Interfaces:**
- Consumes: `Org`, `RepoConfig`, `resolveRouting`, `Entry` (`{Org, Repo, Wildcard, Channel, Mentions}`).
- Produces: `func (p *Provider) Entries() []Entry` — one entry per tier; `Wildcard=true` for the `*` key; `Channel` is the resolved channel.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/provider_test.go`:

```go
func TestEntries_PerTierWithResolvedChannel(t *testing.T) {
	p := mappings.NewProvider(map[string]mappings.Org{
		"acme": {
			"web": {}, // inherits channel from "*"
			"api": {Channel: "C0API"},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
	entries := p.Entries()
	// deterministic order: explicit repos A→Z then wildcard last
	if len(entries) != 3 {
		t.Fatalf("entries = %d; want 3", len(entries))
	}
	if entries[0].Key() != "acme/api" || entries[0].Channel != "C0API" {
		t.Errorf("entries[0] = %+v; want acme/api C0API", entries[0])
	}
	if entries[1].Key() != "acme/web" || entries[1].Channel != "C0DEFAULT" {
		t.Errorf("entries[1] = %+v; want acme/web resolved C0DEFAULT", entries[1])
	}
	if !entries[2].Wildcard || entries[2].Key() != "acme/*" || entries[2].Channel != "C0DEFAULT" {
		t.Errorf("entries[2] = %+v; want acme/* C0DEFAULT", entries[2])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestEntries`
Expected: FAIL — old `Entries` references `o.Repositories`.

- [ ] **Step 3: Rewrite `Entries`**

In `internal/mappings/provider.go`, replace `Entries`:

```go
// Entries returns validation units in deterministic order: orgs A→Z, explicit
// repos within each org A→Z, the wildcard entry last. Each entry's Channel is
// the resolved channel (the tier's own, or inherited from org/*), so the
// validator and lock operate on what a webhook would actually route to.
func (p *Provider) Entries() []Entry {
	orgs := make([]string, 0, len(p.file.Mappings))
	for org := range p.file.Mappings {
		orgs = append(orgs, org)
	}
	sort.Strings(orgs)

	var out []Entry
	for _, org := range orgs {
		o := p.file.Mappings[org]
		var starPtr *RepoConfig
		if sc, has := o[starKey]; has {
			starPtr = &sc
		}
		repos := make([]string, 0, len(o))
		for k := range o {
			if k != starKey {
				repos = append(repos, k)
			}
		}
		sort.Strings(repos)
		for _, r := range repos {
			rc := o[r]
			res := resolveRouting(starPtr, &rc)
			out = append(out, Entry{Org: org, Repo: r, Channel: res.Channel, Mentions: res.Mentions})
		}
		if starPtr != nil {
			res := resolveRouting(starPtr, nil)
			out = append(out, Entry{Org: org, Wildcard: true, Channel: res.Channel, Mentions: res.Mentions})
		}
	}
	return out
}
```

- [ ] **Step 4: Run the whole mappings package**

Run: `go test ./internal/mappings/`
Expected: PASS (every test in the package now compiles and passes). If `lock_test.go`/`entry_test.go` reference the old `Org` shape, update those literals to the map form (entries themselves are unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat: Provider.Entries emits per-tier entries with resolved channel"
```

---

### Task 6: Wire the new `Org` type through `internal/config`

`config.Config.Mappings` / `fileSchema.Mappings` are `map[string]mappings.Org`; the underlying `Org` type changed, so the config decoder now parses the nested per-repo shape. Verify decoding and update fixtures.

**Files:**
- Modify: `internal/config/config_test.go` (the mappings fixture in `TestLoad_OverridesAndMappings`)
- Verify (likely no code change): `internal/config/config.go` (the `Mappings map[string]mappings.Org` field + `applyFileSchema` assignment)

**Interfaces:**
- Consumes: `mappings.Org` (now `map[string]RepoConfig`).
- Produces: `config.Config.Mappings` populated from the nested `mappings:` section; no signature changes.

- [ ] **Step 1: Update the config fixture to the per-repo shape (failing)**

In `internal/config/config_test.go`, change the `mappings:` block inside `TestLoad_OverridesAndMappings` from the old per-org form to per-repo tiers, and assert a tier:

```go
mappings:
  acme:
    web:
      channel: C0123ABCDE
```

and after `Load()`:

```go
	org, ok := cfg.Mappings["acme"]
	if !ok {
		t.Fatalf("Mappings missing acme: %+v", cfg.Mappings)
	}
	if org["web"].Channel != "C0123ABCDE" {
		t.Errorf("acme/web channel = %q; want C0123ABCDE", org["web"].Channel)
	}
```

- [ ] **Step 2: Run config tests**

Run: `go test ./internal/config/`
Expected: PASS if `config.go` needs no change (the field type flows through). If it FAILS to compile because `applyFileSchema` or the struct references a removed field, fix the assignment so `cfg.Mappings = fs.Mappings` and `fileSchema.Mappings` is `map[string]mappings.Org`.

- [ ] **Step 3: Confirm strict decode end to end**

Add a test asserting an unknown tier key in a full `config.yaml` is rejected:

```go
func TestLoad_RejectsUnknownTierKey(t *testing.T) {
	writeConfig(t, "mappings:\n  acme:\n    api:\n      channel: C0API\n      bogus: x\n")
	setSecrets(t)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for unknown tier key in mappings")
	}
}
```

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config
git commit -m "feat: config decodes per-repo mapping tiers"
```

---

### Task 7: Verify validation, lock, doctor, and the full module

No production code should need changing here — the `Entry` contract is preserved — but the integration must be confirmed and any fixtures that still build the old `Org` shape updated.

**Files:**
- Modify (fixtures only, as needed): `internal/app/integration_test.go`, `internal/doctor/doctor_test.go`, `cmd/notifycat-config/main_test.go`, `internal/validate/*_test.go`
- Verify (no change expected): `internal/app/app.go`, `internal/validate/validator.go`, `internal/mappingcli/*`

**Interfaces:**
- Consumes: `mappings.NewProvider(map[string]mappings.Org, *DigestConfig)` (unchanged), `Provider.Entries`/`Get` (Tasks 4-5).

- [ ] **Step 1: Build the whole module to find stale `Org` literals**

Run: `go build ./... 2>&1 | head -40`
Expected: compile errors only in test files or call sites that construct `mappings.Org{Channel:..., Repositories:...}`. List each.

- [ ] **Step 2: Update each stale fixture to the tier shape**

For every reported site, convert `mappings.Org{Channel: "C0X", Repositories: mappings.Repositories{List: []string{"web"}}}` to the map form `mappings.Org{"web": {Channel: "C0X"}}` (or `{"*": {Channel: "C0X"}}` where the old entry used `Repositories.All`). Apply mechanically until `go build ./...` is clean.

- [ ] **Step 3: Run the full suite**

Run: `go test -race ./...`
Expected: all packages PASS. Pay attention to `internal/app` (startup validation builds entries + writes `config.lock`) and `internal/validate` (wildcard expansion) — both should pass unchanged because `Entry` is unchanged.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test: update fixtures to per-repo mapping tiers"
```

---

### Task 8: Migration guide, example, and docs

Document the breaking schema change and update the per-repo example. (Version bump to `0.18` happens via the eventual `feat!` PR title; do not edit the manifest.)

**Files:**
- Create: `docs/0.18-per-repo-mappings-migration.md`
- Modify: `config.example.yaml` (mappings section → per-repo tiers)
- Modify: `docs/mappings.md` (schema → per-repo tiers + resolution rules)
- Modify: `docs/configuration.md` (the `### mappings` blurb)

**Interfaces:** none (docs).

- [ ] **Step 1: Rewrite the `config.example.yaml` mappings section**

Replace the per-org examples with per-repo tiers, preserving the commentary style. Show: an org with explicit repos + an `org/*` default (acme: api with its own channel, web inheriting, `*` as catch-all); an org that is whole-org via `*` only (beta); an org with `mentions: []` on a tier (legacy). Keep the leading comment that other config sections have defaults. Do not hard-wrap.

- [ ] **Step 2: Write `docs/0.18-per-repo-mappings-migration.md`**

Cover, each as a long unwrapped paragraph/table:
  - Intro: 0.18 makes mappings per-repository; manual, breaking; no converter.
  - The shape change: `org → {channel, mentions, repositories: [api, web]}` becomes `org → {api: {channel, mentions}, web: {channel, mentions}}`. A whole-org `repositories: "*"` becomes a single `"*": {channel, mentions}` tier.
  - Sharing a channel across an explicit allowlist now means repeating `channel:` on each repo tier (or using `"*"`, which ALSO routes unlisted repos — call this out explicitly as the trade-off).
  - The `mentions` behavior change: absent now means *inherit* (with `@channel` as the final fallback when no tier sets it); `[]` still pings nobody.
  - A worked before/after example.
  - Note that `notifycat-config validate` / `list` operate on the new shape unchanged.

- [ ] **Step 3: Update `docs/mappings.md` and `docs/configuration.md`**

In `docs/mappings.md`, replace the per-org schema description with the per-repo tier schema and the resolution rules (org/repo over org/*; mentions inherit; channel inheritance; `*` is both default-tier and catch-all). In `docs/configuration.md`, update the one-paragraph `### mappings` blurb to describe the per-repo shape and point to `mappings.md`. Do not hard-wrap.

- [ ] **Step 4: Verify docs reference the new shape**

Run: `grep -rn "repositories:" docs/ config.example.yaml | grep -v "0.18-per-repo" | grep -v "superpowers/"`
Expected: only intentional "before/old" references in the migration guide. Fix stragglers.

- [ ] **Step 5: Commit**

```bash
git add docs config.example.yaml
git commit -m "docs: per-repo mappings schema, example, and 0.18 migration guide"
```

---

### Task 9: Phase A verification

- [ ] **Step 1: Full local check**

Run: `just check`
Expected: vet/lint/vuln clean, all race tests pass, all binaries build.

- [ ] **Step 2: Boot smoke test with a per-repo config**

Create a scratch `config.yaml` with a per-repo `mappings:` block (one org, an explicit repo + `*`) and dummy secrets; boot `notifycat-server` from a directory with no `.env`, confirm it logs `listening` and validates/writes `config.lock`. (Use the Task 9 boot pattern from the 0.17 plan: build the binary, run from the scratch dir with `NOTIFYCAT_CONFIG_FILE` set, 4s timeout.)

- [ ] **Step 3: Confirm an old-shape config is rejected**

Boot with a `config.yaml` using the retired `repositories:` form and confirm `config.Load`/startup fails with a parse error (the per-repo decoder cannot read the old shape). This is the intended hard cutover.

---

## Self-Review

**Spec coverage (Phase A = the routing+schema half of the per-repo spec):**
- `org → {repo|*: {...}}` schema → Tasks 1, 2, 6.
- Deep-merge resolution (routing keys), most-specific-wins, mentions inherit→@channel → Tasks 3, 4.
- `*` as org-tier default AND catch-all → Tasks 3, 4 (resolver + Get), 2 (validation).
- Channel-required invariant, strict keys → Task 2.
- Per-`org/repo` validation + lock unchanged → Tasks 5, 7.
- Migration (breaking, manual) + docs → Task 8.
- **Per-repo behavioral override (reactions/reviews/digest) + handler/digest rewire → deferred to Phase B** (out of scope here; behavioral settings stay global).

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to Task N". Tasks 6/7 use build-then-fix-fixture steps because the exact stale-literal sites depend on current test contents; the conversions are spelled out mechanically.

**Type consistency:** `Org = map[string]RepoConfig` (Task 1) is consumed identically in parse (2), resolver (3), Get (4), Entries (5), config (6). `resolveRouting(star, repo *RepoConfig) Resolved` is defined in Task 3 and called in Tasks 4-5. `RepoConfig{Channel, Mentions, MentionsPresent}` fields are stable across all tasks. `starKey = "*"` is defined in Task 2 and used in Tasks 4-5. The `Entry` shape is unchanged, so `NewProvider`, validate, and lock signatures hold.
