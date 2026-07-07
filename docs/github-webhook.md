# GitHub webhook setup

Notifycat receives GitHub webhook requests at:

```text
POST /webhook/github
```

GitHub must send JSON payloads and sign them with the same secret you set in `GITHUB_WEBHOOK_SECRET`.

For production setup, use the shell script directly. It only needs `sh` and `curl`; `jq` is optional and only makes the
output easier to read.

## Create the webhook with the script

**1. Generate the webhook secret** — as described in [Generating the webhook secret](security.md#generating-the-webhook-secret).

**2. Create a fine-grained GitHub token** for the target repository with only:

```text
Repository permissions: Webhooks: Read and write
```

**3. Run the script** with the secret from step 1:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-generated-secret \
NOTIFYCAT_PUBLIC_URL=https://notifycat.example.com \
./scripts/github-webhook-create.sh owner/repo
```

The script validates the inputs before calling GitHub. It creates an active repository webhook with:

| Field | Value |
| --- | --- |
| Payload URL | `${NOTIFYCAT_PUBLIC_URL}/webhook/github` |
| Content type | `application/json` |
| Secret | `GITHUB_WEBHOOK_SECRET` |
| SSL verification | enabled |
| Events | `pull_request`, `pull_request_review`, `pull_request_review_comment`, `issue_comment` |

The GitHub token is setup-only. Do not store it in Notifycat production configuration.

`NOTIFYCAT_PUBLIC_URL` is **script-only**. It is the public address you want GitHub to deliver to (your reverse proxy,
your tunnel, the `*.example.com` you have in DNS) and is never read by the running server. Setting it as an environment
variable on `notifycat-server` has no effect.

## Local development shortcut

If you use `just` while working on the repository, this recipe calls the same script:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-generated-secret \
NOTIFYCAT_PUBLIC_URL=https://notifycat.example.com \
just github-webhook-create owner/repo
```

Production instructions should use `./scripts/github-webhook-create.sh` directly so operators do not need to install
`just`.

## Manual fallback

If you cannot use the GitHub API, create the webhook in the repository settings:

1. Open the GitHub repository.
2. Go to **Settings**.
3. Open **Webhooks**.
4. Click **Add webhook**.
5. Set **Payload URL** to your public Notifycat URL:

   ```text
   https://notifycat.example.com/webhook/github
   ```

6. Set **Content type** to `application/json`.
7. Set **Secret** to the same value as `GITHUB_WEBHOOK_SECRET` — see [Generating the webhook secret](security.md#generating-the-webhook-secret).
8. Choose **Let me select individual events**.
9. Enable:
   - **Pull requests**
   - **Pull request reviews**
   - **Pull request review comments**
   - **Issue comments**
10. Keep **Active** checked.
11. Save the webhook.

GitHub sends a ping after creation. Notifycat only handles pull request events, so use GitHub's delivery view to test a
real PR event after the webhook is registered.

## Organization-level webhook

A repository webhook covers one repository. If your `mappings:` use an org-wide `"*"` tier, an **organization webhook** is the better fit: one registration delivers PR events for every repository in the org — including repositories created later — so the catch-all actually catches everything.

The creation script only handles repository webhooks; create an organization webhook in the GitHub UI (org admin required):

1. Open the organization's **Settings**.
2. Open **Webhooks** and click **Add webhook**.
3. Use the same **Payload URL** and **Content type** (`application/json`) as for a repository webhook, and set **Secret** to the same value as `GITHUB_WEBHOOK_SECRET` in `.env` — see [Generating the webhook secret](security.md#generating-the-webhook-secret).
4. Select the same individual events: **Pull requests**, **Pull request reviews**, **Pull request review comments**, **Issue comments**.
5. Keep **Active** checked and save.

Deliveries from an organization webhook are identical in payload shape and signature to repository-webhook deliveries — the server needs no extra configuration.

!!! warning "Preflight caveat"
    `notifycat-config validate` and `notifycat-doctor` verify webhook coverage by listing each **repository's own** hooks, and an organization webhook doesn't appear there. With `GITHUB_TOKEN` set, the `webhook` check reports `FAIL` ("no active webhook … points at notifycat") for repositories covered only by the organization webhook — delivery still works; the check is a false negative in this topology. Until the check learns about org hooks, treat that `FAIL` as expected in an org-webhook setup (or leave `GITHUB_TOKEN` unset so the check is skipped).

## Security notes

Use a fine-grained GitHub token scoped to the target repository with **Webhooks: Read and write**. Avoid broad classic
`repo` tokens unless your organization cannot use fine-grained tokens.

Use HTTPS for `NOTIFYCAT_PUBLIC_URL`. The script rejects plain `http://` URLs and creates the webhook with SSL
verification enabled.

Generate the webhook secret exactly as described in [Generating the webhook secret](security.md#generating-the-webhook-secret), and set the same value in Notifycat and in the GitHub webhook. To change it later, follow [Rotating the webhook secret](security.md#rotating-the-webhook-secret).

## Event coverage

Notifycat handles these event states:

| GitHub event | Actions or states |
| --- | --- |
| `pull_request` | opened, closed, converted to draft |
| `pull_request_review` | approved, commented, changes requested |
| `pull_request_review_comment` | line-specific PR comments |
| `issue_comment` | comments on the PR conversation tab (created) |

To verify the webhook is subscribed to all three events after setup, run `notifycat-config validate owner/repo` with
`GITHUB_TOKEN` exported. The validator queries the repository's webhook configuration and reports any missing event. The
PAT needs `admin:repo_hook` (or `repo` for private repositories) — the same token used by
`scripts/github-webhook-create.sh` is sufficient.

GitHub uses different events for different comment surfaces:

- A submitted review with "Comment" uses `pull_request_review`.
- A line-specific comment on the diff uses `pull_request_review_comment`.
- A comment in the PR conversation tab uses `issue_comment`, which Notifycat does not handle today.

## Signature verification

Notifycat verifies `X-Hub-Signature-256` with HMAC-SHA256. Requests without a valid signature are rejected before the JSON payload is processed. The full validation model is in [Security & permissions](security.md#signature-validation); a delivery failing with `401` has its runbook in [Troubleshooting → Webhook returns 401](troubleshooting.md#webhook-returns-401).

## Local testing

GitHub needs a public URL. For local testing, run Notifycat on your machine and expose it with a tunnel:

```sh
go run ./cmd/notifycat-server
```

Then use the tunnel base URL as `NOTIFYCAT_PUBLIC_URL`:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-generated-secret \
NOTIFYCAT_PUBLIC_URL=https://your-tunnel.example \
./scripts/github-webhook-create.sh owner/repo
```

Open or update a pull request to generate a delivery.
