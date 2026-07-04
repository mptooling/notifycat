# Phase 5 — notification domain (core)

The core domain and largest surface: receive a GitHub PR event and keep one Slack message per (PR, channel) in sync. Absorbs `pullrequest` (dispatcher + open/close/draft + reaction handlers), `botpr`, `aireview`, `githubhook` parsing, `webhook`, and the messages repository. Depends on routing (Phase 2). Eight independently-green sub-steps, each its own commit. Also populates the shared `kernel` and creates `platform/httpx` + `platform/security`.

Target: `internal/notification/{domain,application,infrastructure}` + `internal/kernel/*` + `internal/platform/{httpx,security}`.

## The central design decision (authoritative)

The handlers today take `*slack.Composer` + a `Messenger` (slack.Client) and **compose `slack.Message` values inside the handler** (NewMessage/BotMessage/UpdatedMessage + append ReviewedBy/Reviewing markers). depguard forbids `notification/application` from importing slack/store (locked layering rule; digest/validation precedent). Therefore:

- **The `Messenger` port is intent-based.** The application hands the messenger *domain data* ("post an opened-PR notification for this PR with these mentions in this format"); the **message shaping — which composer method, which blocks, marker appends — moves entirely into the infra Slack messenger adapter** (`slack_messenger.go`), which owns the `*slack.Composer` + `*slack.Client`. This is exactly what the sub-step spec means by "shaping lives here."
- **Handler (application) tests become behavioral** — same forced move as digest's reporter test. They assert *the domain intent the handler drives the ports with* (which `Messenger` method, on which channel/messageID, with which PR/mentions/merged/emoji/reviewerIDs; which `MessageStore` calls; which `ignored webhook event` reason). They do **not** import slack and do **not** assert Slack text.
- **The Slack-text/block assertions relocate**, losing no coverage, to: (1) `internal/slack/composer_test.go` (already covers NewMessage/BotMessage/UpdatedMessage/markers), and (2) a focused **infra `slack_messenger_test.go`** (N5-d) asserting the adapter composes the correct blocks per intent (this is the new home for close_test/open_test/review_handlers_test's "which blocks" assertions — merged decoration, ReviewedBy block, bot vs standard template, review-finished rebuild).

This is a large but principled reorganization — documented per the guardrail, not a papered-over smell. Behavior is byte-identical; total coverage is preserved and relocated to the layer that owns each concern.

## Ports & DTOs (N5-b)

`Messenger` (infra `SlackMessenger` satisfies; composes internally):
- `PostOpen(ctx, channel string, req OpenRequest) (messageID string, err error)`
- `UpdateClosed(ctx, channel, messageID string, req ClosedRequest) error`
- `AddReaction(ctx, channel, messageID, emoji string) error`
- `Delete(ctx, channel, messageID string) error`
- `UpdateReviewFinished(ctx, channel, messageID string, req ReviewFinishedRequest) error`

DTOs (`models.go`) — carry `kernel.PR` + the business-decided data; infra maps `kernel.PR → slack.PRDetails` and renders:
- `OpenRequest{ Repository string; PR kernel.PR; Mentions []string; NewPREmoji string; Bot *BotFormat }` — `Bot` non-nil selects the compact bot template. `BotFormat{ Name string; Security bool }`.
- `ClosedRequest{ Repository string; PR kernel.PR; Merged bool; Emoji string; ReviewerIDs []string }` — infra: `UpdatedMessage(pr, merged, emoji)` then append `ReviewedByMarker(ReviewerIDs)` when non-empty.
- `ReviewFinishedRequest{ Repository string; PR kernel.PR; ReviewerIDs []string; NewPREmoji string }` — infra: `NewMessage(pr, nil, NewPREmoji)` then append `ReviewedByMarker` when non-empty.

**NOTE (Repository on DTOs):** `slack.PRDetails` needs `Repository`, which lives on `kernel.Event`, not `kernel.PR`. N5-b's models.go omitted it; **N5-d adds `Repository string` to the three request DTOs** (edit models.go) and the infra `prDetails(repository string, pr kernel.PR) slack.PRDetails` maps it. The N5-d `SlackMessenger` exposes unexported `composeOpen/composeClosed/composeReviewFinished` methods (each returns `slack.Message`); `PostOpen/UpdateClosed/UpdateReviewFinished` = compose + client call. The infra test targets the three compose methods directly (no Slack server needed) — that is the new home for the relocated block-shape assertions.

**NOTE (bot classification lands in N5-f, not N5-g):** `BotKind` enum is domain (N5-b, done). `DetectBot(login) BotKind` + `IsSecurityAdvisory(body) bool` (from `botpr`) move to `notification/application` in **N5-f** because `OpenHandler` needs them; `botpr` is deleted in N5-h. N5-g is only the reaction handlers + AI suppression (`IsBot` policy already in domain). The `MessageStore.Messages` "not found" and `RepoBehavior.Get` "not found" both surface `routingdomain.ErrNotFound` (store alias) — handlers compare `errors.Is(err, routingdomain.ErrNotFound)`; no notification-domain ErrNotFound needed. A small infra `ReviewSessionsRepo` over `store.CodeReviews` (maps `CodeReview→ReviewSession`, `ErrNotFound→ErrNoActiveReview`) satisfies the `ReviewSessions` port until Phase 6 — it's only needed at wiring time (the reaction-handler tests use the fake), so it **lands in N5-h** alongside the dispatcher + adapter assembly, not N5-g. N5-g is purely the three application reaction handlers + their behavioral tests; the AI-suppression path uses `domain.IsBot(event.Sender.Type)` (no detector), and `clearInReviewState` drives `messenger.UpdateReviewFinished`.

`MessageStore` (infra `MessageRepo` over `store.PullRequests`):
- `AddMessage(ctx, repository string, prNumber int, channel, messageID string) error`
- `Messages(ctx, repository string, prNumber int) ([]Message, error)` — `Message{Channel, MessageID}` domain DTO (mirror of store.Message).
- `Touch(ctx, repository string, prNumber int) error`
- `MarkClosed(ctx, repository string, prNumber int) error`
- `Delete(ctx, repository string, prNumber int) error`

Routing ports (satisfied structurally by routing `*Provider`/`*Router`, transition debt):
- `RepoBehavior.Get(ctx, repository string) (routingdomain.RepoMapping, error)`
- `TargetResolver.ResolveTargets(ctx, repository string, prNumber int) (routingdomain.RepoMapping, []routingdomain.Target, error)`

`ReviewSessions` port (**left for the review domain to satisfy in Phase 6** — `store.CodeReviews` satisfies it now; declared in notification/domain so N5-g compiles without importing review):
- `GetActive(ctx, repository string, prNumber int) (ReviewSession, error)` — `ReviewSession{SlackUserID, SlackUserName string}` domain DTO; `ErrNoActiveReview` sentinel.
- `Finish(ctx, repository string, prNumber int) error`
- `Reviewers(ctx, repository string, prNumber int) ([]ReviewSession, error)`

Use-case interfaces + `Handler` port:
- `Handler { Applicable(kernel.Event) bool; Handle(ctx, kernel.Event) error }` (replaces `EventHandler`).
- `OpenPR`, `ClosePR`, `DraftPR`, `ReactionApply` (each a `Handler`).
- `Dispatcher` use-case: `Dispatch(ctx, kernel.Event) error`.

Enums (`enums.go`): reason codes `ReasonNoHandler/NoMapping/NoStoredMessage/AlreadySent` (values `no_handler`/`no_mapping`/`no_stored_message`/`already_sent`); `BotKind` (None/Dependabot/Renovate) + `.Name()` moved from `botpr`. AI policy (pure): `IsBot(senderType string) bool { return senderType == kernel.SenderTypeBot }` gated by `behavior.IgnoreAIReviews` at the call site.

## Kernel (N5-a)

`internal/kernel` value objects (pure; only stdlib `time`; kernel-purity depguard applies):
- `PR{ Number int; Title, URL, Author string; Merged, Draft bool; Body string; CreatedAt time.Time }`
- `Sender{ Login string; Type string }`
- `Review{ State ReviewState }`
- `Event{ GitHubEvent GitHubEventType; Action Action; Repository string; PR PR; Review *Review; PRComment bool; Sender Sender }`
- enums (defined types + consts): `GitHubEventType` (`pull_request`, `pull_request_review`, `pull_request_review_comment`, `issue_comment`); `Action` (`opened`, `closed`, `ready_for_review`, `converted_to_draft`, `submitted`, `created`, `edited`); `ReviewState` (`approved`, `commented`, `changes_requested`). Untyped string consts `SenderTypeUser="User"`, `SenderTypeBot="Bot"` (Sender.Type stays a plain string — the AI policy takes a string).

`eventSink` maps `githubhook.Payload → kernel.Event` with the enum conversions (`kernel.Action(p.Action)` etc.).

## Sub-steps

- **N5-a** kernel value objects + enums · `refactor(kernel): add PR/Event/Sender/Review value objects and enums`
- **N5-b** notification/domain: ports (Messenger/MessageStore/RepoBehavior/TargetResolver/ReviewSessions/Handler + use-cases), DTOs, reason/reaction/BotKind enums, AI policy · `refactor(notification): add domain ports, DTOs, reason/reaction enums`
- **N5-c** notification/infra `message_repo.go` over `store.PullRequests` (maps store.Message ⇄ domain Message); move pull_requests repo tests as needed · `refactor(notification): relocate messages repository`
- **N5-d** notification/infra `slack_messenger.go` (composes via `*slack.Composer`, posts via `*slack.Client`) + `slack_messenger_test.go` (the relocated block-shape assertions) · `refactor(notification): relocate Slack messenger adapter`
- **N5-e** (platform extraction only — the inbound *receiver* moves with the dispatcher in N5-h to avoid a throwaway kernel↔pullrequest bridge): `internal/webhook → internal/platform/httpx` (rename package `webhook`→`httpx`, update importers githubhook+slackhook); new `internal/platform/security` (`SignatureVerifier` port + `GitHubVerifier` HMAC-SHA256 adapter with `Sign`/`Verify`/`SignatureHeader`/`ErrInvalidSignature`, moved from `githubhook/verifier.go`, byte-identical). Repoint `githubhook.SignatureMiddleware` (`*security.GitHubVerifier` + `security.SignatureHeader` + `httpx.Signature`), `app.buildMux` (`security.NewGitHubVerifier`), and the smoke command (`security.Sign`). `githubhook` keeps Payload/ParsePayload/handler/SignatureMiddleware/EventSink for now. Delete `internal/webhook` + `githubhook/verifier{,_test}.go`. · `refactor(platform): extract httpx middleware and security verifier`
- **N5-f** notification/app open/close/draft use cases (drive Messenger/MessageStore/routing ports; draft-always-deleted; every reason preserved) + behavioral tests · `refactor(notification): relocate open/close/draft use cases`
- **N5-g** notification/app reaction handlers (Approve/Commented/RequestChange), BotKind classification (from `botpr`) in application + enums, AI suppression as domain policy; `ReviewSessions` stays a port (no review import) + behavioral tests · `refactor(notification): relocate reaction handlers, bot-PR, AI suppression`
- **N5-h** dispatcher → application over `[]Handler` (fx value group `group:"handlers"`); rewire `app.buildDispatcher`/`eventSink`/`buildMux`; delete `internal/{pullrequest,botpr,aireview,githubhook,webhook}`; fill `notification.Module` + fxtest · `refactor(notification): relocate dispatcher and add fx module`

## depguard (add per step)
- `kernel-purity` already exists (Phase 0) — N5-a lands under it.
- `notification-domain`: allow kernel + notification/domain + routing/domain.
- `notification-application`: allow kernel + notification/domain + notification/application + routing/domain.
- (no infrastructure rule; no platform/* rule — platform may import SDKs.)

## Gate
- [x] build + race tests + lint green (handler tests behavioral; slack-text coverage relocated to infra messenger test + slack/composer_test)
- [x] kernel populated; `platform/httpx` + `platform/security` exist; GitHub inbound verified via the port; sig error strings/codes byte-identical
- [x] `pullrequest`/`botpr`/`aireview`/`githubhook`/`webhook` gone; `notification.Module` fxtested
- [x] handler-group pattern in place (no notification↔review import); depguard probe-verified; plan box checked

**Done.** 10 commits `f9456b4`→`ec55ad6`. Race suite 30 test-pkgs ok / 483 tests, 0 FAIL; lint clean; depguard blocks slack in domain / store in application (probe-verified). Messenger is an intent port (shaping in infra `SlackMessenger`); handler tests are behavioral (assert port intent), slack-text covered by infra `slack_messenger_test` + `slack/composer_test`. `platform/httpx` + `platform/security` extracted; GitHub receiver in notification/infra maps `Payload → kernel.Event`. Dispatcher over `[]Handler` fx value group. `ReviewSessions` left as a port (transition adapter `ReviewSessionsRepo`) for the review domain to own in Phase 6.
