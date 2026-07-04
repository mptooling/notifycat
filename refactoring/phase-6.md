# Phase 6 — review domain

The interactive "Start review" flow: a Slack button click records a reviewer and appends an in-review marker to the PR message. Absorbs `startreview`, `slackhook` parsing, the `code_reviews` repository, and the Slack signature verifier. Depends on notification (implements its `ReviewSessions` port).

## Dependency decision (resolved at expansion — clean port, not the fallback)

startreview is **self-contained**: it uses the code_reviews store, a store-message existence check, a Slack raw-blocks updater, and the Slack `ReviewingMarker` composer — **no dependency on notification's Messenger or domain**. And the **finish-on-submit + clear-in-review logic stays in notification** (it is bundled into the reaction handler's single dispatched invocation on a `pull_request_review` submitted event; splitting it out would either change the "first-applicable-handler-wins" dispatcher semantics or create a notification→review→notification cycle). So:

- **The `ReviewSessions` port stays owned by notification/domain** (its consumers — the reaction + close handlers — live there). **Review provides the implementation**: `review/infrastructure.CodeReviewsRepo` implements *both* review's own `Recorder` port *and* `notificationdomain.ReviewSessions`. The only cross-domain import is `review/infrastructure → notification/domain` (one-way). notification never imports review; the fx wiring binds review's repo to notification's port in the composition root.
- notification's Phase-5 transition adapter `notification/infrastructure.ReviewSessionsRepo` is **deleted** — replaced by review's `CodeReviewsRepo`.

This is the clean-port outcome (review is a peer domain), not the "fold into notification" fallback.

## Layer assignment

