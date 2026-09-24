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

1. Every Slack message for that PR is deleted — one per channel the PR fanned out to, including [per-path](monorepo.md) channels. A message with a thread discussion is the exception: see [Messages with a thread](#messages-with-a-thread).
2. Any open review session is closed.
3. The PR row is dropped from the database, so the PR also leaves the [stuck-PR digest](digest.md) immediately.

No `[Merged]` update and no closing reaction are sent: there is no message left to carry them. A repository's `reactions` settings are untouched — they still apply to reviews while the PR is open.

Merged and declined PRs behave identically. There is no way to delete on one and keep the other.

## Messages with a thread

Before it deletes a message, Notifycat checks whether anyone replied in Slack its thread (`reply_count` on `conversations.history`). A message with at least one reply is **kept**, because deleting the parent would take the discussion with it. It gets the normal `[Merged]` / `[Closed]` tag and closing reaction instead, as if the flag were off for that one message.

The decision is per message. A PR that fanned out to two channels can lose its message in one channel and keep the threaded one in the other.

If the lookup fails — a missing scope, a Slack outage — Notifycat keeps the message and logs `could not read thread replies; keeping message`. An unknown thread state never risks a delete. A kept message logs `kept message with thread replies`.

## What does not change

- The PR-open post, review reactions, and the in-review decoration all behave exactly as before.
- Converting a PR back to draft already deleted its message. That is unrelated and unaffected.
- The TTL sweep (`cleanup.message_ttl_days`) still runs. It only ever deleted database rows, never Slack messages, and that is still true.

## Requirements and limits

- The bot needs `chat:write`, which it already has — Slack lets an app delete **its own** messages with no extra scope.
- The bot also needs `channels:history` and `groups:history`, to read the thread before a delete. Startup validation fails an entry that has the flag on while either scope is missing. See [Bot scopes](slack-app.md#optional-channelshistory-groupshistory).
- A message somebody already deleted by hand is not an error. Slack answers `message_not_found` and Notifycat treats that as the outcome it wanted.
- **Deleting is permanent.** Slack has no undo, and Notifycat keeps no copy of the message. A message with no thread replies is gone for good.
- There is no reopen flow in Notifycat. A PR reopened after its message was deleted is not re-announced.

## Which one should you pick?

| | `delete_on_close: false` (default) | `delete_on_close: true` |
| --- | --- | --- |
| Channel shows | every PR, finished ones tagged | only PRs still in flight |
| Merged PR history | stays in Slack | gone |
| Digest | PR drops out when closed | PR drops out when closed |
| Recoverable | yes | no |

Pick `true` for a busy review channel that nobody scrolls back through. Keep the default if merged PR messages double as a record your team reads.
