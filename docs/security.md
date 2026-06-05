# Security & permissions

Security guidance for Notifycat lives in a few focused pages — Slack scopes in [Slack app setup](slack-app.md), signature
verification in [GitHub webhook setup](github-webhook.md), and vulnerability reporting in the repository's
[security policy](https://github.com/mptooling/notifycat/blob/main/SECURITY.md). This page pulls the operational answer to
"is my install safe?" into one checklist and explains the least-privilege model Notifycat is built around.

## Least-privilege checklist

Run through this before you point a production webhook at Notifycat:

- [ ] `.env` is owned by you and not world-readable (`0600`). The setup wizard writes it this way; see
  [File permissions](#file-permissions) to confirm or fix an existing install.
- [ ] `GITHUB_WEBHOOK_SECRET` is a long random string (32+ characters), set to the **same** value in `.env` and in the
  GitHub webhook settings.
- [ ] The running server has **no** GitHub token configured — it needs only the webhook secret to verify deliveries.
- [ ] If you set the optional `GITHUB_TOKEN`, it is read-only (fine-grained **Webhooks: Read**), not a write-scoped
  `repo` PAT.
- [ ] The Slack bot has only the [documented scopes](slack-app.md#bot-scopes) — nothing broader.
- [ ] Public traffic terminates TLS in front of Notifycat (the Compose install does this with Caddy); the server itself
  speaks plain HTTP only on the internal network.

## What Notifycat asks for — and what it doesn't

**Notifycat is not a GitHub App.** It receives ordinary repository (or organization) webhooks authenticated by a shared
secret. There is no OAuth flow, no installation, and no app-level permission grant to review.

The runtime server needs exactly two secrets:

| Secret | Why | Scope |
| --- | --- | --- |
| `GITHUB_WEBHOOK_SECRET` | Verify the HMAC signature on every incoming webhook. | A shared string — not a token, grants no API access. |
| `SLACK_BOT_TOKEN` | Post, update, and react on the PR's Slack message. | Only the [bot scopes](slack-app.md#bot-scopes) Notifycat documents. |

It deliberately never requires GitHub org-admin, a repo-write PAT, a GitHub App installation, or broad Slack scopes such
as `chat:write` on every channel. If a setup guide ever asks you to over-provision beyond the table above, that is a bug.

### The optional read-only GitHub token

`GITHUB_TOKEN` is **not** used by the server. It is read only by `notifycat-mapping validate` (and the doctor, which
delegates to the same check) to query a repository's webhook configuration and confirm the expected PR events are
subscribed. Without it, that one coverage check is skipped and everything else works.

When you do set it, a fine-grained token with **Webhooks: Read** on the target repository is enough. Creating a webhook
in the first place needs **Webhooks: Read and write** (or classic `admin:repo_hook`); see
[GitHub webhook setup](github-webhook.md#security-notes) — but the ongoing validation token never needs write.

## Signature validation — why the secret matters

Notifycat verifies the `X-Hub-Signature-256` header on every request with HMAC-SHA256 and rejects anything without a
valid signature before the payload is parsed. That shared secret is the only thing standing between your Slack channel
and a forged PR event, so treat it like a password: long, random, stored in your secret manager, and rotated if exposed.
Details and the `401`/`403` troubleshooting list are in
[Signature verification](github-webhook.md#signature-verification).

## File permissions

`.env` holds your webhook secret and Slack token, so it must not be world-readable. The `notifycat setup` wizard writes
it as `0600` (owner read/write only) and prints `.env written (0600)` when it does.

To confirm or fix the permissions on an existing install:

```sh
ls -l .env            # expect: -rw------- (0600)
chmod 600 .env        # fix if it is anything more permissive
```

`.env` is gitignored — never commit it, and never paste its contents into an issue or PR. The same applies to
`mappings.yaml` and anything under `data/`.

## Reporting a vulnerability

Found a security issue? Do not open a public issue. Follow the private process in the
[security policy](https://github.com/mptooling/notifycat/blob/main/SECURITY.md).
