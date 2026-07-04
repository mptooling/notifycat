# Refactoring execution playbook

This folder turns [`../REFACTORING_PLAN.md`](../REFACTORING_PLAN.md) into **small, independently-dispatchable tasks** that a cheap sub-agent can run inside a 200k context window. One file per phase (`phase-0.md` … `phase-8.md`); each file is a list of tasks; each task is self-contained.

Read this playbook once, then work one phase file at a time. The authoritative rules are in [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — this file is *how we execute*, not *what we're building*.

## How the orchestrator runs a phase

1. Open the phase file. Do **not** load other phase files — they are not needed and cost context.
2. Run tasks in dependency order (each task lists `Depends on`). Independent tasks in the same phase may be dispatched in parallel.
3. For each task, dispatch **one sub-agent** with the task's `Context budget` + `Goal` + `Changes` + `Spec` + `Verify` + `Commit`. The sub-agent reads only the files the task names.
4. When the sub-agent reports green, the orchestrator (not the sub-agent) commits — one commit per task, using the task's `Commit` line. Batch trivially-related tasks into one commit only if they were verified together.
5. At the end of a phase, run the **phase gate** (below). Only then move to the next phase.

## Invariants (every task, no exceptions)

- **One branch, one PR.** Everything lands on `refactor/full-refactoring`. Never open a second branch or PR.
- **Pure refactoring — zero behaviour change.** No new features, no bug "fixes", no changed log lines, HTTP responses, or Slack payloads. If a genuine bug is discovered, write it down for a *separate* PR later; do not fix it here.
- **Tests pass unchanged.** A move should not require editing a test's assertions. Editing a test body beyond its import paths / constructor call-sites is a smell — stop and surface it. New tests are fine only when a task explicitly says to add one (e.g. a port's contract test); they must not change coverage of existing behaviour.
- **Green before commit.** The task's `Verify` block must pass before the orchestrator commits. Never commit red.
- **No operator state.** Never add `config.yaml`, `config.lock`, `.env`, or anything under `/data/` (all gitignored). `graphify-out/` is gitignored too — leave it.

## Context hygiene (staying under 200k)

- A task's `Context budget` lists the **only** files the sub-agent should read. Trust it. Do not "explore the codebase first."
- To learn a package's public surface without reading it whole, use `grep -rnE '^(func|type|const|var) ' <pkg>/*.go | grep -v _test.go` — the same trick that produced these specs.
- Keep `ARCHITECTURE.md` and the current phase file in context; drop everything else.
- If a task genuinely needs more than ~15 files read, it is too big — split it and note that in the phase file before running.

## Task-spec template

Every task in a phase file uses these fields:

```
### <PHASE>-T<N> — <title>
- Depends on: <task IDs, or "—">
- Context budget: read ONLY <explicit file list>. Surface others via grep.
- Goal: <1–2 sentences>
- Changes: create/modify/delete <exact paths>
- Spec: <the detailed instructions — interfaces with signatures, DTOs, enums, move map, wiring>
- Constraints: <task-specific reminders on top of the global invariants>
- Verify: <exact commands + expected result>
- DoD: <checklist>
- Commit: <type(scope): summary>
```

## Verification protocol

Do **not** run `just test` / `just check` inside a task — `just` loads the repo `.env`. Run the underlying tools directly:

| Level | Commands | When |
| --- | --- | --- |
| **Per task** | `gofmt -l <changed dirs>` (expect empty) · `go build ./...` · `go vet ./<changed>/...` · `go test ./<changed>/...` | before every commit |
| **Phase gate** | `go build ./...` · `go test -race ./...` · `golangci-lint run` (includes depguard from Phase 0) | end of each phase |
| **CI only** | `govulncheck` (Docker, slow) | leave to CI on the PR |

`go test -race ./...` is the safety net — it is the same 460-test suite; it must stay green and, for a pure move, its assertions unchanged.

## Commit protocol

