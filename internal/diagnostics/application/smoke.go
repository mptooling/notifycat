package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// smokeTitlePrefix marks synthetic PRs so an operator can recognise and
// delete the Slack message they produce. It is part of the acceptance contract.
const smokeTitlePrefix = "[notifycat smoke]"

// smokeGHEvent describes one synthetic webhook to replay.
type smokeGHEvent struct {
	event       string // X-GitHub-Event header value
	action      string
	reviewState string // review.state, empty when there is no review object
	merged      bool
	senderType  string // sender.type; empty defaults to "User" in buildPayload
}

// smokeLifecycleStep pairs a synthetic event with the emoji the server is
// expected to add for it, plus a label for the report.
type smokeLifecycleStep struct {
	name  string
	emoji string
	ev    smokeGHEvent
}

// SmokeUseCase runs the delivery test. Construct via NewSmokeUseCase; Run is
// safe to call repeatedly (each call derives a unique PR number from the clock).
type SmokeUseCase struct {
	mappings  diagnosticsdomain.SmokeMappings
	messages  diagnosticsdomain.SmokeMessages
	reactions diagnosticsdomain.SmokeReactions
	cleanup   diagnosticsdomain.SmokeCleanup
	signer    diagnosticsdomain.Signer
	sender    diagnosticsdomain.WebhookSender
	cfg       diagnosticsdomain.SmokeConfig
}

// NewSmokeUseCase wires a SmokeUseCase.
func NewSmokeUseCase(
	mappings diagnosticsdomain.SmokeMappings,
	messages diagnosticsdomain.SmokeMessages,
	reactions diagnosticsdomain.SmokeReactions,
	cleanup diagnosticsdomain.SmokeCleanup,
	signer diagnosticsdomain.Signer,
	sender diagnosticsdomain.WebhookSender,
	cfg diagnosticsdomain.SmokeConfig,
) *SmokeUseCase {
	return &SmokeUseCase{
		mappings:  mappings,
		messages:  messages,
		reactions: reactions,
		cleanup:   cleanup,
		signer:    signer,
		sender:    sender,
		cfg:       cfg,
	}
}

