# Refactoring plan

The global plan for moving notifycat onto the [target architecture](ARCHITECTURE.md): Domain-Driven Design, hexagonal layering, and uber/fx dependency injection. This is the roadmap and the progress tracker; the *rules* live in `ARCHITECTURE.md`, and the per-phase, task-level execution specs (sized for sub-agents in a 200k window) live in [`refactoring/`](refactoring/README.md).

## Strategy: strangler-fig, one domain at a time

We do **not** big-bang rewrite. The entire refactor lives on **one branch (`refactor/full-refactoring`) and ships as a single PR**, built up over **many commits** across multiple working sessions (and compactions). Each phase moves one slice onto the new architecture and leaves `just check` green; phases and sub-steps are **commit groups within the one PR**, not separate PRs. Mechanical moves may be delegated to cheap sub-agents, with the orchestrator verifying green before each commit. This is **pure refactoring — no business-logic changes**: the 460-test race suite is the safety net and must stay green at every commit boundary, and it should keep passing *unchanged* (a move that needs a test edited is a smell to investigate, not paper over). The old `internal/app.Wire` keeps the binaries running until the last domain is migrated, then it is deleted.

### Guardrails (every phase)

- `just check` (vet + lint + govulncheck + `go test -race` + build) is green before a phase is called done. Run `go test` directly during a phase (`just` loads the repo `.env`).
- **TDD** for any new behavior; pure moves keep their existing tests, relocated alongside the code.
- **No behavior change inside a move.** Restructuring and behavior changes never share a commit.
- **Import-boundary discipline** — the dependency rule from `ARCHITECTURE.md` is enforced by review, and (Phase 0) by a lint guard where feasible:
  - `domain/` imports only the shared kernel.
  - `application/` imports only its own `domain/` + kernel.
  - no domain imports another domain's `application/` or `infrastructure/`.
- Docs and `CLAUDE.md` stay truthful — each phase that changes a public shape updates the relevant `docs/` page in the same PR.

### Definition of done (per domain phase)

Package relocated into `internal/<domain>/{domain,application,infrastructure}`; every use case and infra service has a domain-layer interface; wide signatures wrapped in DTOs; literals promoted to enums/constants; doc blocks on the interfaces; an `fx.Module` for the domain; the old package deleted; tests moved and green; `just check` clean.

## Phase order

Sequenced lowest-risk-first and respecting the dependency arrows (a domain migrates after the domains it depends on). Notification is the core and the largest, so its foundations (routing, kernel, platform) go first.

| # | Phase | Why here | Key risk |
| --- | --- | --- | --- |
| 0 | **Scaffolding & guardrails** | Land fx, the `kernel` package, this plan, `ARCHITECTURE.md`, `CLAUDE.md` rules, and depguard. The entrypoint/lifecycle rewrite is **deferred to Phase 8** so each domain phase stays a uniform low-risk move; per-domain fx modules are proven by `fxtest` and `app.Wire` stays the live wiring until cutover. | fx learning curve; keep `Wire` green |
| 1 | **maintenance** (cleanup + reconcile) | Leaf domains, own scheduler/job, minimal fan-in. Best place to prove the layer pattern + an `fx.Lifecycle` hook end-to-end. | low |
| 2 | **routing** (mappings + router) | Foundational — notification, digest, and validation all depend on it. Migrate before its consumers. | tier/path-rule surface area |
| 3 | **validation** | Depends on routing; shared by startup gate, doctor, config CLI. Isolating it de-risks diagnostics. | startup-gate wiring |
| 4 | **digest** | Depends on routing; otherwise self-contained (own scheduler + reporter). | second `fx.Lifecycle` scheduler |
| 5 | **notification** (core) | The largest surface: dispatcher, open/close/draft, reactions, bot-PR, AI suppression, GitHub inbound, messages repo. Needs routing (2) already in place. | biggest blast radius; do in sub-steps |
| 6 | **review** | Depends on notification's message model (shared kernel). Start-review inbound, sessions, reviewers, in-review markers. | Slack interactions inbound + code_reviews repo |
| 7 | **diagnostics** (doctor + config CLI + smoke) | Cross-cutting readers over routing + validation; safest last. | 3 binaries' entrypoints |
| 8 | **Platform extraction & cutover** | Move remaining shared clients under `platform/` (`store`→`persistence`, `config`, `slack`, `github`); rewrite each `cmd/*/main.go` as `fx.New(...)` with `fx.Lifecycle`; delete `internal/app`; tighten depguard; finalize `CLAUDE.md` + docs. | binary lifecycle parity; entrypoint error/exit parity |

### Phase 5 (notification) sub-steps

The core domain is migrated in slices, each independently green: (a) ports + shared kernel model; (b) messages repository (infra) over `platform/persistence`; (c) Slack messenger adapter over `platform/slack`; (d) GitHub inbound adapter over `platform/httpx`; (e) open/close/draft use cases; (f) reaction use cases + bot-PR + AI-suppression policy; (g) dispatcher + fx module; (h) delete old `pullrequest` remnants not owned by routing/review.

## Tracking

- [x] Phase 0 — scaffolding, fx, kernel + platform skeleton, CLAUDE.md rules
- [x] Phase 1 — maintenance
- [x] Phase 2 — routing
- [x] Phase 3 — validation
- [x] Phase 4 — digest
- [x] Phase 5 — notification (a–h)
- [x] Phase 6 — review
- [x] Phase 7 — diagnostics
- [x] Phase 8 — teardown & cutover

**Migration complete.** All eight phases are done. The architecture described in [`ARCHITECTURE.md`](ARCHITECTURE.md) is the current codebase.

## Open decisions to confirm before Phase 1

- **Shared kernel vs. per-domain models.** Starting with a minimal shared kernel (`PR`/`Message`/`Event`). The stricter DDD alternative is per-domain models with anti-corruption translation at boundaries — more isolation, more boilerplate. Revisit if the kernel starts accreting domain-specific fields.
- **Commit granularity (settled: single PR).** The whole refactor is one branch → one PR; phases and sub-steps are commit groups within it (notification's a–h land as separate commits). Cheap sub-agents may execute the mechanical moves; the orchestrator verifies `just check` green before committing each.
- **Import-boundary enforcement.** Whether to add a lint guard (e.g. depguard rules in `.golangci.yml`) in Phase 0, or rely on review. Recommendation: add depguard — it makes the dependency rule mechanical rather than a matter of vigilance.

## Out of scope: future domains

This refactor restructures today's single-tenant feature set — it does not add capabilities. Two domains are deliberately **not** created now because they have no behaviour yet; the layering simply leaves room for them:

- **identity / authentication** and **access / authorization** — meaningful only under the multi-tenant SaaS direction (tenants, GitHub-App OAuth installs, API keys, per-tenant isolation). Until then, the only authentication is inbound webhook signature verification, which lives in `platform/security` (see [`ARCHITECTURE.md`](ARCHITECTURE.md)), and there is no authorization concern at all.
