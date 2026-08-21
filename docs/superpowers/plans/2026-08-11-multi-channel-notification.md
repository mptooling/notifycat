# Multi-channel Notification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a repository declare multiple Slack channels unconditionally (each with its own mentions) via a `channels:` list, reusing the existing per-channel fan-out machinery.

**Architecture:** All changes are confined to the `routing` domain (schema, decode, resolve, structural validation, lock hash) plus a rename in the `validation` domain so per-channel membership checks cover every channel. The `notification` handlers, persistence, message updates, reactions, and close/delete are unchanged — they already loop over `[]Target` / per-channel `slack_messages` rows.

**Tech Stack:** Go 1.25.10, `gopkg.in/yaml.v3` (hand-rolled `UnmarshalYAML` decoders), uber/fx wiring (untouched here), `go test -race`.

## Global Constraints

- Go toolchain pinned at **1.25.10**. Verify with `just check` (vet + lint + vuln + race tests + build) before finishing.
- **DDD + hexagonal layering:** domain layer owns contracts (`interfaces.go`, `models.go`, `enums.go`, `constants.go`); application holds use cases; infrastructure holds adapters. Dependencies point inward only.
- **Readable names over terse Go idiom** (`repoConfig`, `channelSpec`, `baseTargets` — not `rc`, `cs`, `bt`), matching the surrounding code.
- **No comments that restate code.** Only comment a non-obvious *why*.
- **TDD:** RED → verify failure → GREEN → REFACTOR for every task. New behavior starts with a failing test.
- **Commits:** Conventional Commits; PR title = commit message. **No `Co-Authored-By` / Claude footer** in commit messages (repo convention). Avoid the literal string `BREAKING CHANGE` in commit bodies.
- **Schema is additive and non-breaking:** every existing config must still parse and behave identically; the single `channel:` form stays first-class.
- Slack channel IDs match `^[CGD][A-Z0-9]{2,}$` (`channelPattern` in `internal/routing/application/validate.go`). The no-mentions default is `domain.ChannelMention` = `"<!channel>"` (`internal/routing/domain/enums.go`).

---

## Semantics reference (read before starting)

Resolution the tasks implement, stated once:

- **Base target set** comes from the most-specific tier (org/repo, else org/\*) that declares a channel — single (`channel:`) or list (`channels:`) — as a **whole-tier replacement** (lists are not merged element-wise across tiers). Single form: one target, mentions resolved by the existing cross-tier tri-state. List form: one target per entry, mentions per-entry tri-state defaulting to `ChannelMention` (no cross-tier inheritance in list form).
- The **primary** channel is the first base target (`base[0]`). It fills `RepoMapping.SlackChannel` and `Entry.Channel`, exactly as the single channel does today.
- **`paths:` is replace-on-match, unchanged.** When matched path rules declare channels (single or list), they replace the base set entirely; a matched rule that omits channels inherits the **primary** base channel. List-form path entries default mentions to `ChannelMention`; single-form path rules keep their "absent → inherit base mentions" tri-state. Final targets are deduped by channel with mentions merged per channel.
- **Additional channels** (for validation + lock hash) = sorted distinct union of (base channels beyond the primary) ∪ (all path channels). For an existing single-channel config this is exactly today's `pathChannels(rc.Paths)`, so hashes stay stable and nothing mass-revalidates.

---

## Task 1: Decode `channels:` at the tier level

**Files:**
- Modify: `internal/routing/domain/models.go` (add `ChannelSpec`; add `Channels []ChannelSpec` to `RepoConfig`)
- Modify: `internal/routing/infrastructure/config_decode.go` (add `channelSpecWire`; extend `repoConfigWire`)
- Test: `internal/routing/infrastructure/config_decode_test.go`

**Interfaces:**
- Produces: `domain.ChannelSpec{ Channel string; Mentions []string; MentionsPresent bool }`; `domain.RepoConfig.Channels []domain.ChannelSpec`.

- [ ] **Step 1: Write failing tests for tier-level `channels:` decode**

Add to `internal/routing/infrastructure/config_decode_test.go`. Use the same parse helper the existing tests in this file use (locate it at the top of the file — it wraps `yaml.Unmarshal` into the wire types / `domain.File`). Mirror the existing tests' call style; the assertions below are the contract:

```go
func TestDecode_TierChannelsList(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels:
        - channel: C0API1
          mentions: ["<@U0ALICE>"]
        - channel: C0API2
`
	f := parseMappings(t, doc) // reuse this file's existing parse helper
	api := f.Mappings["acme"]["api"]
	if len(api.Channels) != 2 {
		t.Fatalf("want 2 channels, got %d", len(api.Channels))
	}
	if api.Channels[0].Channel != "C0API1" || !api.Channels[0].MentionsPresent ||
		len(api.Channels[0].Mentions) != 1 || api.Channels[0].Mentions[0] != "<@U0ALICE>" {
		t.Fatalf("entry 0 wrong: %+v", api.Channels[0])
	}
	if api.Channels[1].Channel != "C0API2" || api.Channels[1].MentionsPresent {
		t.Fatalf("entry 1 should have absent mentions: %+v", api.Channels[1])
	}
}

func TestDecode_TierChannelsRejectsMixWithChannel(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channel: C0BASE
      channels:
        - channel: C0API1
`
	if _, err := tryParseMappings(doc); err == nil { // non-fatal parse variant returning (File, error)
		t.Fatal("want error mixing channel and channels")
	}
}

func TestDecode_TierChannelsRejectsMentionsSibling(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      mentions: ["<@U0ALICE>"]
      channels:
        - channel: C0API1
`
	if _, err := tryParseMappings(doc); err == nil {
		t.Fatal("want error: tier-level mentions alongside channels")
	}
}

