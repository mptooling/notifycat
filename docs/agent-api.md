# Agent API

The agent API lets a coding agent, such as Claude Code, review the pull requests of your teammates. The agent polls notifycat over HTTPS. Each poll returns the open pull requests (PRs) it has not seen yet. Notifycat never calls the agent.

The API serves PR metadata only: title, description, link, branch names, author id, and head commit SHA. It serves no code. The agent reads the diff from your git host with its own credentials.

This is for peer review. Engineers review their own PRs in their own pipelines.

## Turn it on

1. Set `agent_api.enabled: true` in `config.yaml`. See [`agent_api`](configuration.md#agent_api).
2. Set up the private admin port, the admin key, and the developer invites. [Agent API access](security.md#agent-api-access) has the steps. This page does not repeat them.
3. Restart notifycat.

With `agent_api.enabled: false`, notifycat records nothing for the feed and serves no `/api/v1/subs*` route. It cannot see a PR close while the API is off, so at startup it removes every stored PR. Turning the API off and on again therefore starts the feed empty, like a first enable. Tokens stay, and so do subscriptions to mapped repositories. A PR comes back with its next recorded event and counts as new, so your token can receive it again.

When the API is on, notifycat records the open PRs of every repository in your `mappings:`. It learns about a PR from its webhook. A PR that was already open before you turned the feed on appears after its next [recorded event](#what-notifycat-records). A PR that gets no such event is never listed. Webhooks that arrive while notifycat is down are lost, so those PRs are missed too.

A repository that you remove from `mappings:` is no longer served. Its stored PRs and its subscriptions, for every token, are removed at the next start. If you map the repository again, subscribe again. With no repositories in `mappings:`, notifycat keeps all subscriptions.

Serve the public API over HTTPS only. Your reverse proxy does this.

### What notifycat records

Notifycat stores a PR, or refreshes its stored copy, on these webhook events:

| Git host | Events |
| --- | --- |
| GitHub | `opened`, `synchronize` (a push), `reopened`, `ready_for_review` |
| Bitbucket | The PR is created, and every update of an open PR that is not a draft |

Other events do not refresh a stored PR, such as a GitHub edit, a review, a comment, or a label. A new title or description shows up with the next event in the table. A close, a merge, or a switch to draft removes the PR. A draft PR is never stored.

## Set up the agent

Each developer redeems their invite once and gets a token (`nct_<id>_<secret>`). The admin sends you one line. Run it in bash or zsh within one hour, exactly as you got it:

```sh
curl -fsS -X POST -H 'Accept: text/plain' -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_<id>_<secret>'
```

It prints two lines. Paste them into your shell profile, such as `~/.zshrc`:

```sh
export NOTIFYCAT_URL=https://notifycat.example.com
export NOTIFYCAT_TOKEN='nct_<id>_<secret>'
```

The token is shown only once. Run `chmod 600 ~/.zshrc`, then open a new shell. If `server.domain` is not set, only the `NOTIFYCAT_TOKEN` line is printed, so keep your own `NOTIFYCAT_URL` line too.

If curl prints `error: 401`, the invite is used, expired, or the line was changed, for example by losing a quote. Ask the admin for a new invite. Without `Accept: text/plain`, the redeem call returns JSON: `{"user":"alice","token":"nct_<id>_<secret>"}`. The admin side is in [Agent API access](security.md#agent-api-access).

Subscribe once to the repositories you want to review:

```sh
curl --fail-with-body -sS -X POST -H @- "$NOTIFYCAT_URL/api/v1/subs" \
  -d '{"repositories":["acme/api","acme/web"]}' <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

The header comes from stdin (`-H @-`), so the token is not in the process list or the shell history. `--fail-with-body` needs curl 7.76 or newer. On a 4xx or 5xx it makes curl fail and still prints the JSON error.

Poll for new pull requests:

```sh
curl --fail-with-body -sS -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

Poll every minute or slower.

## Routes

All routes need `Authorization: Bearer nct_<id>_<secret>`. Every feed response with a body is JSON. All feed responses carry `Cache-Control: no-store`. Times are RFC 3339 in UTC, such as `2026-10-28T09:14:03Z`.

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

- An org tier such as `acme/*` cannot be subscribed to. It gives `400 unknown_repository`. Use `*` for all repositories, or list them.
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

- `author_id` is the git host's id for the author. GitHub gives the numeric user id as a string, such as `"4242"`. Bitbucket gives the `account_id`.
- `created_at` is when the PR was opened on the git host. It is `null` when the webhook did not carry it.
- `updated_at` is when notifycat last saw a change to the PR.
- The list is ordered by `updated_at` (oldest first), then repository, then number.
- Nothing new, or no subscriptions, gives an empty list.

A call without a query returns the open, non-draft PRs of your subscribed repositories that your token has not received yet. Notifycat marks them as received in the same step.

`?since=<time>` returns every open PR of those repositories with `updated_at` at or after that time, whether you received it or not. It marks nothing as received.

`since` must be RFC 3339. Use the `Z` form, or write `+` as `%2B`. An unencoded `+` turns into a space and gives `400 invalid_since`. Let curl encode it:

```sh
curl --fail-with-body -sS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

An empty `since`, a value that is not RFC 3339, or a query that cannot be parsed (for example `?since=%zz`) gives `400 invalid_since`. Other query parameters are ignored.

`HEAD /api/v1/subs/prs` gives `405 method_not_allowed` with `Allow: GET`. A request that fails never marks PRs as received. But once the server has answered, its PRs are marked, even if the response is lost on the way to you. See [Recover a lost list](#recover-a-lost-list).

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
- The first poll after you subscribe returns every open PR that notifycat knows for those repositories. There is no paging: they all come in one response.
- If you unsubscribe and subscribe again, PRs you already received are not sent again. You get only the PRs that are new to your token.

### Recover a lost list

If a run crashed after the poll and before it finished the reviews, the PRs are already marked as received. The same is true when the response was lost on the way to you. Ask for them again with `?since=`.

`since` matches a PR's last change (`updated_at`), not the time it was delivered. So use a time a few minutes before the last good poll that came before the lost one, or any earlier time. Do not use the exact time: `updated_at` comes from the notifycat server's clock, and a webhook can still be recording while you poll. An earlier time only returns more PRs. The time of the lost poll itself is too late: it misses the PRs that changed before it.

```sh
curl --fail-with-body -sS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

If the lost poll was your first one, or your first after a new subscription, use an old time such as `1970-01-01T00:00:00Z`. Then skip the PRs you already reviewed.

## Review with Claude Code

Run `/loop 5m` in Claude Code with this prompt:

> Run `curl --fail-with-body -sS -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"`. For each pull request: check it is still open, read its diff, and post a review. Never print `$NOTIFYCAT_TOKEN`.

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
    curl --fail-with-body -sS -u "$BITBUCKET_USER:$BITBUCKET_APP_PASSWORD" \
      "https://api.bitbucket.org/2.0/repositories/acme/api/pullrequests/42/diff"

    curl --fail-with-body -sS -u "$BITBUCKET_USER:$BITBUCKET_APP_PASSWORD" -X POST \
      -H "Content-Type: application/json" \
      -d '{"content":{"raw":"Review text"}}' \
      "https://api.bitbucket.org/2.0/repositories/acme/api/pullrequests/42/comments"
    ```

## Limits

- 200 subscriptions per token.
- 16 KiB request body.
- Notifycat misses PRs whose webhooks arrived while it was down.
- A PR that was open before you enabled the feed is listed only after one of the events in [What notifycat records](#what-notifycat-records).
- A repository that loses its mapping is no longer served or recorded. Its stored PRs and its subscriptions are removed at the next start. Subscribe again if it is mapped again.
- Turning the API off removes every stored PR at the next start. The feed starts empty when you turn it on again.
- A stolen token reads PR titles, descriptions, branch names, author ids, links, and commit SHAs. No code. See [What a stolen token gets](security.md#what-a-stolen-token-gets).

## Logs

The feed logs `feed subscriptions added`, `feed subscription removed`, `feed pull requests delivered`, `feed pull request not recorded`, `feed pull requests removed`, `feed subscriptions removed`, and `feed cleanup failed`. See [Operations](operations.md#logging).
