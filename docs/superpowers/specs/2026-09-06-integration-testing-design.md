# Integration testing for notifycat — design

Date: 2026-09-06
Status: approved, ready for implementation planning

## Problem

Notifycat is verified today by unit tests plus manual poking at
`mptooling/notificat-manual-testing` and a Slack channel. Nothing exercises
the real chain — a real GitHub repository, a real webhook delivery, the real
container image, and a real Slack workspace. The regressions that matter most
are exactly the ones unit tests cannot see: a webhook that no longer verifies,
a Slack payload Slack rejects, a routing change that fans a PR out to the wrong
channel, a validation gate that stops aborting startup.

This design introduces an automated suite in the existing (currently empty)
`mptooling/notifycat-integration-tests` repository, run from CI against the
container image built from `main`.

## Goals

- Exercise the published container image, not the source tree.
- Use real GitHub repositories, real webhook deliveries and a real Slack
  workspace.
- Run unattended on every merge to `main` and nightly, and report failures
  without blocking releases.
- Stay under a 15-minute wall-clock budget for a typical full run.

  As built, the *ceilings* are far higher — summing every timeout constant
  gives roughly 48 minutes, so the test binary allows 55 and the CI job 60.
  Those are failure ceilings, not targets: blowing the test binary's own
  timeout is precisely what skips teardown and leaks a container, a tunnel,
  open pull requests and Slack messages. Nothing currently enforces the
  15-minute expectation, so a suite that silently degrades from five minutes
  to thirty would still report green.
- Fail honestly: a scenario whose credentials are missing skips with a named
  reason; a scenario whose infrastructure broke fails loudly.

## Non-goals

The following are deliberately out of the first delivery. They are recorded
here so a later phase does not have to rediscover the reasoning.

| Deferred | Why |
| --- | --- |
| Bitbucket provider | Doubles fixture setup (workspace, repos, access token, second webhook secret, second config profile) for a provider whose adapter is already unit-tested. Permanently out per product decision. |
| Digest and maintenance (`reconcile`, `relocate`, TTL cleanup) | Needs clock or database manipulation inside the container; high complexity relative to the first delivery. |
| `ignored webhook event` reason-code assertions | Cheap, but requires a log-scraping contract this delivery does not yet establish. |
| Slack "Start review" interactivity, bot suppression, dependabot format | Depends on a bot identity fixture that does not exist yet. |

Deferring these means a regression in digest scheduling, message cleanup, or
the review button is still caught only by unit tests. That is an accepted
trade for a first delivery that actually ships.

## Architecture

### Components

```
GitHub fixture repo ──webhook──▶ cloudflared quick tunnel ──▶ notifycat container (:8080)
        ▲                                                              │
        │ githubx (PR actions, webhook CRUD)                           │ chat.postMessage
        │                                                              ▼
   Go test harness ◀────────── slackx (history, reactions) ──── Slack test workspace
```

Four units, each independently understandable and testable:

- **`internal/harness`** — owns the run environment: renders `config.yaml` from
  a template, starts and stops the notifycat container, starts the tunnel and
  proves it reachable, collects container logs. Knows nothing about scenarios.
- **`internal/githubx`** — the only code that talks to the GitHub API: branch
  push, pull-request create/merge/close, review submit, webhook create/delete/
  list. Returns plain structs; no assertions.
- **`internal/slackx`** — the only code that talks to the Slack API:
  `conversations.history`, `conversations.replies`, `reactions.get`,
  `chat.delete`. Returns plain structs; no assertions.
- **`tests/`** — scenarios. Read as prose against the three units above.

Nothing in `tests/` constructs an HTTP request directly. That boundary is what
keeps a scenario readable and lets the Slack polling policy live in one place.

### Language and conventions

Go with testify, matching notifycat's toolchain and test conventions:
`require` for anything whose failure makes the rest meaningless, `assert` for
independent end-of-test checks, `(t, want, got)` argument order, spelled-out
identifiers, table tests driven through `t.Run(testCase.name, …)`.

Every Slack assertion goes through `require.Eventually` — Slack delivery is
asynchronous and a fixed sleep is both slower and flakier than a poll.

## Fixtures

### GitHub repositories

Three new public repositories under `mptooling`, created and seeded once by the
implementation and thereafter treated as immutable infrastructure.
`mptooling/notificat-manual-testing` is left alone for manual work.

