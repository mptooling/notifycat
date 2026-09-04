# CLI binaries

The image (and a source build) ships six binaries. `notifycat-server` is the long-running process; the rest are operator tools you run ad hoc.

| Binary | Purpose |
| --- | --- |
| `notifycat-server` | The HTTP server — webhooks in, Slack out |
| `notifycat-config` | Parse and validate `config.yaml` against Slack and the git host |
| `notifycat-doctor` | Preflight diagnostics: config, database, mappings, per-repository probes |
| `notifycat-smoke` | Forge a signed PR event end-to-end to prove Slack delivery |
| `notifycat-migrate` | Apply or inspect the embedded SQLite migrations |
| `notifycat-reconcile` | One-time backfill: mark long-closed PRs closed in the database |
| `notifycat-relocate` | Move open PRs' Slack messages to another channel after repointing a repo |

Under Docker, pass the binary name as the command; the Compose install wraps the common ones (`./notifycat doctor`, `./notifycat smoke`):

```sh
docker compose run --rm notifycat notifycat-config validate
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-doctor
```

## notifycat-config

```sh
notifycat-config list                    # print the parsed config — no network
notifycat-config validate                # validate entries, cache-aware
notifycat-config validate owner/repo     # validate one entry, ignoring its cache
notifycat-config validate --force        # revalidate everything, ignore the lock
```

`list` prints a tab-aligned table of every parsed entry — the quick sanity check after an edit. A tier with a [`channels:` list](mappings.md#schema) or [path channels](monorepo.md) shows every channel it can post to, comma-separated. `validate` runs the real checks, one line per check in greppable `OK`/`WARN`/`FAIL`/`SKIP` form. Only `FAIL` sets exit code `1`; `WARN` reports an actionable problem in external state and still exits `0`:

| Check | Verifies | Remediation |
| --- | --- | --- |
| `mapping` | An entry covers `owner/repo` (explicit tier or `"*"`). | Add a tier — see [routing](routing.md). |
| `channel-format` | The channel ID matches `[CGD][A-Z0-9]{2,}`. | Use the Slack channel ID, not the display name. |
| `slack-auth` | `auth.test` passes and the token carries `chat:write` + `reactions:write`. | Rotate `SLACK_BOT_TOKEN` or reinstall the app with the right [scopes](slack-app.md#bot-scopes). |
| `slack-channel` | The channel exists, isn't archived, and the bot is a member. | `/invite @notifycat`; fix the ID; unarchive. |
| `webhook` | With a read token set: an active webhook targets `/webhook/<provider>` and subscribes to the PR events Notifycat needs. Skipped without a token. | `WARN`, never `FAIL`: create it — [GitHub](github-webhook.md) / [Bitbucket](bitbucket-webhook.md) — or add the missing events. A `403` on the hooks listing means the token's identity lacks write/admin on the repository. |
| `org-repos` | For an org with a `"*"` tier and a read token set: lists the org's repositories and validates each one. Skipped without a token. | `WARN`, never `FAIL`: usually a token-scope problem — grant the token read access to the org's repositories. |

Successful results are hashed into the sibling `config.lock`, so steady-state boots and re-runs only revalidate what changed. Entries that warned are left out of the lock — including on `validate owner/repo` — so they are re-probed until they pass. Commit the lock next to `config.yaml` in the repository that owns your deployment; the [operator workflow](routing.md#validate-and-deploy-a-change) is edit → validate → commit both → deploy. The server runs this same validation at boot and refuses to start on a `FAIL`; a `WARN` is logged and tolerated ([operations](operations.md#startup-and-shutdown)).

## notifycat-doctor

Preflight for everything `validate` doesn't cover: secrets present, TTL sane, database reachable, mappings file parseable — plus the per-repository Slack/webhook probes when you name a repository. Safe to run in production; read-only apart from opening the database. It has [its own page](doctor.md).

```sh
notifycat-doctor              # config + database + mappings
notifycat-doctor owner/repo   # + Slack and webhook probes for one repository
```

## notifycat-smoke

The last step of an install and the first step of a delivery investigation: it forges a correctly-signed `opened` event for a mapped repository, POSTs it to the **running** server, and reports the channel and message timestamp Slack returned. Real signature check, real dispatcher, real Slack call.

```sh
./notifycat smoke acme/api                 # posts a throwaway "[notifycat smoke] …" message
./notifycat smoke --reactions acme/api     # + replays comment, approval, merge; verifies the emoji landed
```

A secret mismatch surfaces as a clear `401`; an unmapped repository is rejected before anything is sent. `--reactions` verification needs the optional `reactions:read` scope — without it the reactions still post, and the check reports "could not verify" instead of failing. Delete the throwaway message when you're done.

## notifycat-migrate

The server applies migrations itself at startup, so most deployments never run this. Use it when you want migrations as a separate, controlled step:

```sh
notifycat-migrate up        # apply pending migrations
notifycat-migrate status    # show migration state
```

## notifycat-reconcile

One-time repair after enabling the [digest](digest.md) on an older deployment: walks every row the database believes is open, asks the git host, and marks the merged/closed ones. Idempotent; needs a read token; a PR it can't read is left alone. Usage and the summary format: [digest → first run](digest.md#reconcile).

## notifycat-relocate

Changing a repository's `channel:` only redirects *new* PRs. The messages of PRs that were already open stay where they were posted — the database records the channel each one lives in — so reviews and merges keep updating the old channel until those PRs close. This tool carries them over.

```sh
notifycat-relocate -audit                          # what sits in channels config no longer mentions
notifycat-relocate -from C0OLD -to C0NEW -dry-run  # preview
notifycat-relocate -from C0OLD -to C0NEW           # move them
notifycat-relocate -from C0OLD -to C0NEW -repo acme/api
notifycat-relocate -from C0OLD                     # delete them, no replacement
```

Start with `-audit`: it lists every stored message whose channel is absent from the repository's current configuration, which is exactly the set that needs moving. It cannot tell you *where* each should go — a `paths:`-routed PR legitimately has a message in only one of several possible channels — so the destination stays yours to name.

Per PR, one of three things happens:

- **Moved** — the message is reposted in the destination, the stored row is retargeted, the repo's own reactions are carried over, and the original is deleted.
- **Merged** — the PR already has a message in the destination (a `channels:` overlap), so it keeps that one and only the original goes. No duplicate is ever posted.
- **Dropped** — no `-to` given, or the original is already gone from Slack: the message and its row are removed.

The reposted message announces itself: `:truck: [moved from another channel] please review …`. It **pings nobody** — you changed a routing target, which isn't news worth a notification, and the original mentions belonged to the old channel. Everything else survives: the context line, accumulated "reviewing" / "reviewed by" markers, and a working **Start review** button. Reactions come across too, filtered to the emoji the repo's own notifications use, since the bot can't re-add a human's reaction as that human.

Thread replies do not survive — deleting a message deletes its thread.

Runs are idempotent: the row is retargeted immediately after the repost, so a re-run skips whatever already moved, and a failure mid-run can only leave an orphaned message in the source channel (which `-audit` will show you), never a row pointing at a message that was never posted.

`SLACK_BOT_TOKEN` needs two scopes beyond the server's own — `reactions:read` and the history scopes `channels:history` / `groups:history` — because reading a posted message is the one thing the server never does. The run refuses to start without them, and refuses a destination the bot isn't a member of, rather than failing partway. Only this tool needs them; the server's required scopes are unchanged.
