# Troubleshooting

Start from the symptom. Each row links to a runbook on this page or to the page that owns the fix.

| Symptom | Start at |
| --- | --- |
| Opened a PR — **nothing in Slack** | [No message for a new PR](#no-message-for-a-new-pr) |
| Webhook delivery returns **401** | [Webhook returns 401](#webhook-returns-401) |
| Delivery returns **200** but Slack didn't change | [200 OK, no Slack change](#200-ok-no-slack-change) |
| A review happened but **no reaction** appeared | [No reaction on a review](#no-reaction-on-a-review) |
| Server **exits immediately** on startup | [Server exits at startup](#server-exits-at-startup) |
| HTTPS / **certificate** not issued | [Certificate failures](#certificate-failures) |
| **Database** errors, or the DB "disappeared" after an upgrade | [Database issues](#database-issues) |
| `validate` / `reconcile` gets **401 from the Bitbucket API** | [Bitbucket API 401](#bitbucket-api-401) |
| Digest **never posts**, lists **already-merged PRs**, or posts more than expected | [Digest surprises](#digest-surprises) |

Not sure where the problem is? Run the [preflight tools](#verify-the-whole-path) first — they localize most faults in under a minute.

## No message for a new PR

You opened a pull request and the channel stayed silent. Two quick checks before any webhook plumbing:

- **Draft PRs are silent by design.** A draft posts nothing until it's marked ready for review.
- **Dependabot and Renovate PRs look different.** They post one compact line instead of the "please review" message — easy to scan past. See [What you see in Slack](features.md#dependabot-and-renovate-compacted).

Neither applies? Open the webhook's **delivery history** on the git host (GitHub: repository **Settings → Webhooks → Recent Deliveries**; Bitbucket: **Repository settings → Webhooks → View requests**). What you find there picks the runbook:

| Delivery history shows | Meaning | Runbook |
| --- | --- | --- |
| No delivery at all | The git host never sent it — no webhook covers this repository, or the PR events aren't subscribed. | Register or fix the webhook: [GitHub](github-webhook.md) · [Bitbucket](bitbucket-webhook.md) |
| Delivery failed — timeout or connection error | The request never reached the server: DNS, ingress, or TLS. | `curl -i https://your-domain/healthz`, then [Certificate failures](#certificate-failures) |
| `401` | The signature check failed. | [Webhook returns 401](#webhook-returns-401) |
| `200`, but still no message | The server received it and deliberately ignored it — usually `no_mapping`. | [200 OK, no Slack change](#200-ok-no-slack-change) |

## Webhook returns 401

401 means the HMAC-SHA256 signature check failed before the payload was even parsed: the secret configured on the webhook doesn't match the one in `.env` (`GITHUB_WEBHOOK_SECRET` or `BITBUCKET_WEBHOOK_SECRET`).

1. Copy the exact secret from the webhook settings page — no trailing whitespace, and **byte-identical** in both places. Paste, don't retype.
2. In `.env`, store it unquoted (Bitbucket) or single-quoted if it contains shell-special characters (GitHub via Compose): quotes that reach the value become part of the secret and break the HMAC.
3. Restart after editing: `docker compose restart notifycat`.
4. Redeliver the failed event from the webhook's delivery history and confirm a `200`.

Two provider-specific traps: Bitbucket sends **no signature header at all** when the webhook's secret field is blank — the secret is mandatory, not optional. And on either provider, a proxy that rewrites the request body invalidates the signature; the bytes must arrive exactly as signed.

To avoid this class of failure entirely, generate secrets exactly as described in [Generating the webhook secret](security.md#generating-the-webhook-secret) — a hand-made secret with `$`, `#`, quotes, or spaces is the classic cause of a 401 that survives careful re-pasting. Also remember that a deployment has exactly **one** webhook secret: if several repository webhooks were created with different secrets, the mismatched ones 401 while the rest work, and `validate` cannot catch it (the git host's API never returns a webhook's secret).

## 200 OK, no Slack change

<a id="debugging-a-200-ok-with-no-slack-change"></a>

The git host records a successful delivery whenever Notifycat returns 200 — **including when the event is intentionally ignored**. Every silent no-op leaves a structured log line, `ignored webhook event`, with a `reason` field, so this question is answerable from logs alone:

```sh
docker compose logs notifycat | grep "ignored webhook event"   # or: ./notifycat logs
```

| `reason` | Level | Meaning | Fix |
| --- | --- | --- | --- |
| `no_handler` | Debug | No handler for this event kind — fires for `synchronize`, `labeled`, and other unmapped deliveries. | Expected. Set `server.log_level: debug` to see these. |
| `no_mapping` | Warn | The repository isn't covered by any `mappings:` entry. | Add the repository (or an org `"*"` tier) — see [routing](routing.md) — or remove the webhook from that repository. |
| `no_stored_message` | Info | The handler found no Slack message row for this PR — common when the PR predates Notifycat or the row was cleaned up. | Toggle the PR to draft and back to re-announce it, or wait for the next PR. |
| `already_sent` | Info | `OpenHandler` saw an existing message — idempotency. Expected on `ready_for_review` after a prior `opened`. | Nothing to fix. |

Every line also carries `handler`, `provider`, `kind` (the provider-neutral event classification — `opened`, `merged`, `changes_requested`, `unknown`, …), `repository`, and `pr`, so you can slice by any of them in a log aggregator.

If there's no `ignored webhook event` line at all, the delivery didn't reach the server — check the webhook's delivery history for the response code and your ingress logs.

## No reaction on a review

You expected ✅ / 💬 / ❗ on the PR message and nothing landed:

1. Check the logs at debug level for `skipped bot reviewer reaction`. If it's there, bot suppression fired: `reviews.ignore_ai_reviews` is `true` and the reviewer's account is a bot/app. Either accept that (it's the configured policy) or turn suppression off — see [bot reviewers](bots-and-reactions.md#bot-reviewers).
2. No suppression line? Work through [200 OK, no Slack change](#200-ok-no-slack-change) — the usual culprit is `no_stored_message` on a PR that predates Notifycat.
3. Reactions also fail when the bot isn't a **member** of the channel. `chat:write.public` covers posting, but `reactions.add` needs membership — `/invite @notifycat` in the channel, then `notifycat-config validate` to confirm.

## Server exits at startup

Notifycat fails fast on configuration it can't trust. `docker compose logs notifycat` (or the process output) names the cause:

| Log says | Cause | Fix |
| --- | --- | --- |
| `startup validation failed for N entries` | A mapping failed its Slack or git-host checks at boot. | Run `notifycat-config validate` for per-entry detail; fix the entries in `config.yaml`. The remediation table is under [CLI → validate](cli.md#notifycat-config). |
| `these env vars are no longer read` | Pre-0.17 environment variables still set. | Remove them; their values now live in `config.yaml`. See the [0.17 migration](0.17-config-migration.md). |
| `required key git_provider is missing` / `git_provider … is invalid` | Config predates the provider switch. | Add `git_provider: github` (or `bitbucket`) — one line, see [Upgrading](upgrading.md#git_provider-is-now-required). |
| an error naming `digest.schedule` or `digest.timezone` | Invalid cron expression or unrecognized IANA zone. | Fix the value — [digest config](digest.md#schedule-and-timezone). |
| `SLACK_BOT_TOKEN` / webhook-secret missing | A required secret is unset for the selected `git_provider`. | Set it in `.env` — [secrets reference](configuration.md#secrets-environment-variables-only). |

## Certificate failures

Applies to the Compose stack and the manual-Caddy VM install alike. First, read Caddy's own account of it:

```sh
docker compose logs caddy        # Compose
journalctl -u caddy              # host-installed Caddy
```

| Symptom in the log | Cause | Fix |
| --- | --- | --- |
| `connection refused` / `timeout` on the ACME challenge | Port 80 blocked by a firewall or security group | Open inbound TCP 80 to `0.0.0.0/0` |
| `no such host` / `NXDOMAIN` | DNS record not propagated yet | Wait; check with `dig +short notifycat.example.com` |
| `unauthorized` | DNS points at a different IP than this host | Compare `curl -s https://api.ipify.org` on the host vs `dig +short` from elsewhere |
| `rate limited` | Let's Encrypt's 5-failures-per-week limit hit | Wait it out — or debug against the staging endpoint (below) |

Port 80/443 already taken is its own failure: `sudo ss -tlnp | grep ':80\|:443'` and stop the conflicting `nginx`/`apache2`/leftover container.

**Debugging without burning rate limits** — point Caddy at the LE staging endpoint (same errors, no limits, cert untrusted by browsers). Add to the `Caddyfile` global block, restart Caddy, and remove before go-live:

```caddyfile
{
    email ops@example.com
    acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
}
```

## Database issues

**`unable to open database file: out of memory (14)`** — the parent directory of `database.url` doesn't exist or isn't writable by the container user. With `file:/app/notifycat.db`, the `/app` mount must be writable: run with `--user $(id -u):$(id -g)`, or `chown 65532:65532` the directory once.

**`permission denied` on the named volume (UID 65532)** — usually after restoring a backup into the volume. One-shot fix: `docker run --rm -v notifycat_notifycat_data:/app alpine chown -R 65532:65532 /app` (check the volume name with `docker volume ls`).

**The database "disappeared" after `docker compose pull && up`** — the data is almost never gone; it's at a path outside the volume. The `notifycat_data` volume mounts at `/app`, so only paths under `/app` persist — a `database.url` like `file:/data/notifycat.db` writes to the container's ephemeral layer and vanishes on recreate. Inspect the volume (`docker run --rm -v notifycat_notifycat_data:/v alpine ls -laR /v`), point `database.url` back at the file you find, and keep it under `/app`.

**`write lock tmp: … permission denied`** — a pre-0.4.0 image quirk; upgrade to `:latest` and see [Upgrading → pre-0.4.0 layout](upgrading.md#pre-040--data-layout).

## Bitbucket API 401

A 401 from `validate`, `doctor`, or `reconcile` (not from webhook delivery) means the auth *scheme* doesn't match the token *type* — it's not a scope problem:

| Token type | Scheme | `BITBUCKET_AUTH_EMAIL` |
| --- | --- | --- |
| Repository / workspace access token | Bearer | must be **unset** |
| Scoped Atlassian API token (Free plan) | Basic | must be **set** |

Mixing them — an access token with the email set, or an API token without it — is the classic cause. Full detail and a `curl` to isolate it: [Bitbucket webhook setup → Access token & scopes](bitbucket-webhook.md#access-token-scopes).

## Digest surprises

**The digest never posts** — in order of likelihood: nothing was stuck (a channel with no stuck PRs gets no digest — working as intended); the schedule fired at 9am **UTC** while your team's morning is elsewhere ([set `digest.timezone`](digest.md#schedule-and-timezone)); `digest.enabled: false` at the global or tier level; or the PRs you expected predate Notifycat — the digest only tracks PRs it announced.

**The first digest lists long-merged PRs** — rows created before the digest feature carry no open/closed marker. Run the one-time [reconcile](digest.md#reconcile).

**A digest showed up uninvited after an upgrade** — it's on by default. `digest: { enabled: false }` restores silence. See [Stuck-PR digest](digest.md#its-on-by-default).

**The same channel gets several digests a day** — two repository tiers with different `digest.schedule` values post to that channel; each schedule runs independently. Align the schedules or move one repository.

## Verify the whole path

Three tools, in escalating order of realism:

```sh
notifycat-config validate            # per-mapping: Slack channel, scopes, membership, webhook events
notifycat-doctor                     # + runtime config, database, mappings file health
./notifycat smoke <org>/<repo>       # forges a real signed PR event through the running server → Slack
```

The doctor proves the configuration is sound; the smoke test proves the **whole delivery path** works — real signature check, real dispatcher, real Slack post (a throwaway message titled `[notifycat smoke] …` appears in the mapped channel; delete it after). Add `--reactions` to replay a comment, approval, and merge against the synthetic PR and verify the emoji landed. See [Doctor](doctor.md) and [CLI → notifycat-smoke](cli.md#notifycat-smoke).
