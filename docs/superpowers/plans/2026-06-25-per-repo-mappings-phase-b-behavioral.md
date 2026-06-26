# Per-Repo Mappings — Phase B (behavioral override + digest) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the per-repo mapping tiers so reactions, review toggles (`ignore_ai_reviews`, `dependabot_format`), and the digest (`enabled`, `schedule`) can be overridden per repository — global `config.yaml` values acting as defaults — and rewire the six PR handlers, the composer, the AI detector, and the digest scheduler to use the effective per-event config.

**Architecture:** A repo's effective behavioral config resolves through three tiers: global (`config.yaml` top-level) → `org/*` → `org/repo`, most-specific-wins (same merge model as Phase A routing). The resolved values ride on the existing `store.RepoMapping` returned by `Provider.Get`, so handlers — which already call `mappings.Get(ctx, repo)` per event — read reactions/toggles from that value object instead of construction-time parameters. The digest resolves per repo and the scheduler runs one cron per distinct effective schedule. Phase B completes the breaking `0.18` release begun in Phase A.

**Tech Stack:** Go 1.25.10, `gopkg.in/yaml.v3`, `github.com/robfig/cron/v3`, GORM/SQLite (untouched), `just`.

## Global Constraints

- Go toolchain pinned at **1.25.10**.
- Phase A + Phase B are ONE breaking release (`0.17` → `0.18`). Do NOT cut a release between phases. This plan runs on branch `feat/per-repo-mappings` (Phase A already merged into it).
- **BRANCH SAFETY:** every task works on `feat/per-repo-mappings`. Confirm `git branch --show-current` before committing. Never `git checkout`/`switch`/`stash`/`clean` to another branch.
- **No Claude attribution** in commits or PRs.
- **No hard-wrapped markdown** in repo docs / PR bodies.
- Do **not** commit `config.yaml`, `config.lock`, `.env`, `/data/`.
- **One constructor per type, all deps injected.** **Consumer-package interfaces.** **No comments restating code.**
- **TDD**: RED → verify failure → GREEN.
- Verify with `just check` before declaring done.
- Per-repo-overridable keys (verbatim from the spec): `channel`, `mentions` (Phase A), `slack.reactions.*`, `reviews.ignore_ai_reviews`, `reviews.dependabot_format`, `digest.enabled`, `digest.schedule`.
- Global-only (never per-repo): `server.*`, `database.url`, `slack.base_url`, `github.base_url`, `cleanup.message_ttl_days`.
- Merge semantics: each key independently takes its value from the most specific tier that sets it; `global` is the always-present base for behavioral keys. `mentions` absent = inherit (Phase A). `digest.enabled` and reaction values follow the same most-specific-wins rule with the global value as the base.
- A per-repo behavioral override must NOT affect validation or the lock (`Entry.Hash` keys on org/repo/channel only) — reactions/toggles are formatting concerns, not validation concerns.

---

### Task 1: Behavioral fields on `store.RepoMapping`

Add the resolved behavioral config to the value object handlers consume.

**Files:**
- Modify: `internal/store/models.go`
- Test: `internal/store/models_test.go` (create if absent)

**Interfaces:**
- Produces:
  - `type Reactions struct { Enabled bool; NewPR, MergedPR, ClosedPR, Approved, Commented, RequestChange, BotReview string }`
  - `RepoMapping` gains `Reactions Reactions`, `IgnoreAIReviews bool`, `DependabotFormat bool`.

- [ ] **Step 1: Write the failing test**

Create/extend `internal/store/models_test.go`:

```go
package store_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func TestRepoMapping_CarriesBehavioralConfig(t *testing.T) {
	m := store.RepoMapping{
		Repository:       "o/r",
		SlackChannel:     "C0",
		Reactions:        store.Reactions{Enabled: true, NewPR: "eyes", Approved: "shipit"},
		IgnoreAIReviews:  true,
		DependabotFormat: false,
	}
	if !m.Reactions.Enabled || m.Reactions.Approved != "shipit" {
		t.Errorf("reactions not carried: %+v", m.Reactions)
	}
	if !m.IgnoreAIReviews || m.DependabotFormat {
		t.Errorf("toggles not carried: ignore=%v dependabot=%v", m.IgnoreAIReviews, m.DependabotFormat)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRepoMapping_CarriesBehavioralConfig`
Expected: FAIL — `Reactions`/`IgnoreAIReviews`/`DependabotFormat` undefined.

- [ ] **Step 3: Add the fields**

In `internal/store/models.go`, after the existing `RepoMapping` struct, add the `Reactions` type and extend `RepoMapping`:

```go
// Reactions is the resolved per-repo reaction-emoji set (Slack emoji names
// without colons). Enabled gates whether close/review reactions are added at
// all. Empty BotReview disables the bot-reviewer marker.
type Reactions struct {
	Enabled       bool
	NewPR         string
	MergedPR      string
	ClosedPR      string
	Approved      string
	Commented     string
	RequestChange string
	BotReview     string
}
```

and add to the `RepoMapping` struct (after `Mentions`):

```go
	// Resolved per-repo behavioral config (global config.yaml defaults merged
	// with org/* and org/repo overrides). Formatting-only — not part of
	// validation or the lock.
	Reactions        Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
```

Update the `RepoMapping` doc comment to note it now also carries resolved behavioral config.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/models_test.go
git commit -m "feat: carry resolved per-repo behavioral config on RepoMapping"
```

---

### Task 2: Optional behavioral fields on `RepoConfig`

Extend a mapping tier to optionally override reactions, review toggles, and digest. Absent keys inherit.

**Files:**
- Modify: `internal/mappings/types.go` (`RepoConfig` + its `UnmarshalYAML`)
- Test: `internal/mappings/types_test.go`

**Interfaces:**
- Produces:
  - `type ReactionsOverride struct { Enabled *bool; NewPR, MergedPR, ClosedPR, Approved, Commented, RequestChange, BotReview *string }` with `UnmarshalYAML`.
  - `RepoConfig` gains `Reactions *ReactionsOverride`, `IgnoreAIReviews *bool`, `DependabotFormat *bool`, `Digest *DigestConfig`.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/types_test.go`:

```go
func TestRepoConfig_BehavioralOverrides(t *testing.T) {
	o := decodeOrg(t, `
api:
  channel: C0API
  reactions:
    approved: shipit
    enabled: false
  reviews:
    ignore_ai_reviews: true
    dependabot_format: false
  digest:
    enabled: false
    schedule: "0 8 * * 1-5"
`)
	api := o["api"]
	if api.Reactions == nil || api.Reactions.Approved == nil || *api.Reactions.Approved != "shipit" {
		t.Fatalf("reactions.approved override missing: %+v", api.Reactions)
	}
	if api.Reactions.Enabled == nil || *api.Reactions.Enabled != false {
		t.Errorf("reactions.enabled override missing")
	}
	if api.IgnoreAIReviews == nil || *api.IgnoreAIReviews != true {
		t.Errorf("ignore_ai_reviews override missing")
	}
	if api.DependabotFormat == nil || *api.DependabotFormat != false {
		t.Errorf("dependabot_format override missing")
	}
	if api.Digest == nil || api.Digest.Enabled != false || api.Digest.Schedule != "0 8 * * 1-5" {
		t.Errorf("digest override missing: %+v", api.Digest)
	}
}

func TestRepoConfig_BehavioralAbsentMeansNil(t *testing.T) {
	api := decodeOrg(t, "api:\n  channel: C0API\n")["api"]
	if api.Reactions != nil || api.IgnoreAIReviews != nil || api.DependabotFormat != nil || api.Digest != nil {
		t.Errorf("absent behavioral keys should be nil (inherit): %+v", api)
	}
}

func TestRepoConfig_UnknownReactionKeyRejected(t *testing.T) {
	var o mappings.Org
	dec := yaml.NewDecoder(strings.NewReader("api:\n  channel: C0API\n  reactions:\n    bogus: x\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err == nil {
		t.Fatal("expected error for unknown reactions key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run 'TestRepoConfig_Behavioral|TestRepoConfig_UnknownReaction'`
Expected: FAIL — fields/type undefined.

- [ ] **Step 3: Add the override types and extend the decoder**

In `internal/mappings/types.go`, add fields to `RepoConfig`:

```go
type RepoConfig struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool

	Reactions        *ReactionsOverride
	IgnoreAIReviews  *bool
	DependabotFormat *bool
	Digest           *DigestConfig
}

// ReactionsOverride is a tier's optional reaction overrides; each nil field
// inherits from a less-specific tier (org/* then the global config.yaml set).
type ReactionsOverride struct {
	Enabled       *bool
	NewPR         *string
	MergedPR      *string
	ClosedPR      *string
	Approved      *string
	Commented     *string
	RequestChange *string
	BotReview     *string
}

// UnmarshalYAML walks the reactions mapping by hand so unknown keys are
// rejected and every leaf is optional (nil = inherit).
func (r *ReactionsOverride) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reactions: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reactions: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		var dst any
		switch key.Value {
		case "enabled":
			r.Enabled = new(bool)
			dst = r.Enabled
		case "new_pr":
			r.NewPR = new(string)
			dst = r.NewPR
		case "merged_pr":
			r.MergedPR = new(string)
			dst = r.MergedPR
		case "closed_pr":
			r.ClosedPR = new(string)
			dst = r.ClosedPR
		case "approved":
			r.Approved = new(string)
			dst = r.Approved
		case "commented":
			r.Commented = new(string)
			dst = r.Commented
		case "request_change":
			r.RequestChange = new(string)
			dst = r.RequestChange
		case "bot_review":
			r.BotReview = new(string)
			dst = r.BotReview
		default:
			return fmt.Errorf("reactions: unknown field %q", key.Value)
		}
		if err := val.Decode(dst); err != nil {
			return fmt.Errorf("reactions.%s: %w", key.Value, err)
		}
	}
	return nil
}
```

Then extend `RepoConfig.UnmarshalYAML`'s switch with the new keys (after the existing `channel`/`mentions` cases, before `default`):

```go
		case "reactions":
			r := &ReactionsOverride{}
			if err := valNode.Decode(r); err != nil {
				return fmt.Errorf("reactions: %w", err)
			}
			rc.Reactions = r
		case "reviews":
			if err := decodeReviews(rc, valNode); err != nil {
				return err
			}
		case "digest":
			d := &DigestConfig{}
			if err := valNode.Decode(d); err != nil {
				return fmt.Errorf("digest: %w", err)
			}
			rc.Digest = d
```

And add a `decodeReviews` helper in the same file:

```go
// decodeReviews parses a tier's `reviews:` block (ignore_ai_reviews,
// dependabot_format), each optional, rejecting unknown keys.
func decodeReviews(rc *RepoConfig, node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reviews: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reviews: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		var dst *bool
		switch key.Value {
		case "ignore_ai_reviews":
			rc.IgnoreAIReviews = new(bool)
			dst = rc.IgnoreAIReviews
		case "dependabot_format":
			rc.DependabotFormat = new(bool)
			dst = rc.DependabotFormat
		default:
			return fmt.Errorf("reviews: unknown field %q", key.Value)
		}
		if err := val.Decode(dst); err != nil {
			return fmt.Errorf("reviews.%s: %w", key.Value, err)
		}
	}
	return nil
}
```

