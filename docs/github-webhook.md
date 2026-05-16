# GitHub Webhook Setup

notifycat receives GitHub webhook requests at:

```text
POST /webhook/github
```

GitHub must send JSON payloads and sign them with the same secret you set in
`GITHUB_WEBHOOK_SECRET`.

For production setup, use the shell script directly. It only needs `sh` and
`curl`; `jq` is optional and only makes the output easier to read.

## Create the Webhook with the Script

Create a fine-grained GitHub token for the target repository with only:

```text
Repository permissions: Webhooks: Read and write
```

Then run:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
NOTIFYCAT_PUBLIC_URL=https://notifycat.example.com \
./scripts/github-webhook-create.sh owner/repo
```

The script validates the inputs before calling GitHub. It creates an active
repository webhook with:

| Field | Value |
| --- | --- |
| Payload URL | `${NOTIFYCAT_PUBLIC_URL}/webhook/github` |
| Content type | `application/json` |
| Secret | `GITHUB_WEBHOOK_SECRET` |
| SSL verification | enabled |
| Events | `pull_request`, `pull_request_review`, `pull_request_review_comment` |

The GitHub token is setup-only. Do not store it in notifycat production
configuration.

## Local Development Shortcut

If you use `just` while working on the repository, this recipe calls the same
script:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
NOTIFYCAT_PUBLIC_URL=https://notifycat.example.com \
just github-webhook-create owner/repo
```

Production instructions should use `./scripts/github-webhook-create.sh`
directly so operators do not need to install `just`.

## Manual Fallback

If you cannot use the GitHub API, create the webhook in the repository settings:

1. Open the GitHub repository.
2. Go to **Settings**.
3. Open **Webhooks**.
4. Click **Add webhook**.
5. Set **Payload URL** to your public notifycat URL:

   ```text
   https://notifycat.example.com/webhook/github
   ```

6. Set **Content type** to `application/json`.
7. Set **Secret** to the same value as `GITHUB_WEBHOOK_SECRET`.
8. Choose **Let me select individual events**.
9. Enable:
   - **Pull requests**
   - **Pull request reviews**
   - **Pull request review comments**
10. Keep **Active** checked.
11. Save the webhook.

GitHub sends a ping after creation. notifycat only handles pull request events,
so use GitHub's delivery view to test a real PR event after the webhook is
registered.

## Security Notes

Use a fine-grained GitHub token scoped to the target repository with
**Webhooks: Read and write**. Avoid broad classic `repo` tokens unless your
organization cannot use fine-grained tokens.

Use HTTPS for `NOTIFYCAT_PUBLIC_URL`. The script rejects plain `http://` URLs
and creates the webhook with SSL verification enabled.

Use a long random `GITHUB_WEBHOOK_SECRET`. A good default is at least 32
characters from your password manager or secret manager. Set the same value in
notifycat and in the GitHub webhook.

To rotate the secret:

1. Generate a new random secret.
2. Update `GITHUB_WEBHOOK_SECRET` in notifycat.
3. Update the GitHub webhook secret to the same value.
4. Restart notifycat if your runtime does not reload environment variables.

## Event Coverage

notifycat handles these event states:

| GitHub event | Actions or states |
| --- | --- |
| `pull_request` | opened, closed, converted to draft |
| `pull_request_review` | approved, commented, changes requested |
| `pull_request_review_comment` | line-specific PR comments |

To verify the webhook is subscribed to all three events after setup, run
`notifycat-mapping validate owner/repo` with `GITHUB_TOKEN` exported. The
validator queries the repository's webhook configuration and reports any
missing event. The PAT needs `admin:repo_hook` (or `repo` for private
repositories) — the same token used by `scripts/github-webhook-create.sh` is
sufficient.

GitHub uses different events for different comment surfaces:

- A submitted review with "Comment" uses `pull_request_review`.
- A line-specific comment on the diff uses `pull_request_review_comment`.
- A comment in the PR conversation tab uses `issue_comment`, which notifycat
  does not handle today.

## Signature Verification

notifycat verifies `X-Hub-Signature-256` with HMAC-SHA256. Requests without a
valid signature are rejected before the JSON payload is processed.

If deliveries fail with `401` or `403`, check that:

- GitHub and notifycat use the same webhook secret.
- The payload is sent as `application/json`.
- No proxy rewrites the request body before it reaches notifycat.

## Local Testing

GitHub needs a public URL. For local testing, run notifycat on your machine and
expose it with a tunnel:

```sh
go run ./cmd/notifycat-server
```

Then use the tunnel base URL as `NOTIFYCAT_PUBLIC_URL`:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
NOTIFYCAT_PUBLIC_URL=https://your-tunnel.example \
./scripts/github-webhook-create.sh owner/repo
```

Open or update a pull request to generate a delivery.
