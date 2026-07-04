# Phase 0 — Scaffolding & guardrails

Land the tools the whole refactor needs, with **zero risk to the running binaries**. No domain code moves in this phase.

**Refinement of the plan:** the original plan said "wrap `app.Wire` behind an fx app" in Phase 0. We now **defer all entrypoint changes to Phase 8 (cutover)**. Reason: it keeps every domain phase a uniform, low-risk "move code + define ports + build module + prove with fxtest" task, and isolates the one-time entrypoint/lifecycle rewrite to a single focused phase. Through Phases 1–7, `app.Wire` stays the live wiring (its call-sites update as packages move); each domain's `fx.Module` is proven by an `fxtest` in its own phase but not yet used by the production binaries. Phase 8 flips all `cmd/*` to `fx.New(...)` and deletes `Wire`.

Outcome of Phase 0: `go.uber.org/fx` is available, `internal/kernel` exists and is guarded pure, and `.golangci.yml` has the depguard framework ready for per-layer rules.

---

### P0-T1 — Add the uber/fx dependency
- Depends on: —
- Context budget: read ONLY `go.mod`.
- Goal: make `go.uber.org/fx` available to the module.
- Changes: modify `go.mod`, `go.sum`.
- Spec: run `go get go.uber.org/fx@latest` then `go mod tidy`. This also pulls `go.uber.org/dig` and `go.uber.org/zap` (fx's own logger; we silence it with `fx.NopLogger`, so it never emits) and promotes `go.uber.org/multierr` from indirect. Do not add any other dependency.
- Constraints: no code changes; dependency addition only.
- Verify: `go build ./...` green; `go mod verify` ok; `grep -q 'go.uber.org/fx' go.mod`.
- DoD: fx resolvable; nothing else in the tree changed.
- Commit: `build(deps): add uber/fx`

### P0-T2 — Create the shared kernel package
- Depends on: —
- Context budget: read ONLY `ARCHITECTURE.md` (the "Shared code" section).
- Goal: establish `internal/kernel` as a real, empty, pure package that later phases fill with shared value objects.
- Changes: create `internal/kernel/doc.go`.
- Spec: `doc.go` contains only the package clause and a doc comment: package `kernel` holds the shared domain value objects (`PR`, `Message`, `Event`, `Sender`, `ReviewState`) and cross-domain enums used by the notification, review, and routing domains; it is pure and imports nothing from other `internal/...` packages. No types yet — they arrive when their domains migrate (notification in Phase 5).
- Constraints: no types, no imports.
- Verify: `go build ./internal/kernel/...` green; `gofmt -l internal/kernel` empty.
- DoD: `internal/kernel` compiles as an empty documented package.
- Commit: `chore(kernel): add shared kernel package skeleton`

### P0-T3 — Seed depguard import-boundary linting
- Depends on: P0-T2
- Context budget: read ONLY `.golangci.yml`. Check the installed linter version with `golangci-lint version`.
- Goal: add the depguard linter with the framework for per-layer rules, plus the first live rule — the kernel must stay pure.
- Changes: modify `.golangci.yml`.
- Spec: enable `depguard` and add a rule that forbids any file under `internal/kernel/` from importing `github.com/mptooling/notifycat/internal/...` (kernel purity). Adapt the exact schema to the installed golangci-lint major version (v1 and v2 differ — inspect the existing file's shape and match it; if unsure, confirm the depguard schema via the context7 MCP docs for golangci-lint). Include a commented template block showing how a per-domain layer rule will look (e.g. "files under `internal/*/domain/**` may not import `**/application/**` or `**/infrastructure/**`") so later phases fill it in by pattern. Do not add layer rules for domains that don't exist yet — they'd match nothing and add noise.
- Constraints: the rule set must not flag any current file — run the linter and confirm zero new findings.
- Verify: `golangci-lint run` green (no new issues); the kernel rule is present.
- DoD: depguard active; kernel guarded pure; template comment in place for later phases.
- Commit: `chore(lint): add depguard with kernel-purity rule`

---

## Phase 0 gate
- [ ] `go build ./...` green
- [ ] `go test -race ./...` green (unchanged)
- [ ] `golangci-lint run` green
- [ ] fx available, `internal/kernel` pure + guarded, depguard framework ready
- [ ] `REFACTORING_PLAN.md` Phase 0 box checked
