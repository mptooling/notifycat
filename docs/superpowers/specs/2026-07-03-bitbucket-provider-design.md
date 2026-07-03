# Bitbucket Cloud support — provider-seam refactor + adapter design

Date: 2026-07-03. Status: accepted design for the Bitbucket support epic and its five sub-issues (see the tracker milestone "Bitbucket support (v1)").

## 1. Goal and criteria

Add Bitbucket **Cloud** (bitbucket.org) as a second git provider to notifycat, refactoring upfront so providers are strategy-pattern adapters. Acceptance criteria:

1. Code refactored upfront to a strategy pattern for git providers.
2. Readable, maintainable code (house conventions: consumer-package interfaces, one constructor per type, descriptive names).
3. Minimal config adjustments for operators.
4. Documentation updated.
5. Bitbucket integration implemented in the most secure and stable way (verified against primary Atlassian docs, 2026-07-03).

## 2. Locked decisions

- **D1 — Target: Bitbucket Cloud only.** Data Center differs as much from Cloud as from GitHub (event keys `pr:*`, `/rest/api/1.0`, different payloads); it can be a later adapter against the same seam.
- **D2 — Refactor depth: git seams only.** Neutral inbound event taxonomy + per-provider ingestion + per-provider API client. Messenger/Slack side untouched (no second messenger exists; neutralizing Block Kit now is churn without proof). Rejected: refactoring the outbound/messenger model in the same motion (too broad), and a translation shim mapping Bitbucket payloads onto GitHub vocabulary (permanent debt, lying logs).
- **D3 — Config shape: required top-level `git_provider:` enum (`github` | `bitbucket`; later `gitlab`), one provider per deployment.** `mappings:` keeps its exact schema, no reserved keys. Absent/invalid `git_provider` is a fail-fast startup error naming the key (breaking for existing configs: the upgrade is adding one line `git_provider: github`; upgrading.md entry + pre-1.0 minor bump; retired-env-vars precedent). `github:` section stays, optional, solely for the GHES/data-residency `base_url` (documented feature — do not remove); no `bitbucket:` section exists (Cloud has one host; test override is a struct-only Config field, not YAML). Mixed-provider single instances are out of scope; the future escape hatch is an optional per-org `provider:` override layered on top. `git_provider` joins every lock-entry hash — flipping it revalidates all entries.
- **D4 — `pullrequest:updated` semantics: self-healing.** Bitbucket has no draft-transition event and no `changes` diff object, so `updated` on an open non-draft PR maps to `ReadyForReview` and relies on OpenHandler's per-channel idempotency. Side effect (documented, accepted): first activity on a never-tracked open PR posts its message retroactively. `updated` with `draft=true` maps to `ConvertedToDraft` (draft rows are always deleted — standing rule). Requires no new state; consistent with draft-never-in-DB.
- **D5 — Bot rule: `Sender.IsBot = actor.type != "user"`** (`app_user` and `team` are non-human). Documented blind spot: bots signed up as regular user accounts (typical self-hosted Renovate) are undetectable — same class of blind spot GitHub has for human-account bots. `botpr` (dependabot compact format) stays GitHub-only in v1: Bitbucket has no `[bot]` logins and no `username` field at all (removed 2019, GDPR).
- **D6 — Auth: Access Tokens first (Bearer), API-token fallback (Basic).** `BITBUCKET_TOKEN` alone ⇒ Bearer (Repository token = all plans; Workspace token = Premium, required for `"*"` wildcard listing). `BITBUCKET_TOKEN` + `BITBUCKET_AUTH_EMAIL` ⇒ Basic with a scoped Atlassian API token (Free-plan wildcard path; ≤365-day rotation). App passwords are never supported: they are in brownout now and die 2026-07-28.
- **D7 — Packaging: epic + 5 sub-issues + milestone `Bitbucket support (v1)`** (epic carries the full design). Labels `enhancement` + `claude-generated`, type `Feature`.
- **D9 — No provider column in the database.** One provider per instance makes per-row provider state pay off only for mixed-provider DBs (out of scope). The provider-flip hazard (stale-row collisions → silently skipped posts, digest/reconcile noise within the TTL window) is handled **docs-only**: "switching `git_provider` requires a fresh database." A boot-time `instance_meta` guard was considered and declined.
- **D8 — Secrets follow the selected provider.** `git_provider: github` ⇒ `GITHUB_WEBHOOK_SECRET` required, `GITHUB_TOKEN` optional (existing degradation). `git_provider: bitbucket` ⇒ `BITBUCKET_WEBHOOK_SECRET` required, `BITBUCKET_TOKEN` optional with exact `GITHUB_TOKEN` parity (absent ⇒ path rules inert + validation/reconcile probes skip, same warnings). A Bitbucket deployment boots without any GitHub credential and vice versa.

