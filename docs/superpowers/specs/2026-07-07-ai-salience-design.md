# AI salience layer — self-hosted v1 design

**Date:** 2026-07-07 · **Status:** approved design, pre-implementation · **Related:** [launchpad-be#15](https://github.com/notifycat/launchpad-be/issues/15) (original brainstorm, SaaS-inclusive), engine#31 (deterministic CI state machine, stage-2 prerequisite), engine#110 (monorepo path routing)

## Summary

An optional, default-off AI layer that makes notifycat's notifications adaptive: it decides *what to surface, when, and how loudly* — never what the message fundamentally says. Operators bring their own API key (Gemini, or any OpenAI-compatible endpoint including local models). When the AI is enabled it becomes the decider for salience choices; when it is unreachable, misconfigured, rate-limited, or produces invalid output, the existing deterministic behavior runs unchanged. This spec covers the self-hosted engine only; the SaaS angle from launchpad-be#15 is out of scope here.

## Locked guardrails (inherited from launchpad-be#15, amended)

1. **Decisions over content.** The AI never composes messages. It fills a clamped schema: enums, subsets of operator-configured values, and two narrowly bounded text fields (a ≤120-char muted context block, ≤200-char thread notes). No PR summaries — explicitly rejected.
2. **Salience, never existence.** The AI can make something quieter, later, or differently decorated — it can never suppress, hide, or drop a PR. The decision schema structurally lacks a "don't post" option.
3. **Policy outranks AI.** Existing deterministic policies (`ignore_ai_reviews` bot suppression, dependabot compact format, draft deletion) run before the advisor is consulted. The AI modulates within what policy allows and can never un-suppress or override an operator decision.
4. **Rule-sufficient decisions skip the model.** Anything a regex or a config lookup answers (known bot authors, breaking-change marker detection) is computed deterministically and fed to the model as a signal — never asked of it.

## Scope and phasing

**V1 surfaces (this spec, existing events only):**

| Surface | Trigger | AI decides |
| --- | --- | --- |
| New-PR salience | `opened` / `ready_for_review` | per-channel loudness, mention subset, leading emoji, standard-vs-compact format, breaking-change emphasis, context block, thread note |
| Monorepo semantic routing | same event, multi-candidate mappings | which of the mapping-declared candidate channels to post to, per-channel decisions for each |
| Message update | `approved` / `commented` / `changes_requested` / `merged` / `closed` | which allowlisted emoji to react with (or decorate the close with) |
| Digest intelligence | digest cron | stuck-PR ordering, per-PR highlight, per-PR thread note, parent ping-vs-quiet |

**Stage 2 (out of this spec; the contract is shaped for it):** CI-failed and PR-updated (push/synchronize) triggers — natural extensions of the `DecideUpdated` surface — once the deterministic CI state machine (engine#31) lands; flaky-vs-real CI classification; a deferred-ping upgrade primitive (post quiet now, ping when CI turns green — the same primitive off-hours batching would use).

Breaking-change detection needs no new ingestion: `kernel.PR` already carries Title and Body, so the conventional-commits marker (title `!`, breaking-change footer in the body) is a deterministic v1 signal.

## Architecture

A new eighth domain, `internal/salience/`, following the standard three-layer layout. Operator-facing name is "AI" (config key `ai:`, doc page `ai.md`); the code name matches the issue's language.

```
internal/salience/
  domain/          interfaces.go   Advisor (use-case port), ModelGateway (provider port; tiny, SDK-free)
                   models.go       decision request/response DTOs
                   enums.go        Loudness, Format, Emphasis, Highlight, FallbackReason, ProviderName
                   constants.go    timeouts, truncation caps, circuit thresholds, cache size
  application/     deterministic_advisor.go   pure repackaging of today's config-driven behavior
                   model_advisor.go           minimize → guard → gateway → parse → clamp
                   resilient_advisor.go       timeout, circuit breaker, cache; falls back
                   signals.go                 deterministic pre-computation (breaking, revert, docs-only, deps-only)
                   minimize.go / guard.go / clamp.go   pure pipeline stages
  infrastructure/  gemini/         hand-rolled REST client (generateContent + responseSchema) + fx.Module
                   openaicompat/   hand-rolled chat-completions client (response_format json_schema) + fx.Module
  module.go        salience.Module — domain wiring + advisors, no provider
```

**Provider modules are plug-and-play at the DI level.** Each provider package is self-contained, exports its own `fx.Module` providing `domain.ModelGateway`, and imports nothing from its sibling. The composition root appends exactly one provider module based on `ai.provider`, and none when `ai.enabled: false` — no gateway constructed, no HTTP client, no AI code path active. (In Go, "not loaded" means not wired/instantiated; the compiled code remains in the binary, which is harmless. Dynamic `.so` loading via Go's `plugin` package was evaluated and rejected: platform-restricted, CGO-dependent — it would break the static alpine build — and requires exact toolchain/dependency lockstep.)

**External plugin repositories** are a supported future direction, not v1: everything under `internal/` is unimportable cross-repo, so the day external adapters materialize, `ModelGateway` + its DTOs get promoted to a public `pkg/` package — designed deliberately tiny and SDK-free so that promotion is mechanical. For LLM providers specifically, the OpenAI-compatible adapter already acts as an out-of-process plugin system: pointing `base_url` at LiteLLM/Ollama/OpenRouter covers the provider long tail with zero new machinery. Where separate closed modules genuinely pay off later is the open-core SaaS seam.

**Adapters are hand-rolled `net/http` clients**, matching `platform/slack` / `platform/github` style — no official SDKs, keeping the `govulncheck` surface flat.

**Bindings (composition root):**

- `ai.enabled: false` (default): `DeterministicAdvisor` bound as `Advisor`. Regression guarantee: Slack output is byte-identical to today, proven by golden tests.
- `ai.enabled: true`: `ResilientAdvisor` bound, wrapping `ModelAdvisor` + the selected provider module, falling back to `DeterministicAdvisor` internally.

Consumers (notification handlers, digest reporter) inject `saliencedomain.Advisor` — the same cross-domain pattern as notification → routingdomain today — and route their existing choices through a decision. Handlers never know whether AI is on.

## Decision contract

```go
type Advisor interface {
    DecideOpen(ctx context.Context, request OpenDecisionRequest) OpenDecision
    DecideUpdated(ctx context.Context, request UpdatedDecisionRequest) UpdatedDecision
    DecideDigest(ctx context.Context, request DigestDecisionRequest) DigestDecision
}
```

No `error` in the signatures: the advisor cannot fail from a consumer's viewpoint. The resilient implementation always returns a valid decision and records a `FallbackReason` for the log line: `timeout`, `transport_error`, `rate_limited` (provider 429/quota), `malformed_output`, `guard_tripped`, `clamp_violation`, `circuit_open`, `disabled` (per-tier opt-out).

**One event = one model call.** All open-time decisions (routing, loudness, format, emphasis, emoji, notes) come back in a single structured response. Update events (reviews, merge/close) cost one small call each. A digest run costs one call per channel report.

### OpenDecisionRequest

Repository; minimized PR summary (truncated title/body, author login, bot flags); deterministic signals from `signals.go` (breaking-change marker, revert pattern, docs-only / generated-only / deps-only path classes); changed file paths (capped list — carried on the target-resolution result, which already fetches them for path routing; no second fetch); candidate targets from the mapping (channel + that channel's configured mentions); the emoji allowlist; concatenated operator instructions.

### OpenDecision — per-target, every field clamped

`Targets []TargetDecision`, one per selected channel, plus a `Rationale` (logged, never posted). Per-channel independence is the point: a monorepo fan-out can ping the owning team loudly while a neighboring channel gets a quiet FYI.

| Field | Meaning | Clamp rule (violation ⇒ that channel's deterministic values + `clamp_violation`) |
| --- | --- | --- |
| `Channel` | routing pick | must be a mapping-declared candidate; decisions for unknown channels are dropped; empty/fully-invalid target list ⇒ all candidates, deterministic |
| `Loudness` | `ping` / `quiet` | enum; `quiet` still posts — never-skip is structural |
| `Mentions` | subset ping | ⊆ that target's configured mentions; never free-form handles |
| `LeadingEmoji` | message emoji | ∈ allowlist (configured reaction emojis + a small curated set defined in `constants.go`, e.g. `rocket`, `warning`, `lock`, `package`, `sparkles`) |
| `Format` | `standard` / `compact` | enum; compact = the existing dependabot-style one-liner |
| `Emphasis` | `none` / `breaking` | enum; rendering (alarm emoji, bold label) stays in the deterministic template |
| `ContextBlock` | one muted context-type line in the channel message | ≤ 120 chars, single line, sanitized (see pipeline stage 4) |
| `ThreadNote` | optional reply under the PR message | ≤ 200 chars, sanitized, thread-only |

### UpdatedDecisionRequest / UpdatedDecision

Request: event kind, sender (login + bot flag), truncated PR title, configured default emoji for the event, allowlist, operator instructions. Decision: one `Emoji` (∈ allowlist, else configured default) + `Rationale`. The decided emoji substitutes wherever the configured one would appear for that event — the reaction itself and, on merge/close, the updated message's leading emoji swap. Stateless — no persistence, no migration. Consulted only when policy allows the reaction at all.

### DigestDecisionRequest / DigestDecision

Request: stuck-PR list (repo, number, truncated title, idle days, breaking signal), channel, configured mentions, operator instructions, capped at 30 PRs in the prompt. Decision: `Order` (must be a valid permutation of input indices, else deterministic age order), `Highlights` (per-PR `normal`/`attention`), `PerPRNote` (≤ 120 chars each, thread-only, sanitized), `ParentLoudness` (`ping`/`quiet` — the digest always posts). Parent message text stays fully deterministic.

### Changes to existing ports

- `notification/domain.Messenger` gains `PostThreadReply(ctx, channel, messageID, ThreadNoteRequest) error` (Slack `chat.postMessage` with `thread_ts`; the digest poster already has this shape).
- The `TargetResolver` result carries the `ChangedFiles []string` it already fetched for path routing, so the advisor request reuses it.
- `OpenHandler`, reaction handlers, `CloseHandler`, and the digest `Reporter` are refactored to flow their existing choices through the advisor's decision structs. With the deterministic advisor bound, output is byte-identical to today.

## Configuration and secrets

```yaml
ai:
  enabled: false                 # default off — fully optional feature
  provider: gemini               # gemini | openai_compatible
  model: gemini-2.5-flash        # required when enabled; never silently defaulted
  base_url:                      # gemini: optional override; openai_compatible: REQUIRED
  instructions: |                # optional operator guidance embedded in every prompt
    Changes under /billing are payment-critical.
```

- **Secret:** `AI_API_KEY` env var, `Secret`-typed. Required at boot for `provider: gemini`; optional for `openai_compatible` (local endpoints run keyless; unset ⇒ no auth header). The key never appears in `config.yaml`.
- **Per-tier overrides in `mappings:`** (same inheritance as `reactions`/`reviews`/`digest`): `ai.enabled` tri-state (absent = inherit) to opt a repo/org out, and `ai.instructions`, which concatenates global → org-tier → repo-tier so guidance narrows rather than replaces. Deliberately **not** per-tier: provider, model, key — one provider per deployment, mirroring the `git_provider` stance. Instructions are operator-trusted (whoever writes config.yaml owns the server), so they are prompt input, not an injection surface.
- **Boot validation (fail-fast):** unknown provider; enabled without `model`; gemini without `AI_API_KEY`; `openai_compatible` without `base_url`. Provider *unreachability* is deliberately not a boot check — the runtime fallback owns outages.
- **Deliberately not configurable in v1:** the per-decision timeout (a 2.5 s constant in `salience/domain/constants.go`), any daily request cap (v1 ships uncapped — cost control is per-decision token logging, the decision cache, the circuit breaker, and the provider's own quotas), and per-surface toggles (enabling AI enables all three surfaces; per-tier `ai.enabled` remains the opt-out). Each can be promoted to a config key later without migration.
- **Doctor:** new AI section — config shape, key presence (redacted), endpoint reachability, and a one-token structured-output probe against the configured model (proves key validity and model availability, measures latency). The probe also reports **rate-limit headroom, best-effort**: OpenAI-compatible endpoints that send `x-ratelimit-*` response headers (OpenAI, OpenRouter, LiteLLM setups that forward them) get requests/tokens remaining surfaced as an info-level check; local endpoints without the headers report "no limits exposed". Gemini offers no quota-read API for a bare API key (quota lives in the AI Studio / Cloud console), so the check reports limits as provider-enforced — and if the probe itself gets a 429, doctor surfaces the provider's error detail naming the tripped quota and any retry-after. Remediation text links the provider's quota console.
- **No changes to `config.lock` or the validation domain** — AI settings don't affect mapping-entry hashes and need no per-entry external checks.
- In code, `platform/config`'s file schema gains the `ai` block parsed into a salience-owned config DTO (precedent: `routingdomain.DigestConfig` inside `Config`).

## Guarding pipeline

Four pure-Go stages inside `ModelAdvisor`; all caps and patterns are constants; all stages unit-test without a network.

**1. Minimize.** Title ≤ 200 chars, body ≤ 1,500, file list ≤ 100 paths + "…and N more", digest ≤ 30 PRs. Strip HTML comments (dependabot bodies collapse from ~10 KB to a paragraph), markdown badges/images, base64-ish blobs; collapse whitespace. Redact secret-shaped strings before anything leaves the process (`ghp_`/`gho_` tokens, `xoxb-`/`xoxp-`, `AKIA…`, PEM headers, JWT-shaped triplets, long high-entropy hex) — PR bodies occasionally contain leaked credentials and must not reach a third-party API. Rule-sufficient cases (known bot authors) never consult the model at all.

**2. Guard inbound.** All attacker-influenced fields (title, body, login, paths, digest titles) are placed only inside a delimited data envelope; the system prompt declares the envelope data-never-instructions; delimiter collisions in content are neutralized. A heuristic tripwire ("ignore previous instructions"-class patterns) does not refuse the event — it flags `guard_tripped`, routes that one event to the deterministic path, and logs a warning. **The model receives zero tools and one turn.** The app posts to Slack; the model only fills a schema — there is no agentic loop to hijack.

**3. Structured output.** Gemini `responseSchema` / OpenAI `response_format: json_schema` enforce shape provider-side; the client strict-parses regardless. No lenient repair, no retry — a malformed response is a fallback; systemic failure is the circuit breaker's job.

**4. Clamp and sanitize.** The table above, applied per field, pure and provider-independent — the only door into consumer code. Text-field sanitizer: mrkdwn-escape; strip Slack mention syntax (`@here`, `@channel`, `<!…>`, `<@…>`); strip URLs except the PR's own; enforce length; single-line for context blocks.

**Net threat model:** a successful injection can at worst pick a wrong-but-valid enum or produce a weird ≤ 200-char sanitized note. It can never mint mentions, channels, or links, never ping anyone not already configured, and never hide a PR.

## Resilience

- **Timeout:** per-decision context deadline, a 2.5 s constant in `salience/domain/constants.go` — safely inside GitHub's 10 s webhook window; the digest cron path may use a more generous internal deadline.
- **Circuit breaker:** 5 consecutive gateway failures → open 10 minutes → half-open single probe. Constants in `salience/domain/constants.go`.
- **Cache:** in-memory LRU (512 entries, 24 h TTL) keyed by surface + repo + PR number + content hash — absorbs GitHub redeliveries and duplicate deliveries. Re-spend after a restart is acceptable — decisions are cheap and duplicate deliveries are rare.
- Every skip lands on `DeterministicAdvisor`: zero I/O, always succeeds.

## Observability

One structured `ai decision` log line per consultation: surface, provider, model, latency_ms, tokens in/out, cache hit/miss, `fallback_reason` (empty = model decision applied), truncated rationale. This mirrors the existing `ignored webhook event` contract; the operations doc gets a matching reason table. Per-decision token logging is the v1 cost-visibility story.

## Testing

- **Regression anchor:** golden tests assert `ai.enabled: false` produces byte-identical Slack payloads to current behavior — the deterministic advisor is provably a pure repackaging; this is what makes the consumer refactor safe.
- **ModelAdvisor** (fake gateway): table-driven clamp tests (out-of-set mentions/channels/emoji, oversize notes, invalid permutations), sanitizer tests (mention-syntax stripping, link stripping, mrkdwn escaping), minimizer goldens (real dependabot body, secret-redaction fixtures), injection-corpus fixtures proving `guard_tripped` routing.
- **ResilientAdvisor:** timeout → fallback; circuit opens after 5 and half-opens; cache hits skip the gateway (fake counts calls).
- **Provider adapters:** `httptest` contract tests — request shape (auth header, model, schema), parse paths, 429/5xx taxonomy, keyless openai_compatible mode.
- **Wiring:** runtime tests assert disabled ⇒ deterministic binding and no provider module; enabled ⇒ resilient + correct provider module; malformed AI config aborts boot.
- No live-API calls in CI. TDD throughout, per repo rules.

## Documentation plan

New `docs/ai.md` (what the AI decides, what it can never do, provider setup including an Ollama example, cost expectations, log-line reference). Updates: `configuration.md` (ai block + `AI_API_KEY` row), `mappings.md` (per-tier override), `operations.md` (`ai decision` reason table), `doctor.md` (AI section), `security.md` (AI threat model: what leaves the process, redaction, injection stance), `features.md` (mention).

## Rollout and implementation phasing

Ships default-off as a `feat:`. Implementation proceeds in independently-green phases:

1. Salience domain skeleton + `DeterministicAdvisor` + consumers refactored through decisions, with byte-identical goldens.
2. Config, secrets, boot validation.
3. Guard pipeline + `ResilientAdvisor` (timeout, circuit, budget, cache).
4. Gemini provider module.
5. OpenAI-compatible provider module.
6. Doctor section + documentation.

## Success criteria

- AI off ⇒ byte-identical output (goldens prove it).
- AI on with a dead provider ⇒ every delivery still lands via fallback; zero AI-caused webhook failures; added p99 latency ≤ the configured timeout.
- Every model consultation is attributable via one log line with a rationale.
- The injection test corpus can never produce unclamped output.
- Cost is observable: every decision logs its token usage.

## Out of scope

PR summaries or any AI-composed message body; AI code review; AI suppressing or hiding a PR; SaaS/multi-tenant concerns (launchpad-be#15 covers those separately); CI-failed and PR-updated surfaces (stage 2, after engine#31); external plugin repositories (future; enabled by promoting the gateway port to `pkg/` when needed).
