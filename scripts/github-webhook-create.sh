#!/bin/sh
set -eu

usage() {
  cat <<'USAGE'
Create a GitHub repository webhook for notifycat.

Usage:
  ./scripts/github-webhook-create.sh owner/repo

Required:
  GITHUB_TOKEN            Fine-grained token with Webhooks: write on the repository
  GITHUB_WEBHOOK_SECRET   Secret shared by GitHub and notifycat, at least 32 characters
  NOTIFYCAT_PUBLIC_URL    Public HTTPS base URL, for example https://notifycat.example.com

Examples:
  GITHUB_TOKEN=github_pat_... GITHUB_WEBHOOK_SECRET=... NOTIFYCAT_PUBLIC_URL=https://notifycat.example.com ./scripts/github-webhook-create.sh octo/widget
  GITHUB_TOKEN=github_pat_... GITHUB_WEBHOOK_SECRET=... NOTIFYCAT_PUBLIC_URL=https://abc123.ngrok-free.app ./scripts/github-webhook-create.sh octo/widget
USAGE
}

fail() {
  printf 'error: %s\n\n' "$1" >&2
  usage >&2
  exit 1
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

repo=${1:-}
[ "$repo" ] || fail "repository is required in owner/name format"

case "$repo" in
  */*) ;;
  *) fail "repository must use owner/name format" ;;
esac

owner=${repo%%/*}
name=${repo#*/}

[ "$owner" ] || fail "repository owner is empty"
[ "$name" ] || fail "repository name is empty"

case "$owner" in
  *[!A-Za-z0-9._-]*) fail "repository owner contains unsupported characters" ;;
esac

case "$name" in
  *[!A-Za-z0-9._-]*) fail "repository name contains unsupported characters" ;;
esac

[ "${GITHUB_TOKEN:-}" ] || fail "GITHUB_TOKEN is required"
[ "${GITHUB_WEBHOOK_SECRET:-}" ] || fail "GITHUB_WEBHOOK_SECRET is required"
[ "${NOTIFYCAT_PUBLIC_URL:-}" ] || fail "NOTIFYCAT_PUBLIC_URL is required"

if ! command -v curl >/dev/null 2>&1; then
  fail "curl is required but was not found in PATH"
fi

secret_len=$(printf '%s' "$GITHUB_WEBHOOK_SECRET" | wc -c | tr -d ' ')
if [ "$secret_len" -lt 32 ]; then
  fail "GITHUB_WEBHOOK_SECRET must be at least 32 characters"
fi

case "$GITHUB_WEBHOOK_SECRET" in
  *\"* | *\\* | *'
'* | *'
'*) fail "GITHUB_WEBHOOK_SECRET must not contain quotes, backslashes, or newlines" ;;
esac

case "$NOTIFYCAT_PUBLIC_URL" in
  https://*) ;;
  *) fail "NOTIFYCAT_PUBLIC_URL must start with https://" ;;
esac

base_url=${NOTIFYCAT_PUBLIC_URL%/}
case "$base_url" in
  *\"* | *\\*) fail "NOTIFYCAT_PUBLIC_URL must not contain quotes or backslashes" ;;
esac

webhook_url="$base_url/webhook/github"
api_url="https://api.github.com/repos/$owner/$name/hooks"
response_file=$(mktemp)
trap 'rm -f "$response_file"' EXIT INT TERM

printf 'Creating GitHub webhook for %s -> %s\n' "$repo" "$webhook_url" >&2

http_status=$(
  curl -sS -L \
    -X POST "$api_url" \
    -H "Accept: application/vnd.github+json" \
    -H "Authorization: Bearer $GITHUB_TOKEN" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "Content-Type: application/json" \
    --data-binary @- \
    -o "$response_file" \
    -w '%{http_code}' <<EOF
{
  "name": "web",
  "active": true,
  "events": [
    "pull_request",
    "pull_request_review"
  ],
  "config": {
    "url": "$webhook_url",
    "content_type": "json",
    "secret": "$GITHUB_WEBHOOK_SECRET",
    "insecure_ssl": "0"
  }
}
EOF
)

case "$http_status" in
  200|201)
    if command -v jq >/dev/null 2>&1; then
      printf 'GitHub webhook created.\n'
      jq -r '
        "id: \(.id)",
        "active: \(.active)",
        "events: \(.events | join(","))",
        "url: \(.config.url)",
        "ping_url: \(.ping_url)",
        "",
        "Next step: open or update a pull request in the mapped repository and check notifycat logs."
      ' "$response_file"
    else
      printf 'GitHub webhook created. API response:\n'
      cat "$response_file"
      printf '\n'
    fi
    ;;
  *)
    printf 'error: GitHub API request failed with HTTP %s\n' "$http_status" >&2
    if command -v jq >/dev/null 2>&1; then
      jq . "$response_file" >&2 || cat "$response_file" >&2
    else
      cat "$response_file" >&2
      printf '\n' >&2
    fi
    exit 1
    ;;
esac
