# Phase 4 — digest domain

Post a periodic digest of stuck (stale) PRs per the enabled cron schedules. Depends on routing (Phase 2); otherwise self-contained (own reporter + scheduler). The scheduler is a lifecycle component — its `Run(ctx)` loop is registered as an `fx.Lifecycle` hook at cutover (Phase 8); here it stays behind its use-case interface and `app.Wire` keeps running it.

Target: `internal/digest/{domain,application,infrastructure}` + `module.go`.

## In-place migration (not a parallel copy)

Unlike routing/validation (which lived at a *different* old path — `internal/mappings`, `internal/validate`), the digest already lives at the target path `internal/digest` as a **flat** `package digest` (`reporter.go`, `scheduler.go`). So:

- New layers go under `internal/digest/{domain,application,infrastructure}` (new packages — no symbol collision with flat `package digest`).
- The flat `reporter.go` / `scheduler.go` (+ their tests) stay intact and green — `app.Wire` keeps importing them — until **T5** flips the wiring and deletes them.
- `module.go` (`package digest`) coexists with the flat files (same package, no duplicate symbols); a stub lands in T1, filled in T5.
- The flat files sit at `internal/digest/*.go`, which matches **no** depguard layer glob (`**/internal/digest/domain/**`, `**/internal/digest/application/**`), so they keep importing slack/store during the transition without complaint.

## Layer assignment (authoritative for this phase)

Locked rule: **domain = abstractions/DTOs/enums/constants only; application = business logic; infrastructure = file/network I/O; a module reaches another module only through its interface.** depguard forbids `application` from importing slack/store/github.