## 3. Current-state coupling map (verified against code, 2026-07-03)

| Layer | GitHub-specific today |
|---|---|
| Ingestion | `internal/githubhook`: HMAC verifier (`X-Hub-Signature-256`), `X-GitHub-Event` header read, GitHub payload parser; route `POST /webhook/github` in `app.buildMux`; generic body-cap middleware already extracted to `internal/webhook` |
| Domain | `pullrequest.Event{GitHubEvent, Action, Review.State, Sender.Type}`; six handlers' `Applicable` match raw GitHub strings; `PRComment` special case |
| Bot detection | `internal/aireview` (`sender.type == "Bot"`), `internal/botpr` (`dependabot[bot]`/`renovate[bot]` logins) |
| Outbound API | `internal/github.Client`: ListHookEvents, ListOrgRepos, changed files, PR state |
| Validation | `validate.GitHubChecker` + `OrgRepoLister`; `validate/constants.go` required-events; doctor delegates |
| Ops tooling | `internal/smoke` forges signed GitHub webhooks; `internal/reconcile` GitHub PR checker |
| Config | `github:` yaml section; `GITHUB_WEBHOOK_SECRET` unconditionally required |
| Store | column `gh_repository` (name legacy; values neutral `owner/repo`); key `(repository, pr_number)` |
| Logs | `github_event` + `action` fields in the ignored-event contract (operations.md table) |

## 4. Design

### 4.1 Neutral event taxonomy (`internal/pullrequest`)

```go
type Event struct {
    Provider   string    // "github" | "bitbucket"
    Kind       EventKind
    Repository string    // "owner/repo" / "workspace/repo_slug"
    PR         PR        // unchanged fields
    Sender     Sender    // Login string; IsBot bool
}

type EventKind int
const (
    KindOpened EventKind = iota + 1
    KindReadyForReview
    KindClosed   // closed/declined without merge
    KindMerged
    KindConvertedToDraft
    KindApproved
    KindChangesRequested
    KindCommented
)
```

- Adapters own all provider vocabulary **and all draft gating** (an adapter never emits `Opened`/`ReadyForReview` for a draft PR); handlers' `Applicable` collapse to pure kind matches. `PR.Draft` stays on the payload struct as data but no handler branches on it. No raw provider verb is kept on Event (decided).
- GitHub kind-mapping (behavior-preserving, pinned by fixture tests): `("pull_request","opened",!draft)→Opened`; `("pull_request","ready_for_review")→ReadyForReview`; `("pull_request","closed",merged)→Merged` else `Closed`; `("pull_request","converted_to_draft")→ConvertedToDraft`; `("pull_request_review","submitted","approved")→Approved`; `(…,"changes_requested")→ChangesRequested`; commented cases (`review submitted|edited` state=commented, `pull_request_review_comment` created, `issue_comment` created on a PR) → `Commented`; plain-issue comments and everything unmapped → no event (adapter returns ok=false; HTTP layer stays 200, dispatcher debug-logs `no_handler` as today).
- `aireview.Detector` dissolves: GitHub adapter sets `IsBot = sender.type == "Bot"`; the per-repo `ignore_ai_reviews` policy check stays in handlers.
- Log contract migrates: `github_event`+`action` → `provider`+`kind` (the only operator-visible change of the refactor phase; operations.md table + upgrading.md note in the same slice).

