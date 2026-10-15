# Notification strategies

Each mapped repository has a notification strategy. It decides **when** Notifycat tells Slack about a new PR.

| Strategy | What the channel sees |
| --- | --- |
| `per-event` (default) | One message per PR, posted the moment the PR opens, then updated with reviews, merge, and close. |
| `scheduled` | Nothing at once. New PRs wait in a queue. On a schedule, **one message per channel** lists every PR queued since the last send. |

With no `notifications:` block anywhere, every repository uses `per-event`. That is how Notifycat always worked.

## Turning it on

Set `notifications:` on a mapping tier — an org `"*"` tier or a repository tier:

```yaml
mappings:
  acme:
    "*":
      channel: C0TEAMDEFAULT
      notifications:
        strategy: scheduled
        mode: hourly                 # every hour at :00
    payments:
      channel: C0PAYMENTS0
      notifications:
        strategy: scheduled
        mode: at
        at: ["10:30", "15:00"]       # HH:MM, in digest.timezone
    billing:
      channel: C0BILLING00
      notifications:
        strategy: scheduled
        mode: cron
        cron: ["0 9 * * *", "0 14 * * *"]
    infra:
      channel: C0INFRA0000
      notifications:
        strategy: per-event          # opt out of the org "*" schedule
```

Each team picks its own strategy. In this example, `acme/infra` keeps instant messages while every other `acme` repository gets lists.

## Modes

| `mode` | Extra key | Sends |
| --- | --- | --- |
| `hourly` | none | Every hour at :00, around the clock. |
| `at` | `at:` — a non-empty list of `HH:MM` times | Once a day at each listed time. |
| `cron` | `cron:` — a non-empty list of 5-field cron specs | On each cron tick. |

All times use `digest.timezone` (default UTC). See [Weekends, holidays, and timezone](#weekends-holidays-and-timezone).

## Rules

The server checks the block at startup. `notifycat-config validate` and `notifycat-doctor` run the same checks. A bad block fails startup.

- `strategy` is required: `per-event` or `scheduled`.
- `per-event` takes no other key.
- `scheduled` requires `mode`.
- `hourly` takes no `at` and no `cron`.
- `at` needs a non-empty `at:` list. Each item is `HH:MM` (24-hour). No `cron`.
- `cron` needs a non-empty `cron:` list. Each item is a standard 5-field cron spec. No `at`.
- Unknown keys fail.

**The block is atomic.** The most specific tier that sets `notifications:` wins as a whole. Keys are not merged across tiers. So a repository tier that sets only `strategy: per-event` fully replaces a scheduled `"*"` tier, and a half-inherited schedule cannot happen.

The strategy is not part of the [lock file](mappings.md#lock-file) hash. Changing it does not revalidate the entry and needs no new Slack scope.

## What a list looks like

```
:eyes: @payments-team, 3 PRs ready for review:
• acme/payments #42: Add retries · by octocat
• acme/payments #43: Fix refund rounding · by hubot
• acme/web #7: New checkout page · by mona
```

- The headline pings the channel's `mentions`. When several repositories send to one channel, their mentions are joined.
- Each line links to the PR.
- A very long list is cut at Slack's block limit and ends with `…and N more`.
- A channel with nothing queued gets no message.

## What happens to each event

| Event | `scheduled` behavior |
| --- | --- |
| PR opened / ready for review | Channels resolve as usual — `channel:`, `channels:` fan-out, and [`paths:`](monorepo.md) rules. The PR is queued once per channel. No Slack call. |
| Schedule tick | Every queued PR of a still-open PR is posted in one list per channel, then marked sent. |
| Reviews and comments | No Slack call — there is no per-PR message to react on. |
| Merged or closed | The PR leaves the queue if it was not sent yet. |
| Converted to draft | The PR leaves the queue. Ready for review again → queued again. |

Each PR is listed **once per channel**. A webhook redelivery after the send does not list it again.

If the post to one channel fails, that channel's PRs stay queued and go out on the next tick. The other channels are not affected.

## Weekends, holidays, and timezone

Scheduled sends share the [stuck-PR digest](digest.md) calendar:

- `digest.timezone` sets the clock for every mode.
- **Weekends are always skipped.**
- Public holidays are skipped when `digest.country` is set.

A skipped tick **keeps the queue**. PRs opened on Saturday go out with the first tick on Monday.

You do not need to enable the digest for this. `digest.timezone` and `digest.country` apply even when `digest.enabled` is off.

## No PR twice: the stuck-PR digest

The digest and a scheduled list can both run in one channel. Notifycat keeps a PR out of one channel's digest when, in that same channel, the PR is:

- still queued for a scheduled list, or
- sent in a scheduled list since the start of today (in `digest.timezone`).

So a PR is never in both messages on the same day. From the next day on, a still-idle PR shows in the digest as usual.

## Logs

```
INFO queued scheduled notification provider=github repository=acme/payments pr=42 channels=1
INFO scheduled notifications posted schedule="30 10 * * *" channel=C0PAYMENTS0 prs=3
ERROR scheduled notification post failed schedule="30 10 * * *" channel=C0PAYMENTS0 prs=3 err=...
INFO skipped scheduled notifications schedule="0 * * * *" reason=weekend date=2026-07-04
```

`channels` is how many channels the PR was queued for. `reason` is `weekend` or `holiday`, the same values as the digest.

## Limits

- **List messages are never updated, reacted on, or deleted.** They hold many PRs. [`cleanup.delete_on_close`](delete-on-close.md) does not apply to them.
- Lists have no "Start review" button.
- Switching a repository from `scheduled` back to `per-event` does not post its queued PRs. They stay unsent, and stay out of the digest, until each PR closes. New PRs post at once.
- A PR opened while its repository was `per-event` keeps its own message after a switch to `scheduled`. Reviews and merges still update that message.