| Repository | Seeded contents | Exercises |
| --- | --- | --- |
| `notifycat-it-alpha` | `README.md`, `src/app.txt` | PR lifecycle, reactions, mentions, message update on merge, multi-channel fan-out via `channels:` |
| `notifycat-it-mono` | `modules/acme/`, `modules/betta/`, `config/`, `src/AuthBundle/`, `vendor/` — one tracked file each | Per-path routing, per-path fan-out, fallback to the base channel when no path matches |
| `notifycat-it-nohook` | `README.md` | The `webhook` coverage `WARN` path, and the invariant that a warned entry is never written to `config.lock` |

`notifycat-it-nohook` never receives a permanent webhook. That is the point of
it: the validation checks probe for a hook, find none, and must report `WARN`
rather than `FAIL`, and must leave the lock untouched.

`notifycat-it-alpha` and `notifycat-it-mono`, by contrast, each need a
**permanent** webhook — created by `scripts/create-fixtures.sh`, carrying all
four of notifycat's `RequiredGitHubEvents`. Without one, they warn exactly like
`notifycat-it-nohook` and never enter `config.lock`, which makes the
"warned entries are never cached" scenario unprovable: nothing would ever be
cached to contrast against. The per-scenario tunnel webhooks cannot serve this
purpose, because they are deleted at the end of each scenario.

Those permanent hooks point at an unreachable placeholder host, so their
deliveries always fail and GitHub will eventually auto-disable them. The
bootstrap script therefore re-activates a disabled hook rather than skipping it,
and is safe to re-run at any time.

### Slack

One test workspace, one Slack app, five pre-created channels with the bot
invited:

| Channel | Role |
| --- | --- |
| `primary` | Default target for `notifycat-it-alpha` |
| `secondary` | Second target for the `channels:` fan-out case |
| `team-a` | Target of the `modules/acme` path rule |
| `team-b` | Target of the `modules/betta` path rule |
| `alerts` | Where CI posts suite failures; never a notification target |

Required bot scopes: `chat:write`, `channels:read`, `channels:history`,
`reactions:read`, `reactions:write`.

Channel IDs are supplied to CI as secrets and interpolated into the rendered
`config.yaml`, so no channel ID is ever committed.

### Identities

Approving a pull request requires an actor other than its author. GitHub
permits a `COMMENT` review on one's own pull request but not `APPROVE` or
`REQUEST_CHANGES`.

The design therefore uses two identities:

- **Author** — a fine-grained PAT for the maintainer account. Scopes on the
  three fixture repositories only: contents write, pull requests write,
  webhooks admin.
- **Reviewer** — a GitHub App installed on the fixture repositories. Its
  installation token is a distinct actor, so it can open pull requests that the
  author PAT then approves. A GitHub App is preferred over a second human
  account: no new email or 2FA enrolment, and its `Bot` sender type is the
  fixture a later bot-suppression phase will need anyway.

Until the App exists, scenarios requiring a second actor skip with the reason
`reviewer identity not configured`. The comment-reaction path is fully covered
from day one with the author PAT alone.

## Run model

### Sequence

1. Resolve the image tag under test (dispatch payload, workflow input, or
   `edge` by default).
2. Render `config.yaml` from `config/config.yaml.tmpl`, substituting channel
   IDs and the fixture repository names.
3. `docker run` the image, publishing `:8080`, with a fresh volume so the
   SQLite database and `config.lock` start empty.
4. Start `cloudflared tunnel --url http://localhost:8080`, parse the
   `*.trycloudflare.com` URL from its output, and poll `GET /healthz` through
   that URL until it returns 200. Three start attempts; on exhaustion the job
   fails with an explicit message rather than skipping silently.
5. Run `go test ./tests/...` serially, with the tunnel URL, tokens, channel IDs
   and run marker in the environment.
6. Teardown, unconditionally: delete the suite's own Slack messages, close open
   fixture pull requests, delete their branches, delete every webhook the run
   created, upload container logs as a job artifact, stop the tunnel and the
   container.

### Per-scenario shape

```
create webhook on fixture repo → tunnel URL
  act on the repository (push branch, open PR, review, merge, close)
  poll Slack until the assertion holds or the deadline passes
  assert
  cleanup: chat.delete, close PR, delete branch, delete webhook
```

Each scenario creates its own webhook rather than relying on a permanent one.
GitHub allows multiple hooks per repository, so a hook orphaned by a crashed
run cannot block the next run. A teardown sweep additionally deletes hooks
whose target URL is a tunnel that no longer resolves.

