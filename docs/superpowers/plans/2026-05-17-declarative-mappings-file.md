# Declarative mappings.yaml + per-entry lock — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `notifycat-mapping add/remove` plus the `github_slack_mapping` table with a declarative `mappings.yaml` (org-keyed, with `*` wildcards for whole-org coverage) and a per-entry `mappings.lock` cache, so the server boots from a file and revalidates only changed entries.

**Architecture:** New `internal/mappings` package owns the YAML schema, the in-memory `Provider` (exact-then-wildcard lookup), and the lock-file cache. The existing `internal/validate` package gains a no-fail-fast orchestrator that expands wildcards via a new `gh.ListOrgRepos` call and produces one `Report` per resolved repo. The CLI drops `add`/`remove`; `list` and `validate` both read from the file (validate writes the lock on success). `internal/app.Wire` loads the provider, runs cache-aware startup validation, and wires it through the existing `RepoMappings.Get`-shaped interface so PR handlers don't change. The `github_slack_mapping` table is dropped via a new migration.

**Tech Stack:** Go 1.22, `gopkg.in/yaml.v3` (already in `go.sum`), existing `gorm`/`goose` for the drop migration, standard library `crypto/sha256` + `encoding/json` for the lock.

**Spec:** https://github.com/mptooling/notifycat/issues/8

---

## File structure

**New files:**
- `internal/mappings/types.go` — `File`, `Org`, `Repositories` structs + `Repositories.UnmarshalYAML`.
- `internal/mappings/parse.go` — `Parse(io.Reader) (File, error)` + shape validation.
- `internal/mappings/provider.go` — `Provider` with `Load`, `Get`, `Entries`, exact-then-wildcard lookup, sorted output for `list`.
- `internal/mappings/entry.go` — `Entry` (validation unit) type + `Key()` + `Hash()` (canonical-JSON sha256).
- `internal/mappings/lock.go` — `Lock` JSON shape, `Read`/`Write`/`Diff`/`Merge`.
- `internal/mappings/types_test.go`, `internal/mappings/parse_test.go`, `internal/mappings/provider_test.go`, `internal/mappings/entry_test.go`, `internal/mappings/lock_test.go`.
- `internal/store/migrations/00003_drop_github_slack_mapping.sql` — drops the table.
- `internal/validate/runner.go` — `RunForEntries(ctx, entries, lister, v) []Report` (no-fail-fast wildcard expansion).
- `internal/validate/runner_test.go`.

**Modified files:**
- `internal/github/client.go` — add `ListOrgRepos(ctx, org) ([]string, error)` (paginated).
- `internal/github/client_test.go` — coverage for the new method.
- `internal/validate/deps.go` — shrink `MappingLookup` interface to just `Get`; add `OrgRepoLister` interface used by the runner.
- `internal/validate/validator.go` — remove `ValidateAll`; keep `Validate(ctx, repo)`.
- `internal/validate/validator_test.go` — drop `ValidateAll` tests.
- `internal/mappingcli/list.go` — rewrite to read the file via `Provider`.
- `internal/mappingcli/validate.go` — rewrite to use `Provider` + lock + `validate.RunForEntries`; add `--force`.
- `internal/mappingcli/run_test.go`, `validate_test.go` — adapt to new shape.
- `internal/mappingcli/add.go` — **delete**.
- `internal/mappingcli/remove.go` — **delete**.
- `cmd/notifycat-mapping/main.go` — drop `add`/`remove` subcommands; rewire dispatch to load the `Provider` + GH client; add `--force`.
- `cmd/notifycat-mapping/main_test.go` — drop add/remove tests; adapt dispatch shape.
- `internal/config/config.go` — add `MappingsFile` field (env `NOTIFYCAT_MAPPINGS_FILE`, default `./mappings.yaml`).
- `internal/config/config_test.go` — coverage for the new field.
- `internal/app/app.go` — replace `store.NewRepoMappings(db)` with `mappings.Load(path)` + startup validation via `RunForEntries`.
- `internal/app/integration_test.go`, `app_test.go` — adapt to the new wiring (build temporary mappings.yaml in tests).
- `internal/store/repo_mappings.go` — **delete** (its `Get` is replaced by `mappings.Provider.Get`; nothing else still uses it).
- `internal/store/store_test.go` — drop `RepoMappings` tests.
- `internal/store/models.go` — keep `RepoMapping` struct (still used as the data shape passed to handlers); delete `TableName()` + `RepoMapping`'s GORM tags.

**Files deliberately untouched (verify in execution):**
- `internal/pullrequest/*.go` — handlers consume `RepoMappings.Get`-shaped interface; only DI wiring changes.
- `internal/slack/*.go` — composer + client unchanged.
- `internal/githubhook/*.go` — webhook plumbing unchanged.

---

## Task 1 — Bootstrap: branch + package skeleton

**Files:**
- Create: `internal/mappings/doc.go`

- [ ] **Step 1: Create a topic branch off main**

```bash
git checkout main
git pull --ff-only
git checkout -b feature/declarative-mappings
```

- [ ] **Step 2: Create the package with a doc.go**

```go
// internal/mappings/doc.go
// Package mappings owns the declarative repository → Slack-channel
// configuration: parsing mappings.yaml, the in-memory Provider used at
// runtime, and the mappings.lock cache that records which entries have been
// validated. The package replaces the database-backed RepoMappings store.
package mappings
```

- [ ] **Step 3: Add yaml.v3 as a direct dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1 && go mod tidy`
Expected: `gopkg.in/yaml.v3` moves from `// indirect` to a direct require in `go.mod`.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/doc.go go.mod go.sum
git commit -m "feat(mappings): bootstrap internal/mappings package"
```

---

## Task 2 — `Repositories` type with dual-shape YAML unmarshal

`Repositories` is `"*"` XOR `[]string`. YAML accepts either; the type normalizes to `{All bool, List []string}` and enforces exclusivity.

**Files:**
- Create: `internal/mappings/types.go`
- Test: `internal/mappings/types_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/types_test.go
package mappings

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRepositories_UnmarshalYAML_Wildcard(t *testing.T) {
	var r Repositories
	if err := yaml.Unmarshal([]byte(`"*"`), &r); err != nil {
		t.Fatalf("unmarshal wildcard: %v", err)
	}
	if !r.All || len(r.List) != 0 {
		t.Errorf("wildcard parse: got %+v; want All=true List=nil", r)
	}
}

func TestRepositories_UnmarshalYAML_List(t *testing.T) {
	var r Repositories
	if err := yaml.Unmarshal([]byte(`["api", "web"]`), &r); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if r.All {
		t.Errorf("list shape set All=true")
	}
	if len(r.List) != 2 || r.List[0] != "api" || r.List[1] != "web" {
		t.Errorf("list parse: got %+v", r.List)
	}
}

func TestRepositories_UnmarshalYAML_RejectsStarInList(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`["api", "*"]`), &r)
	if err == nil || !strings.Contains(err.Error(), `"*"`) {
		t.Fatalf(`expected "*" rejection in list shape; got %v`, err)
	}
}

func TestRepositories_UnmarshalYAML_RejectsEmptyList(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`[]`), &r)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-list rejection; got %v", err)
	}
}

func TestRepositories_UnmarshalYAML_RejectsRandomString(t *testing.T) {
	var r Repositories
	err := yaml.Unmarshal([]byte(`"all"`), &r)
	if err == nil {
		t.Fatalf(`expected rejection of non-"*" string`)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mappings/...`
Expected: FAIL — `Repositories` does not exist.

- [ ] **Step 3: Implement the type**

```go
// internal/mappings/types.go
package mappings

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// File is the parsed mappings.yaml document.
type File struct {
	Mappings map[string]Org `yaml:"mappings"`
}

// Org is one organization's mapping: every configured repository in the org
// shares this channel and mentions list.
type Org struct {
	Channel      string       `yaml:"channel"`
	Mentions     []string     `yaml:"mentions"`
	Repositories Repositories `yaml:"repositories"`
}

// Repositories is "*" (whole org) XOR a non-empty list of bare repo names.
// The YAML accepts either shape; the in-memory representation normalizes.
type Repositories struct {
	All  bool
	List []string
}

// UnmarshalYAML decodes either the wildcard string "*" or a list of repo
// names. "*" inside a list, an empty list, or any other shape is rejected.
func (r *Repositories) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "*" {
			return fmt.Errorf("repositories: scalar must be \"*\"; got %q", node.Value)
		}
		r.All = true
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("repositories: list cannot be empty (use \"*\" for whole-org)")
		}
		items := make([]string, 0, len(node.Content))
		for _, c := range node.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("repositories: list entry must be a string")
			}
			if c.Value == "*" {
				return fmt.Errorf("repositories: \"*\" cannot appear inside a list (use \"*\" alone)")
			}
			items = append(items, c.Value)
		}
		r.List = items
		return nil
	default:
		return fmt.Errorf("repositories: expected scalar \"*\" or a list; got node kind %d", node.Kind)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappings/...`
Expected: PASS, all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/types.go internal/mappings/types_test.go
git commit -m "feat(mappings): add File/Org/Repositories types with YAML unmarshal"
```

