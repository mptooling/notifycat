# Agent API

The agent API lets a coding agent, such as Claude Code, review the pull requests of your teammates. The agent polls notifycat over HTTPS. Each poll returns the open pull requests (PRs) it has not seen yet. Notifycat never calls the agent.

The API serves PR metadata only: title, description, link, branch names, author id, and head commit SHA. It serves no code. The agent reads the diff from your git host with its own credentials.

This is for peer review. Engineers review their own PRs in their own pipelines.

## Turn it on

1. Set `agent_api.enabled: true` in `config.yaml`. See [`agent_api`](configuration.md#agent_api).
2. Set up the private admin port, the admin key, and the developer invites. [Agent API access](security.md#agent-api-access) has the steps. This page does not repeat them.
3. Restart notifycat.

With `agent_api.enabled: false`, notifycat records nothing for the feed and serves no `/api/v1/subs*` or `/api/v1/repos` route. It cannot see a PR close while the API is off, so at startup it removes every stored PR. Turning the API off and on again therefore starts the feed empty, like a first enable. Tokens stay, and so do subscriptions to mapped repositories. A PR comes back with its next recorded event and counts as new, so your token can receive it again.

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

You redeem your invite once and get a token (`nct_<id>_<secret>`). The admin sends you one line. Run it in bash or zsh within one hour, exactly as you got it:

```sh
curl -fsS -X POST -H 'Accept: text/plain' -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_<id>_<secret>'
```

It prints two lines. Paste them into your shell profile, such as `~/.zshrc`:

```sh
export NOTIFYCAT_URL=https://notifycat.example.com
export NOTIFYCAT_TOKEN='nct_<id>_<secret>'
```

The token is shown only once. Run `chmod 600 ~/.zshrc`, then open a new shell.

If the line has `$NOTIFYCAT_URL` instead of an address, the server has no `server.domain`. Then run `export NOTIFYCAT_URL=https://notifycat.example.com` first, with your notifycat address. The command then prints only the `NOTIFYCAT_TOKEN` line, so add your `NOTIFYCAT_URL` line to your profile too.

If curl prints `error: 401`, the invite is used, expired, or the line was changed, for example by losing a quote. Ask the admin for a new invite. Without `Accept: text/plain`, the redeem call returns JSON: `{"user":"alice","token":"nct_<id>_<secret>"}`. The admin side is in [Agent API access](security.md#agent-api-access).

See which repositories you can follow:

```sh
curl --fail-with-body -sS -H @- "$NOTIFYCAT_URL/api/v1/repos" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

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
| `GET /api/v1/subs` | `200 {"subscriptions":[{"id":17,"repository":"acme/api","type":"solo","created_at":"2026-10-27T16:40:12Z"}]}`, ordered by `id`. `type` is `solo` or `party`. |
| `POST /api/v1/subs` | Adds repositories. `200` with the full list, in the same shape as `GET`. |
| `PATCH /api/v1/subs` | Changes the type of subscriptions. `200` with the full list, in the same shape as `GET`. See below. |
| `DELETE /api/v1/subs/{id}` | `204`. `404 not_found` if the id is unknown, belongs to another token, or is not a positive integer. |
| `GET /api/v1/subs/prs` | The pull requests. See below. |
| `GET /api/v1/repos` | The repositories you can subscribe to. See below. |

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
- The body can also have `"type":"solo"` or `"type":"party"`. The default is `solo`. The type applies only to the repositories that this call adds. A repository you already follow keeps its type. Any other value gives `400 invalid_type`, and nothing is added.

### Change the type

`PATCH /api/v1/subs` changes the type of subscriptions that already exist. Use it to join or leave a party without a delete. There are two body forms.

To change every subscription of the token, send only `type`:

```json
{"type": "party"}
```

To change some subscriptions, add `repositories`:

```json
{"repositories": ["acme/api"], "type": "solo"}
```

- Only an absent `repositories` key means every subscription. `"repositories": []`, `"repositories": null`, and a list that is empty after trimming, such as `[""]`, give `400 invalid_body`.
- Names are trimmed, lower-cased, and de-duplicated, as in `POST`. `"*"` means your `*` subscription.
- A missing or unknown `type` gives `400 invalid_type`.
- A repository that you do not follow gives `400 not_subscribed` with the list of these names, lower case and sorted. Nothing changes.

    ```json
    {"error":"not_subscribed","repositories":["acme/web"]}
    ```

- An unknown field, data after the JSON object, or invalid JSON gives `400 invalid_body`.
- The body can be at most 16 KiB.
- A change of type never sends a PR again and never takes one back. See [Switch the type](#switch-the-type).

### List repositories

```json
{"repositories":["acme/api","acme/web"],"organizations":["beta"]}
```

- `repositories` are the repositories in your `mappings:`, lower case and sorted.
- `organizations` have an org tier, such as `beta/*`. You can subscribe to every repository of such an organization. Notifycat does not list them. Your git host does.
- Both lists can be empty.

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

A call without a query is a normal poll. It returns the open, non-draft PRs of your subscribed repositories that your token has not received yet. Notifycat marks them as received in the same step. On a party repository, the [party rule](#review-as-a-party) decides which PRs you get. On a day off, a normal poll returns an empty list. See [Days off](#days-off).

`?since=<time>` reads PRs again. It returns each open PR that matches one of these rules:

- The PR changed at or after that time (`updated_at`). On a solo repository, this rule applies whether you received the PR or not.
- Your token received the PR at or after that time.

On a party repository, it returns only the PRs that your token received, solo or party. It marks nothing as received.

`since` must be RFC 3339. Use the `Z` form, or write `+` as `%2B`. An unencoded `+` turns into a space and gives `400 invalid_since`. Let curl encode it:

```sh
curl --fail-with-body -sS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

An empty `since`, a value that is not RFC 3339, or a query that cannot be parsed (for example `?since=%zz`) gives `400 invalid_since`. Other query parameters are ignored.

`HEAD /api/v1/subs/prs` gives `405 method_not_allowed` with `Allow: GET`. A `4xx` error never marks PRs as received. A `5xx` error can come after the server marked PRs. Once the server answers, its PRs stay marked, even if the response is lost on the way to you. See [Recover a lost list](#recover-a-lost-list).

## Errors

The feed routes answer errors as `{"error":"<code>"}` with `Cache-Control: no-store`.

| Code | Status | When |
| --- | --- | --- |
| `unauthorized` | 401 | The token is missing, malformed, unknown, or wrong. The response has `WWW-Authenticate: Bearer`. |
| `invalid_body` | 400 | The `POST /subs` or `PATCH /subs` body is not valid. |
| `invalid_type` | 400 | `type` is not `solo` or `party`. On `PATCH`, a missing `type` gives this code too. |
| `unknown_repository` | 400 | A repository is not configured. The body adds a `repositories` list. |
| `not_subscribed` | 400 | A `PATCH` names a repository that the token does not follow. The body adds a `repositories` list. |
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
- On a party repository, the party rule limits who gets a PR. See [Review as a party](#review-as-a-party).
- The first poll after you subscribe returns every open PR that notifycat knows for those repositories. There is no paging: they all come in one response.
- If you unsubscribe and subscribe again, PRs you already received are not sent again. You get only the PRs that are new to your token.

### Days off

On Saturday and Sunday, a normal poll returns an empty list and marks no PR as received. It still records the poll time, so the poll counts for the away check. This is true for solo and party subscriptions. When `digest.country` is set, public holidays are days off too. A day off is a whole calendar day in `digest.timezone`, or in UTC when that key is not set. The [digest calendar](digest.md#weekends-and-holidays) decides this.

A PR that opens on a day off waits for the next working day. `?since=` still works on a day off, because it is a manual read.

### Recover a lost list

If a run crashed after the poll and before it finished the reviews, the PRs are already marked as received. The same is true when the response was lost on the way to you. Ask for them again with `?since=`.

If a normal poll returns a 5xx error, the server may have marked PRs before it failed. Call `GET /api/v1/subs/prs?since=<time of the last good poll>` to get the PRs that the failed poll may have marked. On a party repository, `since` returns only PRs that this token received.

`since` matches the time that notifycat delivered a PR to your token. The lost poll came after the last good poll. So the time of the last good poll, on the server clock, returns every PR that the lost poll marked. This includes a PR that changed long before the lost poll. An example is a PR that waited for a party turn or a working day.

`since` also matches the last change of a PR (`updated_at`). A webhook can still record a change while you poll. Both times come from the clock of the notifycat server, and your clock can differ from it. To get these changes, use a time a few minutes before the last good poll. This time also covers a clock difference. An earlier time only returns more PRs.

```sh
curl --fail-with-body -sS --get --data-urlencode "since=2026-10-28T09:00:00Z" \
  -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

If the lost poll was your first one, or your first after a new subscription, use an old time such as `1970-01-01T00:00:00Z`. Then skip the PRs you already reviewed.

## Review as a party

By default every subscription is `solo`. A solo agent gets every PR, as described above. Solo deliveries never count toward a party.

Many agents can follow the same repository. A team can limit how many agents review each PR. For this, the agents subscribe with the type `party`.

- **Party**: all tokens with a `party` subscription to one repository. A repository has one party. Parties have no names.
- **Member**: a token. Two tokens of one person are two members.
- **Cap**: the number of party members that get each PR. Five levels set it. The most specific wins: a monorepo path, a repository tier, the org `"*"` tier, [`agent_api.party_reviewers`](configuration.md#agent_api), and the built-in default of 1. See [Mappings](mappings.md#behavioral-overrides) and [Party settings per path](monorepo.md#party-settings-per-path).
- **Load**: the number of PRs that a member got through the party of that repository today. Load counts per repository. Load stays when a PR closes. It starts again at 0 each day.
- **Away**: a token that has not polled yet, or has not polled for 24 hours. Only a normal poll counts. A `since` call does not change the away state. An away member is not a candidate and never holds PRs back.
- **Timer**: the same five levels set it, with [`agent_api.party_deadline`](configuration.md#agent_api) and a default of 1 hour. See [Mappings](mappings.md#behavioral-overrides) and [Party settings per path](monorepo.md#party-settings-per-path). The timer of a PR starts when notifycat first records the PR. A push does not restart it. Days off do not count.

A normal poll treats each open PR that you have not received on a party repository in this order. The oldest `updated_at` comes first.

1. If the PR already went to as many members as the cap allows, skip it.
2. If the timer of the PR ran out, turns stop. You get the PR, within the cap.
3. Find the candidates. These are you and the other members that are not away and have not received the PR.
4. Count the free slots. This is the cap minus the members that already got the PR.
5. If the candidates with a lower load than yours could fill all free slots, skip the PR. It is not your turn.
6. Otherwise you get the PR, and your load goes up by 1. This also counts for the other PRs in the same poll.

Notifycat does all of this in one atomic step. Two members that poll at the same moment never pass the cap. With a cap of 1, a PR is skipped when any candidate has a lower load.

A party with one candidate has no real cap. That member gets every PR at once.

### Example

The cap is 1 and the timer is 30 minutes. Alice, Bob, and Carol are not away. Their loads today are 3, 1, and 1. PR 42 opens on Tuesday at 10:00.

| Time | Who polls | Result |
| --- | --- | --- |
| 10:00 | Alice | Skipped. Bob and Carol have a lower load. |
| 10:05 | Bob | Gets PR 42. His load is now 2. |
| 10:06 | Carol | Skipped. PR 42 is taken. |

If Bob and Carol are busy with long reviews and do not poll, Alice is skipped until 10:30. At 10:30 the timer runs out, and Alice gets PR 42 on her next poll.

If Bob and Carol are away, Alice is the only candidate. She gets PR 42 at 10:00.

With a cap of 3, Alice gets PR 42 at once. Only two candidates have a lower load, and three slots are free.

PR 43 opens on Friday at 23:50 with a 30-minute timer. The weekend does not count, so its timer runs out on Monday at 00:20. No agent gets it on Saturday or Sunday.

### Switch the type

- The most specific subscription sets the type. The type of a subscription to `acme/api` is the type for that repository. Your `*` subscription sets the type only for repositories that you have no own subscription for.
- Party to solo: you then get every PR that you have not received yet. This includes PRs that the party gave to other members.
- Solo to party: the PRs that you got as solo do not count toward the cap or your load.
- A reopened PR goes to the party again. Closing a PR removes its deliveries, but its count for the day stays.

### Limits of a party

- The feed does not know which token belongs to the author of a PR. An agent can get a PR of its own user.
- A member that gets a PR and never reviews it keeps the slot. The timer only ends the turns. It never takes a PR back.
- A revoked token frees its party slots. Another member can then get those PRs.
- A member that joins during the day starts with load 0. It gets most party PRs until its load reaches the others.
- When parties first go live, no token has polled yet, so every member is away until its first poll. The first agent that polls can take the PRs that waited. The same happens on Monday morning, after a weekend without polls.
- Party settings change nothing in Slack.
- Notifycat finds the cap and the timer again on every recorded event. A config change reaches a PR at its next event. A lower cap never takes a PR back from a member that already has it.

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

The feed logs `feed subscriptions added`, `feed subscriptions changed`, `feed subscription removed`, `feed pull requests delivered`, `feed party pull requests held`, `feed day off`, `feed pull request not recorded`, `feed pull requests removed`, `feed subscriptions removed`, and `feed cleanup failed`. See [Operations](operations.md#logging).
