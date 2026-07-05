# Security & permissions

Security guidance for Notifycat lives in a few focused pages — Slack scopes in [Slack app setup](slack-app.md), signature verification in [GitHub webhook setup](github-webhook.md) and [Bitbucket webhook setup](bitbucket-webhook.md), and vulnerability reporting in the repository's [security policy](https://github.com/mptooling/notifycat/blob/main/SECURITY.md). This page pulls the operational answer to "is my install safe?" into one checklist and explains the least-privilege model Notifycat is built around.

## Least-privilege checklist

Run through this before you point a production webhook at Notifycat:

- [ ] `.env` is owned by you and not world-readable (`0600`). The setup wizard writes it this way; see
  [File permissions](#file-permissions) to confirm or fix an existing install.
- [ ] `GITHUB_WEBHOOK_SECRET` (GitHub) or `BITBUCKET_WEBHOOK_SECRET` (Bitbucket) is a long random string (32+ characters), set to the **same** value in `.env` and in the webhook settings. Only the secret for your active `git_provider` is required.
- [ ] Unless you use per-path routing, the running server has **no** read token configured — it needs only the webhook secret to verify deliveries.
- [ ] If you set the optional `GITHUB_TOKEN` or `BITBUCKET_TOKEN`, it is read-only (fine-grained scopes for webhook config reads and PR file lists only), not a write-scoped token.
- [ ] The Slack bot has only the [documented scopes](slack-app.md#bot-scopes) — nothing broader.
- [ ] Public traffic terminates TLS in front of Notifycat (the Compose install does this with Caddy); the server itself
  speaks plain HTTP only on the internal network.

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

The one runtime use for either token is **[per-path routing](mappings.md#per-path-routing-monorepos)**: when a repo's mapping has a `paths:` block, the server reads each PR's changed files to pick the path channel. Without a token, path rules are inert and PRs route to the repo tier.

For GitHub: a fine-grained token with **Webhooks: Read** is enough for validation; add **Pull requests: Read** for per-path routing. See [GitHub webhook setup](github-webhook.md#security-notes) for details. For Bitbucket: an access token with `repository` + `pullrequest` + `webhook` scopes covers both uses; a scoped Atlassian API token with the equivalent `read:*:bitbucket` scopes works too. See [Bitbucket webhook setup](bitbucket-webhook.md#access-token-scopes) for the full options including the Free-plan Basic-auth path.

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