### Isolation

Three independent mechanisms, because CI reruns and manual dispatches overlap
in practice:

- **Run marker.** Every pull-request title carries `[it-<run_id>]`, which
  notifycat copies into the Slack message. Every Slack assertion filters
  `conversations.history` by that marker, so a message left by an earlier run
  can never satisfy an assertion.
- **Fresh state.** A new container and a new volume per run means an empty
  database and an empty `config.lock`.
- **Serialization.** A GitHub Actions `concurrency` group with
  `cancel-in-progress: false` queues overlapping runs.

### Validation scenarios need no tunnel

The validation, doctor and config-CLI coverage runs the same image with a
different command and asserts on exit codes, stdout and the resulting
`config.lock`. No webhook, no Slack delivery, no tunnel:

| Scenario | Assertion |
| --- | --- |
| `notifycat-config validate` over all fixtures | Exit 0; per-repository status lines; `OK` for mapped repos with a live channel |
| `notifycat-config validate` against `notifycat-it-nohook` | `WARN` on the `webhook` check; process still exits 0 |
| `config.lock` after that run | Contains the `OK` entries, does **not** contain the warned entry — the `EntryResult.Cacheable()` contract |
| `notifycat-config validate` re-run | Warned entry is re-probed and re-warned; cached entries are not revalidated |
| `notifycat-doctor owner/repo` | `[section]` blocks render with per-check status lines; exit 0 while no check is `FAIL`, exit 1 once one is |
| Server startup with a mapping pointing at a nonexistent channel | Container exits non-zero and the failing check detail appears in its logs |

These are the cheapest high-value tests in the suite and they run first, so an
obviously broken image fails fast before any repository is touched.

## Scenario inventory

### Pull-request lifecycle — `notifycat-it-alpha`

1. **Open.** Push a branch, open a PR. Assert one message in `primary`
   containing the marker, the PR title, the PR URL and the configured mentions;
   assert the `new_pr` reaction is present.
2. **Comment review.** Submit a `COMMENT` review. Assert the `commented`
   reaction lands on the same message timestamp.
3. **Approve.** NOT IMPLEMENTED — the suite ships an unconditional skip
   recording the gap, because the GitHub App reviewer identity does not exist
   yet and GitHub forbids approving your own pull request. `request_change` is
   likewise uncovered. When the App is installed, assert the
   `approved` reaction.
4. **Merge.** Assert the `merged_pr` reaction and that the message text
   changed — the same timestamp, updated in place, not a new post.
5. **Close without merge.** A second PR, closed unmerged. Assert the
   `closed_pr` reaction.

### Routing

6. **Multi-channel fan-out.** A repository tier declaring `channels:` with
   `primary` and `secondary`. Assert one message per channel for the same PR,
   each with that entry's own mentions, and assert a later merge updates both.
7. **Per-path routing** — `notifycat-it-mono`. A PR touching only
   `modules/acme/` produces a message in `team-a` and none in `team-b` or the
   base channel.
8. **Per-path fan-out.** A PR touching both `modules/acme/` and
   `modules/betta/` produces a message in each of `team-a` and `team-b`.
9. **Path fallback.** A PR touching only `README.md` matches no path rule and
   produces a single message on the base channel.

Path routing reads a pull request's changed files from the GitHub API, so the
server container is given `GITHUB_TOKEN`. Without it these rules are inert and
scenarios 7–9 would silently degrade into scenario 9 — the suite therefore
asserts the token is present before running them rather than reporting a
false pass.

## Triggers and reporting

### notifycat repository (separate small pull request)

A new `.github/workflows/docker-edge.yml`:

- On push to `main`: build and publish `ghcr.io/mptooling/notifycat:edge`,
  then `repository_dispatch` to `notifycat-integration-tests` with the tag in
  the client payload.
- On release published: dispatch with the release tag, so the released artifact
  is verified too.

Cross-repository dispatch cannot use `GITHUB_TOKEN`. It needs one new secret,
`INTEGRATION_DISPATCH_TOKEN` — a fine-grained PAT scoped to Actions: write on
`notifycat-integration-tests` and nothing else.

The existing `pr-<n>` beta tag is unsuitable as the suite's input: it is deleted
when the pull request closes, which is the same moment the merge to `main`
happens. Hence `edge`.

### notifycat-integration-tests repository

Runs on `repository_dispatch`, on its own pull requests and pushes to `main`,
on a nightly cron, and on `workflow_dispatch` with a tag input for manual runs
against a specific image.