---

## Task 3 — `Parse` with shape validation

Reads + validates `mappings.yaml`: known-fields strict, org-key regex, channel regex, mentions non-nil, no duplicate repo names within an org.

**Files:**
- Create: `internal/mappings/parse.go`
- Test: `internal/mappings/parse_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/parse_test.go
package mappings

import (
	"strings"
	"testing"
)

const validYAML = `
mappings:
  acme:
    channel: C0123ABCDE
    mentions: ["@alice", "@bob"]
    repositories:
      - api
      - web
  beta:
    channel: C0456FGHIJ
    mentions: []
    repositories: "*"
`

func TestParse_Valid(t *testing.T) {
	f, err := Parse(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Mappings) != 2 {
		t.Fatalf("orgs = %d; want 2", len(f.Mappings))
	}
	acme := f.Mappings["acme"]
	if acme.Channel != "C0123ABCDE" || len(acme.Mentions) != 2 || len(acme.Repositories.List) != 2 {
		t.Errorf("acme parsed wrong: %+v", acme)
	}
	if !f.Mappings["beta"].Repositories.All {
		t.Errorf("beta should be wildcard")
	}
}

func TestParse_UnknownTopLevelKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings: {}
something_else: true
`))
	if err == nil || !strings.Contains(err.Error(), "field") {
		t.Fatalf("expected unknown-field error; got %v", err)
	}
}

func TestParse_UnknownOrgKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["x"]
    typo_field: 1
`))
	if err == nil || !strings.Contains(err.Error(), "field") {
		t.Fatalf("expected unknown-field error; got %v", err)
	}
}

func TestParse_BadOrgKey(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  "bad org name":
    channel: C0123ABCDE
    mentions: []
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "org") {
		t.Fatalf("expected org-name error; got %v", err)
	}
}

func TestParse_BadChannel(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: not-a-channel
    mentions: []
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("expected channel error; got %v", err)
	}
}

func TestParse_NilMentionsRejected(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions:
    repositories: ["x"]
`))
	if err == nil || !strings.Contains(err.Error(), "mentions") {
		t.Fatalf("expected mentions error; got %v", err)
	}
}

func TestParse_DuplicateRepoInList(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["api", "api"]
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error; got %v", err)
	}
}

func TestParse_BadRepoName(t *testing.T) {
	_, err := Parse(strings.NewReader(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["bad/name"]
`))
	if err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("expected repo-name error; got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mappings/...`
Expected: FAIL — `Parse` does not exist.

- [ ] **Step 3: Implement `Parse`**

```go
// internal/mappings/parse.go
package mappings

import (
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

var (
	orgPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)
)

// Parse reads + validates the YAML document. Unknown keys and shape errors
// are returned as errors (the server fails fast at startup).
//
// Mentions must be a non-nil list (use [] for empty) so "absent" vs "empty"
// stays explicit.
func Parse(r io.Reader) (File, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("mappings: parse: %w", err)
	}
	if err := f.validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) validate() error {
	for org, o := range f.Mappings {
		if !orgPattern.MatchString(org) {
			return fmt.Errorf("mappings: org %q: invalid name (must match %s)", org, orgPattern)
		}
		if !channelPattern.MatchString(o.Channel) {
			return fmt.Errorf("mappings: org %q: invalid channel %q", org, o.Channel)
		}
		if o.Mentions == nil {
			return fmt.Errorf("mappings: org %q: mentions is required (use [] for empty)", org)
		}
		if !o.Repositories.All {
			seen := make(map[string]struct{}, len(o.Repositories.List))
			for _, repo := range o.Repositories.List {
				if !repoPattern.MatchString(repo) {
					return fmt.Errorf("mappings: org %q: invalid repository %q", org, repo)
				}
				if _, dup := seen[repo]; dup {
					return fmt.Errorf("mappings: org %q: duplicate repository %q", org, repo)
				}
				seen[repo] = struct{}{}
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappings/...`
Expected: PASS, all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/parse.go internal/mappings/parse_test.go
git commit -m "feat(mappings): parse mappings.yaml with shape validation"
```

---

## Task 4 — `Provider` with exact-then-wildcard lookup

`Provider` is the in-memory result of `Load(path)`. `Get(org/repo)` does exact-match-then-wildcard; `Entries()` returns validation units (one per explicit repo, one per wildcard org).

**Files:**
- Create: `internal/mappings/provider.go`
- Create: `internal/mappings/entry.go`
- Test: `internal/mappings/provider_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/provider_test.go
package mappings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func writeMappingsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "mappings.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestProvider_Get_ExactMatch(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("get acme/api: %v", err)
	}
	if got.Repository != "acme/api" || got.SlackChannel != "C0123ABCDE" || len(got.Mentions) != 2 {
		t.Errorf("get acme/api: %+v", got)
	}
}

func TestProvider_Get_Wildcard(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "beta/anything")
	if err != nil {
		t.Fatalf("get beta/anything: %v", err)
	}
	if got.Repository != "beta/anything" || got.SlackChannel != "C0456FGHIJ" {
		t.Errorf("wildcard get: %+v", got)
	}
}

func TestProvider_Get_NotFound(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err = p.Get(context.Background(), "other/repo")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown repo err = %v; want ErrNotFound", err)
	}
}

func TestProvider_Get_ExplicitOverridesWildcard(t *testing.T) {
	body := `
mappings:
  acme:
    channel: C0000WILD
    mentions: []
    repositories: "*"
  acme_explicit:
    channel: C1111EXACT
    mentions: []
    repositories: ["api"]
`
	// Same org name not allowed (map key); ensure precedence is documented
	// via a different scenario: a wildcard org and a webhook for an unrelated
	// org with same suffix should not collide.
	_ = body
	// Precedence is exercised end-to-end in validate_test once both shapes coexist.
	t.Skip("exact-vs-wildcard precedence within one org is impossible by schema (map key uniqueness)")
}

func TestProvider_Entries(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entries := p.Entries()
	// acme has 2 explicit repos + beta has 1 wildcard = 3 entries
	if len(entries) != 3 {
		t.Fatalf("entries = %d; want 3", len(entries))
	}
	keys := make(map[string]bool)
	for _, e := range entries {
		keys[e.Key()] = true
	}
	for _, want := range []string{"acme/api", "acme/web", "beta/*"} {
		if !keys[want] {
			t.Errorf("missing entry %q; got %v", want, keys)
		}
	}
}

