# Agent PR feed — review parties — design

> **Status (2026-11-03): approved by Pavlo.** Builds on [Agent PR feed — design (v1)](2026-10-27-agent-pr-feed-design.md).

## Why

Many developers subscribe their agents to the same repositories. Today every agent gets every PR. So one PR can get five agent reviews, and five agents do the same work.

A team wants to cap this. One agent review per PR is good, two is fine. The work must also spread fairly: an agent that already got more PRs waits, so another agent takes the next one.

Notifycat gives the team this tool. The team picks the numbers. It is not tied to the git host's "default reviewer" rule.

## Use case

Alice, Bob and Carol subscribe their agents to `cover42/billingservice` as a **party**. The cap is 1. A new PR opens. Only one of the three agents gets it. The next PR goes to an agent that got fewer PRs. Dave subscribes **solo** to the same repository. Dave's agent gets every PR, and the party does not count it.

## Decided by Pavlo

- **Each subscription has a type: `solo` or `party`.** The default is `solo`.
- **`solo`** works like today: the agent gets every PR. Solo deliveries never count toward a party.
- **`party`**: all party subscribers of one repository form that repository's party. Parties have no names. There is one party per repository.
- **A cap limits how many party members get each PR.** The setting is `party_reviewers`.
- **The admin sets the cap in `config.yaml`.** It can be set globally, on a repository, and on a monorepo path. The most specific level wins. Global 1, repository 2, path 4 → 4. Global 1, repository 2 → 2.
- **Take turns.** A party member that got more PRs than another member waits its turn.
- **A review timer, `party_deadline`, limits the wait.** Teams that want speed set a short timer. When the timer runs out, turns stop for that PR. Then the first party member who fetches gets it. This can be a member with a high load. The cap still applies.
- **The timer is 15 minutes to 24 hours.** Some agents review for more than 10 minutes. A shorter wait would treat a busy agent as offline. The timer replaces the earlier 10-minute "active" rule.
- **The timer counts clock time.**
- **The feed has no days off.** A normal fetch returns PRs on every day, for solo and party.
- **Away comes from fetch events only.** A token is away until its first fetch. The first fetch makes it active at once. A token that does not fetch its PRs for 24 hours of clock time is away again.
- **Load is a counter per day.** Each day it starts again at 0, at 00:00 UTC.
- **A party with one member has no real cap.** That member gets every PR.
- **A new endpoint changes a subscription's type**, so a developer can join or leave a party without deleting the subscription.
- The git host's "default reviewer" rule is out of scope. The team decides how it uses parties.
- **Party settings are for agents. Slack is for people. They are separate.** A cap or a timer must never change a Slack notification.
- **2026-11-04: no days off and no calendar in the feed.** Pavlo: "let's drop weekends. Bot will receive notifications for PR review at anytime they are ready. It is bot, not user."

## My calls (check these)

1. **The cap levels**, most specific first: monorepo path → repository tier → org `"*"` tier → `agent_api.party_reviewers` → built-in default **1**. This is the same order as the other tier keys.
2. **The cap of a PR is the highest cap of its changed files.** More reviews is the safer side. The cap of one file comes from the most specific path rule that matches the file and sets `party_reviewers`. A nested rule without a cap takes its parent's cap. A file that no capped rule matches takes the repository's cap. Example: repository 2, `docs` 1, a PR touches `docs/a.md` and `README.md` → 2. Example: `services` 4, `services/billing` without a cap, a PR touches `services/billing/x.go` → 4.
3. **A member is a token.** Two tokens of one person are two members.
4. **The most specific subscription sets the type.** A token's subscription to `cover42/api` sets its type for that repository. Its `*` subscription sets the type only for repositories it has no own subscription for.
5. **Load** is the number of PRs a member got through the party of that repository **today**. A day starts at 00:00 UTC. Load stays when a PR closes or merges. Load is per repository, not across all repositories.
6. **A fetch** is a normal `GET /api/v1/subs/prs` call without `since`. Other calls do not change the away state. An away member is not a candidate, so a member on leave never holds PRs back. At release no token has fetched its PRs, so every member starts as away. Notifycat fills nothing from older data.
7. **The turn rule** is in "How it works". A PR that a member takes in one fetch raises that member's load for the rest of the same fetch. So one fetch takes at most a fair share, not every waiting PR.
8. **`POST /api/v1/subs` takes an optional `type`.** It applies only to the repositories that call adds. An existing subscription keeps its type; `PATCH` changes it.
9. **`PATCH /api/v1/subs` changes the type of many subscriptions in one call.** A developer with 50 repositories should not need 50 calls. See "API changes".
10. **`?since=` on a party repository returns only PRs that this token received.** Without this rule, `since` would hand an agent PRs that the party gave to other members. Solo repositories work like today.
11. **Switching type never resends and never takes back.**
    - Party → solo: the agent then gets every PR it has not received yet. This includes PRs that the party gave to other members.
    - Solo → party: PRs it got as solo do not count toward the cap or its load.