// Run validates that target is mapped, posts a signed synthetic
// `pull_request: opened` to the live endpoint, and reports the channel and
// the Slack timestamp read back from the store. Mapping is checked first so an
// unmapped repo fails (ErrNoMapping) without any network traffic.
//
// When withReactions is set and the server has reactions enabled, Run then
// replays a comment, an approval, and a merge for the same PR and verifies the
// configured emoji appeared on the message. A missing emoji is recorded in the
// SmokeResult (not returned as an error) so the CLI can report every step.
func (s *SmokeUseCase) Run(ctx context.Context, target string, withReactions bool) (res diagnosticsdomain.SmokeResult, err error) {
	mapping, lookupErr := s.mappings.Get(ctx, target)
	if errors.Is(lookupErr, routingdomain.ErrNotFound) {
		return diagnosticsdomain.SmokeResult{}, fmt.Errorf("%w: %s", diagnosticsdomain.ErrNoMapping, target)
	}
	if lookupErr != nil {
		return diagnosticsdomain.SmokeResult{}, fmt.Errorf("smoke: look up mapping for %s: %w", target, lookupErr)
	}

	prNumber := int(s.cfg.Now().Unix())
	// Delete the synthetic pull_requests row this run causes the server to
	// create, on every exit path once prNumber is known. The Slack message is
	// left in place for the operator's visual confirmation. Delete is a no-op
	// when the row is absent, so it is safe even if delivery failed.
	defer func() {
		if cleanupErr := s.cleanup.DeletePR(ctx, target, prNumber); cleanupErr != nil {
			cleanupErr = fmt.Errorf("smoke: clean up synthetic PR row %s#%d: %w", target, prNumber, cleanupErr)
			if err == nil {
				err = cleanupErr
			} else {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()

	title := fmt.Sprintf("%s delivery test — safe to delete (PR #%d)", smokeTitlePrefix, prNumber)
	res = diagnosticsdomain.SmokeResult{
		Repository:         target,
		Channel:            mapping.SlackChannel,
		PRNumber:           prNumber,
		Title:              title,
		URL:                s.cfg.WebhookURL,
		ReactionsRequested: withReactions,
		ReactionsEnabled:   s.cfg.Reactions.Enabled,
		IgnoreAIReviews:    s.cfg.IgnoreAIReviews,
		BotReviewMarker:    s.cfg.Reactions.BotReview,
	}

	if deliverErr := s.deliver(ctx, target, prNumber, title, smokeGHEvent{event: "pull_request", action: "opened"}); deliverErr != nil {
		return diagnosticsdomain.SmokeResult{}, deliverErr
	}

	msgs, msgErr := s.messages.Messages(ctx, target, prNumber)
	if msgErr != nil {
		return diagnosticsdomain.SmokeResult{}, fmt.Errorf("smoke: server returned 200 but no Slack message was stored "+
			"(was the repo mapped to a channel the bot can post to?): %w", msgErr)
	}
	if len(msgs) == 0 {
		return diagnosticsdomain.SmokeResult{}, fmt.Errorf("smoke: server returned 200 but stored no Slack message for the PR")
	}
	msg := msgs[0]
	res.Timestamp = msg.MessageID

	if !withReactions || !s.cfg.Reactions.Enabled {
		return res, nil
	}

	// On a mid-lifecycle delivery failure, keep the checks gathered so far so
	// the CLI can still report the steps that ran.
	checks, replayErr := s.replayReactions(ctx, target, prNumber, title, msg)
	res.Reactions = checks
	if replayErr != nil {
		return res, replayErr
	}
	return res, nil
}

// lifecycleSteps is the review lifecycle the reactions pass replays. The
// bot-review step is included only when bot reviews are not muted and a marker
// emoji is configured.
func (s *SmokeUseCase) lifecycleSteps() []smokeLifecycleStep {
	steps := []smokeLifecycleStep{
		{"comment", s.cfg.Reactions.Commented, smokeGHEvent{event: "pull_request_review", action: "submitted", reviewState: "commented"}},
	}
	if !s.cfg.IgnoreAIReviews && s.cfg.Reactions.BotReview != "" {
		steps = append(steps, smokeLifecycleStep{"bot", s.cfg.Reactions.BotReview, smokeGHEvent{
			event: "pull_request_review", action: "submitted", reviewState: "commented", senderType: "Bot",
		}})
	}
	return append(steps,
		smokeLifecycleStep{"approve", s.cfg.Reactions.Approved, smokeGHEvent{event: "pull_request_review", action: "submitted", reviewState: "approved"}},
		smokeLifecycleStep{"merge", s.cfg.Reactions.MergedPR, smokeGHEvent{event: "pull_request", action: "closed", merged: true}},
	)
}

// replayReactions replays each lifecycle step against the live endpoint and
// reads back whether the expected emoji landed on the message. On a delivery
// failure it returns the checks gathered so far alongside the error.
func (s *SmokeUseCase) replayReactions(ctx context.Context, target string, prNumber int, title string, msg diagnosticsdomain.SmokeMessage) ([]diagnosticsdomain.SmokeReactionCheck, error) {
	var checks []diagnosticsdomain.SmokeReactionCheck
	for _, step := range s.lifecycleSteps() {
		if deliverErr := s.deliver(ctx, target, prNumber, title, step.ev); deliverErr != nil {
			return checks, deliverErr
		}
		check := diagnosticsdomain.SmokeReactionCheck{Step: step.name, Emoji: step.emoji}
		reactionNames, gerr := s.reactions.Reactions(ctx, msg.Channel, msg.MessageID)
		if gerr != nil {
			check.VerifyErr = gerr
		} else {
			check.Present = containsReaction(reactionNames, step.emoji)
		}
		checks = append(checks, check)
	}
	return checks, nil
}

// deliver signs and POSTs one synthetic event, mapping the response to a
// sentinel error.
func (s *SmokeUseCase) deliver(ctx context.Context, repository string, number int, title string, ev smokeGHEvent) error {
	body, err := buildPayload(repository, number, title, ev)
	if err != nil {
		return err
	}

	header, value := s.signer.Sign(s.cfg.WebhookSecret, body)
	headers := map[string]string{
		"Content-Type":   "application/json",
		"X-GitHub-Event": ev.event,
		header:           value,
	}

	status, sendErr := s.sender.Send(ctx, s.cfg.WebhookURL, body, headers)
	if sendErr != nil {
		return fmt.Errorf("%w at %s: %v", diagnosticsdomain.ErrUnreachable, s.cfg.WebhookURL, sendErr)
	}

	switch status {
	case 200:
		return nil
	case 401:
		return diagnosticsdomain.ErrSignatureRejected
	default:
		return fmt.Errorf("%w: %d", diagnosticsdomain.ErrUnexpectedStatus, status)
	}
}

func containsReaction(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// buildPayload renders a minimal webhook body carrying only the fields
// githubhook.ParsePayload and the handlers read for ev.
func buildPayload(repository string, number int, title string, ev smokeGHEvent) ([]byte, error) {
	type user struct {
		Login string `json:"login"`
	}
	type review struct {
		State string `json:"state"`
	}
	payload := struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			User    user   `json:"user"`
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
		} `json:"pull_request"`
		Review *review `json:"review,omitempty"`
		Sender struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
	}{Action: ev.action}
	payload.Repository.FullName = repository
	payload.PullRequest.Number = number
	payload.PullRequest.Title = title
	payload.PullRequest.HTMLURL = fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
	payload.PullRequest.User = user{Login: "notifycat-smoke"}
	payload.PullRequest.Merged = ev.merged
	if ev.reviewState != "" {
		payload.Review = &review{State: ev.reviewState}
	}
	payload.Sender.Login = "notifycat-smoke"
	payload.Sender.Type = "User"
	if ev.senderType != "" {
		payload.Sender.Type = ev.senderType
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("smoke: marshal payload: %w", err)
	}
	return body, nil
}
