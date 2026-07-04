# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture rules (authoritative — always follow)

Notifycat follows **Domain-Driven Design + hexagonal layering + uber/fx**. [`ARCHITECTURE.md`](ARCHITECTURE.md) is the source of truth. These rules govern all code in the repository:

- Every domain is a folder under `internal/<domain>/` split into three layers: `domain/`, `application/`, `infrastructure/`. Dependencies point inward only (`infrastructure → application → domain`); the domain layer imports nothing but the shared kernel.
- The domain layer owns the contracts: `interfaces.go` (ports + use-case interfaces), `models.go` (DTOs), `enums.go`, `constants.go`.
- The application layer holds use cases; **every use case has an interface** in `domain/interfaces.go` and depends only on domain interfaces.
- The infrastructure layer holds adapters/clients/repositories; **every infrastructure service implements — and has — a domain-layer interface**. It is the only layer that touches SDKs, the DB, HTTP, or the network.
- Depend on abstractions, never concrete implementations; bind concretes to ports only in the fx wiring.
- No hardcoded values in services — anything enum-like or constant-like lives in `enums.go` / `constants.go`.
- More than three arguments to an exported function or constructor → a single DTO from the domain layer.
- Doc comments live on the interfaces; implementations stay terse.
- Dependency injection is **uber/fx**: one `fx.Module` per domain, runtime lifecycle via `fx.Lifecycle`.

## Commands

All day-to-day commands run through `just` (`brew install just`). The
underlying `go ...` invocations work without `just` if needed.

| Task | Command |
| --- | --- |
| Full local verification (vet + lint + vuln + race tests + build) | `just check` |
| Race-enabled tests | `just test` |
| Faster tests, no race detector | `just test-fast` |
| Single test | `go test -race ./internal/notification/... -run TestApproveHandler` |
| Single package | `go test -race ./internal/notification/...` |
| Lint only | `just lint` (requires `golangci-lint` locally) |
| Vuln scan | `just vuln` (runs in Docker against the pinned `golang:1.25.10-alpine` — slower; CI also runs this) |
| Build all binaries | `just build` |
| Run server locally | `just serve` |
| Apply migrations | `just migrate` (or `just docker-migrate` against `./data`) |
| Build & run Docker image | `just docker-build` then `just docker-serve` |

Go toolchain is pinned at **1.25.10**. CI runs all of the
`just check` steps (`go vet`, `golangci-lint`, `govulncheck`,
`go test -race`, `go build`). `just` is dev-only — it is not in the
runtime image or Go modules.

## Architecture

Notifycat is a single-process HTTP server with a SQLite sidecar. It receives GitHub PR webhooks, looks up which Slack channel owns the repo via a declarative YAML file, and either posts a new message or updates/reacts on an existing one. The full architecture is documented in [`ARCHITECTURE.md`](ARCHITECTURE.md); this section is a navigational summary.

### Domain structure

Seven domains live under `internal/<domain>/`, each with three layers (`domain/`, `application/`, `infrastructure/`) and a `module.go` exporting an `fx.Module`:

| Domain | Responsibility |
| --- | --- |
| `notification` | Core: receive a GitHub PR event, keep one Slack message per (PR, channel) in sync |
| `review` | Interactive "Start review" Slack flow; records reviewers; decorates the message |
| `routing` | Resolve a repo (and changed files) to the Slack channel(s) and behavioral config |
| `validation` | Validate mapping entries against Slack + GitHub; cache results in `config.lock` |
| `digest` | Periodic stuck-PR digest per cron schedule |
| `maintenance` | Background housekeeping: delete stale message rows; reconcile closed PRs |
| `diagnostics` | Operator tooling: `notifycat-doctor`, `notifycat-config`, smoke test |

The shared kernel (`internal/kernel`) holds pure value objects (`PR`, `Event`, `Sender`, `Review`) and GitHub event/action/review-state enums — stdlib only. The shared platform (`internal/platform/`) holds domain-agnostic clients: `config`, `persistence` (GORM/SQLite), `slack`, `github`, `httpx`, and `security` (HMAC `SignatureVerifier` with `GitHubVerifier`/`SlackVerifier` adapters).

### Composition root

`internal/runtime` is an `fx.Module` that builds the full dependency graph, runs the startup-validation gate as an `fx.Invoke`, and drives the HTTP server plus cleanup/digest schedulers via `fx.Lifecycle` hooks. `cmd/notifycat-server/main.go` is `fx.New(fx.Supply(cfg), runtime.Module, fx.NopLogger)` plus manual `Start`/`Wait`/`Stop` (fatal server error → exit 1 via `fx.Shutdowner`; SIGTERM → graceful shutdown → exit 0). The five CLI binaries (`notifycat-{migrate,reconcile,config,doctor,smoke}`) construct their domain use cases directly in `main`.

