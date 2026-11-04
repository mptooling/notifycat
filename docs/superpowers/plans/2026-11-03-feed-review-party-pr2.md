# Party settings per repository and monorepo path: implementation plan (PR 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `party_reviewers` and `party_deadline` also work on mapping tiers and monorepo paths. Notifycat finds the cap and the timer of a PR from its repository and its changed files each time it records the PR.

**Architecture:** The `routing` domain parses the new keys and answers one question through a new port, `routingdomain.PartyRules`. `NewProvider` builds a Slack view without party-only entries, so Slack routing, `Entries()`, `config.lock` and validation never see them. A new feed adapter, `RoutingPartySettings`, reads the changed files only when a path rule sets a party key. It never fails: on a read error it logs and reports a fallback, and the recorder then keeps the stored party state.

**Tech Stack:** Go 1.27.3, uber/fx (dig v1.19.0), `gopkg.in/yaml.v3` node decoders, testify.

**Spec:** `docs/superpowers/specs/2026-11-03-feed-review-party-design.md`, "Delivery in 2 PRs" item 2. PR 2 owns calls 2, 13, 16 and 19, and the tier and path levels of calls 1, 12, 15 and 18.

**Base:** branch `feat/feed-party-path-settings` from the tip of `feat/feed-review-party` (`12c8627`, after the PR 1 fix wave). The plan names symbols, not line numbers.

## Global Constraints

- DDD + hexagonal + uber/fx per `CLAUDE.md`. Every use case and every infrastructure service has an interface in its domain's `interfaces.go`. Enums go in `enums.go`, constants in `constants.go`. More than 3 arguments (counting `ctx`) to an exported func or constructor → a DTO. Doc comments go on interfaces only. One `fx.Module` per domain.
- `feed/domain` imports only the kernel and the standard library. `feed/application` imports only `feed/domain` and the kernel. `feed/infrastructure` may import `routing/domain`, never `routing/application`.
- Readable names (no `m`, `d`, `tc`). Table tests use `testCases`/`testCase`.
- testify, `(t, want, got)`, arrange / act / assert, `require` for setup, `assert` for final checks.
- Minimal comments. None that restate code.
- Conventional Commits. Never mention AI or Claude in commits. No `Co-Authored-By` trailer. Never the literal string `BREAKING CHANGE`. PR title and squash commit: `feat(feed): party settings per repository and monorepo path`.
- Repository names are lower case everywhere in the feed. Mapping keys keep their case. The party lookup folds case (ruling 4).
- Party keys never change Slack. The Slack path matcher, the base targets, `Entries()`, the `config.lock` hashes and the validation checks see only the Slack view (Task 2).
- Values: `party_reviewers` is a whole number of `routingdomain.MinPartyReviewers` (1) or more. `party_deadline` uses hours and minutes, from `15m` to `24h` inclusive, through `routingdomain.ParsePartyDeadline`. Levels, most specific first: path → repository tier → org `"*"` tier → `agent_api` → built-in default (`1`, `1h`). Several matching paths → the highest cap and the shortest timer.
- Log messages and fields exactly as the spec's "Logs" table. The new row: `feed party settings fallback`, warn, fields `repository`, `number`, `err`.
- Tokens never reach logs. The git host clients put the method, the path and the status in errors, never the token.
- The PR 1 fix wave owns `internal/platform/persistence/feed.go`, migration `00012`, the `since` reads and `internal/routing/domain/party_test.go`. PR 2 does not change them.
- **Task gate** (the "run the task gate" step in each task):

  ```bash
  go vet ./... && go test -race ./... && go build ./...
  docker run --rm -v "$PWD":/app -w /app -v "$(go env GOMODCACHE)":/go/pkg/mod golangci/golangci-lint:v2.14.0 golangci-lint run ./...
  ```

  Expected: every command exits 0.

## Review Focus

1. Mapping keys keep their case (`Acme: {API: …}`), but the feed key is `acme/api`. The party lookup must find the tier anyway (Task 3, `TestProvider_PartySettingsFoldsCase`).
2. A party-only `"*"` tier must route nothing and must not add the org's other repositories to the feed (Task 2 `Get` → `ErrNotFound`, Task 6 `POST /api/v1/subs` → `400 unknown_repository`).
3. A party-only nested path (`services/billing` under a Slack `services` rule) must not take files away from the Slack rule (Task 2 equality test).
4. A slow or failing files read must not drop the record and must not lower a stored cap (Task 4 fallback tests, Task 5 timeout test, Task 6 fallback flow).
5. Party keys on tiers that already route must not change the `config.lock` hashes. Otherwise an upgrade revalidates every entry (Task 2 `Entries` equality, Task 6 flow with no `/hooks` call).