**The hard problem (digest analog of validation's SDK crux):** the reporter takes `store.PullRequest` from a finder, and composes/posts via `*slack.Composer` + a slack poster, exchanging `slack.Message`. All three are platform types the application may not import. Resolution — **anti-corruption boundary, validation precedent**:

- **Finder:** `StuckFinder.FindStuck` returns `[]domain.PullRequest` (a slack/store-free mirror of the store row). Infra `StuckRepo` wraps `*store.PullRequests` and maps `store.PullRequest → domain.PullRequest` (same shape as maintenance's `PRRepository`→`PRRow`).
- **Composer + Poster:** both are domain ports exchanging a **domain `Message`** — a small, presentation-neutral rendered-message DTO (`Message{Blocks, Fallback}`, `Block{Type, Text}`, `TextObject{Type, Text}`). It is a pure data carrier that crosses the composer→poster ports; the application never reads its fields (it only passes composer output to the poster). Infra `SlackComposer` wraps `*slack.Composer` and maps `slack.Message → domain.Message`; infra `SlackPoster` wraps `*slack.Client` and maps `domain.Message → slack.Message`. The digest emits only `section` blocks (Text set), so the minimal mirror round-trips byte-identically on the wire.
- **Mapping/digest lookup:** `MappingLookup.Get → routingdomain.RepoMapping` and `DigestResolver.DigestFor → routingdomain.DigestConfig` are satisfied **structurally** by the routing `*Provider` (its `Get`/`DigestFor` already return those types) — no adapter, transition debt as validation's `MappingLookup`.
- **Clock:** injected via `ReporterParams.Now` (kills the old `r.now = …` field-override test-seam and satisfies the >3-args→DTO rule).

### Why the reporter test is rewritten (behavioral), not repointed

The old flat `reporter_test.go` (`package digest`) overrides the unexported `r.now` **and** asserts exact Slack text (`"2 open PRs waiting for review"`, `"· idle 2 days"`) by introspecting `slack.Message` blocks. Under the layer split, the reporter test becomes `package application_test`, which depguard confines to domain types + fakes (it may **not** import slack/store/infra). So it cannot build a real composer to assert text. The faithful resolution:

- The reporter test asserts on the **data the reporter hands its ports** — which channels get a parent+threaded-reply and in what order, the `mentions` and `StuckPR{Repository,Number,URL,IdleDays}` passed to the composer, ghost/unmapped exclusion, schedule filtering, `(repo,number)` dedup, and the timezone cutoff. This is the reporter's actual responsibility, asserted more precisely as structured data than the old substring matching.
- The **Slack text formatting** (headline wording, `idle N days`, URL rendering) is the composer's concern and stays covered by `internal/slack/composer_test.go` (already exercises `StuckDigest*`).
- No behavior changes; total coverage is preserved (and sharpened). This is the layering boundary doing its job, documented per the guardrail — not a smell papered over.

### domain/ (`package domain`) — imports stdlib + `kernel` + `routing/domain`
- `interfaces.go` — ports: `StuckFinder` (FindStuck→`[]PullRequest`), `MappingLookup` (Get→`routingdomain.RepoMapping`), `DigestResolver` (DigestFor→`routingdomain.DigestConfig`), `DigestComposer` (StuckDigestParent(mentions,count)→`Message`; StuckDigestList(prs)→`Message`), `DigestPoster` (PostMessage/PostReply→(string,error), taking `Message`), `ScheduleJob` (ReportSchedule(ctx,spec)). Use cases: `DigestReporter { Report(ctx) error; ReportSchedule(ctx, spec) error }`, `DigestScheduler { Run(ctx) error }`.
- `models.go` — `PullRequest{Repository string; PRNumber int; UpdatedAt time.Time; Messages []MessageRef}`, `MessageRef{Channel, MessageID string}`, `StuckPR{Repository string; Number int; URL string; IdleDays int}`, `Message{Blocks []Block; Fallback string}`, `Block{Type string; Text *TextObject}`, `TextObject{Type, Text string}`, `ReporterParams{Finder,Mappings,Poster,Composer,Digests, Logger *slog.Logger, TZ *time.Location, Now func() time.Time}`, `SchedulerParams{Specs []string; Job ScheduleJob; Logger *slog.Logger; TZ *time.Location}`.
- `constants.go` — `BlockTypeSection = "section"`; `GitHubPRURLPrefix = "https://github.com/"`, `PullPathSegment = "/pull/"` (prURL builds `Prefix + repo + Segment + n`). No inline literals in the application.
- `doc.go` — package doc.

### application/ (`package application`) — imports stdlib + `digest/domain` + `routing/domain`; NO slack/store/github
- `reporter.go` — `Reporter`, `NewReporter(domain.ReporterParams) *Reporter`, `Report`, `ReportSchedule`, `report`, `channelGroup`, `groupByChannel`, `prURL`, `startOfDay`, `idleDays`. Repoint: `store.PullRequest→domain.PullRequest`, `store.Message→domain.MessageRef`, `store.ErrNotFound→routingdomain.ErrNotFound`, `slack.StuckPR→domain.StuckPR`, `slack.Message→domain.Message`, `*slack.Composer→domain.DigestComposer`, poster→`domain.DigestPoster`. `var _ domain.DigestReporter = (*Reporter)(nil)` (also satisfies `domain.ScheduleJob`).
- `scheduler.go` — `Scheduler`, `NewScheduler(domain.SchedulerParams) (*Scheduler, error)`, `Run`. `var _ domain.DigestScheduler = (*Scheduler)(nil)`. Preserve up-front `cron.ParseStandard` validation, tz default UTC, ctx-cancel stop.
- `doc.go`.
- `reporter_test.go` (**`package application_test`**) — behavioral rewrite (see above): fake `DigestComposer` (records parent `{mentions,count}` and list `{prs}` calls, returns a sentinel `Message`), fake `DigestPoster` (records `{channel,threadTS}`), fake/recording finder returning `[]domain.PullRequest`, `fakeMappings`/`fakeDigestResolver` over `routingdomain` types. Build via `application.NewReporter(domain.ReporterParams{Now: fixed, TZ: …})`. Correlate mentions↔channel by first-seen post order. Preserve every behavioral case: parent-then-threaded-reply per channel, C_BETA order, ghost/unmapped exclusion, dedup by (repo,number), schedule filter (9am/6pm), no-stuck→no-posts, base-mentions-only-on-base-channel, timezone cutoff.
- `scheduler_test.go` (**`package application`** internal — needs `s.tz`) — port `fakeScheduleJob` (implements `domain.ScheduleJob`), own `discardLogger`; cases: reject invalid spec, accept valid specs, reject bad-among-many, stores tz, Run stops on ctx cancel. Build via `NewScheduler(domain.SchedulerParams{…})`.

### infrastructure/ (`package infrastructure`) — no depguard rule (transition debt: imports slack/store)
- `stuck_repo.go` — `StuckRepo` wraps `*store.PullRequests`; `NewStuckRepo`; `FindStuck` maps `[]store.PullRequest → []domain.PullRequest` (Messages→MessageRef). `var _ domain.StuckFinder`.
- `slack_composer.go` — `SlackComposer` wraps `*slack.Composer`; `NewSlackComposer`; `StuckDigestParent`/`StuckDigestList` call the slack composer and map `slack.Message → domain.Message` (and `[]domain.StuckPR → []slack.StuckPR`). `var _ domain.DigestComposer`.
- `slack_poster.go` — `SlackPoster` wraps `*slack.Client`; `NewSlackPoster`; `PostMessage`/`PostReply` map `domain.Message → slack.Message` and delegate. `var _ domain.DigestPoster`.
- `adapters_test.go` (`package infrastructure`, internal) — focused boundary coverage: `SlackComposer` output section-text equals the underlying `slack.Composer` output (mapping preserves text + fallback); `domain.Message → slack.Message` mapper reproduces Type/Text; `StuckRepo.FindStuck` over `store.NewTestDB` maps a stuck row to the domain DTO with its MessageRefs.

### module.go (`package digest`)
- `Config{Specs []string; TZ *time.Location}` (the composition root supplies the distinct enabled specs — `provider.Schedules()` — and the digest timezone).
- `Module = fx.Module("digest", fx.Provide(` — `fx.Annotate(infrastructure.NewStuckRepo, fx.As(new(domain.StuckFinder)))`, `fx.Annotate(infrastructure.NewSlackComposer, fx.As(new(domain.DigestComposer)))`, `fx.Annotate(infrastructure.NewSlackPoster, fx.As(new(domain.DigestPoster)))`, `provideReporterParams`, `fx.Annotate(application.NewReporter, fx.As(new(domain.DigestReporter)), fx.As(new(domain.ScheduleJob)))`, `provideSchedulerParams`, `fx.Annotate(application.NewScheduler, fx.As(new(domain.DigestScheduler)))` `))`.
- `provideReporterParams(finder domain.StuckFinder, mappings domain.MappingLookup, poster domain.DigestPoster, composer domain.DigestComposer, digests domain.DigestResolver, logger *slog.Logger, cfg Config) domain.ReporterParams` — `Now: time.Now`, `TZ: cfg.TZ`.
- `provideSchedulerParams(job domain.ScheduleJob, logger *slog.Logger, cfg Config) domain.SchedulerParams` — `Specs: cfg.Specs`, `TZ: cfg.TZ`.
- `module_test.go` (`package digest_test`) — fxtest supplies `*store.PullRequests` (`store.NewTestDB`), `*slack.Composer`, `*slack.Client`, stub `domain.MappingLookup`, stub `domain.DigestResolver`, discard `*slog.Logger`, `digest.Config{Specs: []string{"0 9 * * *"}, TZ: time.UTC}`; `fx.Invoke(func(domain.DigestScheduler, domain.DigestReporter){})`.

### depguard (.golangci.yml) — two rules mirroring validation
- `digest-domain`: allow kernel + digest/domain + routing/domain.
- `digest-application`: allow kernel + digest/domain + digest/application + routing/domain.
- (no infrastructure rule.)

## Consumer rewire (T5)
`internal/app/app.go` `buildDigestScheduler`: replace `digest.NewReporter(pullRequests, provider, slackClient, composer, provider, logger, cfg.DigestTimezone)` + `digest.NewScheduler(specs, reporter, logger, cfg.DigestTimezone)` with the layered constructors —
```
reporter := digestapp.NewReporter(digestdomain.ReporterParams{
    Finder:   digestinfra.NewStuckRepo(pullRequests),
    Mappings: provider, Digests: provider,
    Composer: digestinfra.NewSlackComposer(composer),
    Poster:   digestinfra.NewSlackPoster(slackClient),
    Logger: logger, TZ: cfg.DigestTimezone, Now: time.Now,
})
scheduler, err := digestapp.NewScheduler(digestdomain.SchedulerParams{Specs: specs, Job: reporter, Logger: logger, TZ: cfg.DigestTimezone})
```
Return type of `buildDigestScheduler` and `Wire` becomes `*digestapp.Scheduler`. Preserve nil-when-no-specs and bad-cron-fails-startup exactly. Then delete flat `internal/digest/{reporter,scheduler,reporter_test,scheduler_test}.go` and fill `module.go` + `module_test.go`. Aliases: `digestapp`, `digestdomain`, `digestinfra`.

## Tasks
- **D4-T1** scaffold 3 layers (doc.go each) + `module.go` stub + 2 depguard rules · `refactor(digest): scaffold domain/application/infrastructure`
- **D4-T2** domain (interfaces, models incl. Message mirror + params DTOs, constants, doc) · `refactor(digest): add domain ports and DTOs`
- **D4-T3** application (reporter + scheduler) + behavioral reporter_test + internal scheduler_test · `refactor(digest): relocate reporter and scheduler`
- **D4-T4** infrastructure (stuck_repo + slack_composer + slack_poster + adapters_test) · `refactor(digest): relocate persistence and Slack adapters`
- **D4-T5** rewire app.Wire + fill module + fxtest + delete flat files · `refactor(digest): rewire wiring and add fx module`

## Gate
- [x] `go build ./...` + `go test -race ./...` green · `golangci-lint run` clean · flat `internal/digest/{reporter,scheduler}.go` gone · digest fully layered · module fxtested · depguard empirically verified (planted forbidden import flags) · plan box checked

**Done.** 5 commits `41b27e2`→`ace0848`. Race suite 29 pkgs ok, 0 FAIL; lint clean. Anti-corruption: finder returns domain `PullRequest`, composer/poster exchange a domain `Message`; infra `StuckRepo`/`SlackComposer`/`SlackPoster` map to store/slack; `MappingLookup`/`DigestResolver` satisfied structurally by the routing `*Provider`. Clock injected via `ReporterParams.Now` (killed the `r.now` test-seam). reporter_test rewritten behavioral (`application_test` confined to domain types by depguard) — asserts the data handed to the composer/poster ports; Slack text stays covered in `internal/slack/composer_test.go`. depguard blocks slack in domain / store in application (probe-verified).