func TestProvider_Load_BadFile(t *testing.T) {
	_, err := Load("/no/such/path/mappings.yaml")
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mappings/...`
Expected: FAIL — `Load`, `Provider`, `Entry` undefined.

- [ ] **Step 3: Implement `Entry` and `Provider`**

```go
// internal/mappings/entry.go
package mappings

// Entry is one validation unit: an explicit (org, repo) pair or an
// (org, "*") wildcard. Each entry has its own hash in mappings.lock.
type Entry struct {
	Org      string
	Repo     string // empty when Wildcard is true
	Wildcard bool
	Channel  string
	Mentions []string
}

// Key returns the lock-file key for the entry: "org/repo" or "org/*".
func (e Entry) Key() string {
	if e.Wildcard {
		return e.Org + "/*"
	}
	return e.Org + "/" + e.Repo
}
```

```go
// internal/mappings/provider.go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappings/...`
Expected: PASS (the precedence test is `t.Skip`'d on purpose — schema makes the case unreachable).

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/entry.go internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat(mappings): add Provider with exact-then-wildcard lookup"
```

---

## Task 5 — Canonical hash for entries

Per-entry hash input: canonical JSON of `{org, repo|*, channel, mentions(sorted)}`, sha256.

**Files:**
- Modify: `internal/mappings/entry.go`
- Test: `internal/mappings/entry_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/entry_test.go
package mappings

import "testing"

func TestEntry_Hash_StableAcrossMentionReorder(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@y", "@x"}}
	if a.Hash() != b.Hash() {
		t.Errorf("hash should be stable across mention reorder: %s vs %s", a.Hash(), b.Hash())
	}
}

func TestEntry_Hash_DiffersOnChannel(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C2", Mentions: []string{}}
	if a.Hash() == b.Hash() {
		t.Errorf("hash must differ across channel change")
	}
}

func TestEntry_Hash_DiffersOnWildcardVsExplicit(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	b := Entry{Org: "acme", Wildcard: true, Channel: "C1", Mentions: []string{}}
	if a.Hash() == b.Hash() {
		t.Errorf("wildcard hash must differ from explicit hash")
	}
}

func TestEntry_Hash_DiffersOnMentions(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x"}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	if a.Hash() == b.Hash() {
		t.Errorf("hash must differ when mentions change")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mappings/ -run Hash`
Expected: FAIL — `Entry.Hash` undefined.

- [ ] **Step 3: Implement `Hash`**

Append to `internal/mappings/entry.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Hash is the cache key for an entry: sha256 over canonical JSON, with
// mentions sorted so reordering in YAML is a no-op for the cache.
func (e Entry) Hash() string {
	mentions := append([]string(nil), e.Mentions...)
	sort.Strings(mentions)
	repo := e.Repo
	if e.Wildcard {
		repo = "*"
	}
	payload := struct {
		Org      string   `json:"org"`
		Repo     string   `json:"repo"`
		Channel  string   `json:"channel"`
		Mentions []string `json:"mentions"`
	}{e.Org, repo, e.Channel, mentions}
	b, _ := json.Marshal(payload) //nolint:errcheck // marshaling fixed struct cannot fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappings/ -run Hash`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/entry.go internal/mappings/entry_test.go
git commit -m "feat(mappings): add stable per-entry sha256 hash for the lock"
```

---

## Task 6 — Lock file: Read, Write, Diff, Merge

Lock JSON: `{version, entries: {key → {sha256, validated_at}}}`. `Read` is tolerant — malformed → empty + error so caller can log a warning.

**Files:**
- Create: `internal/mappings/lock.go`
- Test: `internal/mappings/lock_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/lock_test.go
package mappings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLock_WriteThenRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mappings.lock")
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	want := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "abc", ValidatedAt: now},
			"beta/*":   {SHA256: "def", ValidatedAt: now},
		},
	}
	if err := WriteLock(p, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadLock(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries["acme/api"].SHA256 != "abc" {
		t.Errorf("round trip wrong: %+v", got)
	}
}

func TestLock_Read_Missing(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLock(filepath.Join(dir, "no.lock"))
	if err != nil {
		t.Fatalf("missing should not error; got %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("missing should produce empty lock; got %+v", got)
	}
}

func TestLock_Read_Malformed_ReturnsEmptyAndError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.lock")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := ReadLock(p)
	if err == nil {
		t.Fatal("malformed should return an error so caller can warn")
	}
	if len(got.Entries) != 0 {
		t.Errorf("malformed should produce empty lock; got %+v", got)
	}
}

func TestDiffEntries(t *testing.T) {
	current := []Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}}, // new vs lock
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
	}
	lock := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: current[0].Hash()},
			"beta/*":   {SHA256: "stale-different-hash"}, // changed
			"old/dead": {SHA256: "x"},                    // stale
		},
	}
	d := DiffEntries(current, lock)
	needs := make(map[string]bool)
	for _, e := range d.Needs {
		needs[e.Key()] = true
	}
	if !needs["acme/web"] || !needs["beta/*"] {
		t.Errorf("Needs should include new (acme/web) and changed (beta/*); got %v", needs)
	}
	if needs["acme/api"] {
		t.Errorf("Needs should not include unchanged (acme/api)")
	}
	stale := make(map[string]bool)
	for _, k := range d.Stale {
		stale[k] = true
	}
	if !stale["old/dead"] || len(stale) != 1 {
		t.Errorf("Stale should be [old/dead]; got %v", stale)
	}
}

func TestMergeLock(t *testing.T) {
	old := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "keep"},
			"old/dead": {SHA256: "x"},
		},
	}
	validated := map[string]LockEntry{
		"acme/web": {SHA256: "new"},
	}
	got := MergeLock(old, validated, []string{"old/dead"})
	if _, ok := got.Entries["old/dead"]; ok {
		t.Error("stale entry should be dropped")
	}
	if got.Entries["acme/api"].SHA256 != "keep" {
		t.Error("unchanged entry should remain")
	}
	if got.Entries["acme/web"].SHA256 != "new" {
		t.Error("validated entry should be added")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mappings/ -run Lock`
Expected: FAIL — types/functions undefined.

- [ ] **Step 3: Implement the lock module**

```go
// internal/mappings/lock.go
package mappings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// LockFileComment is the human-facing warning baked into every written lock.
const LockFileComment = "DO NOT EDIT — regenerated by notifycat on validation"

// LockVersion is the current lock-file schema version.
const LockVersion = 1

// Lock is the on-disk validation cache.
type Lock struct {
	Comment string               `json:"_comment,omitempty"`
	Version int                  `json:"version"`
	Entries map[string]LockEntry `json:"entries"`
}

// LockEntry records the hash and validation timestamp for one entry.
type LockEntry struct {
	SHA256      string    `json:"sha256"`
	ValidatedAt time.Time `json:"validated_at"`
}

// Diff is the result of DiffEntries: entries needing validation + lock keys
// that should be dropped on the next write.
type Diff struct {
	Needs []Entry
	Stale []string
}

// ReadLock parses path. A missing file returns an empty Lock with no error.
// A malformed file returns an empty Lock and an error so the caller can warn
// and continue.
func ReadLock(path string) (Lock, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied configuration
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Lock{Version: LockVersion, Entries: map[string]LockEntry{}}, nil
		}
		return Lock{Entries: map[string]LockEntry{}}, fmt.Errorf("mappings: read lock %s: %w", path, err)
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return Lock{Entries: map[string]LockEntry{}}, fmt.Errorf("mappings: parse lock %s: %w", path, err)
	}
	if l.Entries == nil {
		l.Entries = map[string]LockEntry{}
	}
	return l, nil
}

// WriteLock writes the lock atomically (write to tmp + rename).
func WriteLock(path string, l Lock) error {
	l.Comment = LockFileComment
	l.Version = LockVersion
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("mappings: marshal lock: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("mappings: write lock tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("mappings: rename lock: %w", err)
	}
	return nil
}

// DiffEntries compares the current entries against the lock and returns
// which entries need validation (new or hash-changed) and which lock keys
// are stale (present in the lock but not in the file).
func DiffEntries(current []Entry, lock Lock) Diff {
	currentKeys := make(map[string]struct{}, len(current))
	var needs []Entry
	for _, e := range current {
		key := e.Key()
		currentKeys[key] = struct{}{}
		prior, ok := lock.Entries[key]
		if !ok || prior.SHA256 != e.Hash() {
			needs = append(needs, e)
		}
	}
	var stale []string
	for k := range lock.Entries {
		if _, ok := currentKeys[k]; !ok {
			stale = append(stale, k)
		}
	}
	return Diff{Needs: needs, Stale: stale}
}

// MergeLock returns a new Lock built from `old` by adding the `validated`
// entries and dropping the keys in `drop`.
func MergeLock(old Lock, validated map[string]LockEntry, drop []string) Lock {
	out := Lock{Version: LockVersion, Entries: map[string]LockEntry{}}
	for k, v := range old.Entries {
		out.Entries[k] = v
	}
	for _, k := range drop {
		delete(out.Entries, k)
	}
	for k, v := range validated {
		out.Entries[k] = v
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappings/...`
Expected: PASS, all lock tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/lock.go internal/mappings/lock_test.go
git commit -m "feat(mappings): add per-entry lock with Read/Write/Diff/Merge"
```

---

## Task 7 — `gh.ListOrgRepos` (paginated)

Adds the only GitHub call the validator needs for `*` expansion.

**Files:**
- Modify: `internal/github/client.go` (append method + helpers)
- Modify: `internal/github/client_test.go` (new tests)

- [ ] **Step 1: Read the existing client to mirror the style**

Run: `wc -l internal/github/client.go internal/github/client_test.go`
Reference: `ListHookEvents` (`internal/github/client.go:74`) for pagination-less pattern; we'll add pagination here.

- [ ] **Step 2: Write the failing tests**

Append to `internal/github/client_test.go`:

```go
func TestListOrgRepos_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/acme/repos" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"name":"api"},{"name":"web"}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), "tok", WithBaseURL(srv.URL))
	got, err := c.ListOrgRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("got %v", got)
	}
}

func TestListOrgRepos_FollowsLinkHeader(t *testing.T) {
	var page atomic.Int32
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch page.Add(1) {
		case 1:
			w.Header().Set("Link", `<`+base+`/orgs/acme/repos?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"name":"api"}]`))
		default:
			_, _ = w.Write([]byte(`[{"name":"web"}]`))
		}
	}))
	defer srv.Close()
	base = srv.URL

	c := NewClient(srv.Client(), "tok", WithBaseURL(srv.URL))
	got, err := c.ListOrgRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("expected [api, web]; got %v", got)
	}
}

func TestListOrgRepos_Non2xxIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), "tok", WithBaseURL(srv.URL))
	_, err := c.ListOrgRepos(context.Background(), "acme")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("want APIError 404; got %T %v", err, err)
	}
}
```