---

### Task 1: Party keys on mapping tiers and paths

**Files:**
- Modify: `internal/routing/domain/models.go`
- Create: `internal/routing/domain/party_override_test.go`
- Modify: `internal/routing/infrastructure/config_decode.go`, `internal/routing/infrastructure/config_decode_test.go`

**Interfaces:**
- Consumes: `routingdomain.ParsePartyDeadline`, `ParsePartyReviewers`, `MinPartyReviewers` (PR 1 and its fix wave).
- Produces (in `routing/domain/models.go`):

```go
type PartyOverride struct {
	Reviewers *int
	Deadline  *time.Duration
}

func (override PartyOverride) IsSet() bool

type PartySettings struct {
	Reviewers int
	Deadline  time.Duration
}

// new fields
RepoConfig.Party PartyOverride
PathRule.Party   PartyOverride
Defaults.Party   PartySettings

func (rule PathRule) PartyOnly() bool
func (tier RepoConfig) PartyOnly() bool
```

- [ ] **Step 1: Write the failing tests.**
  - `TestPathRule_PartyOnly` (table): only `Party.Reviewers` → true. Only `Party.Deadline` → true. Party plus `Channel`, plus `MentionsPresent`, or plus `Channels` → false, one case each. An empty rule → false.
  - `TestRepoConfig_PartyOnly` (table): only `Party` → true. No tier key and only party-only paths → true. Party plus one Slack key → false, one case each for `Channel`, `Channels`, `MentionsPresent`, `Reactions`, `IgnoreAIReviews`, `DependabotFormat`, `DeleteOnClose`, `Digest` and `Notifications`. Party plus one Slack path rule → false. An empty tier → false.
  - `TestRepoConfig_PartyKeys` (`config_decode_test.go`): decode the org below with `decodeOrg` and convert each tier with `toDomain()`. Then `api` has `Party` 2 and 90m, its path `billing` has 4 and 15m, and `"*"` has `Reviewers` 3 and a nil `Deadline`.

    ```yaml
    api:
      channel: C0API
      party_reviewers: 2
      party_deadline: 1h30m
      paths:
        billing:
          party_reviewers: 4
          party_deadline: 15m
    "*":
      channel: C0STAR
      party_reviewers: 3
    ```

  - `TestRepoConfig_PartyDeadlineLimits` (table, each value on a tier and on a path): `15m` → 15m, `24h` → 24h. `14m` and `25h` → error that contains `party_deadline` and `15m to 24h`. `30s` → error that contains `party_deadline` and `hours and minutes`.
  - `TestRepoConfig_PartyReviewersRejected` (table, each value on a tier and on a path): `0` and `-1` → error that contains `party_reviewers` and `1 or more`. `2.5`, `"2"` and `null` → error that contains `party_reviewers` and `whole number`.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test ./internal/routing/...` → `undefined: domain.PartyOverride`.
- [ ] **Step 3: Implement.**
  - `IsSet` is true when one of the two pointers is not nil.
  - `PathRule.PartyOnly` is true when `Party.IsSet()` and the rule sets no `Channel`, no `MentionsPresent` and no `Channels`.
  - `RepoConfig.PartyOnly` is true when the tier sets none of the nine Slack keys above, every path rule is `PartyOnly`, and the tier sets `Party` or has one path rule or more.
  - Add `decodePartyKey(key string, node *yaml.Node, party *domain.PartyOverride) error` to `config_decode.go`. Call it for `party_reviewers` and `party_deadline` in `repoConfigWire.UnmarshalYAML` and in `decodePathRule`.
  - `party_reviewers`: decode the scalar as text and call `routingdomain.ParsePartyReviewers`, the same parse as `agent_api.party_reviewers`. A non-scalar node fails with the key name.
  - `party_deadline`: decode to `string` and call `ParsePartyDeadline`.
  - Each error starts with the key name, like `party_deadline: …`. `decodePaths` already adds the path. `repoConfigWire.toDomain` copies `Party`.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(routing): party keys on mapping tiers and paths`.

### Task 2: Party-only entries stay out of Slack routing

