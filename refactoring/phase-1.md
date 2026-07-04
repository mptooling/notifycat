# Phase 1 — maintenance domain (cleanup + reconcile)

The first real domain migration, chosen because both packages are leaves with tiny fan-in. This phase is the **pattern-setter**: it establishes clean domain DTOs, ports, use-case interfaces, an infra repository adapter that maps from `internal/store`, an `fx.Module`, and an `fxtest` — the template every later domain copies. `app.Wire` and the `notifycat-reconcile` binary keep running throughout (their call-sites just re-point at the new packages).

Target tree:

```
internal/maintenance/
  domain/
    interfaces.go   # ports + use-case interfaces
    models.go       # DTOs (PRRow, Summary)
    constants.go    # Interval
  application/
    cleaner.go      # StaleMessageCleaner use case (was cleanup.Scheduler)
    reconciler.go   # Reconciler use case (was reconcile.Reconciler)
  infrastructure/
    pr_repository.go    # adapter over internal/store, maps store.PullRequest → domain.PRRow
    github_checker.go   # adapter (was reconcile.GitHubChecker) → PRChecker port
  module.go         # fx.Module + bindings
  module_test.go    # fxtest: graph is satisfiable
```

---

### P1-T1 — Scaffold the maintenance domain
- Depends on: Phase 0
- Context budget: read ONLY `refactoring/README.md`, `ARCHITECTURE.md`.
- Goal: create the empty three-layer structure + module skeleton + depguard rules for the maintenance layers.
- Changes: create `internal/maintenance/domain/doc.go`, `.../application/doc.go`, `.../infrastructure/doc.go`, `internal/maintenance/module.go` (empty `var Module = fx.Options()` placeholder); modify `.golangci.yml`.
- Spec: each `doc.go` is a package clause + one-line doc. In `.golangci.yml`, add depguard rules from the Phase 0 template: files under `internal/maintenance/domain/**` may import only `internal/kernel` (no `application`, `infrastructure`, `store`, `slack`, `github`, `platform`); files under `internal/maintenance/application/**` may not import `internal/maintenance/infrastructure` nor `internal/store`/`internal/github`. Confirm the rules match zero files yet (layers are empty) so the lint stays green.
- Verify: `go build ./internal/maintenance/...`; `golangci-lint run` green.
- DoD: structure compiles; depguard rules staged.
- Commit: `refactor(maintenance): scaffold domain/application/infrastructure`

### P1-T2 — Move cleanup into maintenance
- Depends on: P1-T1
- Context budget: read ONLY `internal/cleanup/cleanup.go`, `internal/cleanup/cleanup_test.go`, and `internal/app/app.go` (only `buildCleanupScheduler`).
- Goal: relocate the stale-message cleanup, cleanly layered, behaviour identical.
- Changes: create `internal/maintenance/domain/{interfaces,models,constants}.go` (cleanup parts), `internal/maintenance/application/cleaner.go`, `internal/maintenance/infrastructure/pr_repository.go`; modify `internal/app/app.go`; move `internal/cleanup/cleanup_test.go` → `internal/maintenance/application/cleaner_test.go` (imports only updated); delete `internal/cleanup/`.
- Spec:
  1. Port the `StaleMessageDeleter` interface **verbatim** (same method name/signature) into `domain/interfaces.go`, but re-typed against the domain DTO if it returns rows (it deletes by cutoff, so likely `DeleteStale(ctx, cutoff) (int64, error)` — keep exactly what the source has).
  2. Add use-case interface `StaleMessageCleaner interface { Run(ctx context.Context) error }` to `domain/interfaces.go`, with the doc block (the *why*: deletes rows older than TTL, once at start then every `Interval`; never touches Slack).
  3. `Interval` const → `domain/constants.go`.
  4. `cleanup.Scheduler` → `application.cleaner` implementing `StaleMessageCleaner`; keep `NewCleaner(deleter domain.StaleMessageDeleter, ttl time.Duration, interval time.Duration, logger, now func() time.Time)` — preserve `SetNowFunc`/clock injection as a constructor param (do not add a second constructor). `Run`/`tick` logic copied unchanged.
  5. `infrastructure/pr_repository.go`: a `PRRepository` struct wrapping `*store.PullRequests` (imported from `internal/store` — transition debt, infra only) that satisfies `domain.StaleMessageDeleter`. Since `store.PullRequests` already satisfies the old interface, the adapter is a thin pass-through (or bind `store.PullRequests` directly if its method set matches the port exactly — prefer an explicit adapter for the mapping seam).
  6. `app.go` `buildCleanupScheduler`: construct via the new `application.NewCleaner` + `infrastructure.NewPRRepository(pullRequests)`; keep the returned type usable by `Wire` (it still needs a `Run(ctx) error` — `*application.cleaner` provides it). Update imports.
  7. Move the test; update only imports + constructor name. Assertions unchanged.
- Constraints: behaviour identical; the 24h interval, TTL math, and clock injection all preserved; no new log lines.
- Verify: `gofmt -l internal/maintenance internal/app` empty; `go build ./...`; `go vet ./internal/maintenance/...`; `go test ./internal/maintenance/... ./internal/app/...` green.
- DoD: cleanup gone; maintenance owns it; `internal/cleanup` deleted; Wire green.
- Commit: `refactor(maintenance): relocate stale-message cleanup`

