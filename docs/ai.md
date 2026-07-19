# AI

Notifycat includes an optional AI salience layer that modulates how loudly notifications are presented. It is off by default — set `ai.enabled: true` to activate it. Every failure falls back silently to the deterministic behavior that runs without it.

## What the AI decides

The advisor is consulted at four surfaces across the notification lifecycle.

| Surface | When | What the AI adjusts |
| --- | --- | --- |
| New PR open | A non-draft PR opens or moves to ready-for-review | Loudness (ping vs. quiet), the subset of configured mentions to include, leading emoji, message format (standard vs. compact), emphasis tag (none vs. breaking), a brief context block, a thread note |
| Monorepo routing | Same open event, when the PR fans out to multiple channels | Per-channel loudness and mention subset (the fan-out set is still determined by path rules, never by the AI) |
| Message update | A review (approve / comment / request-changes) or merge/close event | The emoji substituted for the configured reaction emoji wherever it would appear |
| Digest ordering | Each channel's digest run | Reordering of stuck PRs by urgency, per-PR highlight decoration, per-PR notes, and the parent message's loudness |

## What the AI can never do

The salience layer is a bounded adjuster — it can turn the volume up or down, but it cannot change what gets posted, who gets mentioned beyond the operator's configured set, or what the message says.

- **A PR is never suppressed or hidden.** The `Advisor` port returns no error; a quiet decision still posts the message.
- **The AI never composes message bodies.** PR title, body, author, and file paths are inputs to the model; message text is assembled by the deterministic template after the decision is applied.
- **The AI never mints mentions, channels, or links.** It may only select a subset of the operator-configured mentions for a channel; it cannot introduce new handles or URLs.
- **Policy always outranks the AI.** Bot-reviewer suppression (`reviews.ignore_ai_reviews`) and dependabot compact format (`reviews.dependabot_format`) run regardless of the advisor's output. The AI never sees the suppression path.
- **Every failure falls back to the exact deterministic behavior.** A timeout, a network error, a malformed response, a guard trip, or a circuit-open condition all resolve to the same result as if AI were off. No delivery is delayed or blocked; the `fallback_reason` field records why.

## Enabling it

Add an `ai:` block to `config.yaml` and export `AI_API_KEY` in your environment (or `.env`):

```yaml
ai:
  enabled: true
  provider: gemini          # or openai_compatible
  model: gemini-2.0-flash   # required; never defaulted
  # base_url: ...           # optional for gemini; required for openai_compatible
  # instructions: |         # optional operator guidance embedded in every prompt
  #   Focus on security-tagged PRs.
```

```sh
# .env
AI_API_KEY=your-gemini-api-key
```

The server fails fast at startup when `ai.enabled: true` but `ai.model` is missing, or when `ai.provider: gemini` and `AI_API_KEY` is unset.

**Gemini quickstart.** Create an API key at [aistudio.google.com](https://aistudio.google.com), set `AI_API_KEY` in `.env`, and set `ai.provider: gemini` with a model such as `gemini-2.0-flash`. The default Gemini endpoint is used; `ai.base_url` is optional.

**Per-tier opt-out.** Individual repo tiers can opt out even when the global switch is on. See [Mappings → AI overrides](mappings.md#ai-overrides) for the `ai.enabled: false` per-tier key.

## Providers

Two providers are supported. The provider, model, and key are global-only — no per-tier or per-org override.

**`gemini`** — Google Gemini via the REST API. `AI_API_KEY` is required. `ai.base_url` is optional (defaults to the public Gemini endpoint). Example:

```yaml
ai:
  enabled: true
  provider: gemini
  model: gemini-2.0-flash
```

**`openai_compatible`** — any OpenAI-compatible endpoint. `ai.base_url` is required; `AI_API_KEY` is optional (omit for keyless local endpoints). Example with Ollama:

```yaml
ai:
  enabled: true
  provider: openai_compatible
  model: llama3.1
  base_url: http://localhost:11434/v1
  # No AI_API_KEY needed for a local Ollama endpoint
```

## Cost expectations

The AI layer is designed to minimize token spend:

- **One model call per PR-open event.** All decisions for an opened or ready-for-review PR (loudness, mentions subset, emoji, format, emphasis, context block, thread note — across all fan-out channels) are batched into a single structured-output response.
- **One small call per review or close event.** The updated-surface decision asks only for an emoji substitution.
- **One call per channel per digest run.** Each channel's digest report is one call; channels with no stuck PRs are skipped.
- **Prompts are minimized.** PR title is capped at 200 characters, body at 1,500 characters, and changed file paths at 100 entries before being sent to the model.
- **Every decision logs token counts.** The `ai decision` log line carries `tokens_in` and `tokens_out` so you can track spend in your log aggregator.
- **Duplicate deliveries are answered from a 24-hour cache.** If the same PR-open payload arrives twice (e.g. a GitHub retry), the second call hits the cache and costs zero tokens.

## The `ai decision` log line

Every advisor consultation emits a structured log line at `INFO` level with the message `ai decision`. Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `surface` | string | `open`, `updated`, or `digest` |
| `provider` | string | The configured provider name, e.g. `gemini` |
| `model` | string | The configured model name |
| `latency_ms` | int | Wall-clock milliseconds for the model call (0 on a cache hit) |
| `tokens_in` | int | Prompt tokens sent (0 on a cache hit or fallback) |
| `tokens_out` | int | Completion tokens received (0 on a cache hit or fallback) |
| `cache_hit` | bool | `true` when the decision was served from the 24-hour cache |
| `fallback_reason` | string | Empty when the model decision was applied; one of the reason tokens when it was not — see [Operations → AI decisions](operations.md#ai-decisions) for the full table |
| `rationale` | string | The model's free-text reasoning (logged, never posted to Slack) |

## Timeouts and resilience

The advisor is designed so that no failure mode blocks a delivery or delays the server:

- **Webhook-path timeout: 2.5 seconds.** Each model call for an open or updated event is bounded to keep the response inside GitHub's 10-second delivery window.
- **Digest-path timeout: 10 seconds.** The digest cron path has no delivery deadline, so it can wait longer.
- **Circuit breaker: 5 consecutive failures open the circuit for 10 minutes.** When the circuit is open, model calls are skipped and the `fallback_reason` is `circuit_open`. Deliveries continue with fully deterministic behavior.
- **Unreachability never blocks boot or delivery.** The advisor port returns no error; a disabled or unreachable provider is transparent to callers. The server starts normally even if the AI endpoint is down.
