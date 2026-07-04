# Phase 2 — routing domain (mappings + router)

Routing resolves a repo (and optionally a PR's changed files) to the Slack channel(s) + behavioural config that apply, across global/org/repo tiers and monorepo path rules. It is the foundation notification, digest, and validation build on, so it migrates before them. `internal/mappings` is the largest source package (~10 files) — this phase begins with a **survey step** to enumerate the exact surface, then moves it in coherent slices.

Target tree:

```
internal/routing/
  domain/
    interfaces.go   # ports: ChangedFilesReader, + use-case interfaces (TargetResolver, RoutingProvider)
    models.go       # DTOs: RepoMapping, Target, Reactions, PathRule, Entry, Defaults, Digest config
    enums.go        # tier kinds (global/org/repo), wildcard markers
    constants.go
  application/
    provider.go     # tier resolution, defaults merge (was mappings.Provider)
    router.go       # per-PR changed-files → path-channel routing (was pullrequest.Router)
  infrastructure/
    yaml_loader.go  # parse config.yaml mappings section (was mappings parsing)
    lock_store.go   # config.lock read/write/diff/merge
    github_files.go # ChangedFilesReader adapter over internal/github
  module.go
  module_test.go
```

> **Expand before running:** the orchestrator first runs `grep -rnE '^(func|type|const|var) ' internal/mappings/*.go internal/pullrequest/router*.go | grep -v _test.go` and fills each task's exact symbol list. Ports and DTOs below are named by intent; match the real signatures.

## Expanded design (survey done 2026-07-04)

**Reality that reshapes the plan:** `store.RepoMapping`, `store.Target`, `store.Reactions` are **pure value objects — no gorm tags** (only `PullRequest`/`Message`/`CodeReview` are GORM models). They are consumed by four still-unmigrated domains (pullrequest, digest, validate, smoke) plus `internal/app`, and `store.ErrNotFound` is checked via `errors.Is` at 11 call sites. Duplicating these into routing/domain as *distinct* types would force the provider/router to return routing types while consumers expect store types → a cross-domain retype touching dozens of test constructions. That is **not** a pure move.

**Decision — type aliases (zero-churn).** routing/domain owns the canonical `RepoMapping`/`Target`/`Reactions`/`ErrNotFound`. `internal/store/models.go` re-declares them as aliases:
```go
type Reactions = routingdomain.Reactions
type Target = routingdomain.Target
type RepoMapping = routingdomain.RepoMapping
var ErrNotFound = routingdomain.ErrNotFound   // identity preserved; string "store: not found" kept verbatim
```
Every existing `store.X` reference keeps compiling and is now identical to `routingdomain.X`. Consequently the new `routingapp.Provider`/`Router` (returning `routingdomain.*`) satisfy `pullrequest`'s own `RepoBehavior`/`TargetResolver` consumer interfaces (which name `store.*`) **structurally, unchanged** — pullrequest/deps.go and its handler tests do not change. `store` importing `routingdomain` creates no cycle (routingdomain imports only stdlib/yaml/kernel). Transition scaffolding, removed in Phase 8 when store→platform/persistence.

**Layer assignment (CORRECTED per user's architecture directive 2026-07-04: domain = abstractions/DTOs/models/enums/constants ONLY — no logic, no I/O, no YAML; business logic → application; file & network I/O → infrastructure; cross-module deps only through interfaces/facades).** The config-tree types stay as a SINGLE set of pure DTOs in domain; infrastructure decodes YAML into them (no duplicate wire-types).
- **routing/domain** (imports stdlib + kernel only — NO yaml, NO store/github/slack): pure DTOs + contracts. `RepoMapping`,`Target`,`Reactions` (models.go, aliased by store); `ErrNotFound` (errors.go, aliased by store); `Entry`+`Key()`+`Hash()` (entry.go — value-object identity, kept in domain); `Defaults`,`Resolved`,`DigestConfig`,`Org`,`RepoConfig`,`PathRule`,`ReactionsOverride`,`File` as PURE structs, no methods (models.go); `ChannelMention`,`WildcardKey`(was unexported `starKey="*"`) (enums.go); `DefaultDigestSchedule` (constants.go); ports `RoutingProvider`,`TargetResolver`,`ChangedFilesReader` (interfaces.go). **No Parse, no UnmarshalYAML, no ValidateMappings here.**
- **routing/application** (imports routing/domain + stdlib only): the business logic. `Provider`,`NewProvider`,`Digest`,`lookup`,`Get`,`Entries`,`DigestFor`,`Schedules`,`splitRepo` (provider.go — NO `Load`); `resolveRouting`,`resolveBehavior`,`setStr` (resolve.go); `HasPathRules`,`RepoHasPathRules`,`PathChannels`,`pathChannels`,`TargetsForFiles`,`matchedRules`,`fileUnder`,`unionMentions` (paths.go); `ValidateMappings`,`validatePaths`,`orgPattern`/`repoPattern`/`channelPattern` (validate.go — business validation rules); `Router`,`NewRouter`,`ResolveTargets`,`splitOwnerRepo` (router.go). Swap `store.`→`domain.`, config-types→`domain.`, `starKey`→`domain.WildcardKey` in every body.
- **routing/infrastructure** (no depguard rule — may import routing/application+domain, store, github, os, yaml): file/network I/O. `yaml_loader.go` = `Parse` (decode YAML → `domain.File`, calling `application.ValidateMappings` where `File.validate` used to; the old 3 `UnmarshalYAML` methods become decode FUNCTIONS operating on `*domain.*`, since domain DTOs carry no methods) + `decodeReviews`/`decodePaths`/`normalizePathKey`/`decodePathRule`/`markSeen`/`isNullNode` + `Load` (os.Open + Parse + `application.NewProvider`) + `FileNotFoundError`/`ParseError`; `lock_store.go` = `Lock`,`LockEntry`,`Diff`,`ReadLock`,`WriteLock`,`DiffEntries`,`MergeLock`,`LockPath`,`LockFileComment`,`LockVersion`; `github_files.go` = `ChangedFilesReader` adapter over `github.Client`.

**Test placement (corrected):** YAML/file-driven tests (`provider_test`,`paths_resolve_test`,`paths_test`,`digest_test`,`lock_test`) go to `infrastructure_test` (they drive via `Load`/`Parse` and may import application+domain — infra has no depguard rule). `resolve_test` (unexported resolveRouting/resolveBehavior) → `package application`. `router_test` → `application_test` (fakes implement the domain ports).

**Ports (domain/interfaces.go):**
```go
type RoutingProvider interface {           // provider satisfies it; router depends on it (was pullrequest.PathMappings)
    Get(ctx context.Context, repository string) (RepoMapping, error)
    RepoHasPathRules(repository string) bool
    TargetsForFiles(repository string, files []string) []Target
}
type ChangedFilesReader interface {        // was pullrequest.ChangedFiles; github.Client satisfies it
    ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error)
}
type TargetResolver interface {            // the Router
    ResolveTargets(ctx context.Context, repository string, prNumber int) (RepoMapping, []Target, error)
}
```

**Execution model — strangler parallel-copy.** `internal/mappings` + `pullrequest/router.go` stay **fully intact and compiling** through T2–T5; routing is built as a parallel copy. TESTS are **moved** (deleted from mappings/pullrequest, added to routing) as each layer lands, so the race suite runs the same assertions against the copy — no duplication, count preserved. T6 flips every `mappings.X` caller to `routing*.X`, deletes `internal/mappings`, and adds the fx module. Squash-merge collapses the transient parallel source. Because `store` aliases the value objects, an intact `*mappings.Provider` satisfies `routingdomain.RoutingProvider`, so T5 can wire `routingapp.NewRouter(mappingsProvider, …)` before mappings is gone.

**T6 caller-rewire — symbol → new package:**
| old | new |
| --- | --- |
| `mappings.Provider`,`NewProvider` | `routingapp.Provider`,`routingapp.NewProvider` |
| `mappings.Entry`,`Defaults`,`DigestConfig`,`Org`,`RepoConfig`,`PathRule`,`File`,`ValidateMappings`,`ChannelMention`,`WildcardKey`,`DefaultDigestSchedule` | `routingdomain.*` |
| `mappings.Parse` | `routingdomain.Parse` |
| `mappings.Load`,`FileNotFoundError`,`ParseError`,`Lock`,`LockEntry`,`Diff`,`ReadLock`,`WriteLock`,`DiffEntries`,`MergeLock`,`LockPath`,`LockVersion`,`LockFileComment` | `routinginfra.*` |
| `pullrequest.Router`,`NewRouter` | `routingapp.Router`,`routingapp.NewRouter` |
Callers to touch: `internal/config/config.go`, `internal/app/app.go`, `internal/validate/runner.go`, `internal/digest/reporter.go`, `internal/doctor/{doctor,mappings_check}.go`, `internal/mappingcli/{list,validate}.go`, `internal/smoke/smoke.go`, `cmd/notifycat-{config,doctor,smoke}/main.go` — plus each package's `_test.go` that constructs those symbols (qualifier swap only).

---

### P2-T1 — Survey + scaffold routing
- Depends on: Phase 1
- Context budget: read ONLY `refactoring/README.md`, `ARCHITECTURE.md`; grep `internal/mappings/*.go` and `internal/pullrequest/router*.go` for the surface.
- Goal: enumerate the mappings/router surface into this file's task specs, then create the empty three-layer structure + module skeleton + depguard rules.
- Changes: create `internal/routing/{domain,application,infrastructure}/doc.go`, `internal/routing/module.go`; modify `.golangci.yml`; update this phase file's later tasks with the real symbol lists (commit that as `docs:`).
- Spec: depguard rules for routing layers mirror maintenance's (domain imports only kernel; application not importing infrastructure/store/github; infrastructure may import `internal/mappings`-era deps as transition debt until fully moved).
- Verify: `go build ./internal/routing/...`; `golangci-lint run` green.
- DoD: structure compiles; symbol lists filled in; rules staged.
- Commit: `refactor(routing): scaffold domain/application/infrastructure`

### P2-T2 — Move mapping models + enums into routing/domain
- Depends on: P2-T1
- Context budget: read ONLY `internal/store/models.go` (the `RepoMapping`, `Target`, `Reactions` types) and the mappings type files identified in T1.
- Goal: land the pure DTOs and enums the domain owns.
- Changes: create `internal/routing/domain/{models,enums,constants}.go`.
- Spec: define `RepoMapping`, `Target`, `Reactions`, `PathRule`, `Entry`, `Defaults`, and the digest-config DTO as pure structs (no `gorm` tags — these are domain DTOs, distinct from `store`'s GORM models even where fields overlap). Promote tier kinds and wildcard/tier markers to `enums.go`/`constants.go` (no inline `"*"`, `"org"`, etc.). Keep `Entry.Key()`/`Entry.Hash()` as methods on the DTO (they define lock identity — part of the domain contract). Do not delete the `store` versions yet; other unmigrated code still uses them.
- Constraints: pure types only; no behaviour.
- Verify: `go build ./internal/routing/...`; `gofmt -l` empty.
- DoD: routing domain DTOs/enums compile.
- Commit: `refactor(routing): add domain models and enums`

### P2-T3 — Move the provider (tiers + defaults merge) into routing/application
- Depends on: P2-T2
- Context budget: read ONLY the mappings provider/tier/defaults files from T1 and their tests.
- Goal: relocate `mappings.Provider` (tier resolution, defaults merge, `Entries`/`Schedules`/`HasPathRules`) as the routing provider use case.
- Changes: create `internal/routing/application/provider.go`; add `RoutingProvider` + `TargetResolver` use-case interfaces to `domain/interfaces.go`; move provider tests; keep `internal/mappings` compiling for now (callers migrate in T5).
- Spec: port `NewProvider(defaults, mappings, digest)` and the resolution logic unchanged, retyped to routing domain DTOs. Interface `RoutingProvider` exposes what consumers need (`Entries`, `Schedules`, `HasPathRules`, behaviour/target resolution). Doc blocks on the interface.
- Constraints: tier precedence (global → org/* → org/repo) and defaults merge identical.
- Verify: `go test ./internal/routing/...` green.
- DoD: provider logic lives in routing/application behind an interface.
- Commit: `refactor(routing): relocate mappings provider`

### P2-T4 — Move parsing + lock into routing/infrastructure
- Depends on: P2-T3
- Context budget: read ONLY the mappings parsing + lock files from T1 and their tests.
- Goal: relocate YAML parsing of the `mappings:` section and the `config.lock` read/write/diff/merge as infrastructure adapters.
- Changes: create `internal/routing/infrastructure/{yaml_loader,lock_store}.go`; move the corresponding tests; delete the moved files from `internal/mappings`.
- Spec: parsing produces routing domain DTOs. Lock logic (`ReadLock`/`WriteLock`/`DiffEntries`/`MergeLock`/`LockPath`/`LockVersion`) moves verbatim, retyped to domain `Entry`. These are infrastructure (file + YAML I/O).
- Verify: `go test ./internal/routing/...` green.
- DoD: parsing + lock in routing/infrastructure.
- Commit: `refactor(routing): relocate mappings parsing and lock store`

### P2-T5 — Move the per-PR Router + changed-files port
- Depends on: P2-T4
- Context budget: read ONLY `internal/pullrequest/router*.go` (+ test) and `internal/app/app.go` (`buildRouter`) and `internal/github` surface (grep).
- Goal: relocate the per-PR target Router and its `ChangedFiles` dependency as a routing port + github adapter.
- Changes: create `internal/routing/application/router.go`, `internal/routing/infrastructure/github_files.go`; add `ChangedFilesReader` port to `domain/interfaces.go`; modify `internal/app/app.go` (`buildRouter`); move the router test.
- Spec: `pullrequest.Router` → `application` (resolves targets from provider + changed files). `pullrequest.ChangedFiles` → `domain.ChangedFilesReader` port; the github implementation → `infrastructure/github_files.go` wrapping `internal/github` (transition debt). `app.buildRouter` rewired. Behaviour: no fetcher ⇒ repo/org tier fallback, unchanged.
- Verify: `go build ./...`; `go test ./internal/routing/... ./internal/app/...` green.
- DoD: router in routing; `pullrequest` no longer owns routing.
- Commit: `refactor(routing): relocate per-PR router and changed-files port`

### P2-T6 — Rewire remaining callers + delete internal/mappings; fx module
- Depends on: P2-T5
- Context budget: grep the repo for `internal/mappings` and `mappings.` importers; read only those call-sites.
- Goal: point every caller (app.Wire, validate, digest, mappingcli, doctor) at `internal/routing`, delete `internal/mappings`, and add the routing fx module + fxtest.
- Changes: modify all importers of `internal/mappings`; delete `internal/mappings`; fill `internal/routing/module.go`; add `module_test.go`.
- Spec: mechanical import re-point (callers still consume the same interface, now from routing). `Module` binds provider + router + adapters to their ports; fxtest proves the graph with stubbed inputs (config values, github client). Callers that are themselves unmigrated (validate/digest/doctor) keep working against the routing interface — they migrate in their own phases.
- Constraints: no behaviour change to any caller.
- Verify: `go build ./...`; `go test -race ./...` green; `golangci-lint run` green; `! grep -rq 'internal/mappings' --include=*.go .`
- DoD: `internal/mappings` gone; routing owns routing; module proven.
- Commit: `refactor(routing): rewire callers and add fx module`

---

## Phase 2 gate
- [ ] `go build ./...` green · `go test -race ./...` green (unchanged) · `golangci-lint run` green
- [ ] `internal/mappings` deleted; router out of `pullrequest`
- [ ] `routing.Module` proven by fxtest
- [ ] `REFACTORING_PLAN.md` Phase 2 box checked