On failure: open or update a single deduplicated GitHub issue (matched by a
marker in the body, the same upsert pattern `docker-pr.yml` already uses for
its beta-image comment) and post to the `alerts` Slack channel. The suite never
fails a required check, so a flaking tunnel cannot block a release.

## Error handling and flake policy

| Failure | Handling |
| --- | --- |
| Tunnel fails to start | Retry up to three times, then fail the job with the cloudflared output. Never skip — a silent skip would make the whole suite vacuous. |
| Slack assertion times out | Fail. The poll window (60s per assertion) is far above observed delivery latency, so a timeout is a real signal. |
| Missing secret | Skip the scenarios that need it, with the secret name in the skip message. The run stays green and honest. |
| Webhook left behind by a crashed run | Teardown sweep deletes hooks pointing at unresolvable tunnel hosts. |
| Slack rate limit (HTTP 429) | Respect `Retry-After` in `slackx` and retry once. `conversations.history` is Tier 3; the suite's call volume is far below the limit, so this is defence in depth. |

Container logs are uploaded as an artifact on every run, pass or fail — a
passing run's logs are what makes the next failure diagnosable.

## Repository layout

```
notifycat-integration-tests/
  go.mod
  justfile                    # test, test-local, lint
  README.md                   # what it does, how to run locally
  CLAUDE.md                   # conventions for this repo
  config/
    config.yaml.tmpl          # rendered per run; channel IDs from env
  internal/
    harness/
      stack.go                # container lifecycle
      tunnel.go               # cloudflared lifecycle + readiness probe
      config.go               # template render
      logs.go                 # log collection
    githubx/
      client.go               # PR + branch + review + webhook operations
    slackx/
      client.go               # history, replies, reactions, delete
    runid/
      marker.go               # per-run marker derivation
  tests/
    validation_test.go        # runs first; no tunnel needed
    lifecycle_test.go
    routing_test.go
    main_test.go              # TestMain: stack up, tunnel up, teardown
  .github/workflows/
    integration.yml
```

## Manual setup checklist

Everything below needs a browser and cannot be automated. The suite ships with
each scenario declaring its required secrets, so a partially completed
checklist still produces a green run that honestly reports what it skipped.

1. Slack app in the test workspace; bot scopes `chat:write`, `channels:read`,
   `channels:history`, `reactions:read`, `reactions:write`; install; copy the
   `xoxb-` token.
2. Create the five channels; invite the bot to each; record the channel IDs.
3. Fine-grained PAT (author) scoped to the three fixture repositories:
   contents write, pull requests write, webhooks admin.
4. GitHub App (reviewer); install on the three fixture repositories; record the
   app ID and private key.
5. Fine-grained PAT for cross-repository dispatch: Actions write on
   `notifycat-integration-tests` only.
6. Add secrets to `notifycat-integration-tests`: `SLACK_BOT_TOKEN`,
   `SLACK_CHANNEL_PRIMARY`, `SLACK_CHANNEL_SECONDARY`, `SLACK_CHANNEL_TEAM_A`,
   `SLACK_CHANNEL_TEAM_B`, `SLACK_CHANNEL_ALERTS`, `GITHUB_AUTHOR_TOKEN`,
   `REVIEWER_APP_ID`, `REVIEWER_APP_PRIVATE_KEY`, `NOTIFYCAT_WEBHOOK_SECRET`.
7. Add `INTEGRATION_DISPATCH_TOKEN` to `notifycat`.

`NOTIFYCAT_WEBHOOK_SECRET` is generated once by the operator and used on both
sides: it is the secret the harness sets on each webhook it creates and the
`GITHUB_WEBHOOK_SECRET` the container runs with.

## Open risks

- **Cloudflare quick tunnels are best-effort.** They have no SLA and are
  occasionally slow to provision. Mitigated by retries and by keeping the
  validation scenarios tunnel-free, so a tunnel outage still leaves useful
  coverage. If flake rate proves unacceptable, the fallback is a named tunnel
  with a Cloudflare account token — a change confined to `harness/tunnel.go`.
- **Public fixture repositories.** Their pull-request activity is publicly
  visible. Acceptable: the content is inert placeholder files.
- **GHCR package visibility.** `ghcr.io/mptooling/notifycat` must be pullable
  from the private integration repository's runner. If the package is private,
  the workflow needs a read token; verify on first run.