### P1-T3 — Move reconcile into maintenance
- Depends on: P1-T2
- Context budget: read ONLY `internal/reconcile/reconcile.go`, `internal/reconcile/github_checker.go`, their `_test.go`, and `cmd/notifycat-reconcile/main.go`.
- Goal: relocate reconcile, cleanly layered, behaviour identical.
- Changes: extend `internal/maintenance/domain/{interfaces,models}.go`; create `internal/maintenance/application/reconciler.go`, `internal/maintenance/infrastructure/github_checker.go`; modify `cmd/notifycat-reconcile/main.go`; move the two `_test.go` files under the matching new packages (imports only); delete `internal/reconcile/`.
- Spec:
  1. Ports `OpenLister`, `Closer`, `Deleter`, `PRChecker` → `domain/interfaces.go`, **but** re-typed against a domain DTO: define `domain.PRRow` in `models.go` with the fields reconcile actually reads from `store.PullRequest` (Repository, Number, URL, and any others used by `markClosed`/`removeNotFound`/`removeDraft` — read the source to enumerate). `OpenLister.ListOpen` returns `[]domain.PRRow`. `Closer`/`Deleter` keep their `(ctx, repository, number)` shape. Keep the sentinel errors `ErrPRNotFound`/`ErrPRDraft` in `domain` (they are part of the port contract).
  2. Use-case interface `Reconciler interface { Run(ctx) (Summary, error) }`; `Summary` → `models.go`. Doc block on the interface.
  3. `reconcile.Reconciler` → `application.reconciler` implementing it; `NewReconciler(lister, checker, closer, deleter, logger, dryRun)` preserved. The private `markClosed`/`removeNotFound`/`removeDraft`/`prURL` move with it, retyped to `domain.PRRow`.
  4. `reconcile.GitHubChecker` (+ `prGetter`) → `infrastructure/github_checker.go` implementing `domain.PRChecker`; it wraps the github client (via `prGetter`, satisfied by `internal/github` — transition debt, infra only).
  5. `infrastructure/pr_repository.go` (from P1-T2) gains the `OpenLister`/`Closer`/`Deleter` implementations, mapping `store.PullRequest` → `domain.PRRow`.
  6. `cmd/notifycat-reconcile/main.go`: rewire to the new constructors. Behaviour, flags, exit codes, and output unchanged.
  7. Move tests; update imports + constructor names + swap `store.PullRequest` fixtures for `domain.PRRow` where the test constructs port inputs. Do not change assertions about counts/summary.
- Constraints: dry-run behaviour, summary counts, draft/not-found handling all identical.
- Verify: `gofmt -l` empty; `go build ./...`; `go test ./internal/maintenance/... ./cmd/notifycat-reconcile/...` green.
- DoD: reconcile gone; maintenance owns it; `internal/reconcile` deleted; the binary runs identically.
- Commit: `refactor(maintenance): relocate PR reconcile`

### P1-T4 — Wire the maintenance fx module + fxtest
- Depends on: P1-T3
- Context budget: read ONLY `internal/maintenance/**` and `ARCHITECTURE.md` (the fx section).
- Goal: express maintenance as an `fx.Module` and prove the graph is satisfiable, without changing the production binaries yet.
- Changes: fill `internal/maintenance/module.go`; add `internal/maintenance/module_test.go`.
- Spec: `Module` provides the use cases as their domain interfaces (`fx.Annotate(application.NewCleaner, fx.As(new(domain.StaleMessageCleaner)))`, same for `Reconciler`) and the infra adapters bound to their ports (`PRRepository` → `StaleMessageDeleter`/`OpenLister`/`Closer`/`Deleter`; `GitHubChecker` → `PRChecker`). External inputs the module cannot build itself (the `*store.PullRequests`, the github client, `ttl`, `dryRun`, `logger`, clock) are declared as module **parameters** — provide them via `fx.Provide` stubs in the test. `module_test.go` uses `fxtest.New(t, Module, fx.Provide(<test stubs>)).RequireStart().RequireStop()` (or `fx.ValidateApp`) to assert the graph resolves. This module is not imported by any `cmd/` binary yet (cutover is Phase 8) — the fxtest is what keeps it live and honest.
- Verify: `go test ./internal/maintenance/...` green (includes the fxtest); `golangci-lint run` green.
- DoD: `maintenance.Module` resolves under fxtest; ports all bound.
- Commit: `refactor(maintenance): add fx module and graph test`

---

## Phase 1 gate
- [ ] `go build ./...` green
- [ ] `go test -race ./...` green (assertions unchanged)
- [ ] `golangci-lint run` green (maintenance layer boundaries enforced)
- [ ] `internal/cleanup` and `internal/reconcile` deleted; `notifycat-reconcile` runs identically
- [ ] `maintenance.Module` proven by fxtest
- [ ] `REFACTORING_PLAN.md` Phase 1 box checked