If `errors`/`sync/atomic` aren't already imported in `client_test.go`, add them.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/github/...`
Expected: FAIL — `ListOrgRepos` undefined.

- [ ] **Step 4: Implement `ListOrgRepos`**

Append to `internal/github/client.go`:

```go
import "net/http" // already imported

// ListOrgRepos returns the names of every repository in the org, following
// GitHub's Link header for pagination. Empty result is a normal outcome
// (org with no repos or all repos filtered by token scope).
func (c *Client) ListOrgRepos(ctx context.Context, org string) ([]string, error) {
	next := fmt.Sprintf("/orgs/%s/repos?per_page=100", url.PathEscape(org))
	var names []string
	for next != "" {
		page, nextURL, err := c.listOrgReposPage(ctx, next)
		if err != nil {
			return nil, err
		}
		names = append(names, page...)
		next = nextURL
	}
	return names, nil
}

func (c *Client) listOrgReposPage(ctx context.Context, target string) ([]string, string, error) {
	reqURL := target
	if strings.HasPrefix(target, "/") {
		reqURL = c.baseURL + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("github: build list-org-repos request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	//nolint:gosec // baseURL operator-controlled
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("github: list-org-repos: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBytes int64 = defaultMaxRespMiB << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, "", fmt.Errorf("github: list-org-repos: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &APIError{Method: "list-org-repos", Status: resp.StatusCode, Message: extractMessage(body)}
	}

	var page []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("github: list-org-repos: decode: %w", err)
	}
	out := make([]string, 0, len(page))
	for _, r := range page {
		out = append(out, r.Name)
	}
	return out, parseNextLink(resp.Header.Get("Link")), nil
}

// parseNextLink extracts the rel="next" URL from a GitHub Link header.
// Returns "" when no next page is advertised.
func parseNextLink(header string) string {
	for _, segment := range strings.Split(header, ",") {
		segment = strings.TrimSpace(segment)
		if !strings.Contains(segment, `rel="next"`) {
			continue
		}
		open := strings.IndexByte(segment, '<')
		close := strings.IndexByte(segment, '>')
		if open >= 0 && close > open {
			return segment[open+1 : close]
		}
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/github/...`
Expected: PASS, all three new tests plus existing.

- [ ] **Step 6: Commit**

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "feat(github): add ListOrgRepos with Link-header pagination"
```

---

## Task 8 — `validate.RunForEntries` orchestrator (no-fail-fast wildcard expansion)

Walks `[]Entry`, expands `*` via the GH lister, runs the existing per-mapping checks, collects every Report. Used by both server startup and CLI `validate`.

**Files:**
- Modify: `internal/validate/deps.go` (add `OrgRepoLister` interface)
- Create: `internal/validate/runner.go`
- Create: `internal/validate/runner_test.go`

- [ ] **Step 1: Add the `OrgRepoLister` interface**

Append to `internal/validate/deps.go`:

```go
// OrgRepoLister enumerates a GitHub org's repositories. Used to expand "*"
// at validate time. May be nil; the runner reports a skip in that case.
type OrgRepoLister interface {
	ListOrgRepos(ctx context.Context, org string) ([]string, error)
}
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/validate/runner_test.go
package validate

import (
	"context"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/store"
)

type stubLookup struct {
	got store.RepoMapping
}

func (s *stubLookup) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	if repository != s.got.Repository {
		return store.RepoMapping{}, store.ErrNotFound
	}
	return s.got, nil
}

type stubLister struct {
	repos []string
	err   error
}

func (s *stubLister) ListOrgRepos(_ context.Context, _ string) ([]string, error) {
	return s.repos, s.err
}

// stubValidator satisfies the SingleValidator interface defined alongside the
// runner: tests assert which repos were validated.
type stubValidator struct {
	calls []string
	err   func(string) bool // returns true → produce failing report
}

func (s *stubValidator) Validate(_ context.Context, repository string) Report {
	s.calls = append(s.calls, repository)
	if s.err != nil && s.err(repository) {
		return Report{Repository: repository, Checks: []CheckResult{{Name: "x", Status: StatusFail, Detail: "boom"}}}
	}
	return Report{Repository: repository, Checks: []CheckResult{{Name: "x", Status: StatusOK, Detail: "ok"}}}
}

func TestRunForEntries_ExplicitOnly(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, nil, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if sv.calls[0] != "acme/api" || sv.calls[1] != "acme/web" {
		t.Errorf("calls = %v", sv.calls)
	}
}

func TestRunForEntries_WildcardExpansion(t *testing.T) {
	entries := []mappings.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	lister := &stubLister{repos: []string{"r1", "r2", "r3"}}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, lister, sv)
	if len(reports) != 3 {
		t.Fatalf("reports = %d; want 3", len(reports))
	}
	want := []string{"beta/r1", "beta/r2", "beta/r3"}
	for i, w := range want {
		if sv.calls[i] != w {
			t.Errorf("call[%d] = %q; want %q", i, sv.calls[i], w)
		}
	}
}

func TestRunForEntries_WildcardWithoutLister_SkipsButReports(t *testing.T) {
	entries := []mappings.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	reports := RunForEntries(context.Background(), entries, nil, &stubValidator{})
	if len(reports) != 1 {
		t.Fatalf("reports = %d; want 1", len(reports))
	}
	r := reports[0]
	if r.Repository != "beta/*" || len(r.Checks) != 1 || r.Checks[0].Status != StatusSkip {
		t.Errorf("expected single skip on beta/*; got %+v", r)
	}
}

func TestRunForEntries_ListerError_BecomesFailingReportAndContinues(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
	}
	lister := &stubLister{err: errors.New("rate-limited")}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, lister, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if reports[0].Repository != "beta/*" || reports[0].OK() {
		t.Errorf("first report should be failing beta/*; got %+v", reports[0])
	}
	if reports[1].Repository != "acme/api" || !reports[1].OK() {
		t.Errorf("second report should be OK acme/api; got %+v", reports[1])
	}
}

