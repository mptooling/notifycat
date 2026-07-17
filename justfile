set dotenv-load
set shell := ["bash", "-uc"]

app := "notifycat"
go_image := "golang:1.25.12-alpine"

# List available recipes
default:
  @just --list

# Run the full local verification suite
check: vet lint vuln test build

# Run go vet
vet:
  go vet ./...

# Run golangci-lint
lint:
  golangci-lint run ./...

# Run govulncheck with the pinned Go toolchain used by CI
vuln:
  docker run --rm -v "$PWD:/src" -w /src {{go_image}} sh -c 'export PATH=/usr/local/go/bin:$PATH; go install golang.org/x/vuln/cmd/govulncheck@v1.3.0 && /go/bin/govulncheck ./...'

# Run the race-enabled test suite
test:
  go test -race ./...

# Run the standard test suite
test-fast:
  go test ./...

# Build all packages
build:
  go build ./...

# Start the local webhook server
serve:
  go run ./cmd/notifycat-server

# Serve the docs site with live reload at http://127.0.0.1:8000/notifycat/
docs: _docs-venv
  .venv-docs/bin/mkdocs serve

# Build the docs site exactly as CI does (output in ./site)
docs-build: _docs-venv
  .venv-docs/bin/mkdocs build --strict

_docs-venv:
  [ -d .venv-docs ] || python3 -m venv .venv-docs
  .venv-docs/bin/pip install -q -r docs/requirements.txt

# Create a Slack app from the committed manifest
slack-app-create:
  ./scripts/slack-app-create.sh

# Create the GitHub repository webhook for notifycat
github-webhook-create repo:
  ./scripts/github-webhook-create.sh "{{repo}}"

# Apply database migrations
migrate:
  go run ./cmd/notifycat-migrate up

# Show database migration status
migrate-status:
  go run ./cmd/notifycat-migrate status

# One-time: mark slack_messages rows closed from their GitHub state (dry-run first)
reconcile *args:
  go run ./cmd/notifycat-reconcile {{args}}

# List config entries
config-list:
  go run ./cmd/notifycat-config list

# Build the Docker image
docker-build:
  docker build -t {{app}}:test .

# Run migrations in Docker against ./
docker-migrate:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-migrate up

# Show Docker database migration status
docker-migrate-status:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-migrate status

# One-time reconcile in Docker (pass -dry-run to preview); needs GITHUB_TOKEN in .env
docker-reconcile *args:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-reconcile {{args}}

# List config entries in Docker
docker-config-list:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-config list

# Run the `notifycat-config` command with any number of args
docker-config +args:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-config {{args}}

# Validate config.yaml in Docker (against live Slack/GitHub)
docker-validate:
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-config validate

# Run preflight diagnostics in Docker
docker-doctor +args="":
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-doctor {{args}}

# Start the Docker image on localhost:8080 (foreground; for development)
docker-serve:
  docker run --rm -p 8080:8080 --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test

# Start the Docker image detached with restart-on-crash (closer to production)
docker-up:
  docker run -d --name notifycat --restart unless-stopped -p 127.0.0.1:8080:8080 --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test

# Stop and remove the detached container started by docker-up
docker-down:
  -docker rm -f notifycat

# Smoke-test end-to-end delivery in Docker (needs `just docker-up` first) — e.g. just docker-smoke [--reactions] owner/repo
docker-smoke +args="":
  # --network container:notifycat shares the running server's netns so the webhook reaches it on localhost.
  docker run --rm --network container:notifycat --user "$(id -u):$(id -g)" -v "$PWD:/app" --env-file .env {{app}}:test /usr/local/bin/notifycat-smoke --url http://localhost:8080/webhook/github {{args}}