func TestDecode_TierChannelsRejectsDuplicate(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels:
        - channel: C0DUP
        - channel: C0DUP
`
	if _, err := tryParseMappings(doc); err == nil {
		t.Fatal("want error: duplicate channel in list")
	}
}

func TestDecode_ChannelSpecRejectsMissingChannel(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels:
        - mentions: ["<@U0ALICE>"]
`
	if _, err := tryParseMappings(doc); err == nil {
		t.Fatal("want error: entry missing channel")
	}
}

func TestDecode_TierChannelsRejectsEmptyList(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels: []
`
	if _, err := tryParseMappings(doc); err == nil {
		t.Fatal("want error: empty channels list")
	}
}
```

If this file has no `parseMappings`/`tryParseMappings` helpers, add thin ones next to the existing tests: `parseMappings` calls the fatal-on-error variant, `tryParseMappings` returns `(domain.File, error)`. Match whatever unmarshal entrypoint the current tests already use.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/routing/infrastructure/ -run 'TestDecode_(TierChannels|ChannelSpec)' -v`
Expected: FAIL — `Channels` field undefined / compile error.

- [ ] **Step 3: Add the domain types**

In `internal/routing/domain/models.go`, add above `ReactionsOverride`:

```go
// ChannelSpec is one entry in a tier's or path rule's `channels:` list: a Slack
// channel and the mentions to ping there. Mentions carry the same tri-state as a
// single tier (MentionsPresent distinguishes an absent key — which defaults to
// ChannelMention — from an explicit empty list that pings nobody).
type ChannelSpec struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool
}
```

Add `Channels []ChannelSpec` to `RepoConfig`, documenting the invariant:

```go
	// Channels is the tier's `channels:` fan-out list (unconditional multi-channel
	// base). Mutually exclusive with Channel/Mentions — exactly one form is
	// populated per tier, enforced at decode. Nil means the single Channel form.
	Channels []ChannelSpec
```

- [ ] **Step 4: Implement the tier decoder**

In `internal/routing/infrastructure/config_decode.go`, add the wire type and its decoder:

```go
// channelSpecWire is the YAML wire type for one `channels:` list entry.
type channelSpecWire struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool
}

func (c *channelSpecWire) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("channels entry: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("channels entry: malformed mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("channels entry: non-scalar key")
		}
		if err := markSeen(seen, keyNode.Value); err != nil {
			return fmt.Errorf("channels entry: %w", err)
		}
		switch keyNode.Value {
		case "channel":
			if err := valNode.Decode(&c.Channel); err != nil {
				return fmt.Errorf("channels entry: channel: %w", err)
			}
		case "mentions":
			c.MentionsPresent = true
			if isNullNode(valNode) {
				return fmt.Errorf("channels entry: mentions: null is not allowed; omit the key for @channel or use [] for none")
			}
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("channels entry: mentions: must be a list (use [] for none, omit the key for @channel)")
			}
			ms := []string{}
			if err := valNode.Decode(&ms); err != nil {
				return fmt.Errorf("channels entry: mentions: %w", err)
			}
			c.Mentions = ms
		default:
			return fmt.Errorf("channels entry: unknown field %q", keyNode.Value)
		}
	}
	if c.Channel == "" {
		return fmt.Errorf("channels entry: channel is required")
	}
	return nil
}

func (c channelSpecWire) toDomain() domain.ChannelSpec {
	return domain.ChannelSpec{Channel: c.Channel, Mentions: c.Mentions, MentionsPresent: c.MentionsPresent}
}

// decodeChannelsList decodes a `channels:` sequence node into specs, rejecting an
// empty list and duplicate channel IDs (a config error, not a silent merge).
func decodeChannelsList(node *yaml.Node) ([]domain.ChannelSpec, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("channels: must be a list of {channel, mentions} entries")
	}
	if len(node.Content) == 0 {
		return nil, fmt.Errorf("channels: list is empty (use a single channel: instead, or add entries)")
	}
	out := make([]domain.ChannelSpec, 0, len(node.Content))
	seen := map[string]bool{}
	for _, entryNode := range node.Content {
		wire := &channelSpecWire{}
		if err := entryNode.Decode(wire); err != nil {
			return nil, err
		}
		if seen[wire.Channel] {
			return nil, fmt.Errorf("channels: duplicate channel %q", wire.Channel)
		}
		seen[wire.Channel] = true
		out = append(out, wire.toDomain())
	}
	return out, nil
}
```

Add the field to `repoConfigWire`:

```go
	Channels []domain.ChannelSpec
```

Add a `case "channels":` arm to `repoConfigWire.UnmarshalYAML` (inside the `switch keyNode.Value`), and enforce mutual exclusion after the loop. Insert the case alongside `case "channel":`:

```go
		case "channels":
			specs, err := decodeChannelsList(valNode)
			if err != nil {
				return err
			}
			rc.Channels = specs
```

At the end of `repoConfigWire.UnmarshalYAML`, before `return nil`, add:

```go
	if len(rc.Channels) > 0 {
		if rc.Channel != "" {
			return fmt.Errorf("set either channel: or channels:, not both")
		}
		if rc.MentionsPresent {
			return fmt.Errorf("mentions: is not allowed alongside channels: (put mentions inside each entry)")
		}
	}