func TestRunForEntries_PerRepoFailureDoesNotAbort(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{err: func(r string) bool { return r == "acme/api" }}
	reports := RunForEntries(context.Background(), entries, nil, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if reports[0].OK() || !reports[1].OK() {
		t.Errorf("expected first fail, second ok: %+v", reports)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/validate/ -run RunForEntries`
Expected: FAIL — `RunForEntries`, `SingleValidator` undefined.

- [ ] **Step 4: Implement `RunForEntries`**

```go
// internal/validate/runner.go
package validate

import (
	"context"

	"github.com/mptooling/notifycat/internal/mappings"
)

// SingleValidator runs the per-mapping checks for one repository.
// *Validator satisfies this; tests inject a stub.
type SingleValidator interface {
	Validate(ctx context.Context, repository string) Report
}

// RunForEntries validates every entry, expanding wildcards through `lister`.
// Errors at any level become failing/skipped reports — the loop never aborts.
// Reports are returned in entry order; wildcard-expanded repos are emitted
// in the order returned by the lister.
func RunForEntries(ctx context.Context, entries []mappings.Entry, lister OrgRepoLister, v SingleValidator) []Report {
	out := make([]Report, 0, len(entries))
	for _, e := range entries {
		if !e.Wildcard {
			out = append(out, v.Validate(ctx, e.Org+"/"+e.Repo))
			continue
		}
		if lister == nil {
			out = append(out, Report{
				Repository: e.Key(),
				Checks: []CheckResult{
					skip("org-expand", "no GitHub token configured; cannot expand \"*\""),
				},
			})
			continue
		}
		repos, err := lister.ListOrgRepos(ctx, e.Org)
		if err != nil {
			out = append(out, Report{
				Repository: e.Key(),
				Checks: []CheckResult{
					{Name: "org-expand", Status: StatusFail, Detail: "could not list org repos: " + err.Error()},
				},
			})
			continue
		}
		for _, repo := range repos {
			out = append(out, v.Validate(ctx, e.Org+"/"+repo))
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/validate/...`
Expected: PASS, all five new tests plus existing validator tests.

- [ ] **Step 6: Commit**

```bash
git add internal/validate/runner.go internal/validate/runner_test.go internal/validate/deps.go
git commit -m "feat(validate): add RunForEntries with no-fail-fast wildcard expansion"
```

---

## Task 9 — Shrink `validate.MappingLookup` to just `Get`; drop `ValidateAll`

Now that the runner orchestrates the full sweep, `Validator` only needs `Get`. `ValidateAll` is gone.

**Files:**
- Modify: `internal/validate/deps.go`
- Modify: `internal/validate/validator.go`
- Modify: `internal/validate/validator_test.go`

- [ ] **Step 1: Update the test file first (TDD)**

Remove or update any test of `Validator.ValidateAll` (`internal/validate/validator_test.go` — search for `ValidateAll`). The runner_test.go from Task 8 now covers the multi-mapping flow.

Run: `grep -n "ValidateAll" internal/validate/`
Expected: matches only in `validator.go` (to be deleted) and (possibly) `validator_test.go`. Delete those test cases.

- [ ] **Step 2: Shrink the interface**

Edit `internal/validate/deps.go`:

```go
// MappingLookup reads a single repository → channel mapping by full repo
// path ("org/repo"). Returns store.ErrNotFound when nothing matches.
type MappingLookup interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}
```

(Remove the `List` method from the interface.)

- [ ] **Step 3: Delete `Validator.ValidateAll`**

Edit `internal/validate/validator.go` — remove `ValidateAll` and its helpers that are no longer referenced. Keep `Validate(ctx, repository)`, `mappingLookupFailure`, `validateMapping`, `slackChecks`.

- [ ] **Step 4: Build + test**

Run: `go test ./internal/validate/...`
Expected: PASS — `Validate` still works; `ValidateAll` no longer in surface.

Run: `go build ./...`
Expected: FAIL — `internal/mappingcli/validate.go` still calls `ValidateAll`. That's fixed in Task 11.

- [ ] **Step 5: Commit (allow temporary build break — fixed in Task 11)**

```bash
git add internal/validate/deps.go internal/validate/validator.go internal/validate/validator_test.go
git commit -m "refactor(validate): shrink MappingLookup to Get; drop ValidateAll"
```

> Note: the repo will not `go build` cleanly until Task 11 lands. Tasks 9–11 are tightly coupled; if executing inline, run them back-to-back without pushing.

---

## Task 10 — Config: `MappingsFile` + `MappingsLockFile`

Wires the file paths into the config.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestLoad_MappingsFile_Defaults(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	t.Setenv("SLACK_BOT_TOKEN", "x")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MappingsFile != "./mappings.yaml" {
		t.Errorf("default = %q; want ./mappings.yaml", cfg.MappingsFile)
	}
	if cfg.MappingsLockFile != "./mappings.lock" {
		t.Errorf("default lock = %q; want ./mappings.lock", cfg.MappingsLockFile)
	}
}

func TestLoad_MappingsFile_Override(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "x")
	t.Setenv("SLACK_BOT_TOKEN", "x")
	t.Setenv("NOTIFYCAT_MAPPINGS_FILE", "/etc/notifycat/m.yaml")
	t.Setenv("NOTIFYCAT_MAPPINGS_LOCK_FILE", "/etc/notifycat/m.lock")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MappingsFile != "/etc/notifycat/m.yaml" || cfg.MappingsLockFile != "/etc/notifycat/m.lock" {
		t.Errorf("override: %+v", cfg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL — fields undefined.

- [ ] **Step 3: Add fields to `Config`**

Edit `internal/config/config.go`, inside `Config`:

```go
	MappingsFile     string `env:"NOTIFYCAT_MAPPINGS_FILE" envDefault:"./mappings.yaml"`
	MappingsLockFile string `env:"NOTIFYCAT_MAPPINGS_LOCK_FILE" envDefault:"./mappings.lock"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/...`
Expected: PASS, both new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add NOTIFYCAT_MAPPINGS_FILE and lock-file path"
```

---

## Task 11 — Rewrite `mappingcli.Validate` to use Provider + lock + runner

CLI-level `validate` use case: read file, read lock, diff, run only what's needed (unless `--force`), write lock on success, render reports.

**Files:**
- Modify: `internal/mappingcli/validate.go`
- Modify: `internal/mappingcli/validate_test.go`

- [ ] **Step 1: Delete `internal/mappingcli/add.go` and `remove.go`**

```bash
git rm internal/mappingcli/add.go internal/mappingcli/remove.go
```

- [ ] **Step 2: Replace `validate.go` with the file-driven version**

```go
// internal/mappingcli/validate.go
package mappingcli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/validate"
)

// MappingsValidator is the validate use case the cmd binary depends on.
type MappingsValidator interface {
	Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int
}

// ValidatorRunner is the slim surface RunForEntries needs (a SingleValidator).
type ValidatorRunner interface {
	Validate(ctx context.Context, repository string) validate.Report
}

// LockStore reads + writes the per-entry lock cache.
type LockStore interface {
	Read() (mappings.Lock, error)
	Write(mappings.Lock) error
}

type mappingsValidator struct {
	provider *mappings.Provider
	runner   ValidatorRunner
	lister   validate.OrgRepoLister
	lock     LockStore
	now      func() time.Time
}

// NewMappingsValidator wires the production validator: HTTP-backed Slack and
// (optional) GitHub clients, the on-disk lock store, and the file-based
// provider already loaded by the caller.
func NewMappingsValidator(p *mappings.Provider, cfg config.Config) MappingsValidator {
	hc := &http.Client{Timeout: 10 * time.Second}
	s := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var gh *github.Client
	if cfg.GitHubToken.Reveal() != "" {
		gh = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	v := validate.NewValidator(p, s, gh)
	var lister validate.OrgRepoLister
	if gh != nil {
		lister = gh
	}
	return &mappingsValidator{
		provider: p,
		runner:   v,
		lister:   lister,
		lock:     fileLockStore{path: cfg.MappingsLockFile},
		now:      time.Now,
	}
}

// Validate executes the use case: it picks the targeted, full+cached, or
// full+forced path based on inputs, prints reports, updates the lock on
// success, and returns the CLI exit code (0 OK, 1 any failure).
func (v *mappingsValidator) Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int {
	if target != "" {
		return v.runTargeted(ctx, target, stdout)
	}
	return v.runFull(ctx, force, stdout, stderr)
}

func (v *mappingsValidator) runTargeted(ctx context.Context, target string, stdout io.Writer) int {
	report := v.runner.Validate(ctx, target)
	code := renderReports([]validate.Report{report}, stdout)
	// Update only this entry in the lock when the run passed.
	if code == 0 {
		v.updateLockFor(target)
	}
	return code
}

func (v *mappingsValidator) runFull(ctx context.Context, force bool, stdout, stderr io.Writer) int {
	entries := v.provider.Entries()
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no mappings to validate; populate mappings.yaml")
		return 0
	}

	lock, err := v.lock.Read()
	if err != nil {
		fmt.Fprintln(stderr, "warn:", err)
	}

	var toValidate []mappings.Entry
	var staleKeys []string
	if force {
		toValidate = entries
		// On --force we treat the lock as empty; still drop stale (no-op when force).
		staleKeys = nil
	} else {
		d := mappings.DiffEntries(entries, lock)
		toValidate = d.Needs
		staleKeys = d.Stale
	}

	if len(toValidate) == 0 {
		// Nothing changed; just prune stale and re-write if needed.
		if len(staleKeys) > 0 {
			updated := mappings.MergeLock(lock, nil, staleKeys)
			if err := v.lock.Write(updated); err != nil {
				fmt.Fprintln(stderr, "warn: write lock:", err)
			}
		}
		fmt.Fprintln(stdout, "all entries already validated (lock matches)")
		return 0
	}

	reports := validate.RunForEntries(ctx, toValidate, v.lister, v.runner)
	code := renderReports(reports, stdout)

	// Build the validated set from passing reports → entries by key lookup.
	passing := passingEntryKeys(toValidate, reports)
	newEntries := make(map[string]mappings.LockEntry, len(passing))
	now := v.now()
	for _, e := range toValidate {
		if _, ok := passing[e.Key()]; ok {
			newEntries[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: now}
		}
	}
	updated := mappings.MergeLock(lock, newEntries, staleKeys)
	if err := v.lock.Write(updated); err != nil {
		fmt.Fprintln(stderr, "warn: write lock:", err)
	}
	return code
}

func (v *mappingsValidator) updateLockFor(target string) {
	for _, e := range v.provider.Entries() {
		// Targeted lookup only updates explicit entries that match the target
		// directly; a target inside a wildcard org doesn't touch the org's
		// wildcard hash (org-level validation still requires `--force` or an
		// edit to revalidate).
		if e.Wildcard {
			continue
		}
		if e.Org+"/"+e.Repo != target {
			continue
		}
		lock, err := v.lock.Read()
		if err != nil {
			return // best-effort
		}
		lock = mappings.MergeLock(lock, map[string]mappings.LockEntry{
			e.Key(): {SHA256: e.Hash(), ValidatedAt: v.now()},
		}, nil)
		_ = v.lock.Write(lock)
		return
	}
}

// passingEntryKeys maps each entry's reports back to its key. For wildcard
// entries every expanded repo must pass for the wildcard hash to be banked.
func passingEntryKeys(entries []mappings.Entry, reports []validate.Report) map[string]struct{} {
	byRepo := make(map[string]validate.Report, len(reports))
	for _, r := range reports {
		byRepo[r.Repository] = r
	}
	out := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if !e.Wildcard {
			if r, ok := byRepo[e.Org+"/"+e.Repo]; ok && r.OK() {
				out[e.Key()] = struct{}{}
			}
			continue
		}
		// Wildcard: pass only if the org-expand report didn't fail AND every
		// expanded repo passed. We rely on the org-expand report being keyed
		// at e.Key() iff expansion itself errored or skipped.
		if r, ok := byRepo[e.Key()]; ok && !r.OK() {
			continue // expansion failed or skipped
		}
		allOK := true
		prefix := e.Org + "/"
		for repo, r := range byRepo {
			if repo == e.Key() {
				continue
			}
			if len(repo) > len(prefix) && repo[:len(prefix)] == prefix {
				if !r.OK() {
					allOK = false
					break
				}
			}
		}
		if allOK {
			out[e.Key()] = struct{}{}
		}
	}
	return out
}

// fileLockStore reads/writes the lock JSON at a fixed path. Production use.
type fileLockStore struct{ path string }

func (s fileLockStore) Read() (mappings.Lock, error)  { return mappings.ReadLock(s.path) }
func (s fileLockStore) Write(l mappings.Lock) error  { return mappings.WriteLock(s.path, l) }

func renderReports(reports []validate.Report, stdout io.Writer) int {
	allOK := true
	for i, r := range reports {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "%s\n", r.Repository)
		for _, c := range r.Checks {
			fmt.Fprintf(stdout, "  %-4s  %-16s  %s\n", c.Status, c.Name, c.Detail)
		}
		if !r.OK() {
			allOK = false
		}
	}
	if !allOK {
		return 1
	}
	return 0
}
```

- [ ] **Step 3: Rewrite `validate_test.go` against the new shape**

```go
// internal/mappingcli/validate_test.go
package mappingcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/validate"
)

type memLock struct{ l mappings.Lock }

func (m *memLock) Read() (mappings.Lock, error) { return m.l, nil }
func (m *memLock) Write(l mappings.Lock) error  { m.l = l; return nil }

type stubRunner struct {
	fail map[string]bool
	got  []string
}

func (s *stubRunner) Validate(_ context.Context, repository string) validate.Report {
	s.got = append(s.got, repository)
	status := validate.StatusOK
	detail := "ok"
	if s.fail[repository] {
		status, detail = validate.StatusFail, "boom"
	}
	return validate.Report{Repository: repository, Checks: []validate.CheckResult{{Name: "x", Status: status, Detail: detail}}}
}

func loadTestProvider(t *testing.T, body string) *mappings.Provider {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	prov, err := mappings.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return prov
}

func newValidatorForTest(t *testing.T, body string, runner ValidatorRunner) (*mappingsValidator, *memLock) {
	t.Helper()
	prov := loadTestProvider(t, body)
	lock := &memLock{l: mappings.Lock{Version: 1, Entries: map[string]mappings.LockEntry{}}}
	return &mappingsValidator{
		provider: prov,
		runner:   runner,
		lister:   nil,
		lock:     lock,
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}, lock
}

const explicitYAML = `
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["api", "web"]
`

func TestValidate_Full_PopulatesLockOnSuccess(t *testing.T) {
	v, lock := newValidatorForTest(t, explicitYAML, &stubRunner{})
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	if len(lock.l.Entries) != 2 {
		t.Errorf("lock entries = %d; want 2", len(lock.l.Entries))
	}
}

func TestValidate_Full_SkipsValidationWhenLockMatches(t *testing.T) {
	v, lock := newValidatorForTest(t, explicitYAML, &stubRunner{})
	// Pre-fill lock with current hashes
	for _, e := range v.provider.Entries() {
		lock.l.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash()}
	}
	runner := &stubRunner{}
	v.runner = runner
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if len(runner.got) != 0 {
		t.Errorf("runner should not be called when lock matches; got %v", runner.got)
	}
	if !strings.Contains(out.String(), "already validated") {
		t.Errorf("expected cache-hit message; got %q", out.String())
	}
}

