# Agent API

The agent API lets a coding agent, such as Claude Code, review the pull requests of your teammates. The agent polls notifycat over HTTPS. Each poll returns the open pull requests (PRs) it has not seen yet. Notifycat never calls the agent.

The API serves PR metadata only: title, description, link, branch names, author id, and head commit SHA. It serves no code. The agent reads the diff from your git host with its own credentials.

This is for peer review. Engineers review their own PRs in their own pipelines.

## Turn it on

1. Set `agent_api.enabled: true` in `config.yaml`. See [`agent_api`](configuration.md#agent_api).
2. Set up the private admin port, the admin key, and the developer invites. [Agent API access](security.md#agent-api-access) has the steps. This page does not repeat them.
3. Restart notifycat.

With `agent_api.enabled: false`, notifycat records nothing for the feed and serves no `/api/v1/subs*` route.

When the API is on, notifycat records the open PRs of every repository in your `mappings:`. It learns about a PR from its webhook. A PR that was already open before you turned the feed on appears the next time it changes. A PR that never changes after that is never listed. Webhooks that arrive while notifycat is down are lost, so those PRs are missed too.

Serve the public API over HTTPS only. Your reverse proxy does this.

## Set up the agent

Each developer redeems their invite once and gets a token (`nct_<id>_<secret>`). Put the URL and the token in your shell profile:

```sh
# ~/.zshrc
export NOTIFYCAT_URL=https://notifycat.example.com
export NOTIFYCAT_TOKEN='nct_<id>_<secret>'
```

Subscribe once to the repositories you want to review:

```sh
curl -fsS -X POST -H @- "$NOTIFYCAT_URL/api/v1/subs" \
  -d '{"repositories":["acme/api","acme/web"]}' <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

The header comes from stdin (`-H @-`), so the token is not in the process list or the shell history.

Poll for new pull requests:

```sh
curl -fsS -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

Poll every minute or slower.

## Routes

All routes need `Authorization: Bearer nct_<id>_<secret>`. Every response with a body is JSON. All responses carry `Cache-Control: no-store`. Times are RFC 3339 in UTC, such as `2026-10-28T09:14:03Z`.

| Route | Result |
| --- | --- |
| `GET /api/v1/subs` | `200 {"subscriptions":[{"id":17,"repository":"acme/api","created_at":"2026-10-27T16:40:12Z"}]}`, ordered by `id`. |
| `POST /api/v1/subs` | Adds repositories. `200` with the full list, in the same shape as `GET`. |
| `DELETE /api/v1/subs/{id}` | `204`. `404 not_found` if the id is unknown, belongs to another token, or is not a positive integer. |
| `GET /api/v1/subs/prs` | The pull requests. See below. |

### Add subscriptions

The body is `{"repositories":["acme/api","acme/web"]}`. Use `["*"]` to subscribe to every configured repository.

- Names are lower-cased and de-duplicated. A repository you already follow is kept.
- An empty or missing list, unknown fields, or invalid JSON give `400 invalid_body`.
- A repository that is not in your `mappings:` gives `400` with the list of unknown names. Nothing is added.

    ```json
    {"error":"unknown_repository","repositories":["acme/unknown"]}
    ```

- More than 200 subscriptions for one token gives `400 too_many_subscriptions`. Nothing is added. Repositories you already follow do not count.
- The body can be at most 16 KiB.

### Get pull requests

```json
{
  "pull_requests": [
    {
      "repository": "acme/api",
      "number": 42,
      "title": "Add retry to the billing client",
      "description": "Retries 5xx responses up to three times.",
      "url": "https://bitbucket.org/acme/api/pull-requests/42",
      "author_id": "557058:2f1c…",
      "source_branch": "feature/billing-retry",
      "target_branch": "main",
      "head_sha": "9fceb02d0ae5",
      "created_at": "2026-10-27T16:40:12Z",
      "updated_at": "2026-10-28T09:14:03Z"
    }
  ]
}
```

- `created_at` is when the PR was opened on the git host. It is `null` when the webhook did not carry it.
- `updated_at` is when notifycat last saw a change to the PR.
- The list is ordered by `updated_at` (oldest first), then repository, then number.
- Nothing new, or no subscriptions, gives an empty list.

A call without a query returns the open, non-draft PRs of your subscribed repositories that your token has not received yet. Notifycat marks them as received in the same step.

`?since=<time>` returns every open PR of those repositories with `updated_at` at or after that time, whether you received it or not. It marks nothing as received.

`since` must be RFC 3339. Use the `Z` form, or write `+` as `%2B`. An unencoded `+` turns into a space and gives `400 invalid_since`. Let curl encode it:

```sh
curl -fsS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

An empty `since`, a value that is not RFC 3339, or a query that cannot be parsed (for example `?since=%zz`) gives `400 invalid_since`. Other query parameters are ignored.

`HEAD /api/v1/subs/prs` gives `405 method_not_allowed` with `Allow: GET`. A request that fails never marks PRs as received.

## Errors

The feed routes answer errors as `{"error":"<code>"}` with `Cache-Control: no-store`.

| Code | Status | When |
| --- | --- | --- |
| `unauthorized` | 401 | The token is missing, malformed, unknown, or wrong. The response has `WWW-Authenticate: Bearer`. |
| `invalid_body` | 400 | The `POST /subs` body is not valid. |
| `unknown_repository` | 400 | A repository is not configured. The body adds a `repositories` list. |
| `too_many_subscriptions` | 400 | The token would pass 200 subscriptions. |
| `invalid_since` | 400 | `since` or the query is not valid. |
| `not_found` | 404 | The subscription id is unknown or not yours. |
| `method_not_allowed` | 405 | `HEAD` on `/api/v1/subs/prs`. |
| `internal_error` | 500 | A store failure. Notifycat logs `agent api request failed`. |

A path that is not a feed route gets the plain-text 404 or 405 of the Go HTTP server, not JSON.

## Delivery rules

- Notifycat delivers a PR to a token **once**.
- A token is one reader. If you share a token between two agents, they share one list, and each PR goes to only one of them. Give each agent its own token.
- When a PR is closed, merged, or turned into a draft, notifycat forgets it. If it is reopened or marked ready later, it counts as new and your token receives it again.
- New commits on a PR do not send it again. Use `?since=` to read a PR again.
- A draft PR is never delivered.
- `?since=` re-reads and records nothing.

### Recover a lost list

If a run crashed after the poll and before it finished the reviews, the PRs are marked as received. Ask for them again with the time of the lost run:

```sh
curl -fsS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

## Review with Claude Code

Run `/loop 5m` in Claude Code with this prompt:

> Run `curl -fsS -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"`. For each pull request: check it is still open, read its diff, and post a review. Never print `$NOTIFYCAT_TOKEN`.

You can also run `claude -p` with the same prompt from cron.

### Read the diff and post the review

The agent needs a credential for your git host. That is separate from the notifycat token.

=== "GitHub"

    ```sh
    gh pr diff 42 --repo acme/api
    gh pr review 42 --repo acme/api --comment --body "Review text"
    ```

=== "Bitbucket"

    The REST API serves the diff and takes the comment:

    ```sh
    curl -fsS -u "$BITBUCKET_USER:$BITBUCKET_APP_PASSWORD" \
      "https://api.bitbucket.org/2.0/repositories/acme/api/pullrequests/42/diff"

    curl -fsS -u "$BITBUCKET_USER:$BITBUCKET_APP_PASSWORD" -X POST \
      -H "Content-Type: application/json" \
      -d '{"content":{"raw":"Review text"}}' \
      "https://api.bitbucket.org/2.0/repositories/acme/api/pullrequests/42/comments"
    ```

## Limits

- 200 subscriptions per token.
- 16 KiB request body.
- Notifycat misses PRs whose webhooks arrived while it was down.
- A PR that was open before you enabled the feed, and never changes again, is never listed.
- A repository that loses its mapping is no longer served or recorded.
- A stolen token reads PR titles, descriptions, branch names, author ids, links, and commit SHAs. No code. See [What a stolen token gets](security.md#what-a-stolen-token-gets).

## Logs

The feed logs `feed subscriptions added`, `feed subscription removed`, `feed pull requests delivered`, and `feed pull request not recorded`. See [Operations](operations.md#logging).
