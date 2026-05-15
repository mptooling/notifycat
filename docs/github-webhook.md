# GitHub Webhook Setup

notifycat receives GitHub webhooks at:

```text
POST /webhook/github
```

GitHub must send JSON payloads and sign them with the same secret you set in
`GITHUB_WEBHOOK_SECRET`.

## Register the Webhook

In the GitHub repository:

1. Open **Settings**.
2. Open **Webhooks**.
3. Click **Add webhook**.
4. Set **Payload URL** to your public notifycat URL:

   ```text
   https://your-domain.example/webhook/github
   ```

5. Set **Content type** to `application/json`.
6. Set **Secret** to the same value as `GITHUB_WEBHOOK_SECRET`.
7. Choose **Let me select individual events**.
8. Enable:
   - **Pull requests**
   - **Pull request reviews**
9. Keep **Active** checked.
10. Save the webhook.

GitHub sends a ping after creation. notifycat only handles pull request events,
so use GitHub's delivery view to test a real PR event after the webhook is
registered.

## Event Coverage

notifycat handles these event states:

| GitHub event | Actions or states |
| --- | --- |
| `pull_request` | opened, closed, converted to draft |
| `pull_request_review` | approved, commented, changes requested |

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

Then point GitHub at:

```text
https://your-tunnel.example/webhook/github
```

Open or update a pull request to generate a delivery.
