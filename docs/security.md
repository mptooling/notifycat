# Security & permissions

Security guidance for Notifycat lives in a few focused pages — Slack scopes in [Slack app setup](slack-app.md), signature verification in [GitHub webhook setup](github-webhook.md) and [Bitbucket webhook setup](bitbucket-webhook.md), and vulnerability reporting in the repository's [security policy](https://github.com/mptooling/notifycat/blob/main/SECURITY.md). This page pulls the operational answer to "is my install safe?" into one checklist and explains the least-privilege model Notifycat is built around.

## Least-privilege checklist

Run through this before you point a production webhook at Notifycat:

- [ ] `.env` is owned by you and not world-readable (`0600`). The setup wizard writes it this way; see
  [File permissions](#file-permissions) to confirm or fix an existing install.
- [ ] `GITHUB_WEBHOOK_SECRET` (GitHub) or `BITBUCKET_WEBHOOK_SECRET` (Bitbucket) was [generated with `openssl rand -base64 32`](#generating-the-webhook-secret) and is set to the **same** value in `.env` and in the webhook settings. Only the secret for your active `git_provider` is required.
- [ ] Unless you use per-path routing, the running server has **no** read token configured — it needs only the webhook secret to verify deliveries.
- [ ] If you set the optional `GITHUB_TOKEN` or `BITBUCKET_TOKEN`, it is read-only (fine-grained scopes for webhook config reads and PR file lists only), not a write-scoped token.
- [ ] The Slack bot has only the [documented scopes](slack-app.md#bot-scopes) — nothing broader.
- [ ] Public traffic terminates TLS in front of Notifycat (the Compose install does this with Caddy); the server itself
  speaks plain HTTP only on the internal network.
- [ ] If you enable the [agent API](#agent-api-access), the admin port is **not** published to the internet — only reachable through an SSH tunnel, `kubectl port-forward`, or `fly proxy`.
- [ ] `NOTIFYCAT_ADMIN_KEY_HASH` lives in your secret store or the gitignored `.env` — never in git — and is wrapped in single quotes in `.env`, so `$` in the bcrypt hash is not expanded.

## What Notifycat asks for — and what it doesn't

**Notifycat is not a GitHub App or a Bitbucket App.** It receives ordinary repository webhooks authenticated by a shared secret. There is no OAuth flow, no installation, and no app-level permission grant to review.

The runtime server needs exactly two secrets:

| Secret | Why | Scope |
| --- | --- | --- |
| `GITHUB_WEBHOOK_SECRET` or `BITBUCKET_WEBHOOK_SECRET` | Verify the HMAC signature on every incoming webhook. Only the one that matches your `git_provider` is required. | A shared string — not a token, grants no API access. |
| `SLACK_BOT_TOKEN` | Post, update, and react on the PR's Slack message. | Only the [bot scopes](slack-app.md#bot-scopes) Notifycat documents. |

It deliberately never requires org-admin access, write-scoped tokens, a GitHub App installation, or broad Slack scopes such as `chat:write` on every channel. If a setup guide ever asks you to over-provision beyond the table above, that is a bug.

### The optional read-only tokens

By default neither `GITHUB_TOKEN` nor `BITBUCKET_TOKEN` is used by the server. They are read by `notifycat-config validate` (and the doctor) to query webhook configuration and confirm the expected PR events are subscribed. Without them those checks are skipped and everything else works.

The one runtime use for either token is **[per-path routing](monorepo.md)**: when a repository's mapping has a `paths:` block, the server reads each PR's changed files to pick the path channel. Without a token, path rules are inert and PRs route to the repository tier.

For GitHub: a fine-grained token with **Webhooks: Read** is enough for validation; add **Pull requests: Read** for per-path routing. See [GitHub webhook setup](github-webhook.md#security-notes) for details. For Bitbucket: an access token with `repository` + `pullrequest` + `webhook` scopes covers both uses; a scoped Atlassian API token with the equivalent `read:*:bitbucket` scopes works too. See [Bitbucket webhook setup](bitbucket-webhook.md#access-token-scopes) for the full options including the Free-plan Basic-auth path.

## Generating the webhook secret

Generate the secret — `GITHUB_WEBHOOK_SECRET` or `BITBUCKET_WEBHOOK_SECRET`, depending on your provider — with:

```sh
openssl rand -base64 32
```

Set the output as the value in `.env` **and** in the webhook's secret field, byte-identical — paste, don't retype.

Why exactly this way: the signature check accepts any byte string (HMAC-SHA256 has no format requirement), but a hand-made secret containing `$`, `#`, quotes, or spaces routinely gets mangled by `.env` parsing or shell interpolation — and the resulting one-byte difference fails every delivery with `401`. Base64 output is strong (256 bits of randomness) and survives `.env` files, shells, and Compose untouched.

Two rules follow:

- **One secret per deployment.** Every webhook pointing at this Notifycat instance must carry the same secret. A webhook created with a different one 401s while the rest work — and `validate` cannot catch it, because the git host's API never returns webhook secrets.
- **Store it unquoted in `.env`.** The value is read verbatim, so quotes that reach it become part of the secret and break the HMAC.

## Rotating the webhook secret

1. Generate a new secret with the same command.
2. Update `GITHUB_WEBHOOK_SECRET` / `BITBUCKET_WEBHOOK_SECRET` in `.env`.
3. Update the webhook's secret field to the same value.
4. Restart Notifycat if your runtime does not reload environment variables.

Deliveries sent between steps 2 and 3 fail with `401` — keep the window short, then redeliver the failed events from the webhook's delivery history.

## Agent API access

Notifycat can hand developers' coding agents a personal token for its agent API. Access is granted in two steps, and the admin never sees a token:

1. The admin creates an **invite** for a user label such as `alice`. An invite is valid for one hour and can be redeemed once.
2. The developer redeems the invite over HTTPS from their own machine and receives their own **token** (`nct_…`). It is shown once and never again.

Only bcrypt hashes of invites and tokens are stored, in SQLite. They never go into git or `config.yaml`. A token has no end date; deleting its row ends access at once. Developers use the token to read open PRs; see [Agent API](agent-api.md).

### The admin key

Generate a key with `notifycat-keygen`. It writes the key (`nca_…`) to stdout and the env line for its hash to stderr. Split the two streams so you can store each one:

```sh
# only you can read key.txt and hash.txt
umask 077

# pull first, so pull progress does not end up in hash.txt
docker pull ghcr.io/mptooling/notifycat

# the key stays on screen, the hash line goes to hash.txt
docker run --rm ghcr.io/mptooling/notifycat notifycat-keygen 2>hash.txt

# or both go to files
docker run --rm ghcr.io/mptooling/notifycat notifycat-keygen >key.txt 2>hash.txt

# from a source checkout instead of the image
go build -o bin/notifycat-keygen ./cmd/notifycat-keygen
./bin/notifycat-keygen >key.txt 2>hash.txt
```

Keep the key in your password manager. Only the hash goes to the server, as this line:

```sh
NOTIFYCAT_ADMIN_KEY_HASH='$2a$10$…'
```

This line is a secret. Put it in your platform's secret store:

- Fly.io: `fly secrets import < hash.txt`.
- Kubernetes: a `Secret`.
- Docker Compose or the plain binary: the gitignored `.env` (`cat hash.txt >> .env`; make sure `.env` ends with a newline first).

A secret store takes the name `NOTIFYCAT_ADMIN_KEY_HASH` and, as the value, the hash between the single quotes.

Never commit it to git, not even to a deploy repository. Once the key is in your password manager and the hash is in the secret store, delete `key.txt` and `hash.txt`.

The single quotes matter: `godotenv` and Docker Compose expand `$` in unquoted values and would silently break the hash. Startup fails if the value is not a valid bcrypt hash.

### The admin port

The admin routes are served by a second HTTP server, set by `server.admin_addr`. The public server never registers them.

| Where notifycat runs | `server.admin_addr` | How the admin reaches it |
| --- | --- | --- |
| Docker Compose on a VM | `0.0.0.0:9091` in the container, published on the host only as `127.0.0.1:9091` | `ssh -L 9091:localhost:9091 vm` |
| Kubernetes | `0.0.0.0:9091`, no Ingress for it | `kubectl port-forward` |
| Fly.io | `fly-local-6pn:9091`, no `[[services]]` entry | `fly proxy 9091 -a <app>` |

For Docker Compose, set `server.admin_addr: "0.0.0.0:9091"` in `config.yaml` and publish the port on the host's loopback only:

```yaml
# compose.yaml, notifycat service
ports:
  - "127.0.0.1:9091:9091"
```

Never write `"9091:9091"`: it publishes the plain-HTTP admin port on every host interface, and Docker-published ports bypass ufw.

The admin port speaks plain HTTP. Never publish it to the internet; the tunnel provides the encryption. The public agent API, including invite redemption, is HTTPS-only through your reverse proxy.

### Managing access

Every admin call needs `Authorization: Bearer <admin key>`. First set the key in your shell without leaving it in the shell history. Run this line and paste the key from your password manager; nothing is shown:

```sh
read -rs NOTIFYCAT_ADMIN_KEY && export NOTIFYCAT_ADMIN_KEY
```

The examples need bash or zsh (they use `<<<` here-strings) and curl 7.55 or newer (for `-H @-`). The header comes from stdin so the key is not on the command line. Through the tunnel the port is `localhost:9091`:

```sh
# create an invite for alice; prints the line to send to alice
curl -fsS -X POST -H 'Accept: text/plain' -H @- http://localhost:9091/admin/v1/invites -d '{"user":"alice"}' <<<"Authorization: Bearer $NOTIFYCAT_ADMIN_KEY"

# list tokens (never a hash)
curl -fsS -H @- http://localhost:9091/admin/v1/tokens <<<"Authorization: Bearer $NOTIFYCAT_ADMIN_KEY"

# remove a token
curl -fsS -X DELETE -H @- http://localhost:9091/admin/v1/tokens/<id> <<<"Authorization: Bearer $NOTIFYCAT_ADMIN_KEY"
```

The `<id>` in the `DELETE` example is the `id` field from `GET /admin/v1/tokens`.

The invite call prints one line: the command that redeems the invite. Send this line to the developer in a private message, as a code block, so the chat app keeps the quotes:

```sh
curl -fsS -X POST -H 'Accept: text/plain' -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_<id>_<secret>'
```

The command is built from `server.domain`. If `server.domain` is not set, the command uses `$NOTIFYCAT_URL` instead, and the developer sets it to the public base URL first (`export NOTIFYCAT_URL=https://notifycat.example.com`). The developer runs the command once on their own machine, in bash or zsh. It prints the lines for their shell profile, with their token. [Set up the agent](agent-api.md#set-up-the-agent) has the developer steps. The admin never sees the token.

Without `Accept: text/plain`, the invite call returns JSON with `invite`, `user`, `expires_at`, and `redeem_command`.

### What a stolen token gets

A token reads the titles, descriptions, branch names, author ids, links, and commit SHAs of open PRs in the configured repositories. No code. The fix is `DELETE /admin/v1/tokens/{id}`.

### What a stolen invite gets

One token, within the hour, if nobody redeemed the invite first.

Notifycat never logs a token, an invite, the admin key, or an `Authorization` header; a test pins this.

<a id="signature-validation"></a>

## Signature validation — why the secret matters

Notifycat verifies an HMAC-SHA256 signature on every incoming request and rejects anything without a valid signature before the payload is parsed. The two providers differ only in the header name:

| Provider | Header | Value format |
| --- | --- | --- |
| GitHub | `X-Hub-Signature-256` | `sha256=<hex-digest>` |
| Bitbucket | `X-Hub-Signature` | `sha256=<hex-digest>` |

Both sign the raw request body with the configured webhook secret. The shared secret is the only thing standing between your Slack channel and a forged PR event, so treat it like a password: long, random, stored in your secret manager, and rotated if exposed. Unsigned requests (no header) are rejected with `401` on both providers — Bitbucket sends no header at all when the webhook secret field is left blank, so the secret is mandatory. Signature-troubleshooting checklists are in [GitHub webhook setup → Signature verification](github-webhook.md#signature-verification) and [Bitbucket webhook setup → Signature verification](bitbucket-webhook.md#signature-verification).

For Bitbucket deployments, optional IP allowlisting via [ip-ranges.atlassian.com](https://ip-ranges.atlassian.com) provides an additional layer of defense — see [Bitbucket webhook setup → Optional IP allowlisting](bitbucket-webhook.md#optional-ip-allowlisting).

## File permissions

`.env` holds your webhook secret and Slack token, so it must not be world-readable. The `notifycat setup` wizard writes
it as `0600` (owner read/write only) and prints `.env written (0600)` when it does.

To confirm or fix the permissions on an existing install:

```sh
ls -l .env            # expect: -rw------- (0600)
chmod 600 .env        # fix if it is anything more permissive
```

`.env` is gitignored — never commit it, and never paste its contents into an issue or PR. The same applies to `config.yaml` and anything under `data/`.

## Reporting a vulnerability

Found a security issue? Do not open a public issue. Follow the private process in the
[security policy](https://github.com/mptooling/notifycat/blob/main/SECURITY.md).