- **Conventional Commits**, scope = the domain or layer touched: `refactor(maintenance): …`, `chore(fx): …`, `build(deps): add uber/fx`. Use `refactor:` for moves, `build:`/`chore:` for tooling/deps, `docs:` for docs.
- **No attribution footer** (`Co-Authored-By`, tool lines) — repo convention.
- Never write the literal `BREAKING CHANGE` in a body (release-please parses it as a footer). Say "reshapes" / "relocates" instead.
- One commit per task (subject = the task's `Commit` line). The PR title (set once, at the end) is a single `refactor: …` summarising the whole migration.

## Naming & structure conventions (apply everywhere)

- **Folders per domain:** `internal/<domain>/{domain,application,infrastructure}`. The fx module lives at `internal/<domain>/module.go` in package `<domain>`, exporting `var Module fx.Option`.
- **Package names = folder basename** (`domain`, `application`, `infrastructure`). Cross-domain imports alias them: `routingdomain "…/internal/routing/domain"`.
- **Dependency DAG (refines ARCHITECTURE.md):** a `domain` layer may import the shared kernel **and the `domain` layer of domains it depends on** (per the arrows: notification→routing, review→notification, digest→routing, validation→routing, diagnostics→validation) — ports only, never another domain's `application`/`infrastructure`. `application`/`infrastructure` may import their own `domain`, the kernel, dependency domains' `domain` layers, and (infra only) `platform/*`. The DAG stays acyclic.
- **Ports:** every use case and every infra adapter has an interface in its domain's `interfaces.go`. Name adapters' ports for the capability, not the tech: `Messenger`, `MessageStore`, `ChangedFilesReader`, `SignatureVerifier` — not `SlackClient`, `GormRepo`.
- **fx binding:** bind concretes to ports only in `module.go`, via `fx.Annotate(application.NewX, fx.As(new(domain.XUseCase)))` and `fx.Annotate(infrastructure.NewY, fx.As(new(domain.YPort)))`.
- **Lifecycle:** long-running components (server, schedulers) register `fx.Lifecycle` hooks; `OnStart` launches work in a goroutine and returns, `OnStop` cancels/shuts down. `fx.New(...).Run()` owns SIGINT/SIGTERM.

## Modeling: kernel value objects vs. GORM models

A recurring trap. The shared kernel (`internal/kernel`) holds **pure domain value objects** (`PR`, `Message`, `Event`, `Sender`, `ReviewState`). The GORM structs in today's `store` (`store.PullRequest`, `store.Message`, `store.CodeReview`, …) are **persistence models** — they carry `gorm` tags and DB concerns. These are **not the same type**. When a repository moves into a domain's `infrastructure`, its GORM model stays in the persistence layer and the repository **maps** GORM model ⇄ domain DTO at the boundary. Never leak a `gorm`-tagged struct across a port, and never let a `domain` or `application` layer import `internal/store` (or, later, `platform/persistence`).

**Transition debt (infrastructure only).** Until `internal/store` moves to `platform/persistence` in its own phase, a domain's `infrastructure` repository adapter **may** import `internal/store` for the db handle and GORM models, mapping them to the domain's own DTOs at the port. The `domain` and `application` layers never import it. When persistence migrates, only the infra import path changes — the ports and DTOs are already clean. Same rule for `internal/slack`, `internal/github`, `internal/config`: infra adapters may consume today's packages until they move under `platform/`.

## Phase gate checklist

- [ ] `go build ./...` green
- [ ] `go test -race ./...` green (assertions unchanged vs. phase start)
- [ ] `golangci-lint run` green (import boundaries hold)
- [ ] every task in the phase committed with its Conventional-Commit subject
- [ ] `REFACTORING_PLAN.md` tracking box for the phase checked
- [ ] no stray files, no operator state, no second branch

## Just-in-time expansion

`phase-0.md`, `phase-1.md`, and `phase-2.md` are written at full task-level detail. `phase-3.md`–`phase-8.md` carry the **task breakdown** (each task's goal, files, and DoD) but intentionally defer the finest detail — exact symbol lists and signatures — because they depend on how the earlier phases land. Before starting one of those phases, the orchestrator **expands its file to full specs** (re-survey the then-current code with grep, fill each task's `Spec`), commits that as a `docs:` update, then executes. This keeps late-phase specs from going stale against a moving codebase.

## Patterns established in Phase 1 (copy these verbatim)

Phase 1 (maintenance) is the proven template. Every later domain replicates these exact shapes; deviate only with a stated reason.

**Constructors — single Params DTO (locked decision).** Every use-case/adapter constructor takes ONE plain `XParams` struct defined in `domain/models.go` (ports + logger + flags bundled). The domain and application layers stay 100% framework-free — no `go.uber.org/fx` import inward. Constructors return the **exported concrete** struct (`*Cleaner`, `*Reconciler`, `*PRRepository`, `*GitHubChecker`); `module.go` binds concrete→port. Fold former test-seam setters (e.g. `SetNowFunc`) into the params (`Now func() time.Time`) — no second constructor, no setter. Example: `func NewReconciler(params domain.ReconcilerParams) *Reconciler`.

**Return the interface where a port is documented.** Doc blocks live on the domain interface (`domain.StaleMessageCleaner`, `domain.Reconciler`); the impl's exported methods carry a terse `// Run implements domain.X.` (satisfies revive `exported` without duplicating the why). Sentinel errors that are part of a port contract (`ErrPRNotFound`, `ErrPRDraft`) live in the domain layer; keep their exact message strings to preserve behaviour.

**module.go (package `<domain>`, the only fx-aware file).**
- `var Module = fx.Module("<name>", fx.Provide(...))`.
- Bind each concrete to its port(s) with `fx.Annotate(NewX, fx.As(new(domain.Port)))`. A multi-port adapter lists several `fx.As` in one Annotate (the repo binds to `StaleMessageDeleter`/`OpenLister`/`Closer`/`Deleter` at once) — fx builds it once and shares the instance.
- Bundle scalar config into a module-level `Config` struct supplied as ONE value (avoids fx's duplicate-`time.Duration` collision). Small unexported `provideXParams(...) domain.XParams` funcs assemble each Params from the graph.
- External inputs the module can't build (the store repo, a platform client port, `*slog.Logger`, `Config`) are graph inputs the composition root/test supplies.

**module_test.go (package `<domain>_test`) — fxtest graph proof.** `fxtest.New(t, Module, fx.Provide(<stubs for external inputs>), fx.Supply(<domain>.Config{...}), fx.Invoke(func(domain.UseCaseA, domain.UseCaseB){})).RequireStart().RequireStop()`. The `fx.Invoke` forces construction so a missing binding fails the test. Real `store.NewTestDB(t)` (in `store/testing.go`, importable) supplies the repo; a local stub supplies each platform-client port.

**depguard layer rules (`.golangci.yml`).** Per layer, `list-mode: lax` + `deny: pkg github.com/mptooling/notifycat/internal` + `allow:` the permitted internal packages (kernel, own `domain`, dependency domains' `domain`). Lax = stdlib/third-party allowed by default, `allow` carves exceptions to the `internal` deny. **Each rule must `allow` its OWN import path** so external `_test` packages can import the package under test. Infrastructure has NO rule (it may touch `store`/`slack`/`github` as transition debt). Verify each new rule empirically: a planted cross-layer import must be flagged; legitimate imports must pass.

**Transition-debt imports** stay confined to `infrastructure/` (`internal/store`, `internal/github`, …). Repositories map the store's gorm models ⇄ domain DTOs (`store.PullRequest` → `domain.PRRow`) at the boundary; no gorm-tagged type crosses a port.

**app.go / cmd rewiring** re-points construction at the new packages (aliased imports `maintenanceapp`/`maintenancedomain`/`maintenanceinfra`), preserving `Wire`'s and each binary's behaviour. `Wire`'s return type changes from the old concrete to the new one; entrypoints that use `:=` are unaffected. `app.Wire` stays the live wiring until Phase 8.
