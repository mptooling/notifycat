# Doctor

`notifycat-doctor` is the preflight diagnostics binary. Run it in your deployment environment to verify that Notifycat
is wired correctly **before** GitHub fires a real webhook. It complements `notifycat-mapping validate`: the mapping
validator only checks per-entry Slack/GitHub coverage, while the doctor adds runtime config, database, and mappings-file
health on top.

## Usage

```sh
# Preflight: config + database + mappings file
notifycat-doctor

# Preflight + Slack + GitHub webhook checks for one repository
notifycat-doctor owner/repo
```

Exit code is `0` when every check passes (`SKIP` does not count as a failure) and `1` otherwise.

## What it checks

| Section | Check | Notes |
| --- | --- | --- |
| `config` | `GITHUB_WEBHOOK_SECRET` | Reports `set` / `missing` only. Secret values are **never** printed. |
| `config` | `SLACK_BOT_TOKEN` | Same as above. |
| `config` | `NOTIFYCAT_MESSAGE_TTL_DAYS` | Must be `> 0`; the server refuses to start otherwise. |
| `config` | `DATABASE_URL` | Non-empty; actual reachability is the next section's job. |
| `config` | `NOTIFYCAT_MAPPINGS_FILE` | Non-empty; actual file parsing is its own section. |
| `config` | `DOMAIN` | Derives the public webhook URL (`https://$DOMAIN/webhook/github`) the operator pastes into GitHub. `OK` prints that URL; a scheme/path or malformed host is a `FAIL` with a hint; unset is a `SKIP` (expected for local dev / tunnels). |
| `database` | `open` | Opens the SQLite database and pings the underlying connection. Reports the DSN that was used. |
| `mappings` | `file` | Loads the YAML via the same parser the server uses. Surfaces schema errors and missing files. |
| `mappings` | `entries` | Number of parsed entries (`0` is allowed — the server boots and routes nothing). |
| `owner/repo` | `mapping` / `channel-format` / `slack-auth` / `slack-channel` / `github-webhook` | Only when a positional argument is given. Delegates to `internal/validate` — same checks `notifycat-mapping validate owner/repo` runs. |

## Output format

```
[config]
  OK    GITHUB_WEBHOOK_SECRET — set
  OK    SLACK_BOT_TOKEN — set
  OK    NOTIFYCAT_MESSAGE_TTL_DAYS — 30 days
  OK    DATABASE_URL — file:/data/notifycat.db
  OK    NOTIFYCAT_MAPPINGS_FILE — /etc/notifycat/mappings.yaml
[database]
  OK    open — file:/data/notifycat.db
[mappings]
  OK    file — /etc/notifycat/mappings.yaml
  OK    entries — 4 entries
[octo/widget]
  OK    mapping
  OK    channel-format
  OK    slack-auth
  OK    slack-channel
  OK    github-webhook
```

`FAIL` lines include a remediation hint in the `— detail` suffix.

## Production-safe usage

The doctor is **safe to run in production**:

- No `os.Args` parsing beyond a single positional repository name.
- Reads config from environment variables (and `.env` for local dev). Secret values are never written to stdout, stderr,
  or logs.
- Opens the SQLite database read-write but performs **no migrations** and **no writes** — the open + ping happens, then
  the connection is closed.
- Per-repo Slack checks call `auth.test` and `conversations.info` only (read-only Slack API methods).
- The GitHub webhook check, when `GITHUB_TOKEN` is set, lists hooks via the REST API. Read-only.

Recommended deployment hooks:

```sh
# Pre-deploy gate in CI
notifycat-doctor || exit 1

# Per-repo smoke test after an operator change to mappings.yaml
notifycat-doctor "$REPO" || exit 1
```

## When the doctor disagrees with `notifycat-mapping validate`

They share the same Slack + GitHub checking code (the `internal/validate` package). Differences in output indicate one
of:

- Different environment (the doctor and the validator read the same `NOTIFYCAT_MAPPINGS_FILE`, but a stale lock file can
  mask issues only the doctor surfaces — the doctor does not consult the lock).
- The mappings file is unreadable. The doctor surfaces this in the `mappings` section and then skips the per-repo
  checks; `mapping validate` refuses to start.

When in doubt, prefer the doctor for pre-deploy gating and `mapping validate` for operator workflows around
`mappings.yaml` edits.

## Related

- [Configuration](configuration.md) — every environment variable the doctor inspects.
- [Mappings](mappings.md) — the file the `mappings` section parses.
- [Operations](operations.md) — failure-mode remediation for the per-repo Slack + GitHub checks.
