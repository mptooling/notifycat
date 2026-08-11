# Multi-channel notification — design

**Date:** 2026-08-11
**Status:** Approved (brainstorm), pending implementation plan
**Domains touched:** `routing` (schema, decode, resolve, validate-shape, lock hash), `validation` (per-channel membership). No change to `notification`, persistence, or the handlers.

## Problem

Today a repository routes to exactly one Slack channel (its base `channel:`), optionally enriched by `paths:` rules that route file-conditionally to *other* channels. Operators want to announce a single PR to **multiple channels unconditionally** — e.g. an `api` repo that should always post to both a team channel and a stakeholder channel, each with its own mentions.

## Key finding: the fan-out machinery already exists

The multi-channel delivery path was already built for the `paths:` feature and is **not paths-specific**:

- `routing/domain.Target{Channel, Mentions}` is the unit of fan-out.
- `routing` `ResolveTargets` returns `[]Target`; `TargetsForFiles` already groups by channel and unions mentions (`unionMentions`).
- `notification` `OpenHandler.Handle` loops over targets, posts one message per channel, and is **idempotent per channel** (redelivery/retry only fills missing channels).
- Persistence stores **one `slack_messages` row per (PR, channel)**; `close`/`draft`/review/reaction handlers already iterate every stored message and update each channel.
- Validation already validates a primary channel **plus** extra channels (`PathChannels`), and the lock entry hash already folds extra channels in (`Entry.PathChannels`).

So this feature is **not** "build fan-out." It is "let a repo declare multiple channels unconditionally instead of only via path rules." The work is confined to `routing` config schema + decode + resolve, plus generalizing the `validation` membership loop and the lock hash. **The notification side, DB, message updates, reactions, and close/delete require zero changes.**

## Schema (Approach A: `channel` xor `channels`)

At **both** the tier level (`RepoConfig`) and the path-rule level (`PathRule`), `channel:` and `channels:` are mutually-exclusive keys:

- `channel:` (string) + optional tier-level `mentions:` — **exactly today's shape, untouched.** Single-channel behaviour is the one-element degenerate case.
- `channels:` — a non-empty list of `{channel: <id>, mentions: <tri-state>}` entries. Each entry owns its mentions.

```yaml
mappings:
  acme:
    web:                          # single form — unchanged
      channel: C0WEB
      mentions: ["<@U0ALICE>"]
    api:                          # list form — unconditional multi-channel
      channels:
        - channel: C0API1
          mentions: ["<@U0ALICE>"]
        - channel: C0API2         # mentions absent → @channel
    monorepo:
      channel: C0BASE             # base stays single…
      paths:
        services/pay:
          channels:               # …but a path rule can fan out to a list too
            - channel: C0PAY1
            - channel: C0PAY2
              mentions: []        # ping nobody here
```

### Decode rules (hand-rolled `UnmarshalYAML`, matching existing decoders)

Reject, with a clear error:

1. both `channel` and `channels` present on one node;
2. a tier-level `mentions:` alongside `channels:` (mentions belong inside each entry);
3. an entry missing `channel:`, or with an empty / malformed channel ID;
4. an empty `channels:` list;
5. a **duplicate channel ID within one `channels:` list** (config error, surfaced early — not silently merged);
6. unknown keys (already enforced via `markSeen` + the `default:` arm).

Mentions tri-state inside a `channels:` entry mirrors the existing per-path-rule tri-state:
- key absent → `@channel`;
- `mentions: []` → ping nobody;
- `mentions: [ ... ]` → those;
- `mentions: null` → rejected (same as today).

## Resolution semantics

### Base target set

The base target set comes from whichever tier declares a channel — **org/repo, else org/\*** — as a **whole-tier replacement**. This generalizes today's `resolveRouting`: the most-specific tier that sets a channel wins; lists are **not** merged element-wise across tiers.

- If the winning tier uses the **single form**: one base `Target`, mentions resolved by the existing cross-tier tri-state (absent inherits the other tier's mentions, else `@channel`).
- If the winning tier uses the **list form**: one base `Target` per entry, mentions per-entry tri-state defaulting to `@channel` (no cross-tier mention inheritance in list form).

### Interaction with `paths:` — unchanged (replace-on-match)

`paths:` matching logic is unchanged. When matched path rules carry channels (single or list), those channels **replace the base set entirely** (today's behaviour, confirmed as the desired semantics). A matched rule that omits channels inherits the base channel(s). When no path matches, the base set fires.

Final targets are **unioned and deduped by channel, with mentions merged per channel**, reusing the existing `TargetsForFiles` grouping + `unionMentions`. A channel that would otherwise receive two messages receives one, with the union of its mentions.

## Domain model changes (`routing/domain`)

- New value object `ChannelSpec{ Channel string; Mentions []string; MentionsPresent bool }`.
- `RepoConfig` and `PathRule` each gain `Channels []ChannelSpec` (nil ⇒ single form in use). Decode enforces that exactly one form is populated per node.
- `resolveRouting` returns `[]Target` (the base set) instead of a single `Resolved`. `TargetsForFiles` and the application `Router` already speak `[]Target`, so nothing downstream of routing changes.

The single-form fields (`Channel`, `Mentions`, `MentionsPresent`) stay on `RepoConfig`/`PathRule` for the untouched single-channel path.

## Validation & lock

### Validation (`validation`)

The validator validates bot membership (and channel-ID format) for **every** channel in an entry's set — the primary plus all extras — generalizing the existing `slackChecks(primary, PathChannels)` loop. The "extras" set becomes: (extra base channels beyond the primary) ∪ (path channels). `RepoMapping` keeps a primary `SlackChannel` (first base entry) for the "found mapping → X" display and format check; every channel still gets format + membership checks.

### Lock hash (`routing/domain.Entry`)

The entry hash folds in the **full channel set**. Field layout is chosen so that configs which do **not** adopt `channels:` keep their existing hash:

- keep the existing primary `Channel` and the existing path-derived channel field (unchanged JSON tag);
- add extra base channels (beyond the primary) into the hashed set.

Consequence: existing single-channel configs (with or without `paths:`) hash identically → **no mass revalidation** on upgrade. Only repos that adopt a `channels:` list revalidate on next boot, which is self-healing (successful entries merge back into `config.lock`).

## Testing (TDD, RED → GREEN → REFACTOR)

RED-first at each seam:

- **Decoder** (`config_decode_test.go`): valid single + list forms at both tier and path level; every rejection case (1–6 above); mentions tri-state inside an entry.
- **Resolve** (`resolve_test.go`, `paths_test.go`): base list; list + paths replace-on-match; cross-channel dedup + mention union; org/\* → org/repo whole-tier replacement (single-over-list and list-over-single).
- **Validation** (`validator` tests): membership + format check runs for every channel in the set.
- **Lock hash** (`entry_test.go`): hash stable for existing single-channel and single-channel-with-paths configs; hash changes when a repo adopts a `channels:` list or repoints a channel.

No new `notification` handler tests: fan-out, per-channel storage, updates, reactions, and close/delete are unchanged and already covered.

## Out of scope (YAGNI)

- No per-path "additive vs replace" flag — `paths:` stays replace-on-match.
- No element-wise merge of channel lists across org/\* and org/repo tiers — whole-tier replacement only.
- No cross-tier mention inheritance for the list form — per-entry `@channel` default.
- No schema migration or deprecation of `channel:` — the single form stays first-class.

## Compatibility

Additive and non-breaking. Every existing config parses and behaves identically; the lock does not mass-revalidate. Pre-1.0 versioning still applies, but no breaking change is introduced.
