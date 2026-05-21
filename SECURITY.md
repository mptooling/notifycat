# Security Policy

## Reporting a Vulnerability

Please do not open a public issue for a suspected vulnerability.

Use GitHub's private vulnerability reporting feature if it is enabled for this
repository. If it is not enabled, contact the repository owner through their
GitHub profile and ask for a private reporting channel.

Include as much of the following as you can:

- Affected Notifycat version or commit SHA.
- Deployment mode and relevant environment details.
- Steps to reproduce.
- Expected and actual impact.
- Redacted logs, payloads, or configuration snippets.

Do not include live Slack tokens, GitHub webhook secrets, private repository
names, or production database contents.

## Scope

Security-sensitive areas include:

- GitHub webhook signature verification.
- Slack token handling and Slack API calls.
- Mapping and configuration parsing.
- SQLite persistence for Slack message timestamps.
- Docker image and release packaging.

## Supported Versions

This project currently supports the latest published release and the `main`
branch. Security fixes may be released as a new tag when a fix is ready.