Note: `DigestConfig.UnmarshalYAML` already defaults `Enabled` to true on decode; for a tier's `digest:` block that is fine (a tier that writes `digest: { enabled: false }` sets it false; a tier with `digest: { schedule: ... }` leaves enabled true, meaning "enabled, with this schedule").

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run 'TestRepoConfig'`
Expected: PASS.

- [ ] **Step 5: Run the whole mappings package (no regressions)**

Run: `go test ./internal/mappings/`
Expected: PASS (existing routing tests unaffected — new fields are additive and optional).

- [ ] **Step 6: Commit**

```bash
git add internal/mappings/types.go internal/mappings/types_test.go
git commit -m "feat: optional per-repo reactions/reviews/digest overrides on a tier"
```

---

### Task 3: Global defaults type + behavioral resolver

Add the `Defaults` (global tier) type and extend resolution to merge behavioral keys global → org/* → org/repo.

**Files:**
- Modify: `internal/mappings/resolve.go`
- Test: `internal/mappings/resolve_test.go`

**Interfaces:**
- Consumes: `store.Reactions` (Task 1), `RepoConfig`/`ReactionsOverride` (Task 2).
- Produces:
  - `type Defaults struct { Reactions store.Reactions; IgnoreAIReviews bool; DependabotFormat bool }`
  - `func resolveBehavior(global Defaults, star, repo *RepoConfig) (store.Reactions, bool, bool)` — returns resolved (reactions, ignoreAIReviews, dependabotFormat); each key = most-specific tier that set it, else global.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/resolve_test.go` (imports: add `github.com/mptooling/notifycat/internal/store`):

```go
func TestResolveBehavior_RepoOverridesStarOverridesGlobal(t *testing.T) {
	global := Defaults{
		Reactions:        store.Reactions{Enabled: true, NewPR: "eyes", Approved: "white_check_mark", MergedPR: "merge"},
		IgnoreAIReviews:  false,
		DependabotFormat: true,
	}
	shipit := "shipit"
	star := &RepoConfig{Reactions: &ReactionsOverride{Approved: &shipit}}
	disabled := false
	repo := &RepoConfig{
		Reactions:        &ReactionsOverride{Enabled: &disabled},
		IgnoreAIReviews:  boolPtr(true),
	}
	rx, ignoreAI, dependabot := resolveBehavior(global, star, repo)
	if rx.Approved != "shipit" {
		t.Errorf("approved = %q; want star's shipit", rx.Approved)
	}
	if rx.NewPR != "eyes" {
		t.Errorf("new_pr = %q; want global eyes", rx.NewPR)
	}
	if rx.Enabled != false {
		t.Errorf("enabled = %v; want repo's false", rx.Enabled)
	}
	if ignoreAI != true {
		t.Errorf("ignoreAI = %v; want repo's true", ignoreAI)
	}
	if dependabot != true {
		t.Errorf("dependabot = %v; want global true (nobody overrode)", dependabot)
	}
}

func TestResolveBehavior_AllGlobalWhenNoTiers(t *testing.T) {
	global := Defaults{Reactions: store.Reactions{Enabled: true, NewPR: "eyes"}, DependabotFormat: true}
	rx, ignoreAI, dependabot := resolveBehavior(global, nil, nil)
	if rx.NewPR != "eyes" || !rx.Enabled || ignoreAI != false || dependabot != true {
		t.Errorf("got %+v ignoreAI=%v dependabot=%v; want all global", rx, ignoreAI, dependabot)
	}
}

func boolPtr(b bool) *bool { return &b }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestResolveBehavior`
Expected: FAIL — `Defaults`/`resolveBehavior` undefined.

- [ ] **Step 3: Implement `Defaults` and `resolveBehavior`**

In `internal/mappings/resolve.go`, add (and import `github.com/mptooling/notifycat/internal/store`):

```go
// Defaults is the global tier: the config.yaml top-level behavioral settings
// that per-repo tiers override.
type Defaults struct {
	Reactions        store.Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
}

// resolveBehavior merges the global, org/*, and org/repo tiers for the
// behavioral keys. For each key the most specific tier that set it wins; the
// global value is the base. star/repo may be nil.
func resolveBehavior(global Defaults, star, repo *RepoConfig) (store.Reactions, bool, bool) {
	rx := global.Reactions
	ignoreAI := global.IgnoreAIReviews
	dependabot := global.DependabotFormat

	apply := func(rc *RepoConfig) {
		if rc == nil {
			return
		}
		if o := rc.Reactions; o != nil {
			if o.Enabled != nil {
				rx.Enabled = *o.Enabled
			}
			setStr(&rx.NewPR, o.NewPR)
			setStr(&rx.MergedPR, o.MergedPR)
			setStr(&rx.ClosedPR, o.ClosedPR)
			setStr(&rx.Approved, o.Approved)
			setStr(&rx.Commented, o.Commented)
			setStr(&rx.RequestChange, o.RequestChange)
			if o.BotReview != nil { // empty string is a meaningful value (no marker)
				rx.BotReview = *o.BotReview
			}
		}
		if rc.IgnoreAIReviews != nil {
			ignoreAI = *rc.IgnoreAIReviews
		}
		if rc.DependabotFormat != nil {
			dependabot = *rc.DependabotFormat
		}
	}
	apply(star)
	apply(repo)
	return rx, ignoreAI, dependabot
}

func setStr(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run TestResolveBehavior`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/resolve.go internal/mappings/resolve_test.go
git commit -m "feat: behavioral resolver merging global/org/repo tiers"
```

---

### Task 4: `NewProvider` takes `Defaults`; `Provider.Get` populates behavior

Thread the global defaults into the provider and populate the resolved behavioral fields on every `Get`.

**Files:**
- Modify: `internal/mappings/provider.go` (`Provider` struct, `NewProvider`, `Get`)
- Test: `internal/mappings/provider_test.go`

**Interfaces:**
- Consumes: `Defaults`, `resolveBehavior`, `resolveRouting`.
- Produces: `func NewProvider(defaults Defaults, m map[string]Org, digest *DigestConfig) *Provider` (signature CHANGE — adds `defaults` as the first param). `Get` returns a `store.RepoMapping` with behavioral fields populated.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/provider_test.go`:

```go
func TestGet_PopulatesResolvedBehavior(t *testing.T) {
	global := mappings.Defaults{
		Reactions:        store.Reactions{Enabled: true, NewPR: "eyes", Approved: "white_check_mark"},
		DependabotFormat: true,
	}
	shipit := "shipit"
	p := mappings.NewProvider(global, map[string]mappings.Org{
		"acme": {
			"api": {Channel: "C0API", Reactions: &mappings.ReactionsOverride{Approved: &shipit}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
	got, err := p.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0API" {
		t.Errorf("channel = %q", got.SlackChannel)
	}
	if got.Reactions.Approved != "shipit" {
		t.Errorf("approved = %q; want repo override shipit", got.Reactions.Approved)
	}
	if got.Reactions.NewPR != "eyes" || !got.DependabotFormat {
		t.Errorf("global defaults lost: %+v dependabot=%v", got.Reactions, got.DependabotFormat)
	}
}
```

