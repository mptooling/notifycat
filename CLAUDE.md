# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

All day-to-day commands run through `just` (`brew install just`). The
underlying `go ...` invocations work without `just` if needed.

| Task | Command |
| --- | --- |
| Full local verification (vet + lint + vuln + race tests + build) | `just check` |
| Race-enabled tests | `just test` |
| Faster tests, no race detector | `just test-fast` |
| Single test | `go test -race ./internal/pullrequest -run TestApproveHandler` |
| Single package | `go test -race ./internal/store` |
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

Notifycat is a single-process HTTP server with a SQLite sidecar. It
receives GitHub PR webhooks, looks up which Slack channel owns the
repo via a declarative YAML file, and either posts a new message or
updates/reacts on an existing one. Everything is wired in one
composition root — there is no DI framework.

### Composition root: `internal/app/app.go`

`app.Wire(cfg) (*http.Server, *cleanup.Scheduler, Cleanup, error)`
constructs the entire dependency graph. All four binaries under `cmd/`
reuse pieces of this — the doctor and mapping CLIs build subsets of
the graph for their own purposes. **There is no façade/test-seam
split**: a type has exactly one constructor, with every dep injected.

### Request flow

```
POST /webhook/github
  → githubhook.SignatureMiddleware       (HMAC-SHA256 of X-Hub-Signature-256)
  → githubhook.Handler                   (parses JSON into githubhook.Payload)
  → app.eventSink                        (maps Payload → pullrequest.Event)
  → pullrequest.Dispatcher.Dispatch      (routes by github_event + action)
  → one of:
      OpenHandler / CloseHandler / DraftHandler          (pull_request)
      ApproveHandler / CommentedHandler / RequestChangeHandler
                                                          (pull_request_review,
                                                           pull_request_review_comment)
  → store.SlackMessages   (lookup/upsert PR → Slack ts row)
    slack.Client          (chat.postMessage / chat.update / reactions.add)
```

Every silent no-op logs `ignored webhook event` with a `reason` field
(`no_handler`, `no_mapping`, `no_stored_message`, `already_sent`). The
operations doc has the full reason table — that contract matters for
debugging deliveries that return 200 but don't update Slack.

### Mappings & startup validation

Routing comes from a **declarative `mappings.yaml`**, not the DB.
`internal/mappings.Provider` loads it; `internal/validate` runs
per-entry checks against Slack and (optionally) GitHub. On boot,
`app.startupValidate` diffs entries against `mappings.lock` and only
revalidates changed ones — successful entries are merged back into the
lock. Empty mappings boot fine. Any failing entry aborts startup with
the failing details logged. The same code path powers
`notifycat-mapping validate` and `notifycat-doctor owner/repo`.

### Persistence

`internal/store` is GORM over SQLite. The only table is
`slack_messages` (PR ↔ Slack message timestamp). Migrations are
embedded goose SQL under `internal/store/migrations/` and applied at
server startup (or via `notifycat-migrate`).

`internal/cleanup.Scheduler` deletes stale `slack_messages` rows older
than `NOTIFYCAT_MESSAGE_TTL_DAYS` once at startup and every 24h
thereafter. It only deletes the DB row — never the Slack message.

### Bot-reviewer suppression

`internal/aireview.Detector`, gated by `NOTIFYCAT_IGNORE_AI_REVIEWS`,
sits in the three review handlers and skips `reactions.add` when
`sender.type == "Bot"`. The initial PR-open post is unaffected; the
detector intentionally does **not** distinguish AI reviewers from
scripted bots (Copilot vs dependabot vs github-actions all look the
same to GitHub's payload).

### Doctor

`internal/doctor` builds a list of `Section{Checks}` and writes a
preflight report. It owns the config/database/mappings-file checks
and **delegates per-repo Slack/GitHub probing to
`internal/validate`** — so the doctor and `notifycat-mapping validate`
agree by construction.

## Code conventions

- **Consumer-package interfaces.** Interfaces are declared where
  they're *used*, not where they're implemented. `pullrequest`
  consumes a small `SlackMessages` interface; `store.SlackMessages` is
  the concrete satisfier. Don't move interfaces into the producing
  package.
- **One constructor per type, all deps injected.** Never split a type
  into a "production wiring" façade plus a "test seam" constructor.
  If a test needs different deps, pass them through the same
  constructor.
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
- Do not commit `mappings.yaml`, `mappings.lock`, `.env`, or anything
  under `/data/` — all four are gitignored and represent operator
  state, not project state.
