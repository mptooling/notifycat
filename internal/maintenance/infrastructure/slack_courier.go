package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

// slackMessageGone is Slack's error code for a ts that addresses no message.
const slackMessageGone = "message_not_found"

// SlackCourier implements domain.MessageCourier over the Slack Web API: it
// reads a posted message's blocks, reposts them in another channel as a
// mention-free "moved" message, carries the reactions over, and deletes the
// original.
type SlackCourier struct {
	client *slack.Client
}

// NewSlackCourier wraps a Slack client as the relocate courier.
func NewSlackCourier(client *slack.Client) *SlackCourier {
	return &SlackCourier{client: client}
}

// Repost implements domain.MessageCourier.
func (c *SlackCourier) Repost(ctx context.Context, from domain.TrackedMessage, toChannel string) (string, error) {
	content, err := c.client.MessageContent(ctx, from.Channel, from.MessageID)
	if err != nil {
		return "", asGone(err)
	}
	moved, err := slack.MovedMessage(content)
	if err != nil {
		return "", err
	}
	messageID, err := c.client.PostMessageRawBlocks(ctx, toChannel, moved.Blocks, moved.Fallback)
	if err != nil {
		return "", err
	}
	return messageID, nil
}

// CopyReactions implements domain.MessageCourier. Only emoji in allowed are
// carried over: the rest are ad-hoc human reactions the bot cannot attribute,
// and re-adding them would credit the bot for other people's reactions.
func (c *SlackCourier) CopyReactions(ctx context.Context, from, to domain.TrackedMessage, allowed []string) error {
	reactions, err := c.client.GetReactions(ctx, from.Channel, from.MessageID)
	if err != nil {
		return err
	}
	for _, reaction := range reactions {
		if !slices.Contains(allowed, reaction.Name) {
			continue
		}
		if err := c.client.AddReaction(ctx, to.Channel, to.MessageID, reaction.Name); err != nil {
			return fmt.Errorf("relocate: add reaction %q: %w", reaction.Name, err)
		}
	}
	return nil
}

// Delete implements domain.MessageCourier. A message that is already gone is
// success — the outcome the caller wanted is the state of the world.
func (c *SlackCourier) Delete(ctx context.Context, message domain.TrackedMessage) error {
	err := c.client.DeleteMessage(ctx, message.Channel, message.MessageID)
	if errors.Is(asGone(err), domain.ErrMessageGone) {
		return nil
	}
	return err
}

// asGone translates Slack's "this message does not exist" signals — the client
// sentinel and the API error code — into the domain's ErrMessageGone.
func asGone(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, slack.ErrMessageNotFound) {
		return fmt.Errorf("%w: %w", domain.ErrMessageGone, err)
	}
	var apiErr *slack.APIError
	if errors.As(err, &apiErr) && apiErr.Code == slackMessageGone {
		return fmt.Errorf("%w: %w", domain.ErrMessageGone, err)
	}
	return err
}

var _ domain.MessageCourier = (*SlackCourier)(nil)