func TestValidate_Force_RevalidatesEverything(t *testing.T) {
	v, lock := newValidatorForTest(t, explicitYAML, &stubRunner{})
	for _, e := range v.provider.Entries() {
		lock.l.Entries[e.Key()] = mappings.LockEntry{SHA256: e.Hash()}
	}
	runner := &stubRunner{}
	v.runner = runner
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", true, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if len(runner.got) != 2 {
		t.Errorf("--force must revalidate all; called %v", runner.got)
	}
}

func TestValidate_Targeted_ValidatesOnlyThatRepo(t *testing.T) {
	runner := &stubRunner{}
	v, _ := newValidatorForTest(t, explicitYAML, runner)
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "acme/api", false, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if len(runner.got) != 1 || runner.got[0] != "acme/api" {
		t.Errorf("expected single call to acme/api; got %v", runner.got)
	}
}

func TestValidate_Full_FailingEntryKeepsLockUntouchedForThatEntry(t *testing.T) {
	v, lock := newValidatorForTest(t, explicitYAML, &stubRunner{fail: map[string]bool{"acme/api": true}})
	// Pre-populate the lock with a stale "good" hash for acme/api to ensure
	// it's *not* overwritten by a failing run.
	lock.l.Entries["acme/api"] = mappings.LockEntry{SHA256: "old"}
	var out, errOut bytes.Buffer
	code := v.Validate(context.Background(), "", false, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
	if lock.l.Entries["acme/api"].SHA256 != "old" {
		t.Errorf("failed entry should leave its lock hash untouched; got %q", lock.l.Entries["acme/api"].SHA256)
	}
	if _, ok := lock.l.Entries["acme/web"]; !ok {
		t.Errorf("passing entry should be added to lock")
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappingcli/...`
Expected: PASS, all five validate tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mappingcli/validate.go internal/mappingcli/validate_test.go
git rm internal/mappingcli/add.go internal/mappingcli/remove.go
git commit -m "feat(mappingcli): rewrite validate to use Provider + lock + runner"
```

---

## Task 12 — Rewrite `mappingcli.List` to read the file

**Files:**
- Modify: `internal/mappingcli/list.go`
- Modify: `internal/mappingcli/run_test.go`

- [ ] **Step 1: Read the existing `list.go` to understand current shape**

Run: `cat internal/mappingcli/list.go`
(Expected: current implementation calls `store.RepoMappings.List`.)

- [ ] **Step 2: Replace `list.go`**

```go
// internal/mappingcli/list.go
package mappingcli

import (
	"context"
	"fmt"
	"io"

	"github.com/mptooling/notifycat/internal/mappings"
)

// List prints the configured mappings, grouped by org, in deterministic order.
// Wildcards print as "*".
func List(_ context.Context, p *mappings.Provider, stdout, stderr io.Writer) int {
	entries := p.Entries()
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no mappings configured; populate mappings.yaml")
		return 0
	}
	var prevOrg string
	for _, e := range entries {
		if e.Org != prevOrg {
			if prevOrg != "" {
				fmt.Fprintln(stdout)
			}
			fmt.Fprintf(stdout, "%s  →  %s  (mentions: %v)\n", e.Org, e.Channel, e.Mentions)
			prevOrg = e.Org
		}
		repo := e.Repo
		if e.Wildcard {
			repo = "*"
		}
		fmt.Fprintf(stdout, "  - %s\n", repo)
	}
	_ = stderr // unused; signature stays io.Writer for symmetry with other use cases
	return 0
}
```

- [ ] **Step 3: Rewrite `run_test.go` accordingly**

Remove tests that exercise add/remove/list against the old `*store.RepoMappings`. Add a tiny `list` test against a Provider built from a temp file. Example to adapt:

```go
// internal/mappingcli/run_test.go
package mappingcli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestList_PrintsOrgsAndRepos(t *testing.T) {
	p := loadTestProvider(t, explicitYAML)
	var out, errOut bytes.Buffer
	code := List(context.Background(), p, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	got := out.String()
	if !strings.Contains(got, "acme") || !strings.Contains(got, "api") || !strings.Contains(got, "web") {
		t.Errorf("missing expected content: %q", got)
	}
}

func TestList_Wildcard(t *testing.T) {
	body := `
mappings:
  beta:
    channel: C9999XXXXX
    mentions: ["@c"]
    repositories: "*"
`
	p := loadTestProvider(t, body)
	var out bytes.Buffer
	List(context.Background(), p, &out, &out)
	if !strings.Contains(out.String(), "*") {
		t.Errorf("wildcard not rendered: %q", out.String())
	}
}
```

(Remove any other test functions in `run_test.go` that exercise the removed DB-backed CLI.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mappingcli/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappingcli/list.go internal/mappingcli/run_test.go
git commit -m "feat(mappingcli): rewrite list to render mappings.yaml entries"
```

---

## Task 13 — Rewire `cmd/notifycat-mapping/main.go`

Drop add/remove; load the Provider from the configured file; add `--force` to validate.

**Files:**
- Modify: `cmd/notifycat-mapping/main.go`
- Modify: `cmd/notifycat-mapping/main_test.go`

- [ ] **Step 1: Replace `main.go`**

```go
// cmd/notifycat-mapping/main.go
// Command notifycat-mapping reads mappings.yaml and provides `list` and
// `validate` subcommands. Mutations happen by editing the file directly.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/mappings"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	validator := mappingcli.NewMappingsValidator(provider, cfg)
	os.Exit(dispatch(os.Args[1:], provider, validator, os.Stdout, os.Stderr))
}

func dispatch(args []string, provider *mappings.Provider, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "list":
		return mappingcli.List(ctx, provider, stdout, stderr)
	case "validate":
		return runValidate(ctx, args[1:], validator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func runValidate(ctx context.Context, args []string, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "ignore the lock and revalidate every entry")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, code, ok := parseValidateTarget(fs.Args(), stderr)
	if !ok {
		return code
	}
	return validator.Validate(ctx, target, *force, stdout, stderr)
}

func parseValidateTarget(args []string, stderr io.Writer) (string, int, bool) {
	switch len(args) {
	case 0:
		return "", 0, true
	case 1:
		return args[0], 0, true
	default:
		fmt.Fprintln(stderr, "usage: validate [--force] [owner/repo]")
		return "", 2, false
	}
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-mapping list
  notifycat-mapping validate [--force] [owner/repo]
`)
}
```

- [ ] **Step 2: Rewrite `main_test.go`**

Remove all add/remove tests. Adapt dispatch tests to the new signature (Provider in place of `*store.RepoMappings`):

```go
// cmd/notifycat-mapping/main_test.go
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/mappings"
)

type fakeValidator struct {
	called    bool
	gotTarget string
	gotForce  bool
	code      int
}

func (f *fakeValidator) Validate(_ context.Context, target string, force bool, _, _ io.Writer) int {
	f.called = true
	f.gotTarget = target
	f.gotForce = force
	return f.code
}

func loadProvider(t *testing.T) *mappings.Provider {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.yaml")
	if err := os.WriteFile(p, []byte(`
mappings:
  acme:
    channel: C0123ABCDE
    mentions: []
    repositories: ["api"]
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	prov, err := mappings.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return prov
}

func TestDispatch_NoArgs_PrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch(nil, loadProvider(t), &fakeValidator{}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"add"}, loadProvider(t), &fakeValidator{}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "unknown") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestDispatch_List(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"list"}, loadProvider(t), &fakeValidator{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme") {
		t.Errorf("stdout missing org: %q", out.String())
	}
}

func TestDispatch_Validate_RoutesTarget(t *testing.T) {
	fv := &fakeValidator{}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "acme/api"}, loadProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if fv.gotTarget != "acme/api" || fv.gotForce {
		t.Errorf("got=%+v", fv)
	}
}

func TestDispatch_Validate_Force(t *testing.T) {
	fv := &fakeValidator{}
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "--force"}, loadProvider(t), fv, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if !fv.gotForce || fv.gotTarget != "" {
		t.Errorf("expected --force only; got=%+v", fv)
	}
}

