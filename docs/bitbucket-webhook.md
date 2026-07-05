# Bitbucket Webhook Setup

Notifycat receives Bitbucket webhook requests at:

```text
POST /webhook/bitbucket
```

This route is only registered when `git_provider: bitbucket` is set in `config.yaml`. Bitbucket must send JSON payloads and sign them with the same secret you set in `BITBUCKET_WEBHOOK_SECRET`. See [Configuration → git_provider](configuration.md#git_provider) for the provider switch and [Mappings](mappings.md#bitbucket-workspace-and-repo-slug) for how Bitbucket workspaces and repo slugs map to Slack channels.

## Creating the Webhook

There is no creation script for Bitbucket webhooks (unlike GitHub). Create the webhook in the repository settings:

1. Open the Bitbucket repository.
2. Go to **Repository settings**.
3. Open **Webhooks**.
4. Click **Add webhook**.
5. Give it a descriptive title (e.g. `Notifycat`).
6. Set **URL** to your public Notifycat URL:

   ```text
   https://notifycat.example.com/webhook/bitbucket
   ```

7. Set **Secret** to a long random value — at least 32 characters from a password manager or secret store. This becomes `BITBUCKET_WEBHOOK_SECRET`.

   > **Secret is mandatory.** If you leave the secret field blank, Bitbucket sends no `X-Hub-Signature` header. Notifycat hard-requires a valid signature and will reject every unsigned delivery with `401`. The secret must be the same value you set in `BITBUCKET_WEBHOOK_SECRET`.

8. Under **Triggers**, choose **Choose from a full list of triggers** and enable the following pull request events:

   | Event | Trigger key | Purpose |
   | --- | --- | --- |
   | Pull request created | `pullrequest:created` | New PR opened. |
   | Pull request updated | `pullrequest:updated` | PR details changed, and also draft↔ready transitions (Bitbucket has no distinct draft event). |
   | Pull request fulfilled | `pullrequest:fulfilled` | PR merged. |
   | Pull request rejected | `pullrequest:rejected` | PR declined/closed. |
   | Pull request approved | `pullrequest:approved` | Reviewer approved. |
   | Pull request changes requested | `pullrequest:changes_request_created` | Reviewer requested changes. |
   | Pull request comment created | `pullrequest:comment_created` | Comment posted on the PR. |

9. Keep **Active** checked.
10. Save the webhook.

Bitbucket sends a test ping after creation; you can ignore it. Use Bitbucket's **request history** view (below) to inspect real PR deliveries after you open or update a pull request.

## Access Token & Scopes

`BITBUCKET_TOKEN` is optional but required for two capabilities: **per-path routing** (reading a PR's changed files to select a path channel) and **validate/reconcile probes** (checking whether the webhook subscribes to the right events). Without it, path rules are inert and those probes are skipped — behavior identical to how `GITHUB_TOKEN` degrades on a GitHub deployment.

### Repository or workspace access token (recommended)

Create a repository access token or a workspace access token in Bitbucket with the following least-privilege scopes:

| Scope | Why |
| --- | --- |
| `repository` | Read repository metadata and changed files. |
| `pullrequest` | Read PR details and file lists. |
| `webhook` | Read webhook configuration (for `notifycat-config validate`). |

Set the token value in `.env`:

```sh
BITBUCKET_TOKEN=your-access-token
```

Leave `BITBUCKET_AUTH_EMAIL` unset to use Bearer authentication (the default for access tokens).

### Free-plan fallback: scoped Atlassian API token

If your plan does not support repository access tokens, use a scoped Atlassian API token with HTTP Basic auth instead. Create the token at [id.atlassian.com](https://id.atlassian.com) with the following read-only scopes:

| Scope | Why |
| --- | --- |
| `read:repository:bitbucket` | Read repository metadata and changed files. |
| `read:pullrequest:bitbucket` | Read PR details. |
| `read:webhook:bitbucket` | Read webhook configuration. |

Scoped API tokens have a maximum lifetime of **365 days**. Set both variables in `.env`:

```sh
BITBUCKET_TOKEN=your-atlassian-api-token
BITBUCKET_AUTH_EMAIL=you@example.com
```

When `BITBUCKET_AUTH_EMAIL` is set, the client sends HTTP Basic `email:token` instead of Bearer.

### App passwords — not supported

> **App passwords are not supported and are being removed by Atlassian on 2026-07-28.** If your current setup uses a Bitbucket app password, migrate to a repository/workspace access token (Bearer) or a scoped Atlassian API token (Basic) before that date.

## Signature Verification

Bitbucket signs each delivery with HMAC-SHA256 over the raw request body and sends the result in the `X-Hub-Signature` header:

```text
X-Hub-Signature: sha256=<hex-digest>
```

Notifycat verifies this header on every request before the JSON payload is parsed. Requests without a valid signature are rejected with `401`. See [Security & permissions](security.md#signature-validation) for the full validation model.

If deliveries fail with `401`, check that:

- The Bitbucket webhook secret and `BITBUCKET_WEBHOOK_SECRET` are the same value.
- The payload reaches Notifycat as `application/json` without body modification.
- No proxy rewrites the request body before it reaches Notifycat (body must be byte-for-byte identical to what Bitbucket signed).

## Delivery & Troubleshooting

Bitbucket's webhook delivery behavior:

| Property | Value |
| --- | --- |
| Timeout per attempt | 10 seconds |
| Retry attempts on 5xx or timeout | Up to 3 total |
| Attempt number header | `X-Attempt-Number` (1 on the first try) |
| Maximum payload size | 256 KB (Notifycat's 1 MiB body guard covers it) |

To inspect past deliveries, open the Bitbucket repository's **Repository settings → Webhooks → View requests** (or the equivalent in your Bitbucket version). Each entry shows the request headers, payload, response status, and response body — useful for diagnosing silent failures.

Standard troubleshooting sequence:

1. Check Bitbucket's request history to confirm the delivery reached Notifycat and what status it received.
2. Check Notifycat's logs for a line with `ignored webhook event` and a `reason` field — this means the delivery was received and the `200` was intentional. See [Operations → Debugging a 200 OK](operations.md#debugging-a-200-ok-with-no-slack-change) for the full reason table.
3. If the delivery returned `401`, verify the webhook secret matches `BITBUCKET_WEBHOOK_SECRET` and no proxy is rewriting the body.

## Optional IP Allowlisting

For defense in depth, you can restrict inbound traffic to Bitbucket's delivery IP ranges. Atlassian publishes the current list at:

```text
https://ip-ranges.atlassian.com
```

Allowlisting these ranges at your reverse proxy or firewall means only Bitbucket (and your own testing traffic) can reach `/webhook/bitbucket`. This is optional — signature verification provides the same authenticity guarantee — but it limits the attack surface if an attacker can reach your endpoint.

## Secret Rotation

To rotate the webhook secret:

1. Generate a new long random secret.
2. Update `BITBUCKET_WEBHOOK_SECRET` in Notifycat.
3. Update the **Secret** field in the Bitbucket webhook settings to the same value.
4. Restart Notifycat if your runtime does not reload environment variables.

During the window between steps 2 and 3 (or 3 and 2), deliveries will fail with `401`. Keep the window short.
