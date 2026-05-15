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

The server applies embedded migrations at startup. You can still run migrations
as a separate one-shot container if your deployment process prefers an explicit
database step before the server starts.

## Run Migrations

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-migrate up
```

Check migration status:

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-migrate status
```

## Configure Mappings

```sh
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-mapping add owner/repo C123ABCDE '<@U123456>,<!subteam^S123456>'
```

## Run the Server

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat
```

Health check:

```sh
curl -i http://localhost:8080/healthz
```

## Production Notes

For production, keep `/data` on durable storage. Run migrations as a one-shot
job if your platform expects that pattern; otherwise the server will apply
pending migrations during startup. Send logs to your container runtime's normal
log pipeline and set `LOG_FORMAT=json` if you prefer structured logs.

If the container exits immediately, check:

- Required env vars are present.
- `/data` is writable by UID `65532`.
- The Slack bot token and GitHub webhook secret are set in the target runtime.