Update the existing provider tests' `NewProvider(...)` calls to pass a zero `mappings.Defaults{}` as the new first arg (e.g. `tierProvider` helper, `TestNewProvider_BehavesLikeLoad`, etc.).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestGet_PopulatesResolvedBehavior`
Expected: FAIL — `NewProvider` arity / behavioral fields.

- [ ] **Step 3: Thread defaults through the provider**

In `internal/mappings/provider.go`: add `defaults Defaults` to the `Provider` struct; update `NewProvider`:

```go
type Provider struct {
	defaults Defaults
	file     File
}

func NewProvider(defaults Defaults, m map[string]Org, digest *DigestConfig) *Provider {
	return &Provider{defaults: defaults, file: File{Mappings: m, Digest: digest}}
}
```

(`Load`, if still present, should pass a zero `Defaults{}` — it is only used by tests/legacy; update its `&Provider{...}` literal to `&Provider{file: file}` which leaves defaults zero.)

In `Get`, after computing `res := resolveRouting(starPtr, repoPtr)`, also resolve behavior and populate the mapping:

```go
	res := resolveRouting(starPtr, repoPtr)
	rx, ignoreAI, dependabot := resolveBehavior(p.defaults, starPtr, repoPtr)
	return store.RepoMapping{
		Repository:       repository,
		SlackChannel:     res.Channel,
		Mentions:         res.Mentions,
		Reactions:        rx,
		IgnoreAIReviews:  ignoreAI,
		DependabotFormat: dependabot,
	}, nil
```

- [ ] **Step 4: Run the package**