```

Map it in `repoConfigWire.toDomain` — add to the `out` literal:

```go
		Channels: rc.Channels,
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/routing/infrastructure/ -run 'TestDecode_(TierChannels|ChannelSpec)' -v`
Expected: PASS. Also run the full package to catch regressions: `go test ./internal/routing/infrastructure/`

- [ ] **Step 6: Commit**

```bash
git add internal/routing/domain/models.go internal/routing/infrastructure/config_decode.go internal/routing/infrastructure/config_decode_test.go
git commit -m "feat(routing): decode tier-level channels: fan-out list"
```

---

## Task 2: Decode `channels:` at the path-rule level

**Files:**
- Modify: `internal/routing/domain/models.go` (add `Channels []ChannelSpec` to `PathRule`)
- Modify: `internal/routing/infrastructure/config_decode.go` (extend `decodePathRule`)
- Test: `internal/routing/infrastructure/config_decode_test.go`

**Interfaces:**
- Consumes: `domain.ChannelSpec`, `decodeChannelsList` (Task 1).
- Produces: `domain.PathRule.Channels []domain.ChannelSpec`.

- [ ] **Step 1: Write failing tests**

```go
func TestDecode_PathChannelsList(t *testing.T) {
	doc := `
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channels:
            - channel: C0PAY1
            - channel: C0PAY2
              mentions: []
`
	f := parseMappings(t, doc)
	rule := f.Mappings["acme"]["monorepo"].Paths[0]
	if rule.Dir != "services/pay" || len(rule.Channels) != 2 {
		t.Fatalf("want 2 path channels under services/pay, got %+v", rule)
	}
	if rule.Channels[1].Channel != "C0PAY2" || !rule.Channels[1].MentionsPresent || len(rule.Channels[1].Mentions) != 0 {
		t.Fatalf("entry 1 should be explicit-empty mentions: %+v", rule.Channels[1])
	}
}

