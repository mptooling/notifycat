package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/slack"
)

// SlackMessenger implements the notification Messenger port over the Slack
// client and composer. It owns the message shaping: which composer template and
// which marker blocks each notification intent renders to. The application hands
// it domain intent; no Slack type crosses the port.
type SlackMessenger struct {
	client   *slack.Client
	composer *slack.Composer
}

// NewSlackMessenger wraps the platform Slack client and composer.
func NewSlackMessenger(client *slack.Client, composer *slack.Composer) *SlackMessenger {
	return &SlackMessenger{client: client, composer: composer}
}

// PostOpen implements domain.Messenger.
func (m *SlackMessenger) PostOpen(ctx context.Context, channel string, req domain.OpenRequest) (string, error) {
	return m.client.PostMessage(ctx, channel, m.composeOpen(req))
}

// composeOpen renders an opened-PR notification: the compact dependency-bot
// template when Bot is set, otherwise the standard template.
func (m *SlackMessenger) composeOpen(req domain.OpenRequest) slack.Message {
	details := prDetails(req.Repository, req.PR)
	if req.Bot != nil {
		return m.composer.BotMessage(details, req.Mentions, req.Bot.Name, req.Bot.Security)
	}
	return m.composer.NewMessage(details, req.Mentions, req.NewPREmoji)
}

// UpdateClosed implements domain.Messenger.
func (m *SlackMessenger) UpdateClosed(ctx context.Context, channel, messageID string, req domain.ClosedRequest) error {
	return m.client.UpdateMessage(ctx, channel, messageID, m.composeClosed(req))
}

// composeClosed renders the closed/merged decoration, appending a "reviewed by"
// marker when reviewers are present.
func (m *SlackMessenger) composeClosed(req domain.ClosedRequest) slack.Message {
	msg := m.composer.UpdatedMessage(prDetails(req.Repository, req.PR), req.Merged, req.Emoji)
	if len(req.ReviewerIDs) > 0 {
		msg.Blocks = append(msg.Blocks, m.composer.ReviewedByMarker(req.ReviewerIDs))
	}
	return msg
}

// UpdateReviewFinished implements domain.Messenger.
func (m *SlackMessenger) UpdateReviewFinished(ctx context.Context, channel, messageID string, req domain.ReviewFinishedRequest) error {
	return m.client.UpdateMessage(ctx, channel, messageID, m.composeReviewFinished(req))
}

// composeReviewFinished rebuilds the standard message out of the in-review state,
// appending a "reviewed by" marker when reviewers are present.
func (m *SlackMessenger) composeReviewFinished(req domain.ReviewFinishedRequest) slack.Message {
	msg := m.composer.NewMessage(prDetails(req.Repository, req.PR), nil, req.NewPREmoji)
	if len(req.ReviewerIDs) > 0 {
		msg.Blocks = append(msg.Blocks, m.composer.ReviewedByMarker(req.ReviewerIDs))
	}
	return msg
}

// AddReaction implements domain.Messenger.
func (m *SlackMessenger) AddReaction(ctx context.Context, channel, messageID, emoji string) error {
	return m.client.AddReaction(ctx, channel, messageID, emoji)
}

// Delete implements domain.Messenger.
func (m *SlackMessenger) Delete(ctx context.Context, channel, messageID string) error {
	return m.client.DeleteMessage(ctx, channel, messageID)
}

// prDetails adapts a repository + kernel.PR to the composer's PRDetails shape.
func prDetails(repository string, pr kernel.PR) slack.PRDetails {
	return slack.PRDetails{
		Repository: repository,
		Number:     pr.Number,
		Title:      pr.Title,
		URL:        pr.URL,
		Author:     pr.Author,
		CreatedAt:  pr.CreatedAt,
	}
}

var _ domain.Messenger = (*SlackMessenger)(nil)
