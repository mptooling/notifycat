# Provider-aware PR web URLs in the digest

## Problem

The stuck-PR digest reconstructs each PR's web URL from `repository` + `number`, hardcoded to `https://github.com/<repo>/pull/<n>` (`internal/digest/application/reporter.go` `prURL`, backed by constants in `internal/digest/domain/constants.go`). A deployment configured for Bitbucket therefore posts broken `github.com` links for its Bitbucket PRs.

The same latent bug exists in the maintenance reconciler (`internal/maintenance/application/reconciler.go` `prURL`), where the reconstructed URL only feeds per-PR log lines but is still wrong for Bitbucket.

## Context

Notifycat already models the git host as a **global** config enum: `git_provider` (`github` | `bitbucket`), surfaced as `kernel.Provider` on `config.Config.GitProvider`. A deployment serves exactly one provider, so the correct URL host is fully determined by config — no per-PR provider is stored and no DB/schema change is needed.

Bitbucket web URLs are `https://bitbucket.org/<workspace>/<repo>/pull-requests/<n>` (the `repository` slug is `workspace/repo`, matching the existing smoke webhook builder).

## Design

### 1. Single source of truth — a method on `kernel.Provider`

Add to `internal/kernel`:

```go
func (p Provider) PullRequestWebURL(repository string, number int) string
```

- `ProviderBitbucket` → `https://bitbucket.org/<repository>/pull-requests/<number>`
- `ProviderGitHub` and any unknown/zero-value provider → `https://github.com/<repository>/pull/<number>` (GitHub fallback preserves today's behavior)

Host prefixes and path segments are named unexported consts in the kernel package rather than inline literals (repo constants rule). Pure and stdlib-only (`strconv`), consistent with the kernel's charter (pure value objects + the provider enum).

Out of scope, same as today: GitHub Enterprise / self-hosted Bitbucket web hosts.

### 2. Digest threads the provider through

- `digest/domain` `ReporterParams` gains `Provider kernel.Provider`; the `Reporter` stores it.
- Delete `prURL` and the `GitHubPRURLPrefix` / `PullPathSegment` constants in `digest/domain/constants.go` (keep `BlockTypeSection`). The call site in `groupByChannel` becomes `r.provider.PullRequestWebURL(pr.Repository, pr.PRNumber)`.
- `digest.Config` gains `Provider`; `provideReporterParams` passes it through.
- `runtime.buildDigestScheduler` sets `Provider: cfg.GitProvider` on the `ReporterParams` it builds manually.

### 3. Maintenance reconciler reuses the same method

- `maintenance/domain` `ReconcilerParams` gains `Provider kernel.Provider`; the `Reconciler` stores it.
- Delete the reconciler's local `prURL`; its call sites use `r.provider.PullRequestWebURL(...)`.
- `maintenance.Config` gains `Provider`; `provideReconcilerParams` passes it through.
- `cmd/notifycat-reconcile/main.go` sets `Provider: cfg.GitProvider` on the `ReconcilerParams` it builds.

## Testing (TDD)

- **kernel**: table test for `PullRequestWebURL` — GitHub, Bitbucket, and zero-value (defaults to GitHub).
- **digest**: existing reporter tests default to the zero-value provider (GitHub) and keep passing; add a Bitbucket case asserting the composed digest carries a `bitbucket.org/.../pull-requests/` URL.
- **maintenance**: add a Bitbucket case asserting the reconciler's log line carries a `bitbucket.org/.../pull-requests/` URL.

## Non-goals

- No DB or schema change; provider stays a global config value.
- No per-org / per-mapping provider selection.
- GitHub Enterprise / self-hosted Bitbucket web hosts.
