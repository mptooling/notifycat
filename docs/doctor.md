# Doctor

`notifycat-doctor` is the preflight diagnostics binary. Run it in your deployment environment to verify that Notifycat is wired correctly **before** your git host fires a real webhook. It complements `notifycat-config validate`: the config validator only checks per-entry Slack and git-host coverage, while the doctor adds runtime config, database, and mappings health on top.

## Usage

```sh
# Preflight: config + database + mappings file
notifycat-doctor

# Preflight + Slack + webhook checks for one repository
notifycat-doctor owner/repo
```

Exit code is `0` when every check passes (`SKIP` does not count as a failure) and `1` otherwise.

## What it checks

| Section | Check | Notes |
| --- | --- | --- |
| `config` | webhook secret | `GITHUB_WEBHOOK_SECRET` or `BITBUCKET_WEBHOOK_SECRET`, matching `git_provider`. Reports `set` / `missing` only — secret values are **never** printed. |
| `config` | `SLACK_BOT_TOKEN` | Same as above. |
| `config` | `cleanup.message_ttl_days` | Must be `> 0`; the server refuses to start otherwise. |
| `config` | `database.url` | Non-empty; actual reachability is the next section's job. |
| `config` | `server.domain` | Derives the public webhook URL (`https://$domain/webhook/<provider>`) the operator pastes into the git host. `OK` prints that URL; a scheme/path or malformed host is a `FAIL` with a hint; unset is a `SKIP` (expected for local dev / tunnels). |
| `database` | `open` | Opens the SQLite database and pings the underlying connection. Reports the DSN that was used. |
| `mappings` | `file` | Loads the YAML via the same parser the server uses. Surfaces schema errors and missing files. |
| `mappings` | `entries` | Number of parsed entries (`0` is allowed — the server boots and routes nothing). |
| `mappings` | `path routing` | Only when some tier uses [per-path routing](monorepo.md). `OK` when the read token (`GITHUB_TOKEN` / `BITBUCKET_TOKEN`) is set (path rules active); `SKIP` when it is unset (rules inert — PRs route to the repository tier). |
| `owner/repo` | `mapping` / `channel-format` / `slack-auth` / `slack-channel` / `webhook` | Only when a positional argument is given. Delegates to `internal/validation` — same checks `notifycat-config validate owner/repo` runs. A per-path channel adds its own `slack-channel <id>` membership check. |

## Output format

```
[config]
  OK    GITHUB_WEBHOOK_SECRET — set
  OK    SLACK_BOT_TOKEN — set
  OK    cleanup.message_ttl_days — 30 days
  OK    database.url — file:/data/notifycat.db
[database]
  OK    open — file:/data/notifycat.db
[mappings]
  OK    config — /etc/notifycat/config.yaml
  OK    entries — 4 entries
[octo/widget]
  OK    mapping
  OK    channel-format
  OK    slack-auth
  OK    slack-channel
  OK    webhook
```

`FAIL` lines include a remediation hint in the `— detail` suffix.

## Production-safe usage

The doctor is **safe to run in production**:

- No `os.Args` parsing beyond a single positional repository name.
- Reads config from `config.yaml` and secrets from environment variables (and `.env` for local dev). Secret values are never written to stdout, stderr, or logs.
- Opens the SQLite database read-write but performs **no migrations** and **no writes** — the open + ping happens, then
  the connection is closed.
- Per-repository Slack checks call `auth.test` and `conversations.info` only (read-only Slack API methods).
- The webhook-coverage check, when a read token is set, lists hooks via the git host's REST API. Read-only.

Recommended deployment hooks:

```sh
# Pre-deploy gate in CI
notifycat-doctor || exit 1

# Per-repository smoke test after an operator change to config.yaml
notifycat-doctor "$REPO" || exit 1
```

## When the doctor disagrees with `notifycat-config validate`

They share the same Slack + git-host checking code (the `internal/validation` domain). Differences in output indicate one of:

- Different environment (the doctor and the validator read the same `config.yaml`, but a stale lock file can mask issues only the doctor surfaces — the doctor does not consult the lock).
- The config file is unreadable. The doctor surfaces this in the `mappings` section and then skips the per-repository checks; `notifycat-config validate` refuses to start.

When in doubt, prefer the doctor for pre-deploy gating and `notifycat-config validate` for operator workflows around `config.yaml` edits.

## Related

- [CLI → notifycat-smoke](cli.md#notifycat-smoke) — once the doctor is green, `./notifycat smoke <org>/<repo>` posts a real signed event end-to-end and confirms a Slack message is delivered.
- [Configuration](configuration.md) — every environment variable the doctor inspects.
- [Mappings schema](mappings.md) — the section the `mappings` checks parse.
- [CLI → notifycat-config](cli.md#notifycat-config) — failure-mode remediation for the per-repository Slack + webhook checks.