func TestDispatch_Validate_TooManyArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatch([]string{"validate", "a/b", "c/d"}, loadProvider(t), &fakeValidator{}, &out, &errOut)
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

var _ mappingcli.MappingsValidator = (*fakeValidator)(nil)
```

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: PASS for everything except `internal/app/...` (still uses old `store.NewRepoMappings`); that's fixed in Task 14.

- [ ] **Step 4: Commit**

```bash
git add cmd/notifycat-mapping/main.go cmd/notifycat-mapping/main_test.go
git commit -m "feat(notifycat-mapping): drop add/remove; load Provider; add --force"
```

---

## Task 14 — Wire `app.Wire` to load the Provider + run startup validation

Replaces `store.NewRepoMappings(db)` with `mappings.Load(cfg.MappingsFile)`. Before serving, runs cache-aware startup validation; exits non-zero if any check fails.

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`, `internal/app/integration_test.go` (provide a temp mappings.yaml)

- [ ] **Step 1: Update `internal/app/app.go`**

Inside `Wire`, replace the lines:

```go
	messages := store.NewSlackMessages(db)
	mappings := store.NewRepoMappings(db)
```

with:

```go
	messages := store.NewSlackMessages(db)
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		return nil, nil, fmt.Errorf("app: load mappings: %w", err)
	}
	if err := startupValidate(context.Background(), cfg, provider, logger); err != nil {
		return nil, nil, err
	}
```

Then change every handler constructor argument from `mappings` (the old DB repo) to `provider`. The handlers' `RepoMappings` interface already exposes `Get` — `*mappings.Provider` satisfies it.

Add the `mappings` import:

```go
	"github.com/mptooling/notifycat/internal/mappings"
```

Add the helper:

```go
func startupValidate(ctx context.Context, cfg config.Config, p *mappings.Provider, logger *slog.Logger) error {
	entries := p.Entries()
	lock, err := mappings.ReadLock(cfg.MappingsLockFile)
	if err != nil {
		logger.Warn("mappings.lock unreadable; revalidating", slog.String("err", err.Error()))
	}
	diff := mappings.DiffEntries(entries, lock)
	if len(diff.Needs) == 0 {
		// Prune stale keys silently when the lock has drift.
		if len(diff.Stale) > 0 {
			updated := mappings.MergeLock(lock, nil, diff.Stale)
			if werr := mappings.WriteLock(cfg.MappingsLockFile, updated); werr != nil {
				logger.Warn("write lock", slog.String("err", werr.Error()))
			}
		}
		return nil
	}

	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var gh *github.Client
	if cfg.GitHubToken.Reveal() != "" {
		gh = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	v := validate.NewValidator(p, slackClient, gh)
	var lister validate.OrgRepoLister
	if gh != nil {
		lister = gh
	}

	reports := validate.RunForEntries(ctx, diff.Needs, lister, v)
	allOK := true
	passing := map[string]mappings.LockEntry{}
	now := time.Now()
	for _, e := range diff.Needs {
		key := e.Org + "/" + e.Repo
		if e.Wildcard {
			key = e.Key()
		}
		if entryPassed(e, reports) {
			passing[e.Key()] = mappings.LockEntry{SHA256: e.Hash(), ValidatedAt: now}
		} else {
			allOK = false
		}
		_ = key
	}

	for _, r := range reports {
		if !r.OK() {
			logger.Error("startup validation failure",
				slog.String("repository", r.Repository),
				slog.Any("checks", r.Checks),
			)
		}
	}

	if !allOK {
		return fmt.Errorf("app: startup validation failed; see logs above")
	}

	updated := mappings.MergeLock(lock, passing, diff.Stale)
	if werr := mappings.WriteLock(cfg.MappingsLockFile, updated); werr != nil {
		logger.Warn("write lock", slog.String("err", werr.Error()))
	}
	return nil
}

// entryPassed returns true when every report contributing to the entry's
// validation succeeded. For wildcard entries, every expanded repo and the
// (potential) org-expand report must be OK.
func entryPassed(e mappings.Entry, reports []validate.Report) bool {
	if !e.Wildcard {
		key := e.Org + "/" + e.Repo
		for _, r := range reports {
			if r.Repository == key {
				return r.OK()
			}
		}
		return false
	}
	prefix := e.Org + "/"
	for _, r := range reports {
		if r.Repository == e.Key() {
			if !r.OK() {
				return false
			}
			continue
		}
		if strings.HasPrefix(r.Repository, prefix) && !r.OK() {
			return false
		}
	}
	return true
}
```

