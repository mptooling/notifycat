# Docker

The Docker image is a small scratch-based runtime image. It contains three
binaries:

- `/usr/local/bin/notifycat-server`
- `/usr/local/bin/notifycat-mapping`
- `/usr/local/bin/notifycat-migrate`

The default command runs `notifycat-server`.

## Build Locally

```sh
docker build -t notifycat .
```

## Database Persistence

The image runs as UID/GID `65532:65532` and defaults to:

```text
DATABASE_URL=file:/data/notifycat.db
```

Mount `/data` for persistent SQLite storage:

```sh
mkdir -p data
```

On hosts with strict volume ownership, make sure UID `65532` can write to the
mounted directory.

## Run Migrations

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-migrate up
```

Check migration status:

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-migrate status
```

## Configure Mappings

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-mapping add owner/repo C123ABCDE @alice,@bob
```

## Run the Server

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat
```

Health check:

```sh
curl -i http://localhost:8080/healthz
```

## Production Notes

For production, run migrations as a one-shot job before starting the server.
Keep `/data` on durable storage. Send logs to your container runtime's normal
log pipeline and set `LOG_FORMAT=json` if you prefer structured logs.

If the container exits immediately, check:

- Required env vars are present.
- `/data` is writable by UID `65532`.
- Migrations have been applied.
- The Slack bot token and GitHub webhook secret are set in the target runtime.
