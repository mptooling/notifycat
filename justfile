set dotenv-load
set shell := ["bash", "-uc"]

app := "notifycat"
go_image := "golang:1.25.10-alpine"

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

# Add a repo-to-Slack mapping
mapping-add repo channel mentions:
  go run ./cmd/notifycat-mapping add "{{repo}}" "{{channel}}" "{{mentions}}"

# List repo-to-Slack mappings
mapping-list:
  go run ./cmd/notifycat-mapping list

# Remove a repo-to-Slack mapping
mapping-remove repo:
  go run ./cmd/notifycat-mapping remove "{{repo}}"

# Build the Docker image
docker-build:
  docker build -t {{app}}:test .

# Run migrations in Docker against ./data
docker-migrate:
  mkdir -p data
  docker run --rm -v "$PWD/data:/data" --env-file .env {{app}}:test /usr/local/bin/notifycat-migrate up

# Show Docker database migration status
docker-migrate-status:
  mkdir -p data
  docker run --rm -v "$PWD/data:/data" --env-file .env {{app}}:test /usr/local/bin/notifycat-migrate status

# Add a repo-to-Slack mapping in Docker against ./data
docker-mapping-add repo channel mentions:
  mkdir -p data
  docker run --rm -v "$PWD/data:/data" --env-file .env {{app}}:test /usr/local/bin/notifycat-mapping add "{{repo}}" "{{channel}}" "{{mentions}}"

# List repo-to-Slack mappings in Docker against ./data
docker-mapping-list:
  mkdir -p data
  docker run --rm -v "$PWD/data:/data" --env-file .env {{app}}:test /usr/local/bin/notifycat-mapping list

# Remove a repo-to-Slack mapping in Docker against ./data
docker-mapping-remove repo:
  mkdir -p data
  docker run --rm -v "$PWD/data:/data" --env-file .env {{app}}:test /usr/local/bin/notifycat-mapping remove "{{repo}}"

# Start the Docker image on localhost:8080
docker-serve:
  mkdir -p data
  docker run --rm -p 8080:8080 -v "$PWD/data:/data" --env-file .env {{app}}:test