Add imports as needed: `net/http`, `strings`, `time`, `github.com/mptooling/notifycat/internal/github`, `internal/slack`, `internal/validate`.

- [ ] **Step 2: Update tests in `internal/app`**

Existing `app_test.go` / `integration_test.go` likely create the DB then start the server. They must now also write a minimal `mappings.yaml` to a temp dir and set `cfg.MappingsFile` / `cfg.MappingsLockFile` to point there. If the existing tests fed mappings via `RepoMappings.Upsert`, replace that setup with writing the YAML before calling `Wire`.

Run: `grep -n "RepoMappings" internal/app/`
For each hit, switch to writing a temp YAML.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/app/...`
Expected: PASS.

- [ ] **Step 4: Full build + test**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go internal/app/integration_test.go
git commit -m "feat(app): load Provider from file; run cache-aware startup validation"
```

---

## Task 15 — Migration: drop `github_slack_mapping`

**Files:**
- Create: `internal/store/migrations/00003_drop_github_slack_mapping.sql`

> Note on numbering: this assumes #7's migration was registered as 00002. If issue #7 has not landed yet, renumber accordingly; otherwise keep 00003.

- [ ] **Step 1: Create the migration**

```sql
-- internal/store/migrations/00003_drop_github_slack_mapping.sql
-- +goose Up
DROP TABLE github_slack_mapping;

-- +goose Down
CREATE TABLE github_slack_mapping (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repository    TEXT NOT NULL UNIQUE,
    slack_channel TEXT NOT NULL,
    mentions      TEXT NOT NULL DEFAULT '[]'
);
```

- [ ] **Step 2: Run the migration test (existing store_test.go exercises migrate up/down)**

Run: `go test ./internal/store/...`
Expected: PASS — assuming the existing test runs `MigrateUp` then maybe `MigrateDown` against a fresh DB. Adjust expected table list if the test asserts schema shape.

- [ ] **Step 3: Commit**

```bash
git add internal/store/migrations/00003_drop_github_slack_mapping.sql
git commit -m "feat(store): drop github_slack_mapping table"
```

---

## Task 16 — Delete dead store code

`RepoMappings` is no longer used. `RepoMapping` (the struct) stays — it's the data shape handed to handlers — but its GORM tags and `TableName` go away.

**Files:**
- Delete: `internal/store/repo_mappings.go`
- Modify: `internal/store/models.go` (strip GORM tags from `RepoMapping`, remove `(RepoMapping) TableName()`)
- Modify: `internal/store/store_test.go` (drop any `RepoMappings` tests)

- [ ] **Step 1: Delete `repo_mappings.go`**

```bash
git rm internal/store/repo_mappings.go
```

- [ ] **Step 2: Simplify `RepoMapping` in `models.go`**

Replace the struct + TableName with:

```go
// RepoMapping is the in-memory mapping shape passed to PR handlers. The data
// originates from mappings.yaml; there is no longer a backing DB table.
type RepoMapping struct {
	Repository   string
	SlackChannel string
	Mentions     []string
}
```

Drop the `(RepoMapping) TableName()` method.

- [ ] **Step 3: Drop `RepoMappings` tests in `store_test.go`**

Run: `grep -n "RepoMappings" internal/store/`
Delete the matching test functions.

- [ ] **Step 4: Verify build and tests**

Run: `go test ./...`
Expected: PASS for the whole repo.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/store_test.go
git rm internal/store/repo_mappings.go
git commit -m "refactor(store): drop RepoMappings repository; keep RepoMapping as data shape"
```

---

## Task 17 — End-to-end smoke

A small belt-and-braces check that the binaries behave with a real file.

**Files:**
- (No new files; uses /tmp)

- [ ] **Step 1: Build both binaries**

Run: `go build ./cmd/notifycat-mapping ./cmd/notifycat-server`
Expected: success.

- [ ] **Step 2: Create a sample file and run list**

```bash
mkdir -p /tmp/notifycat-smoke
cat > /tmp/notifycat-smoke/mappings.yaml <<'EOF'
mappings:
  acme:
    channel: C0123ABCDE
    mentions: ["@alice"]
    repositories: ["api"]
EOF
NOTIFYCAT_MAPPINGS_FILE=/tmp/notifycat-smoke/mappings.yaml \
NOTIFYCAT_MAPPINGS_LOCK_FILE=/tmp/notifycat-smoke/mappings.lock \
GITHUB_WEBHOOK_SECRET=x SLACK_BOT_TOKEN=x \
./notifycat-mapping list
```

Expected output includes `acme  →  C0123ABCDE` and a line `- api`.

- [ ] **Step 3: Trigger validation on a bad channel (force fail path)**

```bash
sed -i.bak 's/C0123ABCDE/not-a-channel/' /tmp/notifycat-smoke/mappings.yaml
NOTIFYCAT_MAPPINGS_FILE=/tmp/notifycat-smoke/mappings.yaml \
NOTIFYCAT_MAPPINGS_LOCK_FILE=/tmp/notifycat-smoke/mappings.lock \
GITHUB_WEBHOOK_SECRET=x SLACK_BOT_TOKEN=x \
./notifycat-mapping list
```

Expected: file parse error mentioning `channel` — the bad value is rejected by `Parse`.

- [ ] **Step 4: Clean up**

```bash
rm -rf /tmp/notifycat-smoke ./notifycat-mapping ./notifycat-server
```

- [ ] **Step 5: Final verification**

Run: `go test ./... && go vet ./... && golangci-lint run`
Expected: clean across the board.

- [ ] **Step 6: Push the branch**

```bash
git push -u origin feature/declarative-mappings
```

- [ ] **Step 7: Open the PR**

```bash
gh pr create --title "Declarative mappings.yaml + per-entry lock (closes #8)" --body "$(cat <<'EOF'
## Summary

- Replaces `notifycat-mapping add/remove` and the `github_slack_mapping` table with a declarative `mappings.yaml`.
- Adds `internal/mappings`: file parser, `Provider` with exact-then-wildcard lookup, per-entry lock cache.
- Adds `gh.ListOrgRepos` for `*` expansion at validate time.
- `validate` is no-fail-fast and uses the lock to skip unchanged entries; `--force` bypasses the cache.
- Server boots from the file and runs cache-aware startup validation; failed checks exit non-zero.

Closes #8. Related: #7.

## Test plan

- [ ] `go test ./...` passes
- [ ] `golangci-lint run` clean
- [ ] Local smoke (Task 17 in the plan): list + bad-channel rejection
EOF
)"
```

---

## Self-review (run before merging)

- [ ] All eight schema rules from the spec are enforced in `Parse` (Task 3) — org regex, channel regex, mentions non-nil, repo regex, no dup repos, wildcard exclusivity (Task 2), unknown keys.
- [ ] Lookup precedence in `Provider.Get` (Task 4) — exact match before wildcard.
- [ ] `*` resolution at lookup time (no GH call) — Task 4.
- [ ] No-fail-fast in `RunForEntries` (Task 8) — per-repo error → failing report, ListOrgRepos error → failing report, missing token → skip report.
- [ ] Per-entry lock cache with mention-sort-stable hash (Tasks 5–6).
- [ ] Partial-success merge — failing entries leave their old hash intact (Task 11 test `Full_FailingEntryKeepsLockUntouchedForThatEntry`).
- [ ] CLI surface reduced to `list` + `validate [--force]` (Task 13).
- [ ] Server skips revalidation when lock matches (Task 14, exercised in app integration tests).
- [ ] `github_slack_mapping` table dropped (Task 15).
- [ ] Dead code deleted: `RepoMappings` struct & methods, `mappingcli/add.go`, `mappingcli/remove.go` (Tasks 11, 16).

---

## Execution

Plan complete. Per the writing-plans skill, choose execution mode:

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks
2. **Inline Execution** — single session, batch with checkpoints