func TestDecode_PathChannelsRejectsMixWithChannel(t *testing.T) {
	doc := `
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channel: C0PAY
          channels:
            - channel: C0PAY1
`
	if _, err := tryParseMappings(doc); err == nil {
		t.Fatal("want error mixing path channel and channels")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/infrastructure/ -run TestDecode_PathChannels -v`
Expected: FAIL — `PathRule.Channels` undefined.

- [ ] **Step 3: Add the field**

In `internal/routing/domain/models.go`, add to `PathRule`:

```go
	// Channels is the path rule's `channels:` fan-out list. Mutually exclusive
	// with Channel/Mentions (enforced at decode); nil means the single Channel
	// form. List entries default absent mentions to ChannelMention.
	Channels []ChannelSpec
```

- [ ] **Step 4: Extend `decodePathRule`**

In `internal/routing/infrastructure/config_decode.go`, add a `case "channels":` arm inside `decodePathRule`'s switch (next to `case "channel":`):

```go
		case "channels":
			specs, err := decodeChannelsList(valNode)
			if err != nil {
				return err
			}
			rule.Channels = specs
```

At the end of `decodePathRule`, before `return nil`, add mutual exclusion:

```go
	if len(rule.Channels) > 0 {
		if rule.Channel != "" {
			return fmt.Errorf("set either channel: or channels:, not both")
		}
		if rule.MentionsPresent {
			return fmt.Errorf("mentions: is not allowed alongside channels: (put mentions inside each entry)")
		}
	}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/routing/infrastructure/ -run TestDecode_PathChannels -v`
Expected: PASS. Then `go test ./internal/routing/infrastructure/`.

- [ ] **Step 6: Commit**

```bash
git add internal/routing/domain/models.go internal/routing/infrastructure/config_decode.go internal/routing/infrastructure/config_decode_test.go
git commit -m "feat(routing): decode path-rule channels: fan-out list"
```

---

## Task 3: Structural validation for the list form

**Files:**
- Modify: `internal/routing/application/validate.go`
- Test: `internal/routing/infrastructure/parse_test.go` (Parse → ValidateMappings) — reuse the pattern already there for `invalid channel` / `no channel` / `paths are not allowed`.

**Interfaces:**
- Consumes: `domain.RepoConfig.Channels`, `domain.PathRule.Channels`.

- [ ] **Step 1: Write failing tests**

In `internal/routing/infrastructure/parse_test.go`, following the existing error-message tests' structure:

```go
func TestParse_ListChannelInvalidID(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels:
        - channel: not-a-channel
`
	if _, err := parse(doc); err == nil { // reuse this file's existing parse entrypoint
		t.Fatal("want error: invalid channel id in channels list")
	}
}

func TestParse_ListSatisfiesChannelRequirement(t *testing.T) {
	doc := `
mappings:
  acme:
    api:
      channels:
        - channel: C0API1
`
	if _, err := parse(doc); err != nil {
		t.Fatalf("a channels: list should satisfy the channel requirement: %v", err)
	}
}

func TestParse_PathListChannelInvalidID(t *testing.T) {
	doc := `
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channels:
            - channel: bad
`
	if _, err := parse(doc); err == nil {
		t.Fatal("want error: invalid path channel id in channels list")
	}
}
```

Match the existing `parse` helper name/signature in `parse_test.go`; if the existing tests call something else (e.g. `mustParse` / a loader), mirror that exactly.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/infrastructure/ -run 'TestParse_(List|PathList)' -v`
Expected: FAIL — a `not-a-channel`/`bad` list channel is not yet format-checked; `TestParse_ListSatisfiesChannelRequirement` fails with "no channel".

- [ ] **Step 3: Extend `ValidateMappings` and `validatePaths`**

In `internal/routing/application/validate.go`, update the `star` channel probe and the per-repo loop. Replace:

```go
		starHasChannel := hasStar && star.Channel != ""
```

with:

```go
		starHasChannel := hasStar && tierHasChannel(star)
```

Replace the tier channel-format + no-channel block:

```go
			if rc.Channel != "" && !channelPattern.MatchString(rc.Channel) {
				return fmt.Errorf("mappings: org %q repo %q: invalid channel %q", org, repo, rc.Channel)
			}
			// Every resolvable path must yield a channel: this tier sets one,
			// or org/* supplies it.
			if rc.Channel == "" && !starHasChannel {
				return fmt.Errorf("mappings: org %q repo %q: no channel (set channel here or in the org's \"*\" entry)", org, repo)
			}
```

with:

```go
			if rc.Channel != "" && !channelPattern.MatchString(rc.Channel) {
				return fmt.Errorf("mappings: org %q repo %q: invalid channel %q", org, repo, rc.Channel)
			}
			for _, spec := range rc.Channels {
				if !channelPattern.MatchString(spec.Channel) {
					return fmt.Errorf("mappings: org %q repo %q: invalid channel %q", org, repo, spec.Channel)
				}
			}
			// Every resolvable path must yield a channel: this tier sets one
			// (single or list), or org/* supplies it.
			if !tierHasChannel(&rc) && !starHasChannel {
				return fmt.Errorf("mappings: org %q repo %q: no channel (set channel here or in the org's \"*\" entry)", org, repo)
			}
```

Add the helper at the bottom of the file:

```go
// tierHasChannel reports whether a tier declares a channel in either form.
func tierHasChannel(rc *domain.RepoConfig) bool {
	return rc != nil && (rc.Channel != "" || len(rc.Channels) > 0)
}
```

In `validatePaths`, extend the loop to format-check list channels. Replace:

```go
	for _, p := range paths {
		if p.Channel != "" && !channelPattern.MatchString(p.Channel) {
			return fmt.Errorf("mappings: org %q repo %q path %q: invalid channel %q", org, repo, p.Dir, p.Channel)
		}
	}
```

with:

```go
	for _, p := range paths {
		if p.Channel != "" && !channelPattern.MatchString(p.Channel) {
			return fmt.Errorf("mappings: org %q repo %q path %q: invalid channel %q", org, repo, p.Dir, p.Channel)
		}
		for _, spec := range p.Channels {
			if !channelPattern.MatchString(spec.Channel) {
				return fmt.Errorf("mappings: org %q repo %q path %q: invalid channel %q", org, repo, p.Dir, spec.Channel)
			}
		}
	}
```

Note: the `o[domain.WildcardKey]` lookup near the top returns a value; `tierHasChannel(&star)` needs an addressable var. If `star` is already a local (it is: `star, hasStar := o[domain.WildcardKey]`), `tierHasChannel(&star)` compiles.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/routing/infrastructure/ -run 'TestParse_(List|PathList)' -v`
Expected: PASS. Then `go test ./internal/routing/...`.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/application/validate.go internal/routing/infrastructure/parse_test.go
git commit -m "feat(routing): validate channels: list channel ids and requirement"
```

---

## Task 4: Base target resolution (`resolveBaseTargets`)

**Files:**
- Modify: `internal/routing/application/resolve.go`
- Test: `internal/routing/application/resolve_test.go`

**Interfaces:**
- Produces: `resolveBaseTargets(star, repo *domain.RepoConfig) []domain.Target` (always ≥1 target); `resolveRouting` now returns the primary (`base[0]`).

- [ ] **Step 1: Write failing tests**

In `internal/routing/application/resolve_test.go`:

```go
func TestResolveBaseTargets_SingleForm(t *testing.T) {
	repo := &domain.RepoConfig{Channel: "C0WEB", Mentions: []string{"<@U0A>"}, MentionsPresent: true}
	got := resolveBaseTargets(nil, repo)
	if len(got) != 1 || got[0].Channel != "C0WEB" || got[0].Mentions[0] != "<@U0A>" {
		t.Fatalf("single form wrong: %+v", got)
	}
}

func TestResolveBaseTargets_ListForm(t *testing.T) {
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{
		{Channel: "C0API1", Mentions: []string{"<@U0A>"}, MentionsPresent: true},
		{Channel: "C0API2"}, // absent mentions → ChannelMention
	}}
	got := resolveBaseTargets(nil, repo)
	if len(got) != 2 {
		t.Fatalf("want 2 targets, got %d", len(got))
	}
	if got[0].Channel != "C0API1" || got[0].Mentions[0] != "<@U0A>" {
		t.Fatalf("target 0 wrong: %+v", got[0])
	}
	if got[1].Channel != "C0API2" || len(got[1].Mentions) != 1 || got[1].Mentions[0] != domain.ChannelMention {
		t.Fatalf("target 1 should default to @channel: %+v", got[1])
	}
}

func TestResolveBaseTargets_RepoListReplacesStarSingle(t *testing.T) {
	star := &domain.RepoConfig{Channel: "C0STAR"}
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{{Channel: "C0R1"}, {Channel: "C0R2"}}}
	got := resolveBaseTargets(star, repo)
	if len(got) != 2 || got[0].Channel != "C0R1" || got[1].Channel != "C0R2" {
		t.Fatalf("repo list should wholly replace star single: %+v", got)
	}
}

func TestResolveBaseTargets_ExplicitEmptyMentionsListForm(t *testing.T) {
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{
		{Channel: "C0API2", Mentions: []string{}, MentionsPresent: true},
	}}
	got := resolveBaseTargets(nil, repo)
	if len(got[0].Mentions) != 0 {
		t.Fatalf("explicit [] should ping nobody: %+v", got[0])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/application/ -run TestResolveBaseTargets -v`
Expected: FAIL — `resolveBaseTargets` undefined.

- [ ] **Step 3: Implement**

In `internal/routing/application/resolve.go`, add the new functions and refactor `resolveRouting` to delegate:

```go
// resolveBaseTargets resolves a tier's base fan-out: the channels a PR is always
// announced to. The most-specific tier that declares a channel (repo, else star),
// in either form, wins wholesale — lists are not merged across tiers. Always
// returns at least one target (an empty-channel target when no tier sets one,
// preserving the single-form behavior).
func resolveBaseTargets(star, repo *domain.RepoConfig) []domain.Target {
	if repo != nil && len(repo.Channels) > 0 {
		return listTargets(repo.Channels)
	}
	if repo != nil && repo.Channel != "" {
		return []domain.Target{{Channel: repo.Channel, Mentions: resolveMentions(star, repo)}}
	}
	if star != nil && len(star.Channels) > 0 {
		return listTargets(star.Channels)
	}
	if star != nil && star.Channel != "" {
		return []domain.Target{{Channel: star.Channel, Mentions: resolveMentions(star, repo)}}
	}
	return []domain.Target{{Channel: "", Mentions: resolveMentions(star, repo)}}
}

// listTargets expands a channels: list into targets, defaulting an absent
// mentions key to ChannelMention (list form has no cross-tier inheritance).
func listTargets(specs []domain.ChannelSpec) []domain.Target {
	out := make([]domain.Target, 0, len(specs))
	for _, spec := range specs {
		out = append(out, domain.Target{Channel: spec.Channel, Mentions: specMentions(spec)})
	}
	return out
}

// specMentions resolves one list entry's mentions: explicit list/[] as given,
// absent key → ChannelMention.
func specMentions(spec domain.ChannelSpec) []string {
	if spec.MentionsPresent {
		return append([]string(nil), spec.Mentions...)
	}
	return []string{domain.ChannelMention}
}

// resolveMentions is the single-form cross-tier tri-state: the most-specific
// tier that set a mentions key wins; absent everywhere falls back to ChannelMention.
func resolveMentions(star, repo *domain.RepoConfig) []string {
	switch {
	case repo != nil && repo.MentionsPresent:
		return append([]string(nil), repo.Mentions...)
	case star != nil && star.MentionsPresent:
		return append([]string(nil), star.Mentions...)
	default:
		return []string{domain.ChannelMention}
	}
}
```

Replace the body of `resolveRouting` so it returns the primary base target (keep the same signature — `Get` and `Entries` still call it):

```go
// resolveRouting returns the primary base target (channel + mentions) for a
// tier: the first of resolveBaseTargets. Consumers that need only one channel
// (RepoMapping.SlackChannel, Entry.Channel) use this.
func resolveRouting(star, repo *domain.RepoConfig) domain.Resolved {
	primary := resolveBaseTargets(star, repo)[0]
	return domain.Resolved{Channel: primary.Channel, Mentions: primary.Mentions}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/routing/application/ -run 'TestResolve' -v`
Expected: PASS (new tests + existing `resolveRouting` tests still green).

- [ ] **Step 5: Commit**

```bash
git add internal/routing/application/resolve.go internal/routing/application/resolve_test.go
git commit -m "feat(routing): resolve base fan-out targets for channels: lists"
```

---

## Task 5: Generalize `TargetsForFiles`, `pathChannels`, add `BaseTargets` + `additionalChannels`

**Files:**
- Modify: `internal/routing/application/paths.go`
- Test: `internal/routing/infrastructure/paths_resolve_test.go` (uses the `providerDoc` helper to parse YAML into a real `Provider`)

**Interfaces:**
- Consumes: `resolveBaseTargets` (Task 4).
- Produces: `(*Provider).BaseTargets(repository string) []domain.Target`; `additionalChannels(star, repo *domain.RepoConfig) []string`; generalized `pathChannels` and `TargetsForFiles`.

- [ ] **Step 1: Write failing tests**

In `internal/routing/infrastructure/paths_resolve_test.go` (reuse `providerDoc`):

```go
func TestBaseTargets_MultiChannelBaseNoPaths(t *testing.T) {
	p := providerDoc(t, `
mappings:
  acme:
    api:
      channels:
        - channel: C0API1
          mentions: ["<@U0A>"]
        - channel: C0API2
`)
	got := p.BaseTargets("acme/api")
	if len(got) != 2 || got[0].Channel != "C0API1" || got[1].Channel != "C0API2" {
		t.Fatalf("want both base channels, got %+v", got)
	}
	if got[1].Mentions[0] != domain.ChannelMention {
		t.Fatalf("second channel should default to @channel: %+v", got[1])
	}
}

func TestTargetsForFiles_PathChannelsListReplacesBase(t *testing.T) {
	p := providerDoc(t, `
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channels:
            - channel: C0PAY1
            - channel: C0PAY2
              mentions: []
`)
	got := p.TargetsForFiles("acme/monorepo", []string{"services/pay/x.go"})
	if len(got) != 2 || got[0].Channel != "C0PAY1" || got[1].Channel != "C0PAY2" {
		t.Fatalf("matched path list should replace base: %+v", got)
	}
	if len(got[1].Mentions) != 0 {
		t.Fatalf("C0PAY2 explicit [] should ping nobody: %+v", got[1])
	}
}

func TestTargetsForFiles_MultiBaseReturnedWhenNoPathMatch(t *testing.T) {
	p := providerDoc(t, `
mappings:
  acme:
    monorepo:
      channels:
        - channel: C0B1
        - channel: C0B2
      paths:
        services/pay:
          channel: C0PAY
`)
	got := p.TargetsForFiles("acme/monorepo", []string{"README.md"})
	if len(got) != 2 || got[0].Channel != "C0B1" || got[1].Channel != "C0B2" {
		t.Fatalf("no path match should return full base set: %+v", got)
	}
}

func TestTargetsForFiles_ChannelLessPathInheritsPrimary(t *testing.T) {
	p := providerDoc(t, `
mappings:
  acme:
    monorepo:
      channels:
        - channel: C0PRIMARY
        - channel: C0SECOND
      paths:
        services/pay:
          mentions: ["<@U0PAY>"]
`)
	got := p.TargetsForFiles("acme/monorepo", []string{"services/pay/x.go"})
	if len(got) != 1 || got[0].Channel != "C0PRIMARY" || got[0].Mentions[0] != "<@U0PAY>" {
		t.Fatalf("channel-less path should inherit primary base channel: %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/infrastructure/ -run 'TestBaseTargets|TestTargetsForFiles_(PathChannelsList|MultiBase|ChannelLess)' -v`
Expected: FAIL — `BaseTargets` undefined; list-form path channels ignored.

- [ ] **Step 3: Implement**

In `internal/routing/application/paths.go`:

Add `BaseTargets`:

```go
// BaseTargets returns the repository's unconditional base fan-out targets (the
// channels every PR is announced to before any path rules apply). The router
// uses it when the repo has no path rules or no changed-files reader.
func (p *Provider) BaseTargets(repository string) []domain.Target {
	starPtr, repoPtr := p.lookup(repository)
	return resolveBaseTargets(starPtr, repoPtr)
}
```

Replace `pathChannels` so it also collects list-form path channels:

```go
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
```

Add `additionalChannels` (base channels beyond the primary ∪ path channels), used by the lock hash (Task 7) and validation (Task 8):

```go
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
```

Rewrite `TargetsForFiles` to use the base set and expand list-form path rules. Replace the whole function body:

```go
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
```

Delete the now-unused `unionMentions` function (replaced by `unionContributions`). Confirm no other caller: `grep -rn "unionMentions" internal`. If a test references it, update that test to the new behavior.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/routing/infrastructure/ -run 'TestBaseTargets|TestTargetsForFiles' -v`
Expected: PASS (new + existing `TestTargetsForFiles_*` cases). Then `go test ./internal/routing/...`.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/application/paths.go internal/routing/infrastructure/paths_resolve_test.go
git commit -m "feat(routing): fan out base + path channels: lists to targets"
```

---

## Task 6: Router returns the full base target set

**Files:**
- Modify: `internal/routing/domain/interfaces.go` (add `BaseTargets` to `RoutingProvider`)
- Modify: `internal/routing/application/router.go`
- Test: `internal/routing/application/router_test.go` (extend `stubMappings`)

**Interfaces:**
- Consumes: `(*Provider).BaseTargets` (Task 5).
- Produces: `RoutingProvider.BaseTargets(repository string) []Target`.

- [ ] **Step 1: Write failing test**

In `internal/routing/application/router_test.go`, extend `stubMappings` and add a test. First add the field + method to the stub:

```go
// add to stubMappings struct:
	baseTargets []domain.Target

// add method:
func (s *stubMappings) BaseTargets(string) []domain.Target { return s.baseTargets }
```

Then the test:

```go
func TestResolveTargets_NoPathRulesReturnsFullBaseSet(t *testing.T) {
	stub := &stubMappings{
		base:        domain.RepoMapping{SlackChannel: "C0B1"},
		baseTargets: []domain.Target{{Channel: "C0B1"}, {Channel: "C0B2"}},
		hasPathRules: false,
	}
	router := NewRouter(stub, nil, slog.Default())
	_, targets, err := router.ResolveTargets(context.Background(), "acme/api", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Channel != "C0B1" || targets[1].Channel != "C0B2" {
		t.Fatalf("router should return full base set when no path rules: %+v", targets)
	}
}
```

(Reuse the imports/logger style already present in `router_test.go`; if it constructs a logger differently, match it.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/application/ -run TestResolveTargets_NoPathRulesReturnsFullBaseSet -v`
Expected: FAIL — router still returns a single target built from `behavior.SlackChannel`; also the interface may not compile until the method is added.

- [ ] **Step 3: Implement**

In `internal/routing/domain/interfaces.go`, add to `RoutingProvider`:

```go
	// BaseTargets returns the repository's unconditional base fan-out targets,
	// used when the repo has no path rules or no changed-files reader.
	BaseTargets(repository string) []Target
```

In `internal/routing/application/router.go`, replace the `baseTarget` construction and its uses. Replace:

```go
	baseTarget := []domain.Target{{Channel: behavior.SlackChannel, Mentions: behavior.Mentions}}

	if r.files == nil || !r.mappings.RepoHasPathRules(repository) {
		return behavior, baseTarget, nil
	}
```

with:

```go
	baseTargets := r.mappings.BaseTargets(repository)

	if r.files == nil || !r.mappings.RepoHasPathRules(repository) {
		return behavior, baseTargets, nil
	}
```

And replace the two remaining `return behavior, baseTarget, nil` lines (the `!ok` split and the files-error fallback) with `return behavior, baseTargets, nil`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/routing/application/ -run TestResolveTargets -v`
Expected: PASS (new + existing router tests). Some existing router tests may need `baseTargets` set on their stub where they previously relied on `base.SlackChannel`; update those stubs to set `baseTargets` to the expected single-element set.

- [ ] **Step 5: Commit**

```bash
git add internal/routing/domain/interfaces.go internal/routing/application/router.go internal/routing/application/router_test.go
git commit -m "feat(routing): router returns full base target set"
```

---

## Task 7: Lock entry carries all additional channels

**Files:**
- Modify: `internal/routing/domain/entry.go` (rename `PathChannels` → `ExtraChannels`, keep JSON tag `path_channels`)
- Modify: `internal/routing/application/provider.go` (`Entries` uses `additionalChannels` for explicit + wildcard entries)
- Test: `internal/routing/domain/entry_test.go`

**Interfaces:**
- Consumes: `additionalChannels` (Task 5).
- Produces: `domain.Entry.ExtraChannels []string`.

- [ ] **Step 1: Write failing tests**

In `internal/routing/domain/entry_test.go`, add hash-stability + change tests (and update any existing references to `PathChannels`):

```go
func TestEntryHash_StableForSingleChannel(t *testing.T) {
	a := domain.Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}
	b := domain.Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}
	if a.Hash() != b.Hash() {
		t.Fatal("single-channel hash must be stable")
	}
}

func TestEntryHash_ChangesWhenExtraChannelAdded(t *testing.T) {
	before := domain.Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}
	after := domain.Entry{Org: "acme", Repo: "api", Channel: "C0API", ExtraChannels: []string{"C0API2"}, Provider: kernel.ProviderGitHub}
	if before.Hash() == after.Hash() {
		t.Fatal("adding a channels: list must change the entry hash")
	}
}
```

Use whatever `kernel.Provider` value the existing `entry_test.go` uses (match its import + constant; if the existing tests use a literal like `kernel.ProviderGitHub`, reuse it).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/routing/domain/ -run TestEntryHash -v`
Expected: FAIL — `ExtraChannels` field undefined.

- [ ] **Step 3: Rename the field, keep the hash tag**

In `internal/routing/domain/entry.go`, rename the struct field and update its doc, keeping the hashed JSON tag `path_channels` so existing locks stay valid:

```go
	// ExtraChannels are the distinct channels a repo can post to beyond its
	// primary Channel — extra base-list channels plus per-path channels (sorted,
	// deduped). They feed both validation (bot membership) and the entry hash, so
	// adding or repointing one re-triggers validation. Always empty for a wildcard
	// entry unless the org/* tier itself uses a channels: list.
	ExtraChannels []string
```

In `Hash()`, update the payload field name but **keep the JSON tag** so single-channel hashes are unchanged:

```go
		PathChannels []string        `json:"path_channels,omitempty"`
	}{e.Provider, e.Org, repo, e.Channel, e.ExtraChannels}
```

(The struct field in the anonymous payload may keep the name `PathChannels` — only the JSON tag matters for the hash. Assign `e.ExtraChannels` to it.)

- [ ] **Step 4: Update `Entries` construction**

In `internal/routing/application/provider.go`, update both entry constructions to use `additionalChannels`. Replace the explicit-repo append:

```go
			res := resolveRouting(starPtr, &rc)
			out = append(out, domain.Entry{
				Org:          org,
				Repo:         r,
				Channel:      res.Channel,
				Mentions:     res.Mentions,
				PathChannels: pathChannels(rc.Paths),
				Provider:     p.defaults.GitProvider,
			})
```

with:

```go
			res := resolveRouting(starPtr, &rc)
			out = append(out, domain.Entry{
				Org:           org,
				Repo:          r,
				Channel:       res.Channel,
				Mentions:      res.Mentions,
				ExtraChannels: additionalChannels(starPtr, &rc),
				Provider:      p.defaults.GitProvider,
			})
```

Replace the wildcard append so a `channels:` list on org/* also contributes extras:

```go
		if starPtr != nil {
			res := resolveRouting(starPtr, nil)
			out = append(out, domain.Entry{Org: org, Wildcard: true, Channel: res.Channel, Mentions: res.Mentions, ExtraChannels: additionalChannels(starPtr, nil), Provider: p.defaults.GitProvider})
		}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/routing/... -run 'TestEntry|TestEntries' -v`
Expected: PASS. Then `go test ./internal/routing/...` (fix any remaining `PathChannels` field references the compiler flags).

- [ ] **Step 6: Commit**

```bash
git add internal/routing/domain/entry.go internal/routing/application/provider.go internal/routing/domain/entry_test.go
git commit -m "feat(routing): fold all additional channels into the lock entry"
```

---

## Task 8: Validation covers every additional channel

**Files:**
- Modify: `internal/validation/domain/interfaces.go` (rename `PathChannels` → `AdditionalChannels`)
- Modify: `internal/routing/application/paths.go` (rename the `PathChannels` method → `AdditionalChannels`, returning `additionalChannels`)
- Modify: `internal/validation/application/validator.go` (call `AdditionalChannels`)
- Modify: `internal/validation/application/mocks_test.go`, `internal/validation/application/validator_test.go`, `internal/validation/module_test.go` (rename)
- Modify: `internal/routing/infrastructure/paths_resolve_test.go` (any `PathChannels` method calls → `AdditionalChannels`)

**Interfaces:**
- Consumes: `additionalChannels` (Task 5), `resolveBaseTargets`.
- Produces: `MappingLookup.AdditionalChannels(repository string) []string`; `(*Provider).AdditionalChannels`.

- [ ] **Step 1: Write failing test**

In `internal/validation/application/validator_test.go`, add a case asserting every base-list channel is membership-checked. Follow the file's existing `mockMappingLookup` / `mockSlackChecker` setup:

```go
func TestValidate_ChecksEveryBaseListChannel(t *testing.T) {
	checked := map[string]bool{}
	mappings := &mockMappingLookup{
		get: func(_ context.Context, repository string) (routingdomain.RepoMapping, error) {
			return routingdomain.RepoMapping{Repository: repository, SlackChannel: "C0B1"}, nil
		},
		additionalChannels: func(string) []string { return []string{"C0B2"} },
	}
	slack := &mockSlackChecker{
		conversationsInfo: func(_ context.Context, channel string) (domain.ChannelInfo, error) {
			checked[channel] = true
			return domain.ChannelInfo{IsMember: true}, nil
		},
	}
	v := NewValidator(mappings, slack, nil)
	_ = v.Validate(context.Background(), "acme/api")
	if !checked["C0B1"] || !checked["C0B2"] {
		t.Fatalf("both base-list channels must be checked, got %v", checked)
	}
}
```

Adjust field names (`get`, `additionalChannels`, `conversationsInfo`) and `ChannelInfo` fields to match the real mock structs in `mocks_test.go` — rename the mock's `pathChannels` field to `additionalChannels` as part of this task.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/validation/application/ -run TestValidate_ChecksEveryBaseListChannel -v`
Expected: FAIL to compile — `AdditionalChannels` not yet defined / mock field renamed.

- [ ] **Step 3: Rename the port and method**

In `internal/validation/domain/interfaces.go`, rename in `MappingLookup`:

```go
	// AdditionalChannels returns the channels a repo can post to beyond its
	// primary SlackChannel — extra base-list channels plus per-path channels — so
	// the validator confirms bot membership for every one.
	AdditionalChannels(repository string) []string
```

In `internal/routing/application/paths.go`, rename the exported method (currently `PathChannels`) to `AdditionalChannels` and widen it:

```go
// AdditionalChannels returns the channels the repository can post to beyond its
// primary base channel (extra base-list channels plus per-path channels), sorted
// and deduped. The validator checks bot membership for each; the doctor and
// notifycat-config validate share this via the validation port.
func (p *Provider) AdditionalChannels(repository string) []string {
	starPtr, repoPtr := p.lookup(repository)
	return additionalChannels(starPtr, repoPtr)
}
```

Update its doc comment block above the method (the old `PathChannels` comment) accordingly.

In `internal/validation/application/validator.go` line ~64, change:

```go
	r.Checks = append(r.Checks, v.slackChecks(ctx, m.SlackChannel, v.mappings.AdditionalChannels(m.Repository))...)
```

(`slackChecks`'s second parameter and its loop keep working — they iterate whatever extra channels are passed. Rename the parameter from `pathChannels` to `additionalChannels` inside `slackChecks` for readability, and the `"slack-channel "+pc` label loop stays as-is.)

- [ ] **Step 4: Update remaining references**

Update the mock in `internal/validation/application/mocks_test.go` (rename field `pathChannels` → `additionalChannels` and the method `PathChannels` → `AdditionalChannels`), plus any callers in `validator_test.go` and `internal/validation/module_test.go`. In `internal/routing/infrastructure/paths_resolve_test.go`, rename any `p.PathChannels(...)` call to `p.AdditionalChannels(...)` and update expectations (it now includes extra base channels, not only path channels).

Run a sweep to catch stragglers: `grep -rn "PathChannels" internal` — the only remaining hits should be the JSON tag `path_channels` in `entry.go` and internal helper `pathChannels` in `paths.go` (both intentional). Fix anything else.

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/validation/... ./internal/routing/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/validation/domain/interfaces.go internal/routing/application/paths.go internal/validation/application/validator.go internal/validation/application/mocks_test.go internal/validation/application/validator_test.go internal/validation/module_test.go internal/routing/infrastructure/paths_resolve_test.go
git commit -m "feat(validation): check bot membership for every base and path channel"
```

---

## Task 9: Docs + full verification

**Files:**
- Modify: config documentation that shows the `mappings:` schema (find with `grep -rln "channel:" docs README.md resources 2>/dev/null` and `grep -rln "paths:" docs`). Update the mappings reference to document `channels:` at tier and path level, the tri-state mentions default, and the replace-on-match interaction.
- Modify: any example `config.example.yaml` / sample in the repo (find with `grep -rln "mappings:" . --include="*.yaml" --include="*.yml"`; do **not** touch gitignored `config.yaml`).

- [ ] **Step 1: Update the mappings documentation**

Add a `channels:` subsection to the mappings reference doc: the list form at tier level (unconditional multi-channel), the same form on a path rule, per-entry mentions tri-state (`absent → @channel`, `[] → nobody`, `[list] → those`), and the rule that a matched path with channels replaces the base entirely. Do not hard-wrap the markdown (repo convention — GFM renders manual wraps as breaks). Mirror the style of the existing single-channel docs.

- [ ] **Step 2: Run full local verification**

Run: `just check`
Expected: PASS — `go vet`, `golangci-lint`, `govulncheck`, `go test -race`, `go build` all green.

If `just` is unavailable, run: `go vet ./... && go test -race ./... && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add docs README.md resources config.example.yaml 2>/dev/null; git commit -m "docs: document channels: multi-channel fan-out"
```

(Adjust the `git add` paths to the files that actually changed.)

---

## Self-Review (completed during planning)

**Spec coverage:**
- Schema `channel` xor `channels` at tier + path level → Tasks 1, 2 (decode) + 3 (structural validation).
- All decode rejections (both keys, mentions+channels, missing channel, empty list, duplicate) → Task 1/2 tests.
- Base target set + whole-tier replacement + list/single mentions asymmetry → Task 4.
- `paths:` replace-on-match generalized to lists + dedup/union → Task 5.
- Router returns full base set (multi-channel base with no paths) → Task 6.
- Validation per every channel → Task 8.
- Lock hash folds full channel set, stable for existing configs → Task 7.
- Notification/persistence unchanged → no task needed (verified in spec).
- Docs → Task 9.

**Placeholder scan:** No TBD/TODO; every code step shows real code; test helpers reference existing file helpers by name with instructions to match the real signature.

**Type consistency:** `ChannelSpec{Channel, Mentions, MentionsPresent}`, `resolveBaseTargets`/`listTargets`/`specMentions`/`resolveMentions`, `additionalChannels` (helper) vs `AdditionalChannels` (exported method/port), `BaseTargets`, `Entry.ExtraChannels` (Go field) with JSON tag `path_channels` — used consistently across Tasks 4–8. `unionMentions` is replaced by `unionContributions`; Task 5 deletes the former.