Run: `go test ./internal/mappings/`
Expected: PASS (update any remaining `NewProvider` call in tests to the 3-arg form).

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat: NewProvider takes global defaults; Get resolves per-repo behavior"
```

---

### Task 5: Per-repo digest resolution

Resolve a repo's effective digest (enabled + schedule) so the scheduler and reporter can vary it per repo. Global `digest:` is the default; an `org/*` or `org/repo` tier `digest:` overrides.

**Files:**
- Modify: `internal/mappings/provider.go` (add `DigestFor`; keep `Digest()` as the global)
- Test: `internal/mappings/provider_test.go`

**Interfaces:**
- Produces: `func (p *Provider) DigestFor(repository string) DigestConfig` — the effective digest for a repo (global `Digest()` merged with the repo's tiers); and `func (p *Provider) Schedules() []string` — the sorted set of distinct effective schedules across all entries that have the digest enabled.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/provider_test.go`:

```go
func TestDigestFor_RepoOverridesGlobal(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	p := mappings.NewProvider(mappings.Defaults{}, map[string]mappings.Org{
		"acme": {
			"web": {Channel: "C0WEB", Digest: &mappings.DigestConfig{Enabled: true, Schedule: weekdays}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil) // global digest absent → default on, 9am
	d := p.DigestFor("acme/web")
	if !d.Enabled || d.Schedule != weekdays {
		t.Errorf("web digest = %+v; want enabled weekdays", d)
	}
	dd := p.DigestFor("acme/other") // matches "*", no digest override → global default
	if !dd.Enabled || dd.Schedule != mappings.DefaultDigestSchedule {
		t.Errorf("default digest = %+v; want global default", dd)
	}
}

func TestSchedules_DistinctEnabledOnly(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	off := false
	p := mappings.NewProvider(mappings.Defaults{}, map[string]mappings.Org{
		"acme": {
			"web":  {Channel: "C0WEB", Digest: &mappings.DigestConfig{Enabled: true, Schedule: weekdays}},
			"api":  {Channel: "C0API"}, // global default schedule
			"mute": {Channel: "C0MUTE", Digest: &mappings.DigestConfig{Enabled: off}},
			"*":    {Channel: "C0DEFAULT"},
		},
	}, nil)
	got := p.Schedules()
	// weekdays + default 9am; "mute" disabled contributes nothing
	want := map[string]bool{weekdays: true, mappings.DefaultDigestSchedule: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Errorf("Schedules() = %v; want the two distinct enabled schedules", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run 'TestDigestFor|TestSchedules'`
Expected: FAIL — `DigestFor`/`Schedules` undefined.

- [ ] **Step 3: Implement `DigestFor` and `Schedules`**

In `internal/mappings/provider.go`, add:

```go
// DigestFor returns the effective digest config for a repository: the global
// Digest() merged with the org/* and org/repo tiers (most-specific tier that
// sets enabled/schedule wins). An unmapped repo yields the global digest.
func (p *Provider) DigestFor(repository string) DigestConfig {
	d := p.Digest() // global default (enabled + DefaultDigestSchedule)
	org, repo, ok := splitRepo(repository)
	if !ok {
		return d
	}
	o, ok := p.file.Mappings[org]
	if !ok {
		return d
	}
	apply := func(rc RepoConfig, has bool) {
		if has && rc.Digest != nil {
			d.Enabled = rc.Digest.Enabled
			if s := strings.TrimSpace(rc.Digest.Schedule); s != "" {
				d.Schedule = s
			}
		}
	}
	star, hasStar := o[starKey]
	apply(star, hasStar)
	rc, hasRepo := o[repo]
	apply(rc, hasRepo)
	return d
}

// Schedules returns the sorted distinct set of effective digest schedules
// across every mapping entry whose effective digest is enabled. The scheduler
// registers one cron per returned spec.
func (p *Provider) Schedules() []string {
	seen := map[string]struct{}{}
	for _, e := range p.Entries() {
		d := p.DigestFor(e.Key())
		if !d.Enabled {
			continue
		}
		seen[d.Schedule] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
```

Note: `e.Key()` for a wildcard entry is `org/*`; `DigestFor("org/*")` splits to repo `*` and applies the `*` tier — correct, since the wildcard entry's effective digest is the `*` tier merged over global.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat: per-repo digest resolution and distinct-schedule set"
```

---

### Task 6: `composer.NewMessage` takes the per-repo new-PR emoji

The open message's leading emoji is per-repo now. Pass it per call instead of constructor state.

**Files:**
- Modify: `internal/slack/composer.go` (`NewMessage`; `NewComposer` keeps a fallback default)
- Test: `internal/slack/composer_test.go`

**Interfaces:**
- Produces: `func (c *Composer) NewMessage(pr PRDetails, mentions []string, newPREmoji string) Message`.

- [ ] **Step 1: Update the test (failing)**

In `internal/slack/composer_test.go`, find the `NewMessage` test(s) and add the emoji arg, asserting the passed emoji appears in the headline. Example assertion update:

```go
msg := c.NewMessage(pr, nil, "rocket")
if !strings.Contains(msg.Blocks[0].Text.Text, ":rocket:") {
	t.Errorf("headline %q missing per-call emoji", msg.Blocks[0].Text.Text)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestComposer`
Expected: FAIL — arity mismatch.

- [ ] **Step 3: Add the parameter**

In `internal/slack/composer.go`, change `NewMessage` to take `newPREmoji string` and use it instead of `c.newPREmoji`:

```go
func (c *Composer) NewMessage(pr PRDetails, mentions []string, newPREmoji string) Message {
	if newPREmoji == "" {
		newPREmoji = c.newPREmoji
	}
	headline := fmt.Sprintf(
		":%s: %splease review <%s|PR #%d: %s>",
		newPREmoji, mentionsPrefix(mentions), pr.URL, pr.Number, pr.Title,
	)
	// ...unchanged fallback + return...
}
```

(Keep `c.newPREmoji` as the empty-arg fallback so existing construction stays valid.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/slack/`
Expected: PASS (only the open handler calls `NewMessage`; Task 7 updates that call).

- [ ] **Step 5: Commit**

```bash
git add internal/slack/composer.go internal/slack/composer_test.go
git commit -m "feat: composer NewMessage accepts per-repo new-PR emoji"
```

---

### Task 7: OpenHandler reads behavior per event

Drop the construction-time `dependabotFormat`; read it (and the new-PR emoji) from the per-event mapping.

**Files:**
- Modify: `internal/pullrequest/open.go`
- Test: `internal/pullrequest/open_test.go`

**Interfaces:**
- Consumes: `store.RepoMapping{ DependabotFormat, Reactions.NewPR }` (Task 4), `composer.NewMessage(pr, mentions, emoji)` (Task 6).
- Produces: `func NewOpenHandler(messages SlackMessages, mappings RepoMappings, slackClient SlackClient, composer *slack.Composer, logger *slog.Logger) *OpenHandler` (drops the `dependabotFormat bool` param).

- [ ] **Step 1: Update tests (failing)**

In `internal/pullrequest/open_test.go`, change `NewOpenHandler(...)` calls to drop the trailing bool, and make the fake mapping return the desired `DependabotFormat`/`Reactions.NewPR`. Add/adjust a test asserting that when the mapping's `DependabotFormat` is true and the author is a bot, the compact message is used; and that `NewMessage` receives `mapping.Reactions.NewPR`. (Follow the file's existing fake-mapping pattern; set the new fields on the returned `store.RepoMapping`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pullrequest/ -run TestOpen`
Expected: FAIL — arity / behavior.

- [ ] **Step 3: Rewire the handler**

In `internal/pullrequest/open.go`: remove the `dependabotFormat` field and constructor param. Change `composeMessage` to take the mapping (or pass the needed fields) and read `mapping.DependabotFormat` + `mapping.Reactions.NewPR`:

```go
func (h *OpenHandler) Handle(ctx context.Context, e Event) error {
	// ...unchanged dedupe + mapping lookup...
	msg := h.composeMessage(e, mapping)
	ts, err := h.slack.PostMessage(ctx, mapping.SlackChannel, msg)
	// ...unchanged save...
}

func (h *OpenHandler) composeMessage(e Event, mapping store.RepoMapping) slack.Message {
	if mapping.DependabotFormat {
		if kind := botpr.DetectBot(e.PR.Author); kind != botpr.BotKindNone {
			security := botpr.IsSecurityAdvisory(e.PR.Body)
			return h.composer.BotMessage(slackPRFrom(e), mapping.Mentions, kind.Name(), security)
		}
	}
	return h.composer.NewMessage(slackPRFrom(e), mapping.Mentions, mapping.Reactions.NewPR)
}
```

Update `NewOpenHandler` to drop `dependabotFormat`. Add `"github.com/mptooling/notifycat/internal/store"` to imports if needed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pullrequest/ -run TestOpen`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/open.go internal/pullrequest/open_test.go
git commit -m "feat: OpenHandler resolves dependabot format and new-PR emoji per repo"
```

---

### Task 8: CloseHandler reads reactions per event

Drop `CloseOptions`; read merged/closed emoji + enabled from the per-event mapping.

**Files:**
- Modify: `internal/pullrequest/close.go`
- Test: `internal/pullrequest/close_test.go`

**Interfaces:**
- Consumes: `store.RepoMapping{ Reactions.{Enabled,MergedPR,ClosedPR} }`.
- Produces: `func NewCloseHandler(messages SlackMessages, mappings RepoMappings, slackClient SlackClient, composer *slack.Composer, logger *slog.Logger) *CloseHandler` (drops `opts CloseOptions`; remove the `CloseOptions` type).

- [ ] **Step 1: Update tests (failing)**

In `internal/pullrequest/close_test.go`, drop `CloseOptions` from `NewCloseHandler(...)` and set `Reactions.{Enabled,MergedEmoji→MergedPR,ClosedEmoji→ClosedPR}` on the fake mapping. Keep the existing assertions (merged → merged emoji + decoration; reactions-disabled → no AddReaction).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pullrequest/ -run TestClose`
Expected: FAIL.

- [ ] **Step 3: Rewire the handler**

In `internal/pullrequest/close.go`: delete the `CloseOptions` type and the `opts` field/param. In `Handle`, source the emoji and the enabled flag from the mapping:

```go
	emoji := mapping.Reactions.ClosedPR
	if e.PR.Merged {
		emoji = mapping.Reactions.MergedPR
	}
	updated := h.composer.UpdatedMessage(slackPRFrom(e), e.PR.Merged, emoji)
	if err := h.slack.UpdateMessage(ctx, mapping.SlackChannel, stored.TS, updated); err != nil {
		return err
	}
	if err := h.messages.MarkClosed(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	if !mapping.Reactions.Enabled {
		return nil
	}
	return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, emoji)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pullrequest/ -run TestClose`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/close.go internal/pullrequest/close_test.go
git commit -m "feat: CloseHandler resolves close reactions per repo"
```

---

### Task 9: Review handlers read emoji + suppression per event; detector becomes a pure classifier

The three review handlers read their emoji/botEmoji from the mapping and gate suppression on the per-repo `IgnoreAIReviews`. The AI detector loses its global ignore flag and becomes a pure bot classifier.

**Files:**
- Modify: `internal/aireview/aireview.go` (Detector: keep `IsBot`; remove the global-ignore `ShouldSuppress`)
- Modify: `internal/pullrequest/review_handlers.go`
- Test: `internal/aireview/aireview_test.go`, `internal/pullrequest/review_handlers_test.go`

**Interfaces:**
- Consumes: `store.RepoMapping{ IgnoreAIReviews, Reactions.{Approved,Commented,RequestChange,BotReview} }`.
- Produces:
  - `aireview.Detector` with `func (d *Detector) IsBot(senderType string) bool` (remove `ShouldSuppress`); `func NewDetector() *Detector` (drops the `ignore bool` param) — OR keep `IsBot` as a package function if simpler; pick one and use it consistently.
  - Each review-handler constructor drops the `emoji`, `botEmoji` params and the `detector` becomes the pure classifier. Add a `kind` discriminator so the shared handler reads the right reaction from `mapping.Reactions`.

- [ ] **Step 1: Update tests (failing)**

In `internal/aireview/aireview_test.go`, replace `ShouldSuppress` assertions with `IsBot` (a Bot sender → true; a User → false). In `internal/pullrequest/review_handlers_test.go`, drop the emoji/botEmoji/detector-ignore args from the constructors and set `IgnoreAIReviews` + `Reactions.{Approved,...,BotReview}` on the fake mapping; keep the suppression test (bot sender + mapping.IgnoreAIReviews=true → no reaction, no Touch) and the bot-marker test (bot sender + IgnoreAIReviews=false + non-empty BotReview → marker added).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/aireview/ ./internal/pullrequest/ -run 'TestDetector|TestApprove|TestCommented|TestRequestChange'`
Expected: FAIL.

- [ ] **Step 3: Simplify the detector**

In `internal/aireview/aireview.go`, drop the stored `ignore` flag and the `ShouldSuppress` method; keep a constructor `NewDetector() *Detector` and the `IsBot(senderType string) bool` method (returns `senderType == "Bot"`). Keep whatever package doc explains the coarse bot detection.

- [ ] **Step 4: Rewire the review handlers**

In `internal/pullrequest/review_handlers.go`: replace the `emoji`/`botEmoji` fields with a `kind` field (e.g. a small enum or the existing `name`), and a function that selects the emoji from a `store.Reactions`. Read the mapping's reactions + ignore flag in `Handle`:

```go
type reactionHandler struct {
	name       string
	emojiOf    func(store.Reactions) string // picks the state emoji for this handler
	applicable func(Event) bool

	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	logger   *slog.Logger
	detector *aireview.Detector
}
```

In `Handle`, after the mapping lookup, gate suppression on the per-repo flag and source emojis from the mapping:

```go
	if mapping.IgnoreAIReviews && h.detector.IsBot(e.Sender.Type) {
		h.logger.Debug("skipped bot reviewer reaction", /* ...unchanged fields... */)
		return nil
	}
	emoji := h.emojiOf(mapping.Reactions)
	if err := h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, emoji); err != nil {
		return err
	}
	if err := h.messages.Touch(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	if mapping.Reactions.BotReview != "" && h.detector.IsBot(e.Sender.Type) {
		return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, mapping.Reactions.BotReview)
	}
	return nil
