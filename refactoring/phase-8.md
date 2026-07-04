# Phase 8 — platform extraction & cutover

The final phase. Move the remaining shared clients under `internal/platform/`, replace every `cmd/*/main.go` with an `fx.New(...)` composition, and delete `internal/app`. After this, the transition debt is gone and fx owns the runtime. Large — **split into the tasks below**, each green; the entrypoint rewrites are the only behaviour-sensitive part, so treat them carefully and lean on the smoke test.

> Expand before running: grep every remaining importer of `internal/store`, `internal/config`, `internal/slack`, `internal/github`; read all six `cmd/*/main.go` and `internal/app/app.go` in full.

### C8-T1 — Move shared clients under platform (mechanical renames)
- Spec: `internal/store` → `internal/platform/persistence` (db handle, migrations, GORM models, `Open`/`MigrateUp`/`SQLDB`); `internal/config` → `internal/platform/config`; `internal/slack` → `internal/platform/slack`; `internal/github` → `internal/platform/github`. Pure import-path renames (`gofmt`/`goimports` + find-replace). Update every infra adapter's import from the transition-debt path to the platform path. No logic changes.
- Verify: `go build ./...`; `go test -race ./...` green after each move.
- Commit (one per client): `refactor(platform): relocate <store|config|slack|github> under platform`

### C8-T2 — Tighten depguard to forbid the old paths
- Spec: update `.golangci.yml` so no `internal/*/{domain,application}` imports `platform/persistence` (infra only), and the old `internal/store` etc. paths are denied everywhere (they no longer exist). Confirm zero findings.
- Commit: `chore(lint): finalize layer import boundaries`

### C8-T3 — Server runtime module + startup gate
- Spec: turn `app.Wire`'s remaining glue into an fx module (e.g. `internal/runtime`): `config.Load` provider, the composer, the HTTP `*http.Server` provider, and `startupValidate` as an `fx.Invoke` that runs the routing+validation gate at boot (returning an error fails `app.Start`, preserving fail-fast). Server + cleanup + digest schedulers register `fx.Lifecycle` hooks (`OnStart` launches the goroutine, `OnStop` cancels/shuts down). Preserve hardened server timeouts and the slack-interactivity-enabled-only-when-signing-secret route registration.
- Commit: `refactor(runtime): fx module for server, startup gate, lifecycle`

### C8-T4 — Cut cmd/notifycat-server to fx.New
- Context budget: read `cmd/notifycat-server/main.go` + the runtime module.
- Spec: `main.go` becomes `fx.New(platform+kernel+all domain modules+runtime, fx.NopLogger).Run()` (or manual `Start`/`Wait`/`Stop` if byte-identical startup-error stderr/exit codes are required — decide and document). Preserve: SIGINT/SIGTERM graceful shutdown, 15s shutdown timeout, `startupError` formatting, exit codes. Verify the fatal-server-error path still exits non-zero. **Confirm the fx `Wait`/`Done`/`Shutdowner` semantics against the installed fx version (context7 docs) before finalizing.**
- Verify: `go build ./...`; `go test -race ./...`; manual `notifycat-server` boot + `GET /healthz` + SIGTERM shutdown sanity; run the smoke binary.
- Commit: `refactor(server): run via fx.New with lifecycle`

### C8-T5 — Cut the five CLI binaries to fx; delete internal/app
- Spec: `notifycat-{migrate,reconcile,config,doctor,smoke}` each become an `fx.New(<needed modules>)` composing only their modules (see the binary→module table in ARCHITECTURE.md). Delete `internal/app` entirely. Each binary's flags, output, and exit codes unchanged.
- Verify: `go build ./...`; `go test -race ./...`; `golangci-lint run`; smoke-run each CLI; `! test -d internal/app`.
- Commit (batch or per-binary): `refactor(cmd): run <binary> via fx; delete internal/app`

### C8-T6 — Finalize docs
- Spec: rewrite `CLAUDE.md`'s "Architecture (current — migrating)" section to describe the **final** DDD/fx architecture (drop the "migrating" framing and the legacy request-flow that references deleted packages). Update `docs/operations.md` and the architecture-touching docs where package names changed. Mark all `REFACTORING_PLAN.md` boxes done. Leave `ARCHITECTURE.md`/`REFACTORING_PLAN.md`/`refactoring/` in place as the historical record.
- Commit: `docs: describe final DDD/fx architecture`

## Progress
- [x] **C8-T1** platform relocation — `internal/{github,slack,config}` → `internal/platform/{github,slack,config}` (path-only, commit `e47f856`); `internal/store` → `internal/platform/persistence` (package rename `store`→`persistence`, ~80 qualifier refs, compiler-driven; commit `84a3278`). All shared clients now under `platform/`.
- [x] **C8-T2** depguard — no change needed: the existing deny-all-`internal` + per-layer allow-lists already forbid domain/application from importing `platform/persistence` (probe fired). No stale old-path references in `.golangci.yml`.
- [ ] **C8-T3–T6 (REMAINING — the fx-entrypoint cutover):** turn `app.Wire` into `internal/runtime` fx module (providers + `startupValidate` invoke gate + server/scheduler `fx.Lifecycle`); cut `cmd/notifycat-server` (and the CLIs) to `fx.New`; delete `internal/app` (moving its 24K integration test to exercise the fx-built server); finalize `CLAUDE.md`/docs. This is the one behaviour-sensitive unit (server startup/shutdown/exit-code parity) — do it deliberately with a real `notifycat-server` boot + `GET /healthz` + SIGTERM sanity check + smoke run, keeping `app.Wire` (git) as the fallback until the fx path is proven, then delete.

## Gate (whole refactor complete)
- [ ] `go build ./...` · `go test -race ./...` · `golangci-lint run` all green; assertions unchanged vs. the branch's first commit
- [ ] `internal/app`, `internal/store`, `internal/config`, `internal/slack`, `internal/github` (old paths) gone; everything under `internal/<domain>/{domain,application,infrastructure}`, `internal/kernel`, `internal/platform/*`
- [ ] all six binaries run via fx and behave identically; smoke test passes
- [ ] one PR opened from `refactor/full-refactoring`, title `refactor: migrate to DDD + hexagonal layering with uber/fx`