12. **Notifycat finds the cap when it records the PR** and stores it with the PR. It finds the cap again on every recorded event and replaces the stored value. So a push that touches a new path can raise the cap, and a config change applies to open PRs at their next event. A lower cap never takes a PR back.
13. **Path caps need the PR's changed files.** Notifycat reads them with the read token, the same way monorepo routing does. If there is no read token, the PR takes the repository's cap. If the call fails, the PR keeps its stored cap (a new PR takes the repository's cap), and notifycat logs a warning. So a short git host error never lowers a cap. Notifycat reads the files only when a path rule of that repository sets `party_reviewers`.
14. **A reopened PR goes to the party again.** Closing a PR removes its deliveries, as today. Its count for today stays.
15. **`party_reviewers` is an integer of 1 or more.** Other values fail at startup. It is allowed on the global section, the org `"*"` tier, a repository tier, and a path entry. Path entries exist only on named repository tiers, as today.
16. **Cap-only entries.** A tier or path entry that sets only `party_reviewers` is a cap rule. Slack routing ignores it: it never changes a channel, a mention, or which path rule wins for Slack. It also never adds a repository to the feed. A `"*"` tier that sets only `party_reviewers` sets the cap for the org's routed repositories and routes nothing. A cap rule for a repository that no tier routes fails at startup, because it would do nothing. On an entry that also routes Slack, `party_reviewers` changes nothing for Slack either.
17. **The API stays `/api/v1`.** All changes add to it. Agents that never send `type` work as before.
18. **`party_deadline` uses the same levels as `party_reviewers`**: path → repository tier → org `"*"` tier → `agent_api.party_deadline` → built-in default **1 hour**. Calls 12, 13 and 16 apply to it too. Notifycat stores it with the PR and refreshes it on each recorded event. A timer-only entry never touches Slack.
19. **A PR that matches several paths takes the shortest timer** of its changed files. A team that needs speed gets it. Fairness is the softer need.
20. **The timer format is hours and minutes**: `15m`, `2h`, `1h30m`. Seconds, and values below 15 minutes or above 24 hours, fail at startup.
21. **The clock starts when notifycat first records the PR**: opened, reopened, or ready for review. A push does not restart it. Closing a PR or converting it to a draft removes it from the feed, as today. So the next record starts a new clock.

## Non-goals

Named parties. More than one party per repository. Fairness across repositories. A step to give back a PR that an agent will not review. Moving a PR to another member when the winner never reviews it. Hiding a member's own PRs from it. Weights per member. Changing the cap through the API. Showing party members to developers or admins. Slack messages about parties.

## Acceptance criteria

- A solo subscription gets every PR, like today.
- A PR goes to at most `party_reviewers` party members. This is also true when members fetch at the same time.
- Solo deliveries never count toward the cap.
- Before the timer of a PR runs out, turns apply. A member does not get the PR while candidates with a lower load could fill all free slots.
- After `party_deadline` of clock time, the first party member who fetches gets the PR, within the cap.
- A normal fetch returns PRs on every day, also on weekends and holidays, for solo and party.
- An away member never makes others wait.
- A token that never fetched its PRs is away. Its first fetch makes it a candidate.
- A party where only one member is not away gets every PR at once.
- `party_deadline` below 15 minutes or above 24 hours fails at startup.
- The most specific `party_reviewers` wins: path, then repository, then org `"*"`, then global, then 1.
- `?since=` on a party repository never returns a PR that went to another member.
- Agents that do not send `type` see no change.
- Adding, changing or removing `party_reviewers` or `party_deadline` anywhere must not change a Slack notification.
- Tokens must not appear in logs.

## How it works

### Words

- **Party of R**: the tokens whose subscription type for repository R is `party`.
- **Party delivery**: a PR that a member got through the party rule.
- **Cap of P**: the `party_reviewers` value stored with PR P.
- **Load of T in R**: party deliveries T got in R today.
- **Away**: no fetch yet, or no fetch in the last 24 hours of clock time.
- **Timer of P**: the `party_deadline` value stored with PR P. It runs out when that much clock time has passed since notifycat first recorded P.

### A normal fetch by token T

1. **Solo repositories**: the same as today. T gets every open PR it has not received.
2. **Party repositories**: notifycat checks each open PR P of R that T has not received, oldest `updated_at` first:
   1. If P already has as many party deliveries as its cap, skip P. P is taken.
   2. If the timer of P ran out, go to step 5. Turns stop.
   3. The candidates are T, plus the members of R that are not away and have not received P.
   4. The free slots are the cap minus the party deliveries of P. If the candidates with a lower load than T could fill all free slots, skip P. It is not T's turn. With a cap of 1, this means: skip P when any candidate has a lower load.
   5. T gets P as a party delivery, and the load of T goes up by 1.
3. Notifycat records the fetch time of T.

All of step 2 happens in one atomic step. Two members that fetch at the same time never pass the cap.

### Example

The cap is 1 and the timer is 30 minutes. Alice, Bob and Carol are not away. Their loads today are 3, 1 and 1. PR 42 opens on Tuesday at 10:00.

| Time | Who fetches | Result |
| --- | --- | --- |
| 10:00 | Alice | Skipped. Bob and Carol have a lower load. |
| 10:05 | Bob | Gets PR 42. His load is now 2. |
| 10:06 | Carol | Skipped. PR 42 is taken. |

If Bob and Carol are busy with long reviews and do not fetch, notifycat skips Alice until 10:30. At 10:30 the timer runs out, and Alice gets PR 42 on her next fetch.

If Bob and Carol are away, Alice is the only candidate. She gets PR 42 at 10:00.

With a cap of 3, Alice gets PR 42 at once. Only two candidates have a lower load, and three slots are free.

## Config

```yaml
agent_api:
  enabled: true
  party_reviewers: 1          # global; default 1
  party_deadline: 1h          # global; default 1h; 15m to 24h

mappings:
  cover42:
    "*":
      party_reviewers: 1      # org tier; cap only, routes nothing
    protorepo:
      party_reviewers: 2      # repository tier
      paths:
        billing:
          party_reviewers: 4  # monorepo path; Slack ignores it
          party_deadline: 15m
```

## API changes

| Route | Change |
| --- | --- |
| `GET /api/v1/subs` | Each subscription gets `"type": "solo"` or `"type": "party"`. |
| `POST /api/v1/subs` | Optional `"type"` in the body, default `"solo"`. It applies to the repositories this call adds. Any other value → `400 invalid_type`, and nothing is added. |
| `PATCH /api/v1/subs` (new) | Body `{"type": "party"}` changes every subscription of the token. Body `{"repositories": ["cover42/api", "*"], "type": "party"}` changes only those subscriptions. `"*"` means the `*` subscription. `200` with the full list, same shape as `GET`. A repository the token has not subscribed to → `400 not_subscribed` with the list, and nothing changes. Bad `type` → `400 invalid_type`. Unknown fields or a bad body → `400 invalid_body`. Body max 16 KiB. |
| `GET /api/v1/subs/prs` | The party rule applies on party repositories. The PR shape does not change. `?since=` follows call 10. |

## Design

### `internal/feed`

- `domain`: a `SubscriptionType` enum (`solo`, `party`) in `enums.go`. The 24-hour away window goes in `constants.go`. New ports for the fetch time, the daily load counters, and the cap and timer lookup. The feed uses no calendar. The timer counts clock time, and the load day is the UTC date. The turn rule is a pure domain function: it takes a snapshot (candidates, their loads, the cap, the deliveries of P) and returns the PRs that T takes. A use case for changing types, with its interface in `interfaces.go`.
- `application`: `Subscriptions` gets the type on add and the change-type use case. `PullRequests.Next` splits repositories into solo and party. `Since` filters party repositories. `Recorder` stores the cap, the timer and the first record time with the PR.
- `infrastructure`: the `PATCH` handler, `type` in the JSON, and an adapter that finds the cap and the timer through the `routing` domain.

### `internal/routing`

It owns `mappings:`, so it parses and validates `party_reviewers` and `party_deadline` on tiers and paths. It answers one question: "what are the cap and the timer for this repository and these changed files?" Party rules use their own matcher. The Slack path matcher, the base targets, `config.lock` hashes and the validation checks never see an entry that sets only party keys.

### Persistence (suggested; the builder may change it)

New migration `00012`:

- `feed_subscriptions.type TEXT NOT NULL DEFAULT 'solo'`.
- `feed_pull_requests.party_reviewers INTEGER NOT NULL DEFAULT 1`, `party_deadline_minutes INTEGER NOT NULL DEFAULT 60`, and `recorded_at DATETIME`: the first record time, where the timer starts. The existing `created_at` is the git host's PR time, so it cannot be used.
- `feed_deliveries.party BOOLEAN NOT NULL DEFAULT 0`: true for a party delivery. The cap counts these rows. They go when the PR closes, as today.
- `feed_party_loads (token_id, repository, day, taken, PRIMARY KEY (token_id, repository, day))`: the daily load counter. `day` is the UTC date. Each party delivery adds 1 to `taken`, in the same transaction. It has no foreign key to the PR, so it stays when the PR closes. It has a foreign key to `access_tokens` with `ON DELETE CASCADE`. Rows of earlier days are removed at startup and every 24 hours.
- `feed_fetches (token_id PRIMARY KEY, fetched_at)`: the last normal fetch, one upsert per fetch. It has a foreign key to `access_tokens` with `ON DELETE CASCADE`. No backfill. The away check is plain SQL: a member is a candidate when `fetched_at >= now - 24h`. No state column and no background job.
- The party claim runs in one SQLite write transaction. The store loads the snapshot, calls the domain turn function, and inserts the deliveries. SQLite has one writer at a time, so two fetches cannot both pass the cap.

### Config (`internal/platform/config`)

`agent_api.party_reviewers`, default 1, integer of 1 or more. `agent_api.party_deadline`, default `1h`, from `15m` to `24h`.

## Logs

| Message | Level | Fields |
| --- | --- | --- |
| `feed pull requests delivered` | info | Adds `party`: how many of `count` came through a party. |
| `feed party pull requests held` | debug | `token_id`, `taken` (PRs at their cap), `waiting` (not this token's turn). Only when one of them is more than 0. |
| `feed party settings fallback` | warn | `repository`, `number`, `err`. Notifycat could not read the changed files. The PR kept its stored cap and timer, or took the repository's values. |

## Docs

- `docs/agent-api.md`: a new section "Review as a party", the `type` field, `PATCH /api/v1/subs`, and the `since` rule.
- `docs/configuration.md`: `agent_api.party_reviewers` and `agent_api.party_deadline`.
- `docs/mappings.md` and `docs/monorepo.md`: the tier key and the path key.
- `docs/operations.md`: the new log lines.

## Known limits

1. **An agent can get its own user's PR.** The feed does not know which token belongs to which `author_id`. With a cap of 1, that PR may then get no real review.
2. **A winner that never reviews keeps the slot.** The timer only ends the turns. It never takes a PR back.
3. **The first minutes after the release are loose.** Every member is away until its first fetch.
4. **A member that joins during the day catches up.** It starts with load 0. So it gets most party PRs until its load reaches the others' loads. This ends at 00:00 UTC, when every count starts again.

## Delivery in 2 PRs

1. `feat(feed): review parties for shared repositories`: subscription type, `POST` type, `PATCH /api/v1/subs`, the party rule with turns and the timer, the `since` rule, the global `agent_api.party_reviewers` and `agent_api.party_deadline`, migration, docs.
2. `feat(feed): party settings per repository and monorepo path`: the tier and path keys for both settings, the lookup at record time, the changed-files lookup.

## Verification

- Unit tests for the turn rule: cap reached, not your turn, away members, one member, the load rising inside one fetch, the timer running out, the load starting again at 0 at 00:00 UTC.
- Clock tests: a timer that crosses a weekend counts clock time, and a normal fetch on a Saturday returns PRs.
- A race test: many members fetch the same PR at the same time, and party deliveries never pass the cap.
- An integration test through the fx app: two party tokens and one solo token on one repository.
- Config tests for each level of both settings, the highest path cap, the shortest path timer, and the timer limits (`14m`, `15m`, `24h`, `25h`, `30s`).
- A routing test: the Slack targets of a PR are the same with and without party-only entries, on a path, a repository tier and a `"*"` tier.
- vet, golangci-lint v2.14.0, `go test -race ./...`, govulncheck, `mkdocs build --strict`.