```

Update the three constructors to drop `emoji`/`botEmoji` and set `emojiOf`:
- Approve: `emojiOf: func(r store.Reactions) string { return r.Approved }`
- Commented: `return r.Commented`
- RequestChange: `return r.RequestChange`

And `NewDetector()` (no arg). Add the `store` import.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/aireview/ ./internal/pullrequest/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/aireview internal/pullrequest/review_handlers.go internal/pullrequest/review_handlers_test.go internal/aireview/aireview_test.go
git commit -m "feat: review handlers resolve reactions and AI suppression per repo"
```

---

### Task 10: Digest scheduler runs one cron per distinct schedule; reporter filters by schedule

The scheduler registers a cron per `Provider.Schedules()` entry; each tick reports only the repos whose effective schedule matches and whose digest is enabled.

**Files:**
- Modify: `internal/digest/scheduler.go` (multi-schedule), `internal/digest/reporter.go` (filter stuck rows by effective schedule)
- Test: `internal/digest/scheduler_test.go`, `internal/digest/reporter_test.go`

**Interfaces:**
- Consumes: `Provider.Schedules() []string`, `Provider.DigestFor(repo) DigestConfig` (Task 5) via a small consumer interface.
- Produces: `func NewScheduler(specs []string, job ScheduleJob, logger *slog.Logger) (*Scheduler, error)` where `ScheduleJob` is `interface { ReportSchedule(ctx, spec string) error }`; `Reporter.ReportSchedule(ctx, spec)` reports only the matching repos.

- [ ] **Step 1: Write the failing scheduler test**

