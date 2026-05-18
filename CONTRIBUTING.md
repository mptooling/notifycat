# Contributing to notifycat

Thanks for helping make notifycat better. This project is intentionally small,
so the best contributions are focused, tested, and easy to review.

## Before You Start

- Open an issue for larger changes before writing code. This keeps design
  discussion out of a surprise pull request.
- For small fixes, documentation updates, and test-only improvements, a pull
  request is fine without a prior issue.
- Do not include real Slack tokens, webhook secrets, private repository names,
  or production database files in issues, pull requests, logs, or screenshots.

## Local Setup

Install Go and the optional `just` task runner. The CI workflow currently uses
Go 1.25.10.

```sh
cp .env.example .env
cp mappings.example.yaml mappings.yaml
go mod download
```

Edit `.env` and `mappings.yaml` for any manual local testing. Keep `.env`,
`mappings.yaml`, `mappings.lock`, and local SQLite data out of commits unless a
change explicitly needs an example or fixture.

## Development Commands

The preferred local verification command is:

```sh
just check
```

The same checks are available without `just`:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

For faster iteration while developing:

```sh
go test ./...
go test ./internal/githubhook
```

## Pull Request Expectations

- Keep pull requests focused on one behavior, fix, or documentation topic.
- Add or update tests for behavioral changes.
- Update `README.md` or files in `docs/` when users, operators, or contributors
  need to know about the change.
- Prefer clear names and simple control flow over broad abstractions.
- Include manual verification notes when the change touches GitHub webhooks,
  Slack API behavior, Docker, or release packaging.

## Commit Messages

notifycat uses [Conventional Commits](https://www.conventionalcommits.org/)
together with [release-please](https://github.com/googleapis/release-please)
to automate versioning and the changelog. Squash-merge keeps the PR title as
the commit on `main`, and the PR-title lint workflow enforces the format —
so **your PR title is the commit message that release-please reads**.

| Type | Use for |
| --- | --- |
| `feat:` | new user-visible features (minor bump) |
| `fix:` | bug fixes (patch bump) |
| `docs:` | documentation only |
| `chore:` | maintenance / tooling |
| `refactor:` | internal change with no behavior change |
| `perf:` | performance improvement |
| `test:` | tests only |
| `build:` / `ci:` | build system or CI configuration |
| `feat!:` or a `BREAKING CHANGE:` footer | breaking change |

Pre-1.0 caveat: while the project is still on `0.x`, a breaking change bumps
the minor version (`0.1.0` → `0.2.0`). Once `1.0.0` ships, breaking changes
bump the major version per SemVer.

Keep the subject short and imperative. A scope is optional —
`feat(slack): …` is fine, `feat: …` is fine too.

## Reporting Bugs

Use the bug report issue template and include:

- notifycat version or commit SHA.
- Deployment mode: local binary, Docker, or another environment.
- Relevant configuration with secrets removed.
- GitHub webhook event type and a redacted payload excerpt if helpful.
- Expected behavior, actual behavior, and logs.

## Proposing Features

Use the feature request template and describe the workflow first. notifycat
should stay small, so new features should fit its core purpose: routing GitHub
pull request activity into clear Slack updates.