### 4.2 Ingestion (strategy per route)

```
POST /webhook/github     → githubhook:    HMAC-SHA256 X-Hub-Signature-256 → parse → kind-map → dispatch
POST /webhook/bitbucket  → bitbuckethook: HMAC-SHA256 X-Hub-Signature     → parse → kind-map → dispatch
```

Both wrap the existing generic `webhook.Signature` (body cap, read-once, replay). **Only the selected provider's route registers** — a `git_provider: bitbucket` deployment has no `/webhook/github` at all (and vice versa); provider-named paths keep webhook URLs honest across a provider flip. Shared HMAC core with per-provider header name; comparison constant-time.

### 4.3 Config surface

```yaml
git_provider: github        # REQUIRED: github | bitbucket (fail-fast if absent/invalid)
github:                     # optional — only for GitHub Enterprise / data-residency base_url
  base_url: https://ghes.example.com/api/v3
mappings:                   # schema 100% unchanged — no reserved keys, no new fields
  acme:                     # GitHub org, or Bitbucket workspace — meaning follows git_provider
    api: { channel: C0123ABCDE }
    "*": { channel: C0DEFAULT00 }
```

- `git_provider` is decoded and validated in `config.Load` (enum check, fail-fast error naming the key and pointing at upgrading.md). No `bitbucket:` YAML section: Bitbucket Cloud has exactly one host; the test override (`BitbucketBaseURL`) is a struct-only `Config` field set directly by fixtures, like the integration tests already do for Slack.
- Env: `BITBUCKET_WEBHOOK_SECRET` (required when selected), `BITBUCKET_TOKEN` (optional, GITHUB_TOKEN-parity degradation), optional `BITBUCKET_AUTH_EMAIL` (presence switches the client to Basic email:api-token auth). Per D8, `config.Load` requires the selected provider's secret only.
- Lock: keys stay `org/repo`; `git_provider` hashes into every entry → a provider flip revalidates everything (correct: same names now mean different remote objects).

### 4.4 Store