In `internal/digest/scheduler_test.go`, assert that `NewScheduler` accepts multiple specs, validates each (one bad spec → error), and that `Run` fires each spec, calling `job.ReportSchedule(ctx, spec)` with that spec. (Use the existing test's cron/clock seam; a fake job records the specs it was called with.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/digest/ -run TestScheduler`
Expected: FAIL — signature/behavior.

- [ ] **Step 3: Multi-schedule scheduler**

Rewrite `internal/digest/scheduler.go` so the `Scheduler` holds `specs []string` and a `job ScheduleJob`; `NewScheduler` validates every spec up front (`cron.ParseStandard`); `Run` adds one cron func per spec that calls `s.job.ReportSchedule(ctx, spec)`:

```go
type ScheduleJob interface {
	ReportSchedule(ctx context.Context, spec string) error
}

type Scheduler struct {
	specs  []string
	job    ScheduleJob
	logger *slog.Logger
}

func NewScheduler(specs []string, job ScheduleJob, logger *slog.Logger) (*Scheduler, error) {
	for _, spec := range specs {
		if _, err := cron.ParseStandard(spec); err != nil {
			return nil, fmt.Errorf("digest: invalid schedule %q: %w", spec, err)
		}
	}
	return &Scheduler{specs: specs, job: job, logger: logger}, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	c := cron.New()
	for _, spec := range s.specs {
		spec := spec
		if _, err := c.AddFunc(spec, func() {
			if err := s.job.ReportSchedule(ctx, spec); err != nil {
				s.logger.Error("stuck-pr digest run failed", slog.String("schedule", spec), slog.Any("err", err))
			}
		}); err != nil {
			return fmt.Errorf("digest: schedule %q: %w", spec, err)
		}
	}
	c.Start()
	<-ctx.Done()
	<-c.Stop().Done()
	return nil
}
```

- [ ] **Step 4: Write the failing reporter test**

In `internal/digest/reporter_test.go`, add a test: two stuck PRs in repos with different effective schedules; `ReportSchedule(ctx, specA)` posts only the repo whose `DigestFor` schedule == specA and is enabled; a repo with digest disabled is never posted. Extend the fake mapping/provider to implement `DigestFor`.

- [ ] **Step 5: Filter the reporter by schedule**

Add a `DigestResolver` consumer interface to `internal/digest/reporter.go`:

```go
type DigestResolver interface {
	DigestFor(repository string) mappings.DigestConfig
}
```

Give `Reporter` a `digests DigestResolver` dependency (injected via `NewReporter`). Add `ReportSchedule`:

```go
// ReportSchedule runs one digest pass for a single cron spec: it includes only
// stuck PRs whose repo's effective digest is enabled and scheduled at spec.
func (r *Reporter) ReportSchedule(ctx context.Context, spec string) error {
	return r.report(ctx, func(repo string) bool {
		d := r.digests.DigestFor(repo)
		return d.Enabled && d.Schedule == spec
	})
}
```

Refactor the existing `Report` body into `report(ctx, include func(repo string) bool)` that skips a stuck row when `!include(row.Repository)` (in addition to the existing unmapped-repo skip). Keep a parameterless `Report` (used by tests / any non-scheduled path) that includes everything enabled, or delete it if no longer referenced — follow what the build needs.

- [ ] **Step 6: Run digest tests**

Run: `go test ./internal/digest/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/digest
git commit -m "feat: per-schedule digest scheduler and reporter filtering"
```

---

### Task 11: Wire global defaults + multi-schedule digest in `app.Wire`

Build `mappings.Defaults` from `cfg`, pass it to `NewProvider`, simplify the handler construction (handlers no longer take emoji/toggle args), and start one digest scheduler over `Provider.Schedules()`.

**Files:**
- Modify: `internal/app/app.go`
- Test: `internal/app/integration_test.go`

**Interfaces:**
- Consumes: all prior tasks' new signatures.

- [ ] **Step 1: Build defaults and update provider construction**

In `internal/app/app.go`, replace `provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)` with a defaults-carrying construction:

```go
	defaults := mappings.Defaults{
		Reactions: store.Reactions{
			Enabled:       cfg.Reactions.Enabled,
			NewPR:         cfg.Reactions.NewPR,
			MergedPR:      cfg.Reactions.MergedPR,
			ClosedPR:      cfg.Reactions.ClosedPR,
			Approved:      cfg.Reactions.Approved,
			Commented:     cfg.Reactions.Commented,
			RequestChange: cfg.Reactions.RequestChange,
			BotReview:     cfg.Reactions.BotReview,
		},
		IgnoreAIReviews:  cfg.IgnoreAIReviews,
		DependabotFormat: cfg.DependabotFormat,
	}
	provider := mappings.NewProvider(defaults, cfg.Mappings, cfg.Digest)
```

(Every other `NewProvider` call site — doctor, smoke, notifycat-config — must also pass a `mappings.Defaults`. For those CLIs, behavioral config doesn't matter to validation/list, so pass a zero `mappings.Defaults{}`. Grep `NewProvider(` across the repo and update each.)

- [ ] **Step 2: Simplify handler + detector construction**

Update the dispatcher construction to the new constructor arities:

```go
	aiDetector := aireview.NewDetector()
	composer := slack.NewComposer(cfg.Reactions.NewPR) // fallback default for empty per-repo new_pr
	dispatcher := pullrequest.NewDispatcher(
		logger,
		pullrequest.NewOpenHandler(messages, provider, slackClient, composer, logger),
		pullrequest.NewCloseHandler(messages, provider, slackClient, composer, logger),
		pullrequest.NewDraftHandler(messages, provider, slackClient, logger),
		pullrequest.NewApproveHandler(messages, provider, slackClient, logger, aiDetector),
		pullrequest.NewCommentedHandler(messages, provider, slackClient, logger, aiDetector),
		pullrequest.NewRequestChangeHandler(messages, provider, slackClient, logger, aiDetector),
	)
```

(The exact review-handler constructor arity is whatever Task 9 produced — drop emoji/botEmoji; keep messages, provider, slack, logger, detector.)

- [ ] **Step 3: Start the multi-schedule digest**

Replace the single-schedule digest block. Build the reporter with the provider as both mapping lookup and digest resolver, gather `provider.Schedules()`, and start one scheduler over them (skip entirely when there are no enabled schedules):

```go
	var digestScheduler *digest.Scheduler
	if specs := provider.Schedules(); len(specs) > 0 {
		reporter := digest.NewReporter(messages, provider, slackClient, composer, provider, logger)
		digestScheduler, err = digest.NewScheduler(specs, reporter, logger)
		if err != nil {
			closeDB(db)
			return nil, nil, nil, nil, fmt.Errorf("app: digest scheduler: %w", err)
		}
	}
```

(Adjust `digest.NewReporter`'s signature in Task 10 to accept the `DigestResolver` — here `provider` is passed for both the mapping lookup and the digest resolver. If that double-pass reads awkwardly, give `NewReporter` one `*mappings.Provider`-shaped dependency that satisfies both interfaces.)

- [ ] **Step 4: Build + fix the integration test**

Run: `go build ./...` and fix the integration test fixture: it constructs the provider/handlers — update to the new `NewProvider(defaults, ...)` and handler arities, and set behavioral expectations via the global `Defaults` or per-repo tiers as the test needs.

Run: `go test ./internal/app/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/integration_test.go
git commit -m "feat: wire global defaults and multi-schedule digest in app.Wire"
```

---

### Task 12: Module-wide build + remaining call sites

Close any remaining call sites (doctor, smoke, notifycat-config, mappingcli) to the new `NewProvider`/handler signatures.

**Files:**
- Modify: as the build reports (`cmd/notifycat-doctor/main.go`, `cmd/notifycat-smoke/main.go`, `cmd/notifycat-config/main.go`, `internal/doctor/*`, `internal/mappingcli/*`, their tests)

**Interfaces:** consumes the new signatures.

- [ ] **Step 1: Find all stale call sites**

Run: `go build ./... 2>&1 | head -40` and `go vet ./... 2>&1 | head -40`
Expected: errors at each `NewProvider(` / handler / `NewReporter` / `NewScheduler` / `NewDetector` call not yet updated.

- [ ] **Step 2: Update each**

For each reported site, pass `mappings.Defaults{}` (zero) as `NewProvider`'s new first arg (CLIs don't need behavioral config), and update any handler/detector construction to the new arities. These are mechanical.

- [ ] **Step 3: Full suite**

Run: `go test -race ./...`
Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test: update remaining call sites to per-repo behavioral signatures"
```

---

### Task 13: Docs + Phase B verification

**Files:**
- Modify: `config.example.yaml` (add per-repo behavioral override examples), `docs/0.18-per-repo-mappings-migration.md` (behavioral override section), `docs/mappings.md`, `docs/configuration.md`

- [ ] **Step 1: Document per-repo behavioral overrides**

In `config.example.yaml`, add to one repo tier an example `reactions:`, `reviews:`, and `digest:` override block (commented), and a note that any of these inherit from `org/*` then the global section when absent. In `docs/0.18-per-repo-mappings-migration.md`, add a "Per-repo behavioral overrides" section: which keys are overridable (`reactions.*`, `reviews.ignore_ai_reviews`, `reviews.dependabot_format`, `digest.enabled`, `digest.schedule`); the global → org/* → org/repo precedence; that a channel can post at multiple times if its repos disagree on digest schedule. Update `docs/mappings.md` + `docs/configuration.md` to mention behavioral overrides are tier-level. No hard-wrapping.

- [ ] **Step 2: Full local check**

Run: `just check`
Expected: vet/lint/vuln clean, all race tests pass, all binaries build.

- [ ] **Step 3: Boot smoke test with per-repo behavior**

Build the server; from a scratch dir (no `.env`), boot with a `config.yaml` that has a repo tier overriding `reactions.approved` and `digest.schedule`, plus a global `digest:`; confirm clean boot and that `config.lock` is written (or empty-mappings boots if you keep mappings empty). Confirm two distinct digest schedules register (inspect logs / no scheduler error).

- [ ] **Step 4: Commit (if Steps surfaced fixes)**

```bash
git add -A
git commit -m "docs: per-repo behavioral overrides for 0.18"
```

---

## Self-Review

**Spec coverage (Phase B = behavioral half):**
- Per-repo `reactions.*` override → Tasks 2, 3, 4, 7, 8, 9.
- Per-repo `reviews.ignore_ai_reviews` / `dependabot_format` → Tasks 2, 3, 4, 7, 9.
- Per-repo `digest.enabled` / `digest.schedule` → Tasks 2, 5, 10, 11.
- Global config.yaml values as defaults (global tier) → Tasks 3, 4, 11.
- Deep per-key merge global → org/* → org/repo → Task 3 (`resolveBehavior`), Task 5 (`DigestFor`).
- Handlers/composer/detector consume effective per-event config → Tasks 6-9, 11.
- N-schedule digest, channel may post at multiple times → Tasks 5, 10, 11.
- Behavioral overrides do NOT affect validation/lock → kept off `Entry`/`Entry.Hash` (untouched); reactions live only on `store.RepoMapping`/`RepoConfig`.

**Placeholder scan:** No "TBD"/"handle edge cases". Handler-rewire tasks (7-9, 12) include build-then-fix-call-site steps because exact test-fixture line numbers depend on current contents; the production edits are spelled out with code.

**Type consistency:** `store.Reactions` (Task 1) is the single reaction value type, reused by `mappings.Defaults` (Task 3) and `store.RepoMapping` (Task 1) and read by handlers (7-9). `mappings.Defaults` is defined in Task 3 and consumed by `NewProvider` (Task 4) + `app.Wire` (Task 11). `ReactionsOverride`/`RepoConfig` optional fields (Task 2) are consumed by `resolveBehavior` (Task 3). `Provider.DigestFor`/`Schedules` (Task 5) are consumed by the reporter/scheduler (Task 10) and `app.Wire` (Task 11). `NewProvider(defaults, m, digest)` is the consistent 3-arg form across all call sites (Tasks 4, 11, 12). `aireview.Detector.IsBot` + `NewDetector()` (Task 9) are used in Tasks 9, 11.
