#!/bin/sh
set -eu

manifest_file=${SLACK_APP_MANIFEST:-docs/slack-app-manifest.json}

usage() {
  cat <<'USAGE'
Create a Slack app from the notifycat app manifest.

Required:
  SLACK_APP_CONFIG_TOKEN  Slack app configuration token from https://api.slack.com/apps

Optional:
  SLACK_TEAM_ID           Workspace/team ID when using an org-level configuration token
  SLACK_APP_MANIFEST      Manifest path, defaults to docs/slack-app-manifest.json

Examples:
  SLACK_APP_CONFIG_TOKEN=xoxe-your-token just slack-app-create
  SLACK_APP_CONFIG_TOKEN=xoxe-your-token SLACK_TEAM_ID=T123 just slack-app-create
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

[ "${SLACK_APP_CONFIG_TOKEN:-}" ] || fail "SLACK_APP_CONFIG_TOKEN is required"

if ! command -v curl >/dev/null 2>&1; then
  fail "curl is required but was not found in PATH"
fi

if [ ! -f "$manifest_file" ]; then
  fail "manifest file not found: $manifest_file"
fi

if [ ! -s "$manifest_file" ]; then
  fail "manifest file is empty: $manifest_file"
fi

response_file=$(mktemp)
trap 'rm -f "$response_file"' EXIT INT TERM

printf 'Creating Slack app from %s...\n' "$manifest_file" >&2

set -- \
  -fsS \
  -X POST \
  "https://slack.com/api/apps.manifest.create" \
  -H "Authorization: Bearer $SLACK_APP_CONFIG_TOKEN" \
  --data-urlencode "manifest@$manifest_file"

if [ "${SLACK_TEAM_ID:-}" ]; then
  set -- "$@" --data-urlencode "team_id=$SLACK_TEAM_ID"
fi

if ! curl "$@" >"$response_file"; then
  printf 'error: Slack API request failed\n' >&2
  exit 1
fi

if command -v jq >/dev/null 2>&1; then
  ok=$(jq -r '.ok // false' "$response_file")
  if [ "$ok" != "true" ]; then
    printf 'error: Slack rejected the manifest\n' >&2
    jq . "$response_file" >&2
    exit 1
  fi

  printf 'Slack app created.\n'
  jq -r '
    "app_id: \(.app_id)",
    "app_settings_url: https://api.slack.com/apps/\(.app_id)",
    "",
    "Next step: open the app settings URL, go to Install App, install it to the workspace, and copy the Bot User OAuth Token."
  ' "$response_file"
else
  printf 'Slack API response:\n'
  cat "$response_file"
  printf '\n\nNext step: open https://api.slack.com/apps, select the created app, go to Install App, install it to the workspace, and copy the Bot User OAuth Token.\n'
fi
