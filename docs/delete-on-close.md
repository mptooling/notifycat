# Remove messages on close

By default a finished PR keeps its Slack message: the post is updated in place with a `[Merged]` or `[Closed]` tag and the matching reaction. The history stays in the channel.

Some teams want the opposite — a channel that only ever shows work still in flight. `cleanup.delete_on_close` gives you that: when the PR is merged or declined, Notifycat deletes the Slack message instead of updating it.

**Off by default.** Nothing changes until you turn it on.

## Turning it on

Globally, for every mapped repository:

```yaml
cleanup:
  message_ttl_days: 30
  delete_on_close: true
```

## Per repository

The same key works inside a mapping tier, so one repository — or one whole org — can differ from the global setting:

```yaml
cleanup:
  delete_on_close: false        # global default: keep the [Merged] message

mappings:
  acme:
    "*":
      channel: C0TEAMDEFAULT
      cleanup:
        delete_on_close: true   # every acme repo removes its messages
    archive:
      channel: C0ARCHIVE
      cleanup:
        delete_on_close: false  # …except this one, which keeps them
```

Inheritance is the usual most-specific-wins chain: repository tier → org `"*"` tier → the global `cleanup:` section → the built-in default (`false`). See [Behavioral overrides](mappings.md#behavioral-overrides).

`cleanup.message_ttl_days` stays global. Setting it inside a mapping tier fails startup rather than being ignored quietly.

## What happens on a merge or a decline

With the flag **on**, for a PR that Notifycat is tracking:

1. Every Slack message for that PR is deleted — one per channel the PR fanned out to, including [per-path](monorepo.md) channels.
2. Any open review session is closed.
3. The PR row is dropped from the database, so the PR also leaves the [stuck-PR digest](digest.md) immediately.

No `[Merged]` update and no closing reaction are sent: there is no message left to carry them. A repository's `reactions` settings are untouched — they still apply to reviews while the PR is open.

Merged and declined PRs behave identically. There is no way to delete on one and keep the other.

## What does not change

- The PR-open post, review reactions, and the in-review decoration all behave exactly as before.
- Converting a PR back to draft already deleted its message. That is unrelated and unaffected.
- The TTL sweep (`cleanup.message_ttl_days`) still runs. It only ever deleted database rows, never Slack messages, and that is still true.

## Requirements and limits

- The bot needs `chat:write`, which it already has — Slack lets an app delete **its own** messages with no extra scope.
- A message somebody already deleted by hand is not an error. Slack answers `message_not_found` and Notifycat treats that as the outcome it wanted.
- **Deleting is permanent.** Slack has no undo, and Notifycat keeps no copy of the message. If your team refers back to merged PR threads, leave this off.
- Threaded replies under a deleted parent message are removed by Slack along with it. A "Start review" thread on a merged PR goes with the message.
- There is no reopen flow in Notifycat. A PR reopened after its message was deleted is not re-announced.

## Which one should you pick?

| | `delete_on_close: false` (default) | `delete_on_close: true` |
| --- | --- | --- |
| Channel shows | every PR, finished ones tagged | only PRs still in flight |
| Merged PR history | stays in Slack | gone |
| Digest | PR drops out when closed | PR drops out when closed |
| Recoverable | yes | no |

Pick `true` for a busy review channel that nobody scrolls back through. Keep the default if merged PR messages double as a record your team reads.