### Request flow

```
POST /webhook/github
  → platform/httpx body middleware + platform/security.GitHubVerifier (HMAC)
  → notification/infrastructure inbound receiver (parses payload → kernel.Event)
  → notification/application dispatcher (first applicable Handler)
  → one of:
      OpenHandler / CloseHandler / DraftHandler          (pull_request)
      ApproveHandler / CommentedHandler / RequestChangeHandler
                                                          (pull_request_review,
                                                           pull_request_review_comment)
  → Messenger port (Slack adapter — postMessage / update / reactions.add)
    MessageStore port (persistence adapter — lookup/upsert PR row)

POST /webhook/slack/interactions
  → platform/security.SlackVerifier
  → review/infrastructure interactions receiver
  → review/application start-review use case
```

Every silent no-op logs `ignored webhook event` with a `reason` field (`no_handler`, `no_mapping`, `no_stored_message`, `already_sent`). The operations doc has the full reason table — that contract matters for debugging deliveries that return 200 but don't update Slack.

### Mappings & startup validation

Routing comes from the **`mappings:` section of the declarative `config.yaml`**, not the DB. The `routing` domain builds a `Provider` from it; the `validation` domain runs per-entry checks against Slack and (optionally) GitHub. On boot, `internal/runtime` diffs entries against `config.lock` and only revalidates changed ones — successful entries are merged back into the lock. Empty mappings boot fine. Any failing entry aborts startup with the failing details logged. The same code path powers `notifycat-config validate` and `notifycat-doctor owner/repo`.

### Persistence

`internal/platform/persistence` is GORM over SQLite. Tables: `pull_requests` (one row per PR) and `slack_messages` (PR ↔ Slack message timestamp), plus `code_reviews` (review sessions). Migrations are embedded goose SQL applied at server startup (or via `notifycat-migrate`).

The `maintenance` domain deletes stale `slack_messages` rows older than `cleanup.message_ttl_days` (config.yaml) once at startup and every 24h. It only deletes the DB row — never the Slack message.

### Bot-reviewer suppression

The `notification` domain's `IsBot` policy, gated by `reviews.ignore_ai_reviews` (config.yaml), sits in the reaction handlers and skips `reactions.add` when `sender.type == "Bot"`. The initial PR-open post is unaffected; the policy intentionally does **not** distinguish AI reviewers from scripted bots (Copilot vs dependabot vs github-actions all look the same to GitHub's payload).

### Doctor

`internal/diagnostics` builds a list of `Section{Checks}` and writes a preflight report. It owns the config/database/mappings checks and **delegates per-repo Slack/GitHub probing to `internal/validation`** — so the doctor and `notifycat-config validate` agree by construction.

## Code conventions

- **Domain-layer ports.** Interfaces live in the domain layer that *owns* the contract — not in the consumer package. Each use case and each infrastructure service has an interface in its domain's `interfaces.go`.
- **One constructor per type, all deps injected.** Never split a type
  into a "production wiring" façade plus a "test seam" constructor.
  If a test needs different deps, pass them through the same
  constructor.
- **Readable names over terse Go idiom.** Prefer descriptive,
  meaningful identifiers (`repoMapping`, `digestConfig`, `starTier`)
  over the conventional one- or two-letter Go style (`m`, `d`, `rc`),
  including for locals and parameters. The goal is code that reads
  without a mental decode step. Genuinely small scopes are the
  exception — a loop index (`i`), a method receiver, or a trivial
  one-line throwaway can stay short — but any value that carries domain
  meaning should be spelled out. Match this when editing existing code:
  don't reintroduce short names a file has already spelled out.
- **No comments restating the code.** Only add a comment when the
  *why* is non-obvious (hidden constraint, subtle invariant,
  workaround). Method-level docs that summarize the method name are
  noise.
- **TDD for new behavior.** RED → verify failure → GREEN → REFACTOR.
  Bug fixes start with a regression test that reproduces the bug.

## Commits, PRs, releases

- **PR title = commit message.** Squash-merge keeps the PR title as
  the commit on `main`, and the `pr-title` workflow lints it against
  Conventional Commits. `release-please` reads those titles to bump
  the version and update `CHANGELOG.md`.
- **Pre-1.0:** breaking changes bump the minor version (`0.x` → `0.(x+1)`).
- Watch out for the literal string `BREAKING CHANGE` *anywhere* in a
  commit body — release-please parses it as a breaking-change footer
  even when it's prose. Use a different phrasing in normal commits.
- Do not commit `config.yaml`, `config.lock`, `.env`, or anything
  under `/data/` — all four are gitignored and represent operator
  state, not project state.
