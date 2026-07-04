# Phase 3 — validation domain

Validate mapping entries against Slack (channel exists, bot present, scopes) and GitHub (webhook events), and cache results in `config.lock`. Depends on routing (Phase 2); shared by three consumers that stay put for now: `app.startupValidate`, the doctor, and `notifycat-config validate` / `notifycat-doctor`.

Target: `internal/validation/{domain,application,infrastructure}` + `module.go`. Strangler parallel-copy: build `internal/validation` alongside; `internal/validate` stays intact and green until T5 flips callers and deletes it.

## Layer assignment (authoritative for this phase)

Resolved against the locked rule: **domain = abstractions/DTOs/enums/constants only; application = business logic; infrastructure = file/network I/O; a module reaches another module only through its interface.**

**The one hard problem:** `validate.SlackChecker.ConversationsInfo` returns `slack.ChannelInfo`, and the tests inject `*slack.APIError`. Ports must live in the pure domain, which may not import `internal/slack`. Resolution (anti-corruption boundary, same shape as Phase 2 wire-types):
- domain owns its own `ChannelInfo` DTO (mirror of the 4-field `slack.ChannelInfo`) and a `SlackAPIError{Method, Code}` error type (mirror of `slack.APIError`).
- an infra **Slack probe adapter** wraps `*slack.Client`, implements `domain.SlackChecker`, and maps `slack.ChannelInfo → domain.ChannelInfo` and `*slack.APIError → *domain.SlackAPIError` (transport errors pass through untouched).
- application's error-interpretation switches on `*domain.SlackAPIError.Code` — identical code strings, identical operator messages, identical CheckResults. Behavior byte-identical.
- the `github`/`org-repo` ports stay satisfied structurally by `*github.Client` (pure `[]string` signatures — no adapter, transition debt, as routing's `ChangedFilesReader`). `MappingLookup` stays satisfied by the routing `*Provider`.

### domain/ (package `domain`) — imports only stdlib + `kernel` + `routing/domain`
- `interfaces.go` — ports: `MappingLookup` (Get→`routingdomain.RepoMapping`, PathChannels→[]string), `SlackChecker` (AuthTest, ConversationsInfo→`ChannelInfo`), `GitHubChecker` (ListHookEvents→[]string), `OrgRepoLister` (ListOrgRepos→[]string); use-case interface `RepoValidator` (Validate→`Report`).
- `models.go` — DTOs: `CheckResult`, `Report`+`OK()`, `EntryResult`(`routingdomain.Entry`+`[]Report`)+`OK()`, `ChannelInfo{ID,Name,IsMember,IsArchived}`.
- `enums.go` — `Status` + `StatusOK/StatusFail/StatusSkip` + `String()`.
- `errors.go` — `SlackAPIError{Method, Code string}` + `Error()`.
- `constants.go` — `WebhookURLPath`, `RequiredSlackScopes`, `RequiredGitHubEvents`, `ChannelIDPattern` (all exported; application consumes them).
- `doc.go` — package doc (adapted from validate's deps.go header).

Trivial predicate methods (`Report.OK`, `EntryResult.OK`, `Status.String`) stay in domain — precedent: routing `Entry.Key`/`Entry.Hash`.

### application/ (package `application`) — imports stdlib + `validation/domain` + `routing/domain`; NO slack/store/github
- `validator.go` — `Validator`{mappings,slack,github ports}, `NewValidator`, `Validate`, `mappingLookupFailure` (`errors.Is(err, routingdomain.ErrNotFound)`), `validateMapping`, `slackChecks`, `named`; `var _ domain.RepoValidator = (*Validator)(nil)`.
- `slack_check.go` — `slackAuthCheck`, `slackAuthErrorResult` (`errors.As` → `*domain.SlackAPIError`), `slackChannelCheck`, `slackChannelErrorResult` (→ `*domain.SlackAPIError`), `interpretChannelInfo(_ , domain.ChannelInfo)`.
- `github_check.go` — `githubCheck`, `interpretHookEvents` (uses `domain.WebhookURLPath`, `domain.RequiredGitHubEvents`).
- `mapping_check.go` — `mappingFoundCheck(routingdomain.RepoMapping)`, `channelFormatCheck(routingdomain.RepoMapping)` (uses `domain.ChannelIDPattern`).
- `helpers.go` — `skip`, `failResult`, `missingScopes`, `quoteJoin`, `splitRepository`.
- `runner.go` — `RunForEntries`, `reportsFor`, `expandWildcard`, `singleCheckReport`. (`RunForEntries` stays a free function over the `RepoValidator`/`OrgRepoLister` ports — precedent: routing's free `ValidateMappings`; the use-case interface is `RepoValidator`.)
- tests (all become `package application_test`, moved from `internal/validate`): assertions unchanged; only setup type refs repoint — `store.RepoMapping→routingdomain.RepoMapping`, `store.ErrNotFound→routingdomain.ErrNotFound`, `slack.ChannelInfo→validationdomain.ChannelInfo`, `&slack.APIError{…}→&validationdomain.SlackAPIError{…}`, `validate.X→application.X`/`validationdomain.X`.

### infrastructure/ (package `infrastructure`) — no depguard rule (transition debt: imports `internal/slack`)
- `slack_probe.go` — `SlackProbe` wraps `*slack.Client`; `NewSlackProbe`; implements `domain.SlackChecker`; maps ChannelInfo + translates `*slack.APIError → *domain.SlackAPIError`; `var _ domain.SlackChecker = (*SlackProbe)(nil)`.
- `slack_probe_test.go` — new focused test: field mapping through, API-error translation preserves `.Code`, non-API error passes through.

### module.go (package `validation`)
- `fx.Module("validation", fx.Provide(fx.Annotate(infrastructure.NewSlackProbe, fx.As(new(domain.SlackChecker))), fx.Annotate(application.NewValidator, fx.As(new(domain.RepoValidator)))))`.
- `module_test.go` (`package validation_test`) — fxtest supplies a dummy `*slack.Client`, stub `domain.MappingLookup`, stub `domain.GitHubChecker`; `fx.Invoke(func(domain.RepoValidator){})`.

### depguard (.golangci.yml) — add two rules mirroring routing, `validation-domain` also allows `routing/domain`
- `validation-domain`: allow kernel + validation/domain + routing/domain.
- `validation-application`: allow kernel + validation/domain + validation/application + routing/domain.
- (no infrastructure rule.)

## Consumer rewire (T5) — symbol → new package
`CheckResult/Report/EntryResult/Status*/MappingLookup/SlackChecker/GitHubChecker/OrgRepoLister/RepoValidator → validationdomain.*`; `NewValidator/Validator/RunForEntries → validationapp.*`. Production `NewValidator` call sites (app.go:277, cmd/notifycat-config/main.go:64, cmd/notifycat-doctor/main.go:62) wrap the raw `*slack.Client` in `validationinfra.NewSlackProbe(slackClient)`. doctor keeps the concrete `validationapp.Validator` (tightening to the port is Phase 7).

## Tasks
- **V3-T1** scaffold 3 layers + module stub + 2 depguard rules · `refactor(validation): scaffold domain/application/infrastructure`
- **V3-T2** domain (interfaces, models incl. ChannelInfo, enums, errors incl. SlackAPIError, constants, doc) · `refactor(validation): add domain ports, DTOs, enums, and error abstraction`
- **V3-T3** application (validator/slack/github/mapping/helpers/runner) + moved+repointed tests · `refactor(validation): relocate validator and entry runner to application`
- **V3-T4** infrastructure Slack probe adapter + test · `refactor(validation): add Slack probe adapter over the platform client`
- **V3-T5** rewire consumers, delete `internal/validate`, fill module + fxtest · `refactor(validation): rewire consumers and add fx module`

## Gate
- [x] build + race tests + lint green (existing assertions unchanged) · `internal/validate` gone · `! grep -rq 'internal/validate' --include=*.go .` · module fxtested · depguard empirically verified (planted forbidden import flags) · plan box checked

**Done.** 6 commits `2053135`→`5b69d76`. Race suite 468 runs, 0 FAIL, 27 pkgs ok, lint clean. Domain owns `ChannelInfo` + `SlackAPIError` (anti-corruption); infra `SlackProbe` maps SDK types across the boundary; `github`/`org`/`mapping` ports satisfied structurally by the platform clients (transition debt). depguard blocks github in domain / slack in application.
