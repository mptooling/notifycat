# Notifycat

[![CI](https://github.com/mptooling/notifycat/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mptooling/notifycat/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mptooling/notifycat?display_name=tag&sort=semver)](https://github.com/mptooling/notifycat/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mptooling/notifycat)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/mptooling/notifycat)](https://goreportcard.com/report/github.com/mptooling/notifycat)
[![License: MIT](https://img.shields.io/github/license/mptooling/notifycat)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

Notifycat listens for GitHub pull request webhooks and keeps Slack up to
date.

One pull request gets one Slack message. As the PR opens, moves to draft, gets
reviewed, merges, or closes, Notifycat updates that message and adds the
configured reactions. The result is a quieter channel: reviewers can follow the
state of a PR without digging through repeated notifications.

It is intentionally small: one HTTP endpoint, a SQLite database (for Slack
message timestamps), and a declarative `mappings.yaml` that decides which
PRs route to which Slack channels.

## What It Handles

- `pull_request` webhooks for opened, closed, and converted-to-draft PRs.
- `pull_request_review` webhooks for approved, commented, and
  changes-requested reviews.
- `pull_request_review_comment` webhooks for line-specific PR comments.
- GitHub HMAC-SHA256 verification through `X-Hub-Signature-256`.
- Repository routing from a declarative `mappings.yaml` — explicit lists
  or `repositories: "*"` for a whole org. See
  [`mappings.example.yaml`](mappings.example.yaml).
- Slack message updates instead of repeated new messages for the same PR.

## Binaries

| Binary | Purpose |
| --- | --- |
| `notifycat-server` | HTTP server for GitHub webhooks |
| `notifycat-mapping` | CLI for listing and validating the mappings file |
| `notifycat-migrate` | Applies embedded SQLite migrations |
| `notifycat-doctor` | Preflight diagnostics (config, database, mappings, optional per-repo Slack/GitHub) |

## Documentation

Full documentation is published at <https://mptooling.github.io/notifycat/>.

- [Getting started](https://mptooling.github.io/notifycat/getting-started/)
- [Mappings file](https://mptooling.github.io/notifycat/mappings/)
- [Configuration](https://mptooling.github.io/notifycat/configuration/)
- [Slack app setup](https://mptooling.github.io/notifycat/slack-app/)
- [GitHub webhook setup](https://mptooling.github.io/notifycat/github-webhook/)
- [Docker](https://mptooling.github.io/notifycat/docker/)
- [Operations](https://mptooling.github.io/notifycat/operations/)
- [Doctor](https://mptooling.github.io/notifycat/doctor/)

## Quickstart

Create a local env file and the mappings file from the bundled examples:

```sh
cp .env.example .env
cp mappings.example.yaml mappings.yaml
```

Set `SLACK_BOT_TOKEN` and `GITHUB_WEBHOOK_SECRET` in `.env` and replace
the example Slack channel IDs in `mappings.yaml` with real ones — both
`notifycat-mapping validate` and `notifycat-server` will fail fast
without them. Then migrate, validate, and start the server:

```sh
go run ./cmd/notifycat-migrate up
go run ./cmd/notifycat-mapping validate
go run ./cmd/notifycat-server
```

Health check:

```sh
curl -i http://localhost:8080/healthz
```

Exercising the full GitHub → Slack flow against a locally running
server needs a public URL (ngrok, Cloudflare Tunnel, …) so GitHub can
reach `/webhook/github`. See
[Getting started](https://mptooling.github.io/notifycat/getting-started/)
for the end-to-end setup.

## Development

The project includes a `justfile` for common development commands. Install
[`just`](https://github.com/casey/just) (`brew install just` on macOS), then run:

```sh
just
just check
just serve
```

`just` is a developer tool only. It is not part of the Go module, the Docker
runtime image, or production dependencies.

The underlying checks are:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contributor setup, pull request
expectations, and issue reporting guidance.

## Release Process

Releases are automated by
[release-please](https://github.com/googleapis/release-please) and follow
[Semantic Versioning](https://semver.org/) driven by
[Conventional Commits](https://www.conventionalcommits.org/).

1. Merge a PR into `main` with a Conventional Commits title (`feat:`,
   `fix:`, …). The PR-title lint workflow blocks non-conforming titles.
2. release-please opens — and keeps up-to-date — a single "release PR"
   that bumps the version in `.release-please-manifest.json` and writes
   the new section of `CHANGELOG.md`.
3. Merging the release PR creates a Git tag (e.g. `v0.2.0`) and publishes
   a GitHub Release with release notes.
4. The release event triggers the Docker workflow, which builds the
   `Dockerfile` and pushes
   `ghcr.io/mptooling/notifycat:<semver>`, `:<major>.<minor>`, `:<major>`,
   and `:latest` to GHCR.
5. Documentation under `docs/` and `mkdocs.yml` is published to GitHub
   Pages on every push to `main` that changes those paths.

Pre-1.0 caveat: while the project is on `0.x`, a breaking change bumps the
minor version (`0.1.0` → `0.2.0`). Major bumps begin after `1.0.0`.

## Community

- [Code of conduct](CODE_OF_CONDUCT.md)
- [Support](SUPPORT.md)
- [Security policy](SECURITY.md)

## License

MIT. See [`LICENSE`](LICENSE).