**Files:**
- Create: `internal/routing/application/slack_mappings.go`
- Modify: `internal/routing/application/provider.go` (`NewProvider`), `internal/routing/application/validate.go`
- Create: `internal/routing/infrastructure/party_slack_test.go`
- Modify: `internal/routing/infrastructure/parse_test.go`

**Interfaces:**
- Consumes: `PathRule.PartyOnly`, `RepoConfig.PartyOnly` (Task 1).
- Produces: `func slackMappings(mappings map[string]domain.Org) map[string]domain.Org`. `NewProvider` keeps its signature and stores `slackMappings(m)`. Every existing `Provider` method reads only this Slack view.

- [ ] **Step 1: Write the failing tests.**
  - `TestProvider_PartyOnlyEntriesLeaveSlackRoutingUnchanged` (`party_slack_test.go`). Build `slackProvider` from the document below without any `party_` key, and `partyProvider` from the full document, both with `providerFromDoc`.

    ```yaml
    mappings:
      acme:
        "*":
          channel: C0STAR00000
          party_reviewers: 2
        the-monorepo:
          channel: C0BASE00000
          party_deadline: 2h
          paths:
            services: {mentions: ["<@U0S>"]}
            services/billing: {party_reviewers: 4}
            docs: {channel: C0DOCS00000, party_reviewers: 1}
            tools: {party_deadline: 30m}
        web:
          party_reviewers: 3
      beta:
        "*":
          party_reviewers: 2
        api:
          channel: C0BETA00000
    ```

    The Slack-only document drops `services/billing`, `tools`, `web` and `beta/*`, plus each remaining `party_` key. For each repository in `acme/the-monorepo`, `acme/web`, `acme/other`, `beta/api` and `beta/other`, these must be equal for both providers: `Get` (mapping and error), `BaseTargets`, `RepoHasPathRules`, `AdditionalChannels`, `DigestFor`, and `TargetsForFiles` for each file set `{services/billing/x.go}`, `{tools/run.sh}`, `{docs/a.md, README.md}` and `{services/api/y.go, services/billing/x.go}`. These must be equal too: `HasPathRules()`, `Entries()`, the `Hash()` of each entry, `Schedules()` and `NotificationSchedules()`. Also `partyProvider.Get(ctx, "beta/other")` → `require.ErrorIs(t, err, domain.ErrNotFound)`.
  - In `parse_test.go`:
    - `TestParse_PartyOnlyTierUnderRoutingStar`: `acme: {"*": {channel: C0STAR00000}, web: {party_reviewers: 3}}` → no error.
    - `TestParse_PartyOnlyTierWithoutRoutingStarRejected`: `acme: {api: {channel: C0API000000}, web: {party_reviewers: 3}}` → error that contains `repo "web": sets only party keys, but no tier routes this repository`.
    - `TestParse_PartyOnlyStarRoutesNothingRejected`: `acme: {"*": {party_reviewers: 2}}` → error that contains `the "*" entry sets only party keys`.
    - `TestParse_PartyOnlyStarWithRoutedRepository`: `acme: {"*": {party_deadline: 2h}, api: {channel: C0API000000}}` → no error.
    - `TestParse_PartyPathOnStarRejected`: `acme: {"*": {channel: C0STAR00000, paths: {billing: {party_reviewers: 2}}}}` → error that contains `paths are not allowed on the "*" tier`.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test ./internal/routing/...` → `Parse` rejects the party-only `beta/*` tier with `no channel`, and the other validation tests fail.
- [ ] **Step 3: Implement.**
  - `slackMappings` builds new maps and never writes to its input. It skips each tier where `PartyOnly()`. In each kept tier it drops the `PartyOnly()` path rules and keeps the order of the others. `Paths` is nil when no rule remains. It drops an org with no tier left.
  - `ValidateMappings` skips the "no channel" check for a `PartyOnly()` tier and adds two checks:
    - A party-only repository tier needs `starHasChannel`, else `mappings: org %q repo %q: sets only party keys, but no tier routes this repository (set a channel here or in the org's "*" entry)`.
    - A party-only `"*"` tier needs one named tier in the org that is not party-only, else `mappings: org %q: the "*" entry sets only party keys, but no tier of the org routes a repository`.
  - `validatePaths` still runs for every tier.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(routing): keep party-only entries out of slack routing`.

### Task 3: Party settings per repository and path

**Files:**
- Modify: `internal/routing/domain/interfaces.go`
- Create: `internal/routing/application/party.go`
- Modify: `internal/routing/application/provider.go` (field `party`, built in `NewProvider` from the full mappings)
- Modify: `internal/routing/module.go`, `internal/routing/module_test.go`
- Create: `internal/routing/infrastructure/party_resolve_test.go`

**Interfaces:**
- Consumes: Task 1 types, `fileUnder`.
- Produces:

```go
// PartyRules answers the party cap and timer of a repository from the mappings.
// Repository names match in any case.
type PartyRules interface {
	// HasPartyPathRules reports whether a path rule of the repository sets party_reviewers or party_deadline.
	HasPartyPathRules(repository string) bool
	// PartySettings returns the highest cap and the shortest timer over the files.
	// No files give the repository's values.
	PartySettings(repository string, files []string) PartySettings
}
```

`*application.Provider` implements it (`var _ domain.PartyRules = (*Provider)(nil)`). `routing.Module` binds it with `fx.As(new(domain.RoutingProvider), new(domain.PartyRules))`.

- [ ] **Step 1: Write the failing tests** (package `infrastructure_test`). The helper `partyProviderFromDoc(t, body string, global domain.PartySettings) *application.Provider` parses the document and calls `application.NewProvider(domain.Defaults{Party: global}, file.Mappings, nil)`.
  - `TestProvider_PartySettingsLevels` (table, global `{2, 90m}`, document below):

    ```yaml
    mappings:
      acme:
        "*":
          channel: C0STAR00000
          party_reviewers: 3
        api:
          party_deadline: 2h
          paths:
            billing: {party_reviewers: 4, party_deadline: 15m}
        web:
          channel: C0WEB000000
          party_reviewers: 5
      beta:
        api:
          channel: C0BETA00000
    ```

    | name | repository | files | want |
    | --- | --- | --- | --- |
    | global | `beta/api` | nil | `{2, 90m}` |
    | org star | `acme/other` | nil | `{3, 90m}` |
    | repository tier | `acme/web` | nil | `{5, 90m}` |
    | star cap, tier timer | `acme/api` | nil | `{3, 2h}` |
    | path | `acme/api` | `{"/billing/x.go"}` | `{4, 15m}` |
    | file outside the paths | `acme/api` | `{"README.md"}` | `{3, 2h}` |
    | unmapped | `gamma/api` | nil | `{2, 90m}` |

  - `TestProvider_PartySettingsBuiltInDefaults`: the same document with a zero global → `beta/api` gives `{DefaultPartyReviewers, DefaultPartyDeadline}`.
  - `TestProvider_PartySettingsAcrossPaths` (table, spec calls 2 and 19, repository `acme/mono`):

    ```yaml
    mappings:
      acme:
        mono:
          channel: C0MONO00000
          party_reviewers: 2
          party_deadline: 2h
          paths:
            docs: {party_reviewers: 1, party_deadline: 4h}
            services: {mentions: ["<@U0S>"], party_reviewers: 4, party_deadline: 30m}
            services/billing: {mentions: ["<@U0B>"]}
            services/search: {party_deadline: 3h}
            tools: {party_reviewers: 3, party_deadline: 1h}
    ```

    | files | want | why |
    | --- | --- | --- |
    | `docs/a.md`, `README.md` | `{2, 2h}` | spec example 1 |
    | `services/billing/x.go` | `{4, 30m}` | spec example 2 |
    | `services/search/y.go` | `{4, 3h}` | each key from its most specific rule |
    | `tools/a.sh`, `services/billing/x.go` | `{4, 30m}` | highest cap, shortest timer |
    | `tools/a.sh`, `README.md` | `{3, 1h}` | a file outside the paths counts |
    | `toolsx/a.sh` | `{2, 2h}` | segment match only |

  - `TestProvider_PartySettingsFoldsCase`: `Acme: {API: {channel: C0API000000, party_reviewers: 3, paths: {billing: {party_reviewers: 4}}}}` → `PartySettings("acme/api", nil).Reviewers == 3`, `PartySettings("acme/api", {"billing/x.go"}).Reviewers == 4`, `HasPartyPathRules("acme/api") == true`.
  - `TestProvider_HasPartyPathRules` (table, each row names its document): `acme/api` of the levels document → true. `acme/mono` of the paths document → true (mixed rules). `acme/web` of the levels document → false. `acme/the-monorepo` of `monorepoDoc` → false (Slack paths only). `gamma/api` → false.
  - `module_test.go` invokes `func(domain.RoutingProvider, domain.TargetResolver, domain.PartyRules) {}`.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test ./internal/routing/...` → `PartySettings undefined`.
- [ ] **Step 3: Implement.**
  - `NewProvider` builds an index from the full mappings. The key is `strings.ToLower(org + "/" + repo)`, with `"*"` as the repo of the star. Each entry holds the tier's `Party` and only the path rules with `Party.IsSet()`. Walk orgs and repos in sorted order and keep the first entry of a key.
  - Repository values: per key, the repository tier wins over the star, and the star wins over `Defaults.Party`. A global `Reviewers` below `MinPartyReviewers` gives `DefaultPartyReviewers`. A global `Deadline` below `MinPartyDeadline` gives `DefaultPartyDeadline`.
  - Per file, trimmed like `matchedRules`: the cap comes from the longest matching rule that sets `Reviewers`, else the repository cap. The timer works the same way with `Deadline`. The PR cap is the highest file cap. The PR timer is the shortest file timer.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(routing): resolve party settings per repository and path`.

### Task 4: The recorder keeps the stored party state on a fallback

**Files:**
- Modify: `internal/feed/domain/interfaces.go`, `internal/feed/domain/models.go`, `internal/feed/domain/constants.go`
- Modify: `internal/feed/application/recorder.go`, `recorder_test.go`, `fakes_test.go`
- Modify: `internal/feed/infrastructure/config_party_settings.go`, `repos_test.go` (new signature only, Task 6 deletes both)

**Interfaces:**
- Produces:

```go
// PartySettingsResolver gives the party settings that apply to a pull request.
// It never fails. If it cannot read the changed files, it returns the
// repository's values and sets Fallback.
type PartySettingsResolver interface {
	Resolve(ctx context.Context, key PullRequestKey) PartyResolution
}

type PartyResolution struct {
	Settings PartySettings
	Fallback bool
}

const PartyFilesReadTimeout = 2 * time.Second
```

- [ ] **Step 1: Write the failing tests.** `fakePartySettings` gets a `resolution domain.PartyResolution` field in place of `settings` and `err`.
  - `TestRecorder_FallbackKeepsTheStoredPartyState`: the stored PR has `Party{Reviewers: 4, RecordedAt: t0, DeadlineAt: t1}`. The resolver gives `{Settings: {1, time.Hour}, Fallback: true}`. Then the upserted `Party == {4, t0, t1}`, and the fake calendar got no `AddWorkingTime` call.
  - `TestRecorder_FallbackOnANewPullRequestUsesTheResolvedSettings`: no stored PR, `{Settings: {2, 30m}, Fallback: true}` → `Party == {2, Now, deadline}`.
  - `TestRecorder_FallbackOnARowWithoutRecordTimeUsesTheResolvedSettings`: the stored PR has a zero `RecordedAt` → the resolved settings and `RecordedAt == Now`.
  - Delete `TestRecorder_ReturnsResolverError`.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test ./internal/feed/...` → the resolver signature does not match.
- [ ] **Step 3: Implement.** `partyState` calls `Find`, then `Resolve`. If `resolution.Fallback` and the stored PR has a non-zero `RecordedAt`, it returns `stored.Party` unchanged. Otherwise it works as today with `resolution.Settings`. `ConfigPartySettings.Resolve` returns `PartyResolution{Settings: resolver.settings.Party}`.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): keep the stored party state when the files read fails`.

### Task 5: Feed adapter over routing and the changed files

**Files:**
- Create: `internal/feed/infrastructure/routing_party_settings.go`, `routing_party_settings_test.go`
- Modify: `internal/feed/domain/errors.go`

**Interfaces:**
- Consumes: `routingdomain.PartyRules` (Task 3), `routingdomain.ChangedFilesReader`, `domain.PartyResolution` and `domain.PartyFilesReadTimeout` (Task 4).
- Produces: `func NewRoutingPartySettings(rules routingdomain.PartyRules, files routingdomain.ChangedFilesReader, logger *slog.Logger) *RoutingPartySettings`, `var _ domain.PartySettingsResolver = (*RoutingPartySettings)(nil)`, and `domain.ErrMalformedRepository = errors.New("feed: repository is not owner/name")`. A nil `files` means no read token.

- [ ] **Step 1: Write the failing tests.** The test file holds `fakePartyRules` (records the repository and files it got, returns `{Reviewers: 3, Deadline: 2h}`) and `fakeChangedFiles` (records owner, repo, number and `ctx.Deadline()`). The key is `{Repository: "acme/api", Number: 42}`. Capture the log with `slog.NewTextHandler` over a `bytes.Buffer`.
  - `TestRoutingPartySettings_WithoutPartyPathRulesSkipsTheRead`: `HasPartyPathRules` false → no read, the rules got nil files, result `{Settings: {3, 2h}}`.
  - `TestRoutingPartySettings_WithoutReaderUsesTheRepositoryValues`: a nil reader and `HasPartyPathRules` true → result `{Settings: {3, 2h}}`, `Fallback` false, empty log.
  - `TestRoutingPartySettings_ReadsTheChangedFiles`: the reader returns `{"billing/x.go"}` → the reader got `("acme", "api", 42)`, the rules got `("acme/api", {"billing/x.go"})`, `Fallback` false.
  - `TestRoutingPartySettings_ReadHasItsOwnDeadline`: the reader saw a deadline within one second of `time.Now().Add(domain.PartyFilesReadTimeout)`.
  - `TestRoutingPartySettings_ReadFailureFallsBack`: the reader fails with `errors.New("github: list files: 502")` → result `{Settings: {3, 2h}, Fallback: true}`, and the rules got nil files. The log has `level=WARN`, `msg="feed party settings fallback"`, `repository=acme/api`, `number=42` and `err="github: list files: 502"`.
  - `TestRoutingPartySettings_MalformedRepositoryFallsBack`: key `{Repository: "acme", Number: 42}` → `Fallback` true, no read, and the same log line with `ErrMalformedRepository`.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test ./internal/feed/infrastructure/...` → `undefined: infrastructure.NewRoutingPartySettings`.
- [ ] **Step 3: Implement `Resolve`.** A nil reader, or `HasPartyPathRules` false → `PartySettings(key.Repository, nil)`. Else split `key.Repository` with `strings.Cut` on `/` and read the files under `context.WithTimeout(ctx, domain.PartyFilesReadTimeout)`. On a split or read error, log the warning and return the repository values with `Fallback: true`. Else return `PartySettings(key.Repository, files)`. Map the routing `PartySettings` field by field.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): read party settings from routing and the changed files`.

### Task 6: Runtime wiring and integration flows

**Files:**
- Modify: `internal/feed/module.go`, `internal/feed/module_test.go`, `internal/feed/domain/models.go`
- Delete: `internal/feed/infrastructure/config_party_settings.go`. Remove `TestConfigPartySettings_ReturnsTheGlobalSettings` from `repos_test.go`.
- Modify: `internal/runtime/module.go`, `internal/runtime/feed.go`, `internal/runtime/feed_test.go`, `internal/runtime/integration_feed_test.go`

**Interfaces:**
- Consumes: Tasks 3 and 5.
- Produces: `providerFilesFetcher` in the `fx.Provide` list (it may return nil). `func buildRouter(provider *routingapp.Provider, files routingdomain.ChangedFilesReader, logger *slog.Logger) *routingapp.Router`. `func providePartyRules(provider *routingapp.Provider) routingdomain.PartyRules`. `feeddomain.Settings{Location *time.Location}` without `Party`. `feed.Module` binds `NewRoutingPartySettings` as `domain.PartySettingsResolver`, and its doc names `routingdomain.PartyRules` and `routingdomain.ChangedFilesReader` as inputs.

- [ ] **Step 1: Write the failing tests.**
  - `feed_test.go`: replace `TestProvideFeedSettings_FallsBackToPartyDefaults` with `TestBuildProvider_GlobalPartySettings` (`PartyReviewers: 2`, `PartyDeadline: 30m`, mapping `acme/api` → `PartySettings("acme/api", nil) == {2, 30m}`) and `TestBuildProvider_ZeroPartySettingsUseTheDefaults` (→ `{1, time.Hour}`). `TestProvideFeedSettings_NilTimezoneIsUTC` stays, without `Party`.
  - `feed/module_test.go`: supply a `routingdomain.PartyRules` stub and a nil `routingdomain.ChangedFilesReader`, and drop `Party` from the supplied `Settings`.
  - `TestIntegration_PartySettingsFromMappings`, weekday clock:
    - Arrange: start from `feedConfig(t, true)`, which primes the lock from the party-free mapping. Then set party keys in `cfg.Mappings` and do not prime again: `acme/api` gets `Party.Reviewers` 1 and the path rule `{Dir: "billing", Party: {Reviewers: 2}}`. A new tier `acme/*` gets only `Party.Deadline` 2h.
    - A GitHub fake records each request path and answers `GET /repos/acme/api/pulls/42/files` with `[{"filename":"billing/x.go"}]`. Set `cfg.GitHubBaseURL` and `cfg.GitHubToken = config.Secret("gh-read-only")`. Set `cfg.SlackBaseURL` to a `newSlackFake(t)` that the test holds.
    - Act: `alice` and `bob` subscribe to `acme/api` with `"type":"party"` and poll once. PR 42 opens. `alice` polls, then `bob` polls. `alice` posts `{"repositories":["acme/other"]}` to `/api/v1/subs`.
    - Assert: `alice` gets `[42]` and `bob` gets `[42]` ("the billing path sets a cap of 2"). The subscribe call gives `400` with `unknown_repository` ("a party-only star adds no repository"). The GitHub fake saw exactly one path, `/repos/acme/api/pulls/42/files`: no `/hooks` call means the lock hashes did not change, and the Slack router read no files. `slack.findCall("/api/chat.postMessage")` finds a call with `Body["channel"] == "C123ABCDE"`.
  - `TestIntegration_PartyFilesReadFailureFallsBack`: the same setup, but the files endpoint answers `502`. Capture the log with `fx.Replace` and a `lockedBuffer`. `alice` gets `[42]`, `bob` gets `[]` ("the fallback takes the repository cap of 1"). The log has `feed party settings fallback` with `repository=acme/api` and `number=42`. The Slack fake has the `/api/chat.postMessage` call.
- [ ] **Step 2: Run the tests, expect FAIL.** `go test -race ./internal/runtime/... ./internal/feed/...` → `bob` gets `[]` in the first flow, and `buildProvider` ignores the global settings.
- [ ] **Step 3: Implement.** `buildProvider` sets `Defaults.Party` from `cfg.PartyReviewers` and `cfg.PartyDeadline`. `provideFeedSettings` keeps only the location. Bind the module and delete `ConfigPartySettings` and `Settings.Party`.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): wire party settings per repository and path`.

### Task 7: Docs

**Files:**
- Modify: `docs/mappings.md`, `docs/monorepo.md`, `docs/agent-api.md`, `docs/configuration.md`, `docs/operations.md`, `config.example.yaml`, `ARCHITECTURE.md`, `CLAUDE.md`

- [ ] **Step 1: Write.**
  - `docs/mappings.md`: add a **Party** bullet to "Behavioral overrides" for `party_reviewers` and `party_deadline` on a repository tier and on the `"*"` tier. Add both keys to the path-key table of "Per-path routing" and say that Slack ignores them. A new part "Party-only entries" says that such an entry routes nothing, never changes Slack, the lock or validation, and names the two startup errors of Task 2.
  - `docs/monorepo.md`: a new section "Party settings per path". It covers the most specific rule per key, a nested rule that takes its parent's value, the highest cap, the shortest timer, and the two examples of spec call 2. It says when notifycat reads the files and what happens without a token or on a read error (call 13).
  - `docs/agent-api.md`, "Review as a party": the cap and timer bullets list the five levels and link to `mappings.md` and `monorepo.md`. Add call 12: notifycat finds both values again on every recorded event, and a lower cap never takes a PR back.
  - `docs/configuration.md`: the two `agent_api` rows say that they are the global level and that tiers and paths can override them.
  - `docs/operations.md`: the row for `feed party settings fallback` (warn, `repository`, `number`, `err`), with the text of the spec "Logs" table.
  - `config.example.yaml`: a commented example with a party-only `"*"`, a repository tier with a `channel` and `party_reviewers`, and a party path.
  - `ARCHITECTURE.md`: the `routing` row also answers the party cap and timer per repository and path. The `feed` row gets them from `routing` when it records a PR.
  - `CLAUDE.md`: the `routing` row of the domain table names party settings. "Mappings & startup validation" gets one sentence: entries with only party keys stay out of Slack routing, `config.lock` and validation.
- [ ] **Step 2: Check.** Apply the `technical-language` skill (`~/.claude/skills/technical-language/SKILL.md`) and run `python3 ~/.claude/skills/technical-language/techlang-lint.py <each changed doc>`. The per100w score must not rise above the file's score at the base. Then: `docker run --rm -v "$PWD":/docs squidfunk/mkdocs-material:9.5.39 build --strict --site-dir /tmp/site && rm -rf .cache` → exit 0.
- [ ] **Step 3: Commit** `docs(feed): party settings per repository and path`.

---

## Before the PR

- Run `just vuln`. Without `just`: `docker run --rm -v "$PWD:/src" -w /src golang:1.27.3-alpine sh -c 'export PATH=/usr/local/go/bin:$PATH; go install golang.org/x/vuln/cmd/govulncheck@v1.3.0 && /go/bin/govulncheck ./...'` → no vulnerabilities.
- Run code review, security review and QA rounds until nothing relevant is left.
- After PR 1 merges, move the PR 2 commits onto `main` with `git rebase --onto main <fix-wave tip>`. Then open the PR `feat(feed): party settings per repository and monorepo path` and wait for Pavlo to merge.

## Planner rulings

1. **The resolver port loses its error and returns `PartyResolution{Settings, Fallback}`.** Why: a `Resolve` error drops the PR record (caller decision 1). Cost if wrong: one port change.
2. **On a fallback the recorder keeps the stored `Party` as it is.** Why: call 13 says the PR keeps its stored cap and timer. Cost if wrong: a config change waits for the next good read.
3. **`NewProvider` filters party-only entries once, into a Slack view.** Why: one place hides them from every Slack reader, the CLI tools too. Cost if wrong: a new method that reads raw mappings skips the filter.
4. **The party lookup folds the case of org and repository keys. Of two keys that differ only in case, the first in sorted order wins.** Why: the feed key is lower case, the Slack lookup is exact. Cost if wrong: one of two such keys has no effect.
5. **The routing provider owns the global level (`Defaults.Party`, zero → built-in defaults).** `feeddomain.Settings.Party` and `ConfigPartySettings` go away. Why: one resolver for all five levels. Cost if wrong: none at runtime.
6. **fx builds the changed-files reader once, for `buildRouter` and the feed adapter.** Why: one client and one token check. Cost if wrong: a nil interface in the graph, which dig v1.19.0 accepts.
7. **`PartyFilesReadTimeout = 2s`, inside `RecordTimeout = 3s`.** Why: it leaves time for `Find` and `Upsert`. Cost if wrong: a slow git host gives more fallbacks. Tune the constant.
8. **No startup warning for party path rules without a read token. Docs only.** Why: the spec "Logs" table has no such row. Cost if wrong: an operator misses inert path caps.
9. **The fallback log uses the spec fields `repository`, `number`, `err`, not a `reason` field.** Why: the spec "Logs" table is binding. Cost if wrong: a field rename.
10. **The two validation messages of Task 2.** Why: call 16 fails such entries at startup. Cost if wrong: wording only.
11. **A party-only tier sets no Slack key and sets a party key or has only party-only paths. A tier with no keys stays a Slack tier.** Why: an empty tier routes through the star today. Cost if wrong: none.
12. **The adapter reads the files itself and shares no cache with the Slack router.** Why: they run at different times, and a cache adds state. Cost if wrong: one more git host call per event on repositories with both kinds of path rules.
13. **`party_reviewers` on a tier or a path uses `routingdomain.ParsePartyReviewers`, the parse of the PR 1 fix wave for `agent_api.party_reviewers`.** Why: yaml.v3 cuts `2.5` to 2, call 15 wants a whole number, and one parse rule serves every level. Cost if wrong: a quoted `"2"` passes on every level.
14. **The docs example gives the repository tier a `channel`.** Why: the spec's config example routes nothing for `protorepo`, so call 16 fails it. Cost if wrong: none.
15. **Docs also cover `mappings.md`, `monorepo.md` and `operations.md`.** Why: the spec "Docs" section names them. Cost if wrong: none.
16. **`routing.Module` binds `PartyRules` next to `RoutingProvider`.** Why: the module stays complete for its own test. Cost if wrong: none.
17. **A repository name without `/` falls back and logs in the adapter.** Why: the port never fails. Cost if wrong: none, the recorder only sees configured names.
18. **Tier and path errors start with the key name, as other decode errors do.** Why: the existing decode style. Cost if wrong: wording only.
19. **New routing domain tests go in `party_override_test.go`.** Why: the fix wave edits `party_test.go`. Cost if wrong: none.
20. **The base is the fix-wave tip, on a new branch, rebased with `--onto` after PR 1 merges.** Why: PR 2 needs `MinPartyReviewers` and the fix-wave ports. Cost if wrong: one more rebase.
21. **Commits carry no `Co-Authored-By` trailer.** Why: Pavlo's global rule. Cost if wrong: none.