### review/domain (`package domain`) — imports stdlib (+ `encoding/json` for the raw-blocks passthrough)
- `models.go` — `StartReviewCommand{ Repository string; PRNumber int; Reviewer Reviewer; Message MessageRef }`; `Reviewer{ UserID, UserName string }`; `MessageRef{ Channel, TS string; RawBlocks json.RawMessage; Fallback string }`.
- `interfaces.go` — ports: `Recorder` (`HasActiveReview(ctx, repo, pr, userID) (bool, error)`, `Start(ctx, repo, pr, userID, userName) error`), `MessageChecker` (`HasMessages(ctx, repo, pr) (bool, error)`), `MessageDecorator` (`AppendReviewingMarker(ctx, msg MessageRef, reviewer Reviewer, since time.Time) error`). Use-case interface `StartReview { Handle(ctx, StartReviewCommand) error }`.
- `errors.go` — `ErrActiveReviewExists` (mapped from the store's unique-violation sentinel at the infra boundary).
- `doc.go`.

### review/application (`package application`) — imports stdlib + review/domain
- `start_review.go` — `Handler` implementing `StartReview`, with an injected `now func() time.Time`. Flow (behaviour byte-identical to `startreview.Handler.Handle`, minus the inbound parsing which is now infra): (1) `MessageChecker.HasMessages` false → log `no_stored_message` (info), nil; (2) `Recorder.HasActiveReview` true → log `already_reviewing` (debug), nil; (3) `Recorder.Start`; `ErrActiveReviewExists` → log `db_conflict` (debug), nil; other err → return; (4) `MessageDecorator.AppendReviewingMarker(msg, reviewer, now())`; err → log warn, nil. `NewHandler(domain.HandlerParams{Recorder, MessageChecker, Decorator, Logger, Now})` (>3 args → params DTO).
- `start_review_test.go` (`application_test`) — behavioral: assert `Recorder.Start` called with the reviewer, `Decorator.AppendReviewingMarker` called with the right `Reviewer`/`MessageRef`; the dup (app-level + db-race), unknown-message, and decorate-failure-swallowed no-ops. The block-insertion assertions (marker-before-actions) move to the infra decorator test.

### review/infrastructure (`package infrastructure`) — no depguard rule (imports store, slack, notification/domain)
- `code_reviews_repo.go` — `CodeReviewsRepo` over `*store.CodeReviews`. Implements review `Recorder` (`HasActiveReview` via `ActiveForUser`+`store.ErrNotFound`→bool; `Start` mapping `store.ErrActiveReviewExists`→`reviewdomain.ErrActiveReviewExists`) **and** `notificationdomain.ReviewSessions` (`GetActive`→`ReviewSession`/`ErrNoActiveReview`, `Finish`, `Reviewers`). `var _ reviewdomain.Recorder`; `var _ notificationdomain.ReviewSessions`.
- `message_checker.go` — `MessageChecker` over `*store.PullRequests` (`HasMessages` = `Messages` len>0, `ErrNotFound`→false).
- `slack_decorator.go` — `SlackDecorator` over `*slack.Composer` + `*slack.Client`. `AppendReviewingMarker`: `composer.ReviewingMarker(reviewer.UserID, since)` → marshal → `insertBeforeActions(splitBlocks(msg.RawBlocks), marker)` → `client.UpdateMessageRawBlocks(msg.Channel, msg.TS, blocks, msg.Fallback)`. The `splitBlocks`/`insertBeforeActions` helpers move here from startreview. Test asserts marker inserted before the actions block (moved from startreview handler_test).
- `interactions.go` — Slack interactions inbound: `SignatureMiddleware(verifier security.SignatureVerifier)` (Slack HMAC via httpx + platform/security), `NewInteractionsHandler(sink InteractionSink, logger)` (read body → `ParseInteraction` → log → sink), the `Interaction`/`User`/`Channel`/`Message`/`Action` types + `ParseInteraction` + `ErrMissingPayload` (moved from slackhook). Plus a `StartReviewSink(handler reviewdomain.StartReview) InteractionSink` adapter: maps an `Interaction` (block_actions + `start_review` action + `decodeValue` "repo#number") → `StartReviewCommand`, calls the use case; non-actionable → nil. `startReviewActionID` const lives here (inbound concern). Move the slackhook payload/handler/middleware tests here (repointed).

### platform/security (T1) — add the Slack verifier
Move `slackhook/verifier.go` → `platform/security` as `SlackVerifier` (timestamped base-string HMAC, replay window) implementing `SignatureVerifier`; `NewSlackVerifier(secret)`, `SlackSignatureHeader`, `SlackTimestampHeader`. Move its test. Repoint `slackhook.SignatureMiddleware` + `app.buildMux` (`security.NewSlackVerifier`). Error codes/strings byte-identical. (slackhook keeps its middleware/handler/payload until T5.)

## Sub-steps
- **R6-T1** scaffold review 3 layers + module stub + 2 depguard rules (review-domain, review-application); move Slack verifier → platform/security + repoint slackhook/app · `refactor(platform): extract Slack signature verifier` (+ scaffold in same or split commit)
- **R6-T2** review/domain (models, interfaces, errors, doc) · `refactor(review): add domain ports and DTOs`
- **R6-T3** review/application (start-review use case + behavioral tests) · `refactor(review): relocate start-review use case`
- **R6-T4** review/infrastructure (code_reviews repo impl of both ports + slack decorator + message checker + interactions inbound + tests) · `refactor(review): relocate code_reviews repo and Slack interactions inbound`
- **R6-T5** review.Module + fxtest; rewire app.buildMux (slack route → review inbound) + buildDispatcher (reviews → review repo); delete `internal/startreview`, `internal/slackhook`, notification/infra `ReviewSessionsRepo` + test · `refactor(review): add fx module and rewire`

## depguard
- `review-domain`: allow kernel + review/domain. (No routing/notification — domain is pure.)
- `review-application`: allow kernel + review/domain + review/application.
- (no infrastructure rule.)

## Gate
- [x] build + race tests + lint green (unchanged) · `startreview`/`slackhook` gone · notification/infra `ReviewSessionsRepo` gone · one-way review→notification/domain dependency · both verifiers in `platform/security` · module fxtested · depguard probe-verified · plan box checked

**Done.** 6 commits `43a8b3e`→`065a79b`. Race suite 31 test-pkgs ok / 493 tests, 0 FAIL; lint clean; depguard probe-verified (slack blocked in review/domain, store in review/application). Review is a clean peer domain: `CodeReviewsRepo` implements both `reviewdomain.Recorder` and `notificationdomain.ReviewSessions` (one-way review→notification/domain); finish-on-submit stayed in notification (dispatcher semantics preserved). Slack verifier extracted to `platform/security.SlackVerifier`; interactions inbound in review/infra with the `StartReviewSink` adapter. notification.Module dropped its transition `ReviewSessionsRepo`; review.Module now provides `ReviewSessions`.