- **No schema change.** Keys stay `(gh_repository, pr_number)`; `Store`/`RepoBehavior`/`TargetResolver` signatures unchanged.
- **Provider-flip scenario is handled docs-only (decided):** switching `git_provider` against an existing DB risks stale-row collisions (migrated repos keep names; Bitbucket PR numbering restarts at #1 → a colliding row makes OpenHandler skip the post) plus digest/reconcile noise until the cleanup TTL purges old rows. `upgrading.md` + `operations.md` state: **switching `git_provider` requires a fresh database** (or waiting out `message_ttl_days` with digest disabled — not recommended).
- `Event.Provider` (in-memory) still stamps every event for the `provider`+`kind` log contract.

### 4.5 Bitbucket adapter contract (event mapping, all keys verified)

| X-Event-Key | Condition | → Kind |
|---|---|---|
| `pullrequest:created` | `draft=false` | Opened |
| `pullrequest:created` | `draft=true` | — (ignored, GitHub parity) |
| `pullrequest:updated` | `draft=true` | ConvertedToDraft |
| `pullrequest:updated` | `draft=false, state=OPEN` | ReadyForReview (self-healing, D4) |
| `pullrequest:fulfilled` | | Merged (`PR.Merged=true`) |
| `pullrequest:rejected` | | Closed |
| `pullrequest:approved` | | Approved |
| `pullrequest:changes_request_created` | | ChangesRequested |
| `pullrequest:comment_created` | | Commented (inline + general) |
| `unapproved`, `changes_request_removed`, `comment_updated/deleted/resolved/reopened` | | — (ignored; GitHub parity — retractions/dismissals unhandled there too) |

Payload paths: `pullrequest.id` (per-repo unique), `.title`, `.description`, `.links.html.href`, `.author.display_name` (author/actor accounts have `display_name`, `nickname`, `account_id`, `uuid`, `type`; NO `username`), `.state` (OPEN|MERGED|DECLINED|SUPERSEDED), `.draft`, `.created_on` (ISO 8601 with microseconds + offset — `time.Parse(time.RFC3339, …)` handles it), `repository.full_name` (`workspace/repo_slug`), top-level `actor`.

### 4.6 Bitbucket API client (`internal/bitbucket`, mirrors `internal/github` narrowness)

- Base `https://api.bitbucket.org/2.0`; Bearer (access token) or Basic (`email:api_token`) per D6.
- Endpoints: `GET /repositories/{ws}/{slug}` (exists/access check); `GET /repositories/{ws}` (wildcard listing; `values`/`next` pagination — follow `next` URLs); `GET /repositories/{ws}/{slug}/hooks` (validation); `GET /repositories/{ws}/{slug}/pullrequests/{id}` (reconcile state); `GET /repositories/{ws}/{slug}/pullrequests/{id}/diffstat` (path routing; default pagelen 500).
- Encoded gotchas: diffstat 302-redirects to `/diffstat/{spec}?from_pullrequest_id=` — client must follow redirects and re-send auth (verify the http.Client keeps the Authorization header on same-host redirect); stale source refs redirect with `spec=None` and return a typed error body — treat as "diffstat unavailable" soft-fail to the repo tier (existing path-routing fallback semantics). 429 (1,000 req/h per token identity) surfaces through the client error envelope; response-size caps as in the github client.
- Least-privilege scopes documented: access tokens `repository` + `pullrequest` + `webhook`; API tokens `read:repository:bitbucket` + `read:pullrequest:bitbucket` + `read:webhook:bitbucket`.

### 4.7 Validation / doctor

- `validate` declares provider-neutral `HookChecker` + `RepoLister`; app/CLI/doctor wire the selected provider's implementations at construction time (no per-entry dispatch — one provider per deployment). Per-provider required-events constants (GitHub: existing set; Bitbucket: `pullrequest:created`, `pullrequest:updated`, `pullrequest:fulfilled`, `pullrequest:rejected`, `pullrequest:approved`, `pullrequest:changes_request_created`, `pullrequest:comment_created`) + per-provider URL suffix.
- Doctor config checks: selected provider's webhook secret present; token absent → per-repo probes SKIP (existing semantics, both providers).

### 4.8 Ops tooling

- `reconcile`: `PRChecker` is the selected provider's client (wired from `git_provider`, like everything else); Bitbucket semantics: MERGED/DECLINED/SUPERSEDED → closed; draft → delete.
- `smoke`: `--provider bitbucket` forging (X-Event-Key + `bitbuckethook.Sign`) in slice 5; GitHub smoke untouched meanwhile.

## 5. Security model (verified 2026-07-03, primary Atlassian sources)

- Webhook signing: HMAC-SHA256 over raw body, `sha256=<hex>` in `X-Hub-Signature` (GA since 2023-10-25; secret write-only in UI). **No secret ⇒ Bitbucket sends no signature header ⇒ notifycat hard-requires the secret and 401s unsigned deliveries.**
- Delivery: ≤3 attempts on 5xx/timeout (10s), `X-Attempt-Number` header; handlers already replay-idempotent. Upstream payload cap 256KB (our 1MiB body guard covers it). Optional IP allowlisting via https://ip-ranges.atlassian.com/ (documented as defense-in-depth, not enforced in-app).
- Credentials: never app passwords (brownout now, removed 2026-07-28); access tokens have per-token rate-limit identity and a bot email (`{id}@bots.bitbucket.org`); API tokens must be scoped and rotate ≤365 days.
- Verifier test vector: Atlassian's published worked example (secret "It's a Secret to Everybody", body "Hello World!" → `sha256=a4771c39fbe90f317c7824e83ddef3caae9cb3d976c214ace1f2937e133263c9`).

## 6. Docs plan

New `docs/bitbucket-webhook.md` (webhook + secret setup, event checklist, access-token creation + scopes, Free-plan API-token fallback, troubleshoot: 10s/3-attempts/256KB, app-password warning). Updates: `configuration.md` (`git_provider` key, per-provider secrets table), `mappings.md` (org key means workspace under bitbucket), `security.md` (both HMAC schemes, allowlisting), `operations.md` (reason table → `provider`+`kind`), `upgrading.md` (required `git_provider` one-liner + log-field change + fresh-DB-on-provider-flip rule), `config.example.yaml`, README support matrix, `mkdocs.yml` nav. No standalone migration doc — the upgrade is one added line, upgrading.md covers it (digest-timezone precedent).

## 7. Testing strategy

- Refactor slices: existing handler/dispatcher/integration suites re-expressed against kinds (zero-behavior-change proof); table-driven fixture tests pin every GitHub mapping.
- Bitbucket: golden payload fixtures per event key (from Atlassian doc samples); verifier tests incl. the published HMAC vector, missing-header, bad-scheme; client tests against httptest fake covering 302+auth-replay, `next` pagination, 429, stale-ref diffstat error; integration: full `POST /webhook/bitbucket` → fake Slack round trip through `app.Wire`.
- TDD (RED→GREEN→REFACTOR) per house rules; `just check` green per slice.

## 8. Slices (sub-issues; each independently mergeable)

1. **refactor: neutral event taxonomy** — 4.1 complete incl. log contract + ops-doc table. Zero behavior change.
2. **feat: required git_provider config** — 4.3 with enum `{github}` only, fail-fast + upgrading.md entry (incl. the fresh-DB-on-flip note), lock hashing. No store changes. GitHub-only runtime; the breaking one-line upgrade lands here.
3. **feat: bitbucket API client** — 4.6 standalone with tests; no wiring.
4. **feat: bitbucket inbound stack** — `bitbuckethook` (verify/parse/kind-map per 4.5), provider-selected route registration, enum accepts `bitbucket`, 4.7 provider-selected validation, D8 secrets live, `config.example.yaml`. **Tracer bullet: end-to-end complete.**
5. **feat: bitbucket ops parity + docs** — 4.8 reconcile/smoke, section 6 docs.

Epic acceptance = criteria 1–5 + all slices merged + `just check` green throughout.

## 9. Fact appendix — verification levels

**Verified TRUE (primary Atlassian docs/spec, 2026-07-03):** signature header/scheme/GA-date; no-signature-without-secret; all 13 PR event keys; draft GA 2025-03-31 + `draft` payload field + draft always `state:OPEN`; no draft-transition event key; no `changes` object on `updated`; payload paths in 4.5; actor subtypes user/team/app_user; no bot accounts on Cloud (KB); Renovate-as-user-account (vendor doc); app-password timeline (brownouts 2026-06-09→07-27, removal 07-28); API-token Basic + scoped-mandatory + ≤365d; access-token plans/scopes/Bearer/no-expiry; the five endpoints + pagination envelope + diffstat 302/pagelen-500/spec=None (live-probed); rate limits 1,000/h + per-token identity + 429; base URL; retries ≤3/10s/256KB; IP ranges URL.

**UNVERIFIED (design defensively):** which edits fire `pullrequest:updated` (undocumented; community: incl. source pushes — D4 tolerates any trigger); whether batched "Finish review" comments fire one event per comment (assume N individual events, no bundling envelope); exact `app_user` JSON in webhook samples (established via spec discriminator, not a printed sample); PR ids strictly sequential (only per-repo uniqueness documented — we key, never arithmetic).

**Refuted / do not reintroduce:** "Bitbucket Cloud has no webhook secret" (false since 2023-10); "signature header is X-Hub-Signature-256" (false — no `-256`); "app passwords are fine to support" (false — removal 2026-07-28); "there is a pullrequest:merged / pullrequest:declined key" (false — `fulfilled`/`rejected`).
