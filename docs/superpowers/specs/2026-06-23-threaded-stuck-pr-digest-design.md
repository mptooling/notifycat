# Threaded stuck-PR digest

## Problem

The stuck-PR digest posts one message per channel: a headline followed by every stuck PR as bullet lines, packed across multiple Block Kit section blocks to stay under Slack's 3000-char-per-section cap. A busy channel produces a wall of text in the main channel feed.

## Goal

Split each channel's digest into two posts:

1. A short **parent** message — the headline, mentions, and a count.
2. A single **thread reply** under it — the full PR list.

The channel feed shows one quiet line per channel; the list lives in the thread.

## Behavior

- Read stuck PRs via the existing `FindStuck` pass and group them by Slack channel, unioning and deduping each channel's mentions — unchanged from today.
- Mentions live on the **parent** message only. Slack thread replies don't notify the channel, so the parent is the post that pings; the reply is plain.
- Only mentions from repos that actually have a stuck PR appear, deduped. This is already how `groupByChannel` builds the mention set; it carries onto the parent for free and is locked in by a test.
- No reactions are added to either message, and neither message is tracked in the store. The digest already posts without tracking; this stays the same.

## Changes

### `internal/slack/client.go`

Add `PostReply(ctx, channel, threadTS string, msg Message) (string, error)` — identical to `PostMessage` but with `"thread_ts": threadTS` in the `chat.postMessage` payload. Kept separate from `PostMessage` so the webhook path is untouched.

### `internal/slack/composer.go`

Replace `StuckDigest` with two renderers:

- `StuckDigestParent(mentions []string, count int) Message` — the static headline:
  `:hourglass_flowing_sand: <mentions>, N open PR(s) waiting for review since before today:`
  Carries the count and the deduped mentions; no list.
- `StuckDigestList(prs []StuckPR) Message` — the bullet list lifted from today's `StuckDigest` body. Keeps the `maxSectionChars` packing: a single message renders many blocks fine, but any one section over 3000 chars makes Slack reject the whole post, so a long list still splits across section blocks within this one reply.

### `internal/digest/reporter.go`

- `Poster` interface gains `PostReply(ctx, channel, threadTS string, msg slack.Message) (string, error)`.
- `Report`, per channel group:
  1. Post the parent via `PostMessage`, capturing its `ts`.
  2. Post the list as one reply via `PostReply` with that `ts` as `thread_ts`.

## Error handling

- Parent post fails → log and skip the whole channel (no thread root to hang the reply on). Mirrors today's per-channel skip; other channels still go out.
- Reply post fails → log and continue to the next channel. The parent already landed; a missing reply is logged, not retried.

## Testing

- Composer: `StuckDigestParent` (count, plural suffix, mentions prefix, empty-mentions rule) and `StuckDigestList` (link rendering, idle phrase, multi-section packing past `maxSectionChars`).
- Reporter: stub `Poster` records the parent post and the reply; assert the reply carries the parent's `ts`, and that a channel mapping several repos where only some are stuck yields a parent mentioning only the stuck repos' reviewers, deduped.
